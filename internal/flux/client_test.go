package flux

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestExtractResourceInfo(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "kustomize.toolkit.fluxcd.io/v1",
			"kind":       "Kustomization",
			"metadata": map[string]interface{}{
				"name":       "apps",
				"namespace":  "flux-system",
				"generation": int64(5),
			},
			"spec": map[string]interface{}{
				"suspend": true,
			},
			"status": map[string]interface{}{
				"observedGeneration": int64(5),
				"conditions": []interface{}{
					map[string]interface{}{
						"type":    "Ready",
						"status":  "True",
						"reason":  "ReconciliationSucceeded",
						"message": "Applied revision: main@sha1:12345",
					},
				},
			},
		},
	}

	info := ExtractResourceInfo(obj, "Kustomization", "flux-system", "apps")
	if info.Kind != "Kustomization" || info.Namespace != "flux-system" || info.Name != "apps" {
		t.Fatalf("unexpected identity: %+v", info)
	}
	if !info.Suspended {
		t.Errorf("expected Suspended to be true")
	}
	if info.Generation != 5 {
		t.Errorf("expected Generation 5, got %d", info.Generation)
	}
	if info.ObservedGeneration != 5 {
		t.Errorf("expected ObservedGeneration 5, got %d", info.ObservedGeneration)
	}
	if info.Ready != "True" {
		t.Errorf("expected Ready 'True', got %s", info.Ready)
	}
	if info.ReadyReason != "ReconciliationSucceeded" {
		t.Errorf("expected ReadyReason 'ReconciliationSucceeded', got %s", info.ReadyReason)
	}
}

func TestDynamicClientPatchAndGet(t *testing.T) {
	scheme := runtime.NewScheme()
	gvr := schema.GroupVersionResource{
		Group:    GroupKustomize,
		Version:  "v1",
		Resource: "kustomizations",
	}

	initialObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "kustomize.toolkit.fluxcd.io/v1",
			"kind":       "Kustomization",
			"metadata": map[string]interface{}{
				"name":       "apps",
				"namespace":  "flux-system",
				"generation": int64(1),
			},
			"spec": map[string]interface{}{
				"suspend": false,
			},
		},
	}

	dynFake := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		gvr: "KustomizationList",
	}, initialObj)

	client := NewClientFromClients(dynFake, nil)

	ctx := context.Background()
	info, err := client.GetResource(ctx, "Kustomization", "flux-system", "apps")
	if err != nil {
		t.Fatalf("GetResource failed: %v", err)
	}
	if info.Suspended {
		t.Errorf("expected initial Suspended to be false")
	}

	// Patch to suspend = true
	updated, err := client.PatchSuspend(ctx, "Kustomization", "flux-system", "apps", true)
	if err != nil {
		t.Fatalf("PatchSuspend failed: %v", err)
	}
	if !updated.Suspended {
		t.Errorf("expected updated Suspended to be true")
	}
}
