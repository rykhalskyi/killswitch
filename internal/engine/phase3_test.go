package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/rykhalskyi/killswitch/internal/config"
	"github.com/rykhalskyi/killswitch/internal/flux"
	"github.com/rykhalskyi/killswitch/internal/journal"
)

type fakeScaleClient struct {
	mu       sync.Mutex
	replicas map[string]int32
	fail     map[string]error
}

func newFakeScaleClient() *fakeScaleClient {
	return &fakeScaleClient{replicas: make(map[string]int32), fail: make(map[string]error)}
}

func (f *fakeScaleClient) ObserveReplicas(_ context.Context, kind, namespace, name string) (int32, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fmt.Sprintf("%s/%s/%s", kind, namespace, name)
	v, ok := f.replicas[key]
	if !ok {
		return 0, false, fmt.Errorf("not found: %s", key)
	}
	return v, true, nil
}

func (f *fakeScaleClient) Scale(_ context.Context, kind, namespace, name string, replicas int32) (int32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fmt.Sprintf("%s/%s/%s", kind, namespace, name)
	if err, ok := f.fail[key]; ok {
		return 0, err
	}
	if _, ok := f.replicas[key]; !ok {
		return 0, fmt.Errorf("not found: %s", key)
	}
	f.replicas[key] = replicas
	return replicas, nil
}

func scaleConfig(on StateOn) *config.Config {
	onState := config.StateValues{"restore": "previous"}
	if on != nil {
		onState = on
	}
	return &config.Config{
		Switches: []config.Switch{
			{ID: "flux", Name: "Flux Reconciliation", Workflow: "flux"},
			{ID: "media", Name: "Media services", Description: "Media apps", Workflow: "media"},
		},
		Workflows: map[string]*config.Workflow{
			"flux": {
				ID:   "flux",
				Name: "Flux",
				Steps: []config.Step{
					{
						ID: "apps", Action: config.ActionFluxSuspend,
						Target: config.Resource{Kind: config.KindKustomization, Namespace: "flux-system", Name: "apps"},
						States: config.StepStates{Off: config.StateValues{"suspend": true}, On: config.StateValues{"restore": "previous", "default": false}},
					},
				},
			},
			"media": {
				ID:       "media",
				Name:     "Media",
				Requires: config.Requires{Flux: config.RequiresSuspended},
				Steps: []config.Step{
					{
						ID: "jellyfin", Action: config.ActionKubeScale,
						Target: config.Resource{Kind: config.KindDeployment, Namespace: "media", Name: "jellyfin"},
						States: config.StepStates{Off: config.StateValues{"replicas": 0}, On: onState},
					},
					{
						ID: "sonarr", Action: config.ActionKubeScale,
						Target: config.Resource{Kind: config.KindStatefulSet, Namespace: "media", Name: "sonarr"},
						States: config.StepStates{Off: config.StateValues{"replicas": 0}, On: onState},
					},
				},
			},
		},
	}
}

type StateOn = config.StateValues

func newScaleEngine(t *testing.T, cfg *config.Config, scale *fakeScaleClient) (*Engine, *mockFluxClient) {
	t.Helper()
	fluxClient := newMockFluxClient()
	fluxClient.resources["Kustomization/flux-system/apps"] = &flux.ResourceInfo{
		Kind: "Kustomization", Namespace: "flux-system", Name: "apps", Suspended: false, Ready: "True",
	}
	eng := New(cfg, fluxClient, t.TempDir(), "test", nil)
	eng.SetScaleClient(scale)
	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng, fluxClient
}

func TestPhase3ScaleOffCapturesAndOnRestores(t *testing.T) {
	scale := newFakeScaleClient()
	scale.replicas["Deployment/media/jellyfin"] = 3
	scale.replicas["StatefulSet/media/sonarr"] = 1
	eng, _ := newScaleEngine(t, scaleConfig(nil), scale)
	ctx := context.Background()

	report, err := eng.ExecuteSwitch(ctx, "media", DirectionOff, false, nil)
	if err != nil {
		t.Fatalf("scale off failed: %v", err)
	}
	if report.OK != 2 {
		t.Fatalf("expected 2 OK, got %d", report.OK)
	}
	for key, r := range scale.replicas {
		if r != 0 {
			t.Errorf("%s = %d, want 0", key, r)
		}
	}

	report, err = eng.ExecuteSwitch(ctx, "media", DirectionOn, false, nil)
	if err != nil {
		t.Fatalf("scale on failed: %v", err)
	}
	if report.OK != 3 {
		t.Fatalf("expected flux prerequisite + 2 OK on restore, got %d", report.OK)
	}
	if scale.replicas["Deployment/media/jellyfin"] != 3 {
		t.Errorf("jellyfin = %d, want restored 3", scale.replicas["Deployment/media/jellyfin"])
	}
	if scale.replicas["StatefulSet/media/sonarr"] != 1 {
		t.Errorf("sonarr = %d, want restored 1", scale.replicas["StatefulSet/media/sonarr"])
	}
}

func TestPhase3ScaleConflictAfterExternalChange(t *testing.T) {
	scale := newFakeScaleClient()
	scale.replicas["Deployment/media/jellyfin"] = 3
	scale.replicas["StatefulSet/media/sonarr"] = 1
	eng, _ := newScaleEngine(t, scaleConfig(nil), scale)
	ctx := context.Background()

	if _, err := eng.ExecuteSwitch(ctx, "media", DirectionOff, false, nil); err != nil {
		t.Fatalf("scale off failed: %v", err)
	}
	scale.replicas["Deployment/media/jellyfin"] = 5

	plan, err := eng.BuildSwitchPlan(ctx, "media", DirectionOn)
	if err != nil {
		t.Fatalf("BuildSwitchPlan failed: %v", err)
	}
	if len(plan.Conflicts) != 1 || plan.Conflicts[0].Resource.Name != "jellyfin" {
		t.Fatalf("expected jellyfin conflict, got %+v", plan.Conflicts)
	}
	if _, err := eng.ExecuteSwitch(ctx, "media", DirectionOn, false, nil); err != ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	if _, err := eng.ExecuteSwitch(ctx, "media", DirectionOn, true, nil); err != nil {
		t.Fatalf("forced restore failed: %v", err)
	}
	if scale.replicas["Deployment/media/jellyfin"] != 3 {
		t.Errorf("forced restore = %d, want 3", scale.replicas["Deployment/media/jellyfin"])
	}
}

func TestPhase3ScaleRefusesOnWithoutOriginal(t *testing.T) {
	scale := newFakeScaleClient()
	scale.replicas["Deployment/media/jellyfin"] = 3
	scale.replicas["StatefulSet/media/sonarr"] = 1
	eng, _ := newScaleEngine(t, scaleConfig(nil), scale)

	if _, err := eng.BuildSwitchPlan(context.Background(), "media", DirectionOn); err == nil {
		t.Fatal("expected refusal when no original is recorded")
	}
}

func TestPhase3ScaleResumeDoesNotRecapture(t *testing.T) {
	scale := newFakeScaleClient()
	scale.replicas["Deployment/media/jellyfin"] = 0
	scale.replicas["StatefulSet/media/sonarr"] = 1
	cfg := scaleConfig(nil)
	stateDir := t.TempDir()

	j, err := journal.Open(filepath.Join(stateDir, "journal.jsonl"))
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	events := []journal.Event{
		{Op: "01SCALE", Event: journal.EventOpStart, Action: config.ActionKubeScale, Switch: "media", Workflow: "media", Direction: "off", Targets: 2},
		{Op: "01SCALE", Event: journal.EventResourceIntent, Action: config.ActionKubeScale, Switch: "media", Workflow: "media", Step: "jellyfin", Direction: "off",
			Kind: "Deployment", Namespace: "media", Name: "jellyfin", Original: 3, Desired: 0},
		{Op: "01SCALE", Event: journal.EventResourceResult, Action: config.ActionKubeScale, Switch: "media", Workflow: "media", Step: "jellyfin", Direction: "off",
			Kind: "Deployment", Namespace: "media", Name: "jellyfin", Status: journal.StatusOk, Observed: 0, Original: 3, Desired: 0},
		{Op: "01SCALE", Event: journal.EventResourceIntent, Action: config.ActionKubeScale, Switch: "media", Workflow: "media", Step: "sonarr", Direction: "off",
			Kind: "StatefulSet", Namespace: "media", Name: "sonarr", Original: 1, Desired: 0},
	}
	for i := range events {
		if err := j.Append(&events[i]); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	_ = j.Close()

	fluxClient := newMockFluxClient()
	fluxClient.resources["Kustomization/flux-system/apps"] = &flux.ResourceInfo{Kind: "Kustomization", Namespace: "flux-system", Name: "apps"}
	eng := New(cfg, fluxClient, stateDir, "test", nil)
	eng.SetScaleClient(scale)
	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer eng.Close()

	plan, err := eng.BuildSwitchPlan(ctx, "media", DirectionOff)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Step != "sonarr" {
		t.Fatalf("resume should only plan unfinished sonarr, got %+v", plan.Items)
	}
	if _, err := eng.ExecuteSwitch(ctx, "media", DirectionOff, false, nil); err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	if got, ok := journal.IntValue(eng.State().OriginalValues["media/jellyfin"].Value); !ok || got != 3 {
		t.Fatalf("jellyfin original was recaptured: %v", eng.State().OriginalValues["media/jellyfin"].Value)
	}
	if scale.replicas["StatefulSet/media/sonarr"] != 0 {
		t.Fatalf("sonarr was not scaled to 0")
	}
}

func TestPhase3ForwardInterlockSuspendsFlux(t *testing.T) {
	scale := newFakeScaleClient()
	scale.replicas["Deployment/media/jellyfin"] = 2
	scale.replicas["StatefulSet/media/sonarr"] = 1
	eng, fluxClient := newScaleEngine(t, scaleConfig(StateOn{"replicas": 2}), scale)
	ctx := context.Background()

	report, err := eng.ExecuteSwitch(ctx, "media", DirectionOn, false, nil)
	if err != nil {
		t.Fatalf("media on failed: %v", err)
	}
	if len(report.Results) != 3 {
		t.Fatalf("expected flux suspend + 2 scale steps in one op, got %d", len(report.Results))
	}
	if !fluxClient.resources["Kustomization/flux-system/apps"].Suspended {
		t.Fatal("flux was not auto-suspended")
	}
	if got := eng.State().LastCompletedOp; got == nil || got.Switch != "media" || got.Status != journal.StatusSuccess {
		t.Fatalf("cascade was not journaled under the media op: %+v", got)
	}
	history, err := eng.History(10)
	if err != nil || len(history) != 1 {
		t.Fatalf("expected one journaled operation, got %v %+v", err, history)
	}
}

func TestPhase3ReverseInterlockRunsDependentOffFirst(t *testing.T) {
	scale := newFakeScaleClient()
	scale.replicas["Deployment/media/jellyfin"] = 2
	scale.replicas["StatefulSet/media/sonarr"] = 1
	eng, fluxClient := newScaleEngine(t, scaleConfig(StateOn{"replicas": 2}), scale)
	ctx := context.Background()

	if _, err := eng.ExecuteSwitch(ctx, "media", DirectionOn, false, nil); err != nil {
		t.Fatalf("media on failed: %v", err)
	}
	if _, err := eng.ExecuteSwitch(ctx, fluxClientFluxID(eng), DirectionOn, false, nil); err != nil {
		t.Fatalf("flux restore failed: %v", err)
	}
	if fluxClient.resources["Kustomization/flux-system/apps"].Suspended {
		t.Fatal("flux was not restored")
	}
	if scale.replicas["Deployment/media/jellyfin"] != 0 || scale.replicas["StatefulSet/media/sonarr"] != 0 {
		t.Fatalf("dependent OFF workflow did not run before flux restore: %+v", scale.replicas)
	}

	events, err := journal.ReadAll(filepath.Join(eng.stateDir, "journal.jsonl"))
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	firstStep := ""
	for _, ev := range events {
		if ev.Op == eng.State().LastCompletedOp.Op && ev.Event == journal.EventResourceIntent {
			firstStep = ev.Switch
			break
		}
	}
	if firstStep != "media" {
		t.Fatalf("expected dependent OFF steps before flux restore, first step switch=%q", firstStep)
	}
}

func fluxClientFluxID(eng *Engine) string {
	if id := eng.FluxSwitchID(); id != "" {
		return id
	}
	return "flux"
}

func TestPhase3ScaleRestoresAfterRestart(t *testing.T) {
	scale := newFakeScaleClient()
	scale.replicas["Deployment/media/jellyfin"] = 4
	scale.replicas["StatefulSet/media/sonarr"] = 2
	cfg := scaleConfig(nil)
	stateDir := t.TempDir()
	fluxClient := newMockFluxClient()
	fluxClient.resources["Kustomization/flux-system/apps"] = &flux.ResourceInfo{Kind: "Kustomization", Namespace: "flux-system", Name: "apps"}

	eng := New(cfg, fluxClient, stateDir, "test", nil)
	eng.SetScaleClient(scale)
	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := eng.ExecuteSwitch(ctx, "media", DirectionOff, false, nil); err != nil {
		t.Fatalf("off: %v", err)
	}
	_ = eng.Close()

	// Restart against the same state directory.
	eng2 := New(cfg, fluxClient, stateDir, "test", nil)
	eng2.SetScaleClient(scale)
	if err := eng2.Init(ctx); err != nil {
		t.Fatalf("re-init: %v", err)
	}
	defer eng2.Close()
	if _, err := eng2.ExecuteSwitch(ctx, "media", DirectionOn, false, nil); err != nil {
		t.Fatalf("on after restart: %v", err)
	}
	if scale.replicas["Deployment/media/jellyfin"] != 4 || scale.replicas["StatefulSet/media/sonarr"] != 2 {
		t.Fatalf("originals were not restored after restart: %+v", scale.replicas)
	}
}

func TestPhase3FailedDependentOffDoesNotBlockFlux(t *testing.T) {
	scale := newFakeScaleClient()
	scale.replicas["Deployment/media/jellyfin"] = 2
	scale.replicas["StatefulSet/media/sonarr"] = 1
	eng, fluxClient := newScaleEngine(t, scaleConfig(StateOn{"replicas": 2}), scale)
	ctx := context.Background()

	if _, err := eng.ExecuteSwitch(ctx, "media", DirectionOn, false, nil); err != nil {
		t.Fatalf("media on failed: %v", err)
	}
	scale.fail["Deployment/media/jellyfin"] = fmt.Errorf("scale backend down")

	report, err := eng.ExecuteSwitch(ctx, fluxClientFluxID(eng), DirectionOn, false, nil)
	if err != ErrPartialFailure {
		t.Fatalf("expected partial failure, got %v", err)
	}
	if fluxClient.resources["Kustomization/flux-system/apps"].Suspended {
		t.Fatal("flux must still resume after a failed dependent OFF")
	}
	if report.OK == 0 || report.Failed == 0 {
		t.Fatalf("expected mixed results, got OK=%d Failed=%d", report.OK, report.Failed)
	}
}
