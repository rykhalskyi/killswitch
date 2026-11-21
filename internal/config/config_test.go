package config

import (
	"os"
	"path/filepath"
	"testing"
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
