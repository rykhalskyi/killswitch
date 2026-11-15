package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "valid config",
			yaml: `
flux:
  resources:
    - kind: Kustomization
      namespace: flux-system
      name: apps
    - kind: HelmRelease
      namespace: monitoring
      name: kube-prometheus-stack
`,
			wantErr: false,
		},
		{
			name: "empty resources",
			yaml: `
flux:
  resources: []
`,
			wantErr: true,
		},
		{
			name: "unknown kind",
			yaml: `
flux:
  resources:
    - kind: GitRepository
      namespace: flux-system
      name: my-repo
`,
			wantErr: true,
		},
		{
			name: "empty namespace",
			yaml: `
flux:
  resources:
    - kind: Kustomization
      namespace: ""
      name: apps
`,
			wantErr: true,
		},
		{
			name: "empty name",
			yaml: `
flux:
  resources:
    - kind: Kustomization
      namespace: flux-system
      name: ""
`,
			wantErr: true,
		},
		{
			name: "duplicate resource",
			yaml: `
flux:
  resources:
    - kind: Kustomization
      namespace: flux-system
      name: apps
    - kind: Kustomization
      namespace: flux-system
      name: apps
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(cfgPath, []byte(tt.yaml), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			cfg, err := Load(cfgPath)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Load() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && cfg == nil {
				t.Fatal("expected non-nil config on success")
			}
		})
	}
}
