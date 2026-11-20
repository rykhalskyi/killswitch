package actions

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/rykhalskyi/killswitch/internal/config"
)

func newFakeKube() *KubeClient {
	scheme := runtime.NewScheme()
	deployment := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      "jellyfin",
			"namespace": "media",
		},
		"spec": map[string]any{"replicas": int64(3)},
	}}
	cron := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "CronJob",
		"metadata": map[string]any{
			"name":      "backup",
			"namespace": "media",
		},
		"spec": map[string]any{"suspend": false},
	}}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			gvrDeployments: "DeploymentList",
			gvrCronJobs:    "CronJobList",
		}, deployment, cron)
	return NewKubeClient(dyn)
}

func TestKubeClientScale(t *testing.T) {
	client := newFakeKube()
	ctx := context.Background()

	replicas, exists, err := client.ObserveReplicas(ctx, config.KindDeployment, "media", "jellyfin")
	if err != nil || !exists || replicas != 3 {
		t.Fatalf("observe = %d %v %v", replicas, exists, err)
	}
	got, err := client.Scale(ctx, config.KindDeployment, "media", "jellyfin", 0)
	if err != nil || got != 0 {
		t.Fatalf("scale = %d %v", got, err)
	}
	replicas, _, _ = client.ObserveReplicas(ctx, config.KindDeployment, "media", "jellyfin")
	if replicas != 0 {
		t.Fatalf("replicas = %d, want 0", replicas)
	}
	if _, _, err := client.ObserveReplicas(ctx, config.KindDeployment, "media", "missing"); err != nil {
		t.Fatalf("missing observe should not error: %v", err)
	}
}

func TestKubeClientCronJobSuspend(t *testing.T) {
	client := newFakeKube()
	ctx := context.Background()

	suspended, exists, err := client.ObserveSuspend(ctx, "media", "backup")
	if err != nil || !exists || suspended {
		t.Fatalf("observe = %v %v %v", suspended, exists, err)
	}
	got, err := client.PatchSuspend(ctx, "media", "backup", true)
	if err != nil || !got {
		t.Fatalf("patch = %v %v", got, err)
	}
}

func TestRegistryUnknownAction(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Get("missing"); err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestScaleHandlerMissingOriginal(t *testing.T) {
	h := &ScaleHandler{}
	step := config.Step{
		ID:     "jellyfin",
		Action: config.ActionKubeScale,
		Target: config.Resource{Kind: config.KindDeployment, Namespace: "media", Name: "jellyfin"},
		States: config.StepStates{On: config.StateValues{"restore": "previous"}},
	}
	if _, err := h.Desired(step, config.DirectionOn, nil); err == nil {
		t.Fatal("expected missing original error")
	}
}
