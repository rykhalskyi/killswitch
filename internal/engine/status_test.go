package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/rykhalskyi/killswitch/internal/config"
)

type erroringTalosHandler struct {
	*recordingTalosHandler
	fail map[string]bool
}

func (f *erroringTalosHandler) Observe(ctx context.Context, step config.Step) (any, bool, error) {
	if f.fail[step.Target.Name] {
		return nil, false, errors.New("probe failed")
	}
	return f.recordingTalosHandler.Observe(ctx, step)
}

func TestStatusToleratesPerSwitchProbeErrors(t *testing.T) {
	node := func(id, name string) config.Step {
		return config.Step{
			ID:     id,
			Action: config.ActionTalosShutdown,
			Target: config.Resource{Kind: config.KindNode, Name: name},
		}
	}
	cfg := &config.Config{
		Switches: []config.Switch{
			{ID: "bad", Name: "Bad switch", Workflow: "bad"},
			{ID: "good", Name: "Good switch", Workflow: "good"},
		},
		Workflows: map[string]*config.Workflow{
			"bad": {
				ID:     "bad",
				Name:   "Bad",
				OneWay: true,
				Steps:  []config.Step{node("01", "10.0.0.1")},
			},
			"good": {
				ID:     "good",
				Name:   "Good",
				OneWay: true,
				Steps:  []config.Step{node("01", "10.0.0.2")},
			},
		},
	}

	handler := &erroringTalosHandler{
		recordingTalosHandler: &recordingTalosHandler{up: map[string]bool{"10.0.0.2": true}},
		fail:                  map[string]bool{"10.0.0.1": true},
	}
	eng := New(cfg, nil, t.TempDir(), "test", nil)
	eng.SetTalosHandler(handler)
	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	report, err := eng.Status(ctx)
	if err != nil {
		t.Fatalf("Status should tolerate one switch's probe error: %v", err)
	}
	if len(report.Switches) != 2 {
		t.Fatalf("got %d switches, want 2", len(report.Switches))
	}
	bad := report.Switches[0]
	if bad.ID != "bad" || bad.State != SwitchUnknown || bad.Error == "" {
		t.Fatalf("bad switch = %+v, want UNKNOWN with an error", bad)
	}
	good := report.Switches[1]
	if good.ID != "good" || good.State != SwitchOn || good.Error != "" {
		t.Fatalf("good switch = %+v, want ON with no error", good)
	}
}
