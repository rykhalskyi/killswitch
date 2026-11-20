package actions

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	"github.com/rykhalskyi/killswitch/internal/config"
)

var (
	gvrDeployments  = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	gvrStatefulSets = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}
	gvrCronJobs     = schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}
)

// KubeClient implements ScaleClient and CronJob suspend using a dynamic client.
type KubeClient struct {
	dyn dynamic.Interface
}

// NewKubeClient wraps an existing dynamic client.
func NewKubeClient(dyn dynamic.Interface) *KubeClient {
	return &KubeClient{dyn: dyn}
}

func workloadGVR(kind string) (schema.GroupVersionResource, error) {
	switch kind {
	case config.KindDeployment:
		return gvrDeployments, nil
	case config.KindStatefulSet:
		return gvrStatefulSets, nil
	default:
		return schema.GroupVersionResource{}, fmt.Errorf("unsupported workload kind %q", kind)
	}
}

// ObserveReplicas reads spec.replicas for a Deployment or StatefulSet.
func (c *KubeClient) ObserveReplicas(ctx context.Context, kind, namespace, name string) (int32, bool, error) {
	gvr, err := workloadGVR(kind)
	if err != nil {
		return 0, false, err
	}
	obj, err := c.dyn.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if isNotFound(err) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("getting %s/%s/%s: %w", kind, namespace, name, err)
	}
	replicas, found, err := unstructured.NestedInt64(obj.Object, "spec", "replicas")
	if err != nil {
		return 0, false, fmt.Errorf("reading spec.replicas for %s/%s/%s: %w", kind, namespace, name, err)
	}
	if !found {
		replicas = 1
	}
	return int32(replicas), true, nil
}

// Scale patches spec.replicas and reads the value back.
func (c *KubeClient) Scale(ctx context.Context, kind, namespace, name string, replicas int32) (int32, error) {
	gvr, err := workloadGVR(kind)
	if err != nil {
		return 0, err
	}
	patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas)
	if _, err := c.dyn.Resource(gvr).Namespace(namespace).Patch(ctx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{}); err != nil {
		return 0, fmt.Errorf("patching spec.replicas for %s/%s/%s: %w", kind, namespace, name, err)
	}
	replicasOut, exists, err := c.ObserveReplicas(ctx, kind, namespace, name)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, fmt.Errorf("target %s/%s/%s not found after scaling", kind, namespace, name)
	}
	return replicasOut, nil
}

// CronJobClient observes and changes spec.suspend for CronJobs.
type CronJobClient interface {
	ObserveSuspend(ctx context.Context, namespace, name string) (suspend bool, exists bool, err error)
	PatchSuspend(ctx context.Context, namespace, name string, suspend bool) (bool, error)
}

// ObserveSuspend reads spec.suspend for a CronJob.
func (c *KubeClient) ObserveSuspend(ctx context.Context, namespace, name string) (bool, bool, error) {
	obj, err := c.dyn.Resource(gvrCronJobs).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if isNotFound(err) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("getting CronJob/%s/%s: %w", namespace, name, err)
	}
	suspend, found, err := unstructured.NestedBool(obj.Object, "spec", "suspend")
	if err != nil {
		return false, false, fmt.Errorf("reading spec.suspend for CronJob/%s/%s: %w", namespace, name, err)
	}
	if !found {
		suspend = false
	}
	return suspend, true, nil
}

// PatchSuspend patches spec.suspend for a CronJob and reads it back.
func (c *KubeClient) PatchSuspend(ctx context.Context, namespace, name string, suspend bool) (bool, error) {
	patch := fmt.Sprintf(`{"spec":{"suspend":%t}}`, suspend)
	if _, err := c.dyn.Resource(gvrCronJobs).Namespace(namespace).Patch(ctx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{}); err != nil {
		return false, fmt.Errorf("patching spec.suspend for CronJob/%s/%s: %w", namespace, name, err)
	}
	suspended, exists, err := c.ObserveSuspend(ctx, namespace, name)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, fmt.Errorf("CronJob/%s/%s not found after patching", namespace, name)
	}
	return suspended, nil
}

func isNotFound(err error) bool {
	return apierrors.IsNotFound(err)
}

// CronJobSuspendHandler implements kubernetes.suspend.
type CronJobSuspendHandler struct {
	Client CronJobClient
}

func (h *CronJobSuspendHandler) Validate(step config.Step) error {
	if step.Target.Kind != config.KindCronJob {
		return fmt.Errorf("kubernetes.suspend does not support kind %q", step.Target.Kind)
	}
	if h.Client == nil {
		return fmt.Errorf("kubernetes.suspend client is not configured")
	}
	return nil
}

func (h *CronJobSuspendHandler) Observe(ctx context.Context, step config.Step) (any, bool, error) {
	v, exists, err := h.Client.ObserveSuspend(ctx, step.Target.Namespace, step.Target.Name)
	if err != nil {
		return nil, false, err
	}
	return v, exists, nil
}

func (h *CronJobSuspendHandler) Capture(ctx context.Context, step config.Step) (any, error) {
	v, exists, err := h.Observe(ctx, step)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("target %s not found", step.Target.Key())
	}
	return v, nil
}

func (h *CronJobSuspendHandler) Desired(step config.Step, direction string, original any) (any, error) {
	if direction == config.DirectionOn {
		if restore, _ := step.States.On.StringValue("restore"); restore == "previous" {
			if original == nil {
				if def, ok := Bool(step.States.On["default"]); ok {
					return def, nil
				}
				return nil, ErrMissingOriginal
			}
			return original, nil
		}
		if v, ok := Bool(step.States.On["suspend"]); ok {
			return v, nil
		}
		return false, nil
	}
	if v, ok := Bool(step.States.Off["suspend"]); ok {
		return v, nil
	}
	return true, nil
}

func (h *CronJobSuspendHandler) Apply(ctx context.Context, step config.Step, desired any) (any, error) {
	v, ok := Bool(desired)
	if !ok {
		return nil, fmt.Errorf("cronjob desired value is not a bool")
	}
	return h.Client.PatchSuspend(ctx, step.Target.Namespace, step.Target.Name, v)
}

func (h *CronJobSuspendHandler) Restore(ctx context.Context, step config.Step, original any) (any, error) {
	return h.Apply(ctx, step, original)
}

func (h *CronJobSuspendHandler) Equal(a, b any) bool {
	av, aok := Bool(a)
	bv, bok := Bool(b)
	return aok && bok && av == bv
}
