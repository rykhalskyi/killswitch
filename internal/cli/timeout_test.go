package cli

import (
	"testing"
	"time"

	"github.com/rykhalskyi/killswitch/internal/actions"
	"github.com/rykhalskyi/killswitch/internal/config"
)

func TestSwitchTimeoutExtendsForOneWayWorkflows(t *testing.T) {
	node := func(id, name string) config.Step {
		return config.Step{
			ID:     id,
			Action: config.ActionTalosShutdown,
			Target: config.Resource{Kind: config.KindNode, Name: name},
		}
	}
	cfg := &config.Config{
		Switches: []config.Switch{
			{ID: "cluster-shutdown", Name: "Cluster Shutdown", Workflow: "cluster-shutdown"},
			{ID: "media", Name: "Media", Workflow: "media"},
		},
		Workflows: map[string]*config.Workflow{
			"cluster-shutdown": {
				ID:     "cluster-shutdown",
				OneWay: true,
				Steps: []config.Step{
					node("01", "192.168.2.235"),
					node("02", "192.168.2.234"),
					node("03", "192.168.2.233"),
				},
			},
			"media": {ID: "media", Steps: []config.Step{node("01", "192.168.2.235")}},
		},
	}

	base := 60 * time.Second
	perStep := actions.DefaultShutdownCommandTimeout + actions.DefaultNodeDownTimeout
	want := 3*perStep + 30*time.Second

	if got := switchTimeout(cfg, "cluster-shutdown", base, false); got != want {
		t.Fatalf("switchTimeout = %s, want %s", got, want)
	}
	if got := switchTimeout(cfg, "cluster-shutdown", base, true); got != base {
		t.Fatalf("explicit timeout = %s, want %s", got, base)
	}
	if got := switchTimeout(cfg, "media", base, false); got != base {
		t.Fatalf("toggle timeout = %s, want %s", got, base)
	}
	if got := switchTimeout(nil, "cluster-shutdown", base, false); got != base {
		t.Fatalf("nil config timeout = %s, want %s", got, base)
	}
}

func TestSwitchTimeoutUsesWorkflowTimeouts(t *testing.T) {
	cfg := &config.Config{
		Switches: []config.Switch{
			{ID: "cluster-shutdown", Name: "Cluster Shutdown", Workflow: "cluster-shutdown"},
		},
		Workflows: map[string]*config.Workflow{
			"cluster-shutdown": {
				ID:      "cluster-shutdown",
				OneWay:  true,
				Timeout: config.Duration(6 * time.Minute),
				Steps: []config.Step{
					{ID: "01", Action: config.ActionTalosShutdown, Target: config.Resource{Kind: config.KindNode, Name: "192.168.2.235"}},
					{ID: "02", Action: config.ActionTalosShutdown, Target: config.Resource{Kind: config.KindNode, Name: "192.168.2.234"}},
					{ID: "03", Action: config.ActionTalosShutdown, Target: config.Resource{Kind: config.KindNode, Name: "192.168.2.233"}},
				},
			},
		},
	}

	want := 3*(actions.DefaultShutdownCommandTimeout+6*time.Minute) + 30*time.Second
	if got := switchTimeout(cfg, "cluster-shutdown", 60*time.Second, false); got != want {
		t.Fatalf("switchTimeout = %s, want %s", got, want)
	}
}
