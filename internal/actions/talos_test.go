package actions

import (
	"context"
	"errors"
	"testing"

	"github.com/rykhalskyi/killswitch/internal/config"
)

type fakeTalosClient struct {
	up    map[string]bool
	order []string
}

func (f *fakeTalosClient) NodeUp(_ context.Context, node string) (bool, error) {
	return f.up[node], nil
}

func (f *fakeTalosClient) Shutdown(_ context.Context, node string) error {
	f.order = append(f.order, node)
	f.up[node] = false
	return nil
}

func nodeStep(name string) config.Step {
	return config.Step{
		ID:     "shutdown",
		Action: config.ActionTalosShutdown,
		Target: config.Resource{Kind: config.KindNode, Name: name},
	}
}

func TestTalosShutdownObserveAndApply(t *testing.T) {
	client := &fakeTalosClient{up: map[string]bool{"192.168.2.235": true}}
	h := &TalosShutdownHandler{Client: client}
	ctx := context.Background()
	step := nodeStep("192.168.2.235")

	if err := h.Validate(step); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	observed, exists, err := h.Observe(ctx, step)
	if err != nil || !exists || observed != "up" {
		t.Fatalf("Observe = %v, %v, %v; want up, true, nil", observed, exists, err)
	}
	if _, err := h.Desired(step, config.DirectionOff, nil); err != nil {
		t.Fatalf("Desired off failed: %v", err)
	}
	value, err := h.Apply(ctx, step, "down")
	if err != nil || value != "down" {
		t.Fatalf("Apply = %v, %v; want down, nil", value, err)
	}
	observed, _, _ = h.Observe(ctx, step)
	if observed != "down" {
		t.Fatalf("Observe after apply = %v, want down", observed)
	}
}

func TestTalosShutdownIsIrreversible(t *testing.T) {
	h := &TalosShutdownHandler{Client: &fakeTalosClient{up: map[string]bool{}}}
	step := nodeStep("192.168.2.235")

	if _, err := h.Desired(step, config.DirectionOn, nil); !errors.Is(err, ErrIrreversible) {
		t.Fatalf("Desired on = %v, want ErrIrreversible", err)
	}
	if _, err := h.Restore(context.Background(), step, "up"); !errors.Is(err, ErrIrreversible) {
		t.Fatalf("Restore = %v, want ErrIrreversible", err)
	}
}

func TestTalosShutdownRejectsNonNodeTarget(t *testing.T) {
	h := &TalosShutdownHandler{Client: &fakeTalosClient{up: map[string]bool{}}}
	step := nodeStep("192.168.2.235")
	step.Target.Kind = config.KindDeployment

	if err := h.Validate(step); err == nil {
		t.Fatal("expected kind validation error")
	}
}
