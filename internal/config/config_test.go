package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLayered(t *testing.T, switches, workflow string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "workflows"), 0755); err != nil {
		t.Fatalf("creating workflow dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "switches.yaml"), []byte(switches), 0644); err != nil {
		t.Fatalf("writing switches: %v", err)
	}
	if workflow != "" {
		if err := os.WriteFile(filepath.Join(dir, "workflows", "flux.yaml"), []byte(workflow), 0644); err != nil {
			t.Fatalf("writing workflow: %v", err)
		}
	}
	return dir
}

func TestLayeredConfigValidation(t *testing.T) {
	validSwitches := `
switches:
  - id: flux
    name: Flux Reconciliation
    description: Suspend Flux
    workflow: flux
`
	validWorkflow := `
id: flux
steps:
  - id: apps
    action: flux.suspend
    target: {kind: Kustomization, namespace: flux-system, name: apps}
    states:
      off: {suspend: true}
      on: {restore: previous}
`
	tests := []struct {
		name     string
		switches string
		workflow string
		wantErr  bool
	}{
		{"valid", validSwitches, validWorkflow, false},
		{
			"missing workflow file",
			validSwitches,
			"",
			true,
		},
		{
			"unknown workflow reference",
			`
switches:
  - id: media
    name: Media
    workflow: missing
`,
			validWorkflow,
			true,
		},
		{
			"unsupported action",
			validSwitches,
			`
id: flux
steps:
  - id: apps
    action: script.run
    target: {kind: Kustomization, namespace: flux-system, name: apps}
`,
			true,
		},
		{
			"scale requires workload kind",
			validSwitches,
			`
id: flux
steps:
  - id: apps
    action: kubernetes.scale
    target: {kind: Kustomization, namespace: flux-system, name: apps}
`,
			true,
		},
		{
			"invalid requires flux",
			validSwitches,
			`
id: flux
requires: {flux: bananas}
steps:
  - id: apps
    action: flux.suspend
    target: {kind: Kustomization, namespace: flux-system, name: apps}
`,
			true,
		},
		{
			"duplicate step ids",
			validSwitches,
			`
id: flux
steps:
  - id: apps
    action: flux.suspend
    target: {kind: Kustomization, namespace: flux-system, name: apps}
  - id: apps
    action: flux.suspend
    target: {kind: Kustomization, namespace: flux-system, name: other}
`,
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeLayered(t, tt.switches, tt.workflow)
			cfg, err := Load(dir)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Load() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if cfg == nil || len(cfg.Switches) != 1 || cfg.Workflows["flux"] == nil {
					t.Fatalf("expected normalized config, got %+v", cfg)
				}
			}
		})
	}
}

func TestLegacyOnlyWorkflowSynthesized(t *testing.T) {
	cfg := &Config{Flux: FluxConfig{Resources: []Resource{
		{Kind: KindKustomization, Namespace: "flux-system", Name: "apps"},
		{Kind: KindHelmRelease, Namespace: "media", Name: "jellyfin"},
	}}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("legacy validate: %v", err)
	}
	cfg.Normalize()
	if cfg.FluxSwitchID() != "flux" {
		t.Fatalf("expected flux switch, got %q", cfg.FluxSwitchID())
	}
	wf := cfg.WorkflowForSwitch("flux")
	if wf == nil || len(wf.Steps) != 2 {
		t.Fatalf("expected two synthesized steps, got %+v", wf)
	}
}

func TestParseStateValues(t *testing.T) {
	values := StateValues{"suspend": true, "replicas": 3, "restore": "previous"}
	if v, ok := values.BoolValue("suspend"); !ok || !v {
		t.Fatalf("bool value = %v %v", v, ok)
	}
	if v, ok := values.IntValue("replicas"); !ok || v != 3 {
		t.Fatalf("int value = %v %v", v, ok)
	}
	if v, ok := values.StringValue("restore"); !ok || v != "previous" {
		t.Fatalf("string value = %v %v", v, ok)
	}
}

func TestWorkflowAndStepTimeout(t *testing.T) {
	dir := writeLayered(t, `
switches:
  - id: cluster-shutdown
    name: Cluster Shutdown
    workflow: cluster-shutdown
`, `
id: cluster-shutdown
one_way: true
timeout: 6m
steps:
  - id: 01-235
    action: talos.shutdown
    target: {kind: Node, name: "192.168.2.235"}
  - id: 02-234
    action: talos.shutdown
    target: {kind: Node, name: "192.168.2.234"}
    timeout: 8m
`)
	// The helper writes the workflow as flux.yaml; rename it for this workflow id.
	if err := os.Rename(filepath.Join(dir, "workflows", "flux.yaml"), filepath.Join(dir, "workflows", "cluster-shutdown.yaml")); err != nil {
		t.Fatalf("renaming workflow: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	wf := cfg.Workflows["cluster-shutdown"]
	if got := wf.Timeout.Duration(); got != 6*time.Minute {
		t.Fatalf("workflow timeout = %s, want 6m", got)
	}
	if got := wf.StepTimeout(wf.Steps[0]); got != 6*time.Minute {
		t.Fatalf("step 1 timeout = %s, want 6m", got)
	}
	if got := wf.StepTimeout(wf.Steps[1]); got != 8*time.Minute {
		t.Fatalf("step 2 timeout = %s, want 8m", got)
	}
}
