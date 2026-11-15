package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/rykhalskyi/killswitch/internal/config"
	"github.com/rykhalskyi/killswitch/internal/flux"
)

type mockFluxClient struct {
	mu        sync.Mutex
	resources map[string]*flux.ResourceInfo
	failPatch map[string]error
}

func newMockFluxClient() *mockFluxClient {
	return &mockFluxClient{
		resources: make(map[string]*flux.ResourceInfo),
		failPatch: make(map[string]error),
	}
}

func (m *mockFluxClient) DiscoverGVRs(ctx context.Context) error {
	return nil
}

func (m *mockFluxClient) GetGVR(kind string) (schema.GroupVersionResource, error) {
	return schema.GroupVersionResource{Group: "test", Version: "v1", Resource: "test"}, nil
}

func (m *mockFluxClient) GetResource(ctx context.Context, kind, namespace, name string) (*flux.ResourceInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s/%s/%s", kind, namespace, name)
	info, ok := m.resources[key]
	if !ok {
		return nil, fmt.Errorf("resource %s not found", key)
	}
	cp := *info
	return &cp, nil
}

func (m *mockFluxClient) PatchSuspend(ctx context.Context, kind, namespace, name string, suspend bool) (*flux.ResourceInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s/%s/%s", kind, namespace, name)
	if err, fail := m.failPatch[key]; fail {
		return nil, err
	}
	info, ok := m.resources[key]
	if !ok {
		return nil, fmt.Errorf("resource %s not found", key)
	}
	info.Suspended = suspend
	cp := *info
	return &cp, nil
}

func createTestConfig() *config.Config {
	return &config.Config{
		Flux: config.FluxConfig{
			Resources: []config.Resource{
				{Kind: config.KindKustomization, Namespace: "flux-system", Name: "flux-system"},
				{Kind: config.KindKustomization, Namespace: "flux-system", Name: "apps"},
				{Kind: config.KindHelmRelease, Namespace: "monitoring", Name: "kube-prometheus-stack"},
				{Kind: config.KindHelmRelease, Namespace: "traefik", Name: "traefik"},
			},
		},
	}
}

func TestEngineOrdering(t *testing.T) {
	cfg := createTestConfig()
	client := newMockFluxClient()
	eng := New(cfg, client, t.TempDir(), "talos", nil)

	// Suspend order: Kustomizations reverse (apps, flux-system), then HelmReleases reverse (traefik, kube-prometheus-stack)
	suspendOrder := eng.OrderedResources(ActionSuspend)
	if len(suspendOrder) != 4 {
		t.Fatalf("expected 4 resources, got %d", len(suspendOrder))
	}
	expectedSuspend := []string{
		"Kustomization/flux-system/apps",
		"Kustomization/flux-system/flux-system",
		"HelmRelease/traefik/traefik",
		"HelmRelease/monitoring/kube-prometheus-stack",
	}
	for i, r := range suspendOrder {
		if r.Key() != expectedSuspend[i] {
			t.Errorf("suspend index %d: expected %s, got %s", i, expectedSuspend[i], r.Key())
		}
	}

	// Restore order: HelmReleases forward (kube-prometheus-stack, traefik), then Kustomizations forward (flux-system, apps)
	restoreOrder := eng.OrderedResources(ActionRestore)
	expectedRestore := []string{
		"HelmRelease/monitoring/kube-prometheus-stack",
		"HelmRelease/traefik/traefik",
		"Kustomization/flux-system/flux-system",
		"Kustomization/flux-system/apps",
	}
	for i, r := range restoreOrder {
		if r.Key() != expectedRestore[i] {
			t.Errorf("restore index %d: expected %s, got %s", i, expectedRestore[i], r.Key())
		}
	}
}

func TestEngineSuspendAndRestoreFlow(t *testing.T) {
	cfg := createTestConfig()
	client := newMockFluxClient()

	// Populate mock resources
	client.resources["Kustomization/flux-system/flux-system"] = &flux.ResourceInfo{
		Kind: "Kustomization", Namespace: "flux-system", Name: "flux-system", Suspended: false, Ready: "True",
	}
	client.resources["Kustomization/flux-system/apps"] = &flux.ResourceInfo{
		Kind: "Kustomization", Namespace: "flux-system", Name: "apps", Suspended: false, Ready: "True",
	}
	client.resources["HelmRelease/monitoring/kube-prometheus-stack"] = &flux.ResourceInfo{
		Kind: "HelmRelease", Namespace: "monitoring", Name: "kube-prometheus-stack", Suspended: false, Ready: "True",
	}
	client.resources["HelmRelease/traefik/traefik"] = &flux.ResourceInfo{
		Kind: "HelmRelease", Namespace: "traefik", Name: "traefik", Suspended: false, Ready: "True",
	}

	stateDir := t.TempDir()
	eng := New(cfg, client, stateDir, "talos", nil)

	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer eng.Close()

	// 1. Check initial status
	status, err := eng.Status(ctx)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	// Initial state is not suspended, so GlobalStatus should be partial
	if status.GlobalStatus != GlobalStatusPartial {
		t.Errorf("expected initial global status partial, got %s", status.GlobalStatus)
	}

	// 2. Build dry run plan for suspend
	plan, err := eng.BuildPlan(ctx, ActionSuspend)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}
	if len(plan.Items) != 4 {
		t.Fatalf("expected 4 plan items, got %d", len(plan.Items))
	}
	for _, it := range plan.Items {
		if !it.TargetSuspended {
			t.Errorf("expected TargetSuspended=true for %s", it.Resource.Key())
		}
		if !it.WillChange {
			t.Errorf("expected WillChange=true for %s", it.Resource.Key())
		}
	}

	// 3. Execute Suspend
	report, err := eng.Execute(ctx, ActionSuspend, false, nil)
	if err != nil {
		t.Fatalf("Execute(suspend) failed: %v", err)
	}
	if report.OK != 4 || report.Failed != 0 {
		t.Errorf("expected 4 OK, 0 failed, got OK=%d, Failed=%d", report.OK, report.Failed)
	}
	if report.GlobalStatus != GlobalStatusSuspended {
		t.Errorf("expected GlobalStatusSuspended, got %s", report.GlobalStatus)
	}

	// Verify all resources in mock client are now suspended
	for key, res := range client.resources {
		if !res.Suspended {
			t.Errorf("resource %s was not suspended", key)
		}
	}

	// Verify status reports GlobalStatusSuspended
	statusAfterSuspend, err := eng.Status(ctx)
	if err != nil {
		t.Fatalf("Status after suspend failed: %v", err)
	}
	if statusAfterSuspend.GlobalStatus != GlobalStatusSuspended {
		t.Errorf("expected status GlobalStatusSuspended, got %s", statusAfterSuspend.GlobalStatus)
	}

	// 4. Execute Restore
	restoreReport, err := eng.Execute(ctx, ActionRestore, false, nil)
	if err != nil {
		t.Fatalf("Execute(restore) failed: %v", err)
	}
	if restoreReport.OK != 4 || restoreReport.Failed != 0 {
		t.Errorf("expected 4 OK, 0 failed on restore, got OK=%d, Failed=%d", restoreReport.OK, restoreReport.Failed)
	}
	if restoreReport.GlobalStatus != GlobalStatusRestored {
		t.Errorf("expected GlobalStatusRestored, got %s", restoreReport.GlobalStatus)
	}

	// Verify all resources restored to false
	for key, res := range client.resources {
		if res.Suspended {
			t.Errorf("resource %s was not restored to false", key)
		}
	}
}

func TestEnginePartialFailure(t *testing.T) {
	cfg := createTestConfig()
	client := newMockFluxClient()

	client.resources["Kustomization/flux-system/flux-system"] = &flux.ResourceInfo{
		Kind: "Kustomization", Namespace: "flux-system", Name: "flux-system", Suspended: false,
	}
	client.resources["Kustomization/flux-system/apps"] = &flux.ResourceInfo{
		Kind: "Kustomization", Namespace: "flux-system", Name: "apps", Suspended: false,
	}
	client.resources["HelmRelease/monitoring/kube-prometheus-stack"] = &flux.ResourceInfo{
		Kind: "HelmRelease", Namespace: "monitoring", Name: "kube-prometheus-stack", Suspended: false,
	}
	client.resources["HelmRelease/traefik/traefik"] = &flux.ResourceInfo{
		Kind: "HelmRelease", Namespace: "traefik", Name: "traefik", Suspended: false,
	}

	// Introduce a patch failure on traefik
	client.failPatch["HelmRelease/traefik/traefik"] = fmt.Errorf("connection refused")

	stateDir := t.TempDir()
	eng := New(cfg, client, stateDir, "talos", nil)

	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer eng.Close()

	report, err := eng.Execute(ctx, ActionSuspend, false, nil)
	if err != ErrPartialFailure {
		t.Fatalf("expected ErrPartialFailure, got %v", err)
	}
	if report.OK != 3 || report.Failed != 1 {
		t.Errorf("expected 3 OK, 1 Failed, got OK=%d, Failed=%d", report.OK, report.Failed)
	}
	if report.GlobalStatus != GlobalStatusPartial {
		t.Errorf("expected GlobalStatusPartial, got %s", report.GlobalStatus)
	}
}

func TestEngineConflictDetection(t *testing.T) {
	cfg := createTestConfig()
	client := newMockFluxClient()

	client.resources["Kustomization/flux-system/flux-system"] = &flux.ResourceInfo{Kind: "Kustomization", Namespace: "flux-system", Name: "flux-system", Suspended: false}
	client.resources["Kustomization/flux-system/apps"] = &flux.ResourceInfo{Kind: "Kustomization", Namespace: "flux-system", Name: "apps", Suspended: false}
	client.resources["HelmRelease/monitoring/kube-prometheus-stack"] = &flux.ResourceInfo{Kind: "HelmRelease", Namespace: "monitoring", Name: "kube-prometheus-stack", Suspended: false}
	client.resources["HelmRelease/traefik/traefik"] = &flux.ResourceInfo{Kind: "HelmRelease", Namespace: "traefik", Name: "traefik", Suspended: false}

	stateDir := t.TempDir()
	eng := New(cfg, client, stateDir, "talos", nil)
	ctx := context.Background()
	_ = eng.Init(ctx)
	defer eng.Close()

	// Suspend all
	_, err := eng.Execute(ctx, ActionSuspend, false, nil)
	if err != nil {
		t.Fatalf("Suspend failed: %v", err)
	}

	// External change: someone manually unsuspends apps on the cluster
	client.resources["Kustomization/flux-system/apps"].Suspended = false

	// Attempt restore without --force -> should return ErrConflict
	_, err = eng.Execute(ctx, ActionRestore, false, nil)
	if err != ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// Attempt restore with force=true -> should succeed
	report, err := eng.Execute(ctx, ActionRestore, true, nil)
	if err != nil {
		t.Fatalf("Execute with force failed: %v", err)
	}
	if report.OK != 4 {
		t.Errorf("expected 4 OK, got %d", report.OK)
	}

	// Verify history contains the conflict item
	history, err := eng.History(10)
	if err != nil {
		t.Fatalf("History failed: %v", err)
	}
	hasConflict := false
	for _, h := range history {
		if h.Status == "conflict" {
			hasConflict = true
			if h.Action != string(ActionRestore) {
				t.Errorf("expected conflict action %s, got %s", ActionRestore, h.Action)
			}
			break
		}
	}
	if !hasConflict {
		t.Error("expected history to record conflict event")
	}
}

func TestEngineRestoreAfterPartialSuspendDoesNotFalseConflict(t *testing.T) {
	cfg := createTestConfig()
	client := newMockFluxClient()

	client.resources["Kustomization/flux-system/flux-system"] = &flux.ResourceInfo{Kind: "Kustomization", Namespace: "flux-system", Name: "flux-system", Suspended: false}
	client.resources["Kustomization/flux-system/apps"] = &flux.ResourceInfo{Kind: "Kustomization", Namespace: "flux-system", Name: "apps", Suspended: false}
	client.resources["HelmRelease/monitoring/kube-prometheus-stack"] = &flux.ResourceInfo{Kind: "HelmRelease", Namespace: "monitoring", Name: "kube-prometheus-stack", Suspended: false}
	client.resources["HelmRelease/traefik/traefik"] = &flux.ResourceInfo{Kind: "HelmRelease", Namespace: "traefik", Name: "traefik", Suspended: false}

	// Fail one resource on suspend so operation status is partial
	client.failPatch["HelmRelease/traefik/traefik"] = fmt.Errorf("network glitch")

	stateDir := t.TempDir()
	eng := New(cfg, client, stateDir, "talos", nil)
	ctx := context.Background()
	_ = eng.Init(ctx)
	defer eng.Close()

	// Partial suspend
	_, err := eng.Execute(ctx, ActionSuspend, false, nil)
	if err != ErrPartialFailure {
		t.Fatalf("expected ErrPartialFailure, got %v", err)
	}

	// traefik was never suspended (still false)
	// Now attempt restore WITHOUT --force: it must NOT report conflict for traefik
	// Because the previous suspend did not succeed!
	plan, err := eng.BuildPlan(ctx, ActionRestore)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}
	if len(plan.Conflicts) > 0 {
		t.Fatalf("expected 0 conflicts on restore after partial suspend, got %d (%s)",
			len(plan.Conflicts), plan.Conflicts[0].ConflictReason)
	}
}
