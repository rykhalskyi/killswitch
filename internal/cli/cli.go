package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rykhalskyi/killswitch/internal/config"
	"github.com/rykhalskyi/killswitch/internal/engine"
	"github.com/rykhalskyi/killswitch/internal/flux"
	"github.com/rykhalskyi/killswitch/internal/tui"
)

// Main is the entry point for the killswitch CLI application.
func Main(args []string) int {
	if len(args) < 2 {
		printUsage()
		return engine.ExitConfigOrConnection
	}

	cmd, cmdArgs := parseCommand(args)

	switch cmd {
	case "status":
		return handleStatus(cmdArgs)
	case "suspend":
		return handleSuspend(cmdArgs)
	case "restore":
		return handleRestore(cmdArgs)
	case "history":
		return handleHistory(cmdArgs)
	case "tui":
		return handleTUI(cmdArgs)
	case "help", "-h", "--help":
		printUsage()
		return engine.ExitSuccess
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %q\n\n", cmd)
		printUsage()
		return engine.ExitConfigOrConnection
	}
}

func parseCommand(args []string) (string, []string) {
	known := map[string]bool{
		"status":  true,
		"suspend": true,
		"restore": true,
		"history": true,
		"tui":     true,
		"help":    true,
	}

	cmdIdx := -1
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if (arg == "-h" || arg == "--help" || arg == "help") && cmdIdx == -1 {
			// Check if this is a help request without a preceding subcommand
			hasSubcommand := false
			for j := 1; j < len(args); j++ {
				if j != i && known[args[j]] && args[j] != "help" {
					hasSubcommand = true
					break
				}
			}
			if !hasSubcommand {
				return "help", nil
			}
		}
		if known[arg] && arg != "help" {
			cmdIdx = i
			break
		}
	}

	if cmdIdx == -1 {
		if len(args) > 1 {
			return args[1], args[2:]
		}
		return "", nil
	}

	cmd := args[cmdIdx]
	var cmdArgs []string
	cmdArgs = append(cmdArgs, args[1:cmdIdx]...)
	cmdArgs = append(cmdArgs, args[cmdIdx+1:]...)
	return cmd, cmdArgs
}

func printUsage() {
	fmt.Printf(`killswitch — Reversible Flux reconciliation freeze controller

Usage:
  killswitch <command> [flags]

Commands:
  status   Show current Flux reconciliation status on cluster
  suspend  Freeze all configured Flux Kustomizations and HelmReleases
  restore  Unfreeze and restore resources to their recorded states
  history  Show journaled operation history
  tui      Interactive terminal interface

Global Flags:
  --config <path>       Path to config YAML (default: config.yaml)
  --context <name>      Kubernetes context name
  --kubeconfig <path>   Path to kubeconfig file
  --timeout <duration>  Timeout for cluster operations (default: 60s)
  --state-dir <path>    Directory for journal and state snapshots
  --log-level <level>   Log level: debug, info, warn, error (default: info)

Examples:
  killswitch status
  killswitch suspend --dry-run
  killswitch suspend --yes
  killswitch restore --force
  killswitch history --limit 5
  killswitch tui
`)
}

type commonFlags struct {
	configPath string
	context    string
	kubeconfig string
	timeout    time.Duration
	stateDir   string
	logLevel   string
}

func registerCommonFlags(fs *flag.FlagSet) *commonFlags {
	cf := &commonFlags{}
	fs.StringVar(&cf.configPath, "config", "", "Path to YAML config file")
	fs.StringVar(&cf.context, "context", "", "Kubernetes context to use")
	fs.StringVar(&cf.kubeconfig, "kubeconfig", "", "Path to kubeconfig file")
	fs.DurationVar(&cf.timeout, "timeout", 60*time.Second, "Operation timeout")
	fs.StringVar(&cf.stateDir, "state-dir", "", "State directory")
	fs.StringVar(&cf.logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	return cf
}

func setupLogger(levelStr string) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(levelStr) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	return slog.New(handler)
}

func findConfigFile(userPath string) (string, error) {
	if userPath != "" {
		if _, err := os.Stat(userPath); err != nil {
			return "", fmt.Errorf("config file not found at %s: %w", userPath, err)
		}
		return userPath, nil
	}

	candidates := []string{
		"config.yaml",
		"config.yml",
		"config.example.yaml",
		filepath.Join(os.Getenv("HOME"), ".config", "killswitch", "config.yaml"),
	}

	for _, cand := range candidates {
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
	}

	return "", errors.New("no config file found. Please provide --config <path> or create config.yaml")
}

func initEngine(cf *commonFlags, logger *slog.Logger) (*engine.Engine, context.CancelFunc, int) {
	cfgFile, err := findConfigFile(cf.configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return nil, nil, engine.ExitConfigOrConnection
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return nil, nil, engine.ExitConfigOrConnection
	}

	client, activeCtx, err := flux.NewClient(cf.kubeconfig, cf.context)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Kubernetes client error: %v\n", err)
		return nil, nil, engine.ExitConfigOrConnection
	}

	eng := engine.New(cfg, client, cf.stateDir, activeCtx, logger)
	ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)

	if err := eng.Init(ctx); err != nil {
		cancel()
		if errors.Is(err, engine.ErrLockContention) {
			fmt.Fprintf(os.Stderr, "Lock contention: %v\n", err)
			return nil, nil, engine.ExitUnsafeOrLock
		}
		fmt.Fprintf(os.Stderr, "Initialization error: %v\n", err)
		return nil, nil, engine.ExitConfigOrConnection
	}

	return eng, cancel, engine.ExitSuccess
}

func handleStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cf := registerCommonFlags(fs)
	jsonFlag := fs.Bool("json", false, "Output in JSON format")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Flag error: %v\n", err)
		return engine.ExitConfigOrConnection
	}

	logger := setupLogger(cf.logLevel)
	eng, cancel, code := initEngine(cf, logger)
	if code != engine.ExitSuccess {
		return code
	}
	defer cancel()
	defer eng.Close()

	ctx, cancelTimeout := context.WithTimeout(context.Background(), cf.timeout)
	defer cancelTimeout()

	return runStatus(ctx, eng, *jsonFlag)
}

func handleSuspend(args []string) int {
	fs := flag.NewFlagSet("suspend", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cf := registerCommonFlags(fs)
	dryRun := fs.Bool("dry-run", false, "Preview planned operations without executing")
	yes := fs.Bool("yes", false, "Skip confirmation prompt")
	force := fs.Bool("force", false, "Override conflict checks and proceed")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Flag error: %v\n", err)
		return engine.ExitConfigOrConnection
	}

	logger := setupLogger(cf.logLevel)
	eng, cancel, code := initEngine(cf, logger)
	if code != engine.ExitSuccess {
		return code
	}
	defer cancel()
	defer eng.Close()

	ctx, cancelTimeout := context.WithTimeout(context.Background(), cf.timeout)
	defer cancelTimeout()

	return runAction(ctx, eng, engine.ActionSuspend, *dryRun, *yes, *force)
}

func handleRestore(args []string) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cf := registerCommonFlags(fs)
	dryRun := fs.Bool("dry-run", false, "Preview planned operations without executing")
	yes := fs.Bool("yes", false, "Skip confirmation prompt")
	force := fs.Bool("force", false, "Override conflict checks and proceed")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Flag error: %v\n", err)
		return engine.ExitConfigOrConnection
	}

	logger := setupLogger(cf.logLevel)
	eng, cancel, code := initEngine(cf, logger)
	if code != engine.ExitSuccess {
		return code
	}
	defer cancel()
	defer eng.Close()

	ctx, cancelTimeout := context.WithTimeout(context.Background(), cf.timeout)
	defer cancelTimeout()

	return runAction(ctx, eng, engine.ActionRestore, *dryRun, *yes, *force)
}

func handleHistory(args []string) int {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cf := registerCommonFlags(fs)
	limit := fs.Int("limit", 20, "Maximum number of history entries to show")
	jsonFlag := fs.Bool("json", false, "Output in JSON format")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Flag error: %v\n", err)
		return engine.ExitConfigOrConnection
	}

	logger := setupLogger(cf.logLevel)
	eng, cancel, code := initEngine(cf, logger)
	if code != engine.ExitSuccess {
		return code
	}
	defer cancel()
	defer eng.Close()

	return runHistory(eng, *limit, *jsonFlag)
}

func handleTUI(args []string) int {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cf := registerCommonFlags(fs)

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Flag error: %v\n", err)
		return engine.ExitConfigOrConnection
	}

	logger := setupLogger(cf.logLevel)
	eng, cancel, code := initEngine(cf, logger)
	if code != engine.ExitSuccess {
		return code
	}
	defer cancel()
	defer eng.Close()

	if err := tui.Run(eng); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		return engine.ExitConfigOrConnection
	}

	return engine.ExitSuccess
}
