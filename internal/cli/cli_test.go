package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rykhalskyi/killswitch/internal/engine"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantCmd      string
		wantArgsLen  int
		expectedArgs []string
	}{
		{
			name:        "subcommand only",
			args:        []string{"killswitch", "status"},
			wantCmd:     "status",
			wantArgsLen: 0,
		},
		{
			name:         "subcommand with flags after",
			args:         []string{"killswitch", "status", "--json"},
			wantCmd:      "status",
			wantArgsLen:  1,
			expectedArgs: []string{"--json"},
		},
		{
			name:         "flags before subcommand",
			args:         []string{"killswitch", "--config", "my-config.yaml", "status", "--json"},
			wantCmd:      "status",
			wantArgsLen:  3,
			expectedArgs: []string{"--config", "my-config.yaml", "--json"},
		},
		{
			name:        "help flag",
			args:        []string{"killswitch", "--help"},
			wantCmd:     "help",
			wantArgsLen: 0,
		},
		{
			name:        "short help flag",
			args:        []string{"killswitch", "-h"},
			wantCmd:     "help",
			wantArgsLen: 0,
		},
		{
			name:        "unknown command",
			args:        []string{"killswitch", "nonexistent"},
			wantCmd:     "nonexistent",
			wantArgsLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, cmdArgs := parseCommand(tt.args)
			if cmd != tt.wantCmd {
				t.Errorf("parseCommand() cmd = %q, want %q", cmd, tt.wantCmd)
			}
			if len(cmdArgs) != tt.wantArgsLen {
				t.Errorf("parseCommand() len(cmdArgs) = %d, want %d", len(cmdArgs), tt.wantArgsLen)
			}
			if tt.expectedArgs != nil {
				for i, arg := range tt.expectedArgs {
					if i >= len(cmdArgs) || cmdArgs[i] != arg {
						t.Errorf("expected arg[%d] = %q, got %q", i, arg, cmdArgs[i])
					}
				}
			}
		})
	}
}

func TestMainExitCodes(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
	}{
		{
			name:     "no arguments",
			args:     []string{"killswitch"},
			wantCode: engine.ExitConfigOrConnection,
		},
		{
			name:     "help flag",
			args:     []string{"killswitch", "--help"},
			wantCode: engine.ExitSuccess,
		},
		{
			name:     "short help flag",
			args:     []string{"killswitch", "-h"},
			wantCode: engine.ExitSuccess,
		},
		{
			name:     "help subcommand",
			args:     []string{"killswitch", "help"},
			wantCode: engine.ExitSuccess,
		},
		{
			name:     "unknown command",
			args:     []string{"killswitch", "unknown-command"},
			wantCode: engine.ExitConfigOrConnection,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := Main(tt.args)
			if code != tt.wantCode {
				t.Errorf("Main(%v) = %d, want %d", tt.args, code, tt.wantCode)
			}
		})
	}
}

func TestFindConfigFile(t *testing.T) {
	// Explicit non-existent file
	_, err := findConfigFile("/path/to/definitely/nonexistent.yaml")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}

	// Explicit existing file
	tmpDir := t.TempDir()
	existingPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(existingPath, []byte("flux: {}\n"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	found, err := findConfigFile(existingPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != existingPath {
		t.Errorf("expected %s, got %s", existingPath, found)
	}
}

func TestSetupLogger(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error", "unknown"}
	for _, lvl := range levels {
		logger := setupLogger(lvl)
		if logger == nil {
			t.Errorf("expected non-nil logger for level %q", lvl)
		}
	}
}

func TestFormatGlobalStatus(t *testing.T) {
	if s := formatGlobalStatus(engine.GlobalStatusSuspended); s == "" {
		t.Error("expected non-empty string for GlobalStatusSuspended")
	}
	if s := formatGlobalStatus(engine.GlobalStatusRestored); s == "" {
		t.Error("expected non-empty string for GlobalStatusRestored")
	}
	if s := formatGlobalStatus(engine.GlobalStatusPartial); s == "" {
		t.Error("expected non-empty string for GlobalStatusPartial")
	}
	if s := formatGlobalStatus(engine.GlobalStatusUnknown); s == "" {
		t.Error("expected non-empty string for GlobalStatusUnknown")
	}
}
