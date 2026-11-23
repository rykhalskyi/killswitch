package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rykhalskyi/killswitch/internal/actions"
	"github.com/rykhalskyi/killswitch/internal/config"
)

type recordingTalosHandler struct {
	up    map[string]bool
	order []string
}

func (f *recordingTalosHandler) Validate(config.Step) error { return nil }

func (f *recordingTalosHandler) Observe(_ context.Context, step config.Step) (any, bool, error) {
	if f.up[step.Target.Name] {
		return "up", true, nil
	}
	return "down", true, nil
}

func (f *recordingTalosHandler) Capture(ctx context.Context, step config.Step) (any, error) {
	v, _, err := f.Observe(ctx, step)
	return v, err
}

func (f *recordingTalosHandler) Desired(_ config.Step, direction string, _ any) (any, error) {
	if direction == config.DirectionOn {
		return nil, actions.ErrIrreversible
	}
	return "down", nil
}

func (f *recordingTalosHandler) Apply(_ context.Context, step config.Step, _ any) (any, error) {
	f.order = append(f.order, step.Target.Name)
	f.up[step.Target.Name] = false
	return "down", nil
}

func (f *recordingTalosHandler) Restore(context.Context, config.Step, any) (any, error) {
	return nil, actions.ErrIrreversible
}

func (f *recordingTalosHandler) Equal(a, b any) bool {
	as, aok := a.(string)
	bs, bok := b.(string)
	return aok && bok && as == bs
}

func oneWayConfig() *config.Config {
	node := func(id, name string) config.Step {
		return config.Step{
			ID:     id,
			Action: config.ActionTalosShutdown,
			Target: config.Resource{Kind: config.KindNode, Name: name},
		}
	}
	return &config.Config{
		Switches: []config.Switch{
			{ID: "cluster-shutdown", Name: "Cluster Shutdown", Workflow: "cluster-shutdown"},
		},
		Workflows: map[string]*config.Workflow{
			"cluster-shutdown": {
				ID:     "cluster-shutdown",
				Name:   "Cluster shutdown",
				OneWay: true,
				Steps: []config.Step{
					node("01-235", "192.168.2.235"),
					node("02-234", "192.168.2.234"),
					node("03-233", "192.168.2.233"),
				},
			},
		},
	}
}

func TestOneWayRunsInDeclaredOrder(t *testing.T) {
	handler := &recordingTalosHandler{up: map[string]bool{
		"192.168.2.235": true,
		"192.168.2.234": true,
		"192.168.2.233": true,
	}}
	eng := New(oneWayConfig(), nil, t.TempDir(), "test", nil)
	eng.SetTalosHandler(handler)
	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	if _, err := eng.ExecuteSwitch(ctx, "cluster-shutdown", DirectionOff, false, nil); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	want := []string{"192.168.2.235", "192.168.2.234", "192.168.2.233"}
	if fmt.Sprint(handler.order) != fmt.Sprint(want) {
		t.Fatalf("shutdown order = %v, want %v", handler.order, want)
	}
}

func TestOneWayRejectsOn(t *testing.T) {
	eng := New(oneWayConfig(), nil, t.TempDir(), "test", nil)
	eng.SetTalosHandler(&recordingTalosHandler{up: map[string]bool{}})
	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	if _, err := eng.BuildSwitchPlan(ctx, "cluster-shutdown", DirectionOn); err == nil {
		t.Fatal("expected one-way switch to reject ON")
	}
}

func TestConfigLoadsOneWayNodeWorkflow(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "workflows"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "switches.yaml"), []byte(`
switches:
  - id: cluster-shutdown
    name: Cluster Shutdown
    workflow: cluster-shutdown
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workflows", "cluster-shutdown.yaml"), []byte(`
id: cluster-shutdown
one_way: true
steps:
  - id: 01-235
    action: talos.shutdown
    target: {kind: Node, name: "192.168.2.235"}
`), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !cfg.Workflows["cluster-shutdown"].OneWay {
		t.Fatal("expected one_way to be loaded")
	}
}

func TestOneWayPlanCarriesStepTimeout(t *testing.T) {
	cfg := oneWayConfig()
	wf := cfg.Workflows["cluster-shutdown"]
	wf.Timeout = config.Duration(6 * time.Minute)
	wf.Steps[2].Timeout = config.Duration(9 * time.Minute)

	handler := &recordingTalosHandler{up: map[string]bool{
		"192.168.2.235": true,
		"192.168.2.234": true,
		"192.168.2.233": true,
	}}
	eng := New(cfg, nil, t.TempDir(), "test", nil)
	eng.SetTalosHandler(handler)
	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	plan, err := eng.BuildSwitchPlan(ctx, "cluster-shutdown", DirectionOff)
	if err != nil {
		t.Fatalf("BuildSwitchPlan failed: %v", err)
	}
	want := []time.Duration{6 * time.Minute, 6 * time.Minute, 9 * time.Minute}
	for i, item := range plan.Items {
		if got := item.Timeout.Duration(); got != want[i] {
			t.Fatalf("item %d timeout = %s, want %s", i, got, want[i])
		}
	}
}
