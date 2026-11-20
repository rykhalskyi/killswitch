package actions

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/rykhalskyi/killswitch/internal/config"
	"github.com/rykhalskyi/killswitch/internal/flux"
)

// ErrMissingOriginal indicates an ON transition without a recorded original.
var ErrMissingOriginal = errors.New("no recorded original state")

// Handler implements one reversible action capability.
type Handler interface {
	Validate(step config.Step) error
	Observe(ctx context.Context, step config.Step) (value any, exists bool, err error)
	Capture(ctx context.Context, step config.Step) (original any, err error)
	Desired(step config.Step, direction string, original any) (any, error)
	Apply(ctx context.Context, step config.Step, desired any) (value any, err error)
	Restore(ctx context.Context, step config.Step, original any) (value any, err error)
	Equal(a, b any) bool
}

// Registry maps action names to handlers.
type Registry struct {
	handlers map[string]Handler
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler)}
}

// Register adds or replaces a handler for an action name.
func (r *Registry) Register(action string, h Handler) {
	r.handlers[action] = h
}

// Get returns the handler for an action.
func (r *Registry) Get(action string) (Handler, error) {
	h, ok := r.handlers[action]
	if !ok {
		return nil, fmt.Errorf("no handler registered for action %q", action)
	}
	return h, nil
}

// FluxHandler implements flux.suspend and flux.resume.
type FluxHandler struct {
	Client flux.Client
}

func (h *FluxHandler) Validate(step config.Step) error {
	if step.Target.Kind != config.KindKustomization && step.Target.Kind != config.KindHelmRelease {
		return fmt.Errorf("flux handler does not support kind %q", step.Target.Kind)
	}
	return nil
}

func (h *FluxHandler) Observe(ctx context.Context, step config.Step) (any, bool, error) {
	info, err := h.Client.GetResource(ctx, step.Target.Kind, step.Target.Namespace, step.Target.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return info.Suspended, true, nil
}

func (h *FluxHandler) Capture(ctx context.Context, step config.Step) (any, error) {
	v, exists, err := h.Observe(ctx, step)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("target %s not found", step.Target.Key())
	}
	return v, nil
}

func (h *FluxHandler) Desired(step config.Step, direction string, original any) (any, error) {
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
		if def, ok := Bool(step.States.On["default"]); ok {
			return def, nil
		}
		return false, nil
	}
	if v, ok := Bool(step.States.Off["suspend"]); ok {
		return v, nil
	}
	return true, nil
}

func (h *FluxHandler) Apply(ctx context.Context, step config.Step, desired any) (any, error) {
	v, ok := Bool(desired)
	if !ok {
		return nil, fmt.Errorf("flux desired value is not a bool")
	}
	info, err := h.Client.PatchSuspend(ctx, step.Target.Kind, step.Target.Namespace, step.Target.Name, v)
	if err != nil {
		return nil, err
	}
	return info.Suspended, nil
}

func (h *FluxHandler) Restore(ctx context.Context, step config.Step, original any) (any, error) {
	return h.Apply(ctx, step, original)
}

func (h *FluxHandler) Equal(a, b any) bool {
	av, aok := Bool(a)
	bv, bok := Bool(b)
	return aok && bok && av == bv
}

// ScaleClient observes and changes workload replicas.
type ScaleClient interface {
	ObserveReplicas(ctx context.Context, kind, namespace, name string) (replicas int32, exists bool, err error)
	Scale(ctx context.Context, kind, namespace, name string, replicas int32) (int32, error)
}

// ScaleHandler implements kubernetes.scale.
type ScaleHandler struct {
	Client ScaleClient
}

func (h *ScaleHandler) Validate(step config.Step) error {
	if step.Target.Kind != config.KindDeployment && step.Target.Kind != config.KindStatefulSet {
		return fmt.Errorf("kubernetes.scale does not support kind %q", step.Target.Kind)
	}
	if h.Client == nil {
		return errors.New("kubernetes.scale client is not configured")
	}
	return nil
}

func (h *ScaleHandler) Observe(ctx context.Context, step config.Step) (any, bool, error) {
	replicas, exists, err := h.Client.ObserveReplicas(ctx, step.Target.Kind, step.Target.Namespace, step.Target.Name)
	if err != nil {
		return nil, false, err
	}
	if !exists {
		return nil, false, nil
	}
	return int(replicas), true, nil
}

func (h *ScaleHandler) Capture(ctx context.Context, step config.Step) (any, error) {
	v, exists, err := h.Observe(ctx, step)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("target %s not found", step.Target.Key())
	}
	return v, nil
}

func (h *ScaleHandler) Desired(step config.Step, direction string, original any) (any, error) {
	if direction == config.DirectionOn {
		if restore, _ := step.States.On.StringValue("restore"); restore == "previous" {
			if original == nil {
				if def, ok := Int(step.States.On["default"]); ok {
					return def, nil
				}
				return nil, ErrMissingOriginal
			}
			return original, nil
		}
		if v, ok := Int(step.States.On["replicas"]); ok {
			return v, nil
		}
		if def, ok := Int(step.States.On["default"]); ok {
			return def, nil
		}
		return nil, ErrMissingOriginal
	}
	if v, ok := Int(step.States.Off["replicas"]); ok {
		return v, nil
	}
	if def, ok := Int(step.States.Off["default"]); ok {
		return def, nil
	}
	return 0, nil
}

func (h *ScaleHandler) Apply(ctx context.Context, step config.Step, desired any) (any, error) {
	v, ok := Int(desired)
	if !ok {
		return nil, fmt.Errorf("scale desired value is not an int")
	}
	replicas, err := h.Client.Scale(ctx, step.Target.Kind, step.Target.Namespace, step.Target.Name, int32(v))
	if err != nil {
		return nil, err
	}
	return int(replicas), nil
}

func (h *ScaleHandler) Restore(ctx context.Context, step config.Step, original any) (any, error) {
	return h.Apply(ctx, step, original)
}

func (h *ScaleHandler) Equal(a, b any) bool {
	av, aok := Int(a)
	bv, bok := Int(b)
	return aok && bok && av == bv
}

// Bool converts JSON/YAML numeric or boolean values to bool.
func Bool(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case *bool:
		if t == nil {
			return false, false
		}
		return *t, true
	default:
		return false, false
	}
}

// Int converts JSON/YAML numeric values to int.
func Int(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int32:
		return int(t), true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	case *int:
		if t == nil {
			return 0, false
		}
		return *t, true
	default:
		return 0, false
	}
}
