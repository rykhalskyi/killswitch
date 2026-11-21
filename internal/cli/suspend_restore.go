package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/rykhalskyi/killswitch/internal/config"
	"github.com/rykhalskyi/killswitch/internal/engine"
)

// handleSwitch parses flags, initializes the engine, and runs an on/off transition.
func handleSwitch(args []string, dir engine.Direction) int {
	return handleSwitchWithDefault(args, dir, "")
}

func handleSwitchWithDefault(args []string, dir engine.Direction, defaultSwitch string) int {
	switchID, rest := extractSwitchID(args)
	fs := flag.NewFlagSet(string(dir), flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cf := registerCommonFlags(fs)
	dryRun := fs.Bool("dry-run", false, "Preview planned operations without executing")
	yes := fs.Bool("yes", false, "Skip confirmation prompt")
	force := fs.Bool("force", false, "Override conflict checks and proceed")
	if err := fs.Parse(rest); err != nil {
		return engine.ExitConfigOrConnection
	}

	logger := setupLogger(cf.logLevel)
	eng, cancel, code := initEngine(cf, logger)
	if code != engine.ExitSuccess {
		return code
	}
	defer cancel()
	defer eng.Close()

	if switchID == "" {
		switchID = defaultSwitch
	}
	if switchID == "" {
		switchID = eng.FluxSwitchID()
	}
	if switchID == "" {
		fmt.Fprintln(os.Stderr, "No switch specified and no Flux switch configured.")
		return engine.ExitConfigOrConnection
	}

	ctx, cancelTimeout := context.WithTimeout(context.Background(), cf.timeout)
	defer cancelTimeout()

	return runSwitchTransition(ctx, eng, switchID, dir, *dryRun, *yes, *force)
}

func handleSuspend(args []string) int {
	return handleSwitchWithDefault(args, engine.DirectionOff, "flux")
}

func handleRestore(args []string) int {
	return handleSwitchWithDefault(args, engine.DirectionOn, "flux")
}

func extractSwitchID(args []string) (string, []string) {
	valueFlags := map[string]bool{
		"config": true, "context": true, "kubeconfig": true, "timeout": true,
		"state-dir": true, "log-level": true, "limit": true, "switch": true,
	}
	var out []string
	var id string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			out = append(out, arg)
			name := strings.TrimLeft(arg, "-")
			if idx := strings.Index(name, "="); idx >= 0 {
				name = name[:idx]
			}
			if valueFlags[name] && !strings.Contains(arg, "=") && i+1 < len(args) {
				out = append(out, args[i+1])
				i++
			}
			continue
		}
		if id == "" {
			id = arg
			continue
		}
		out = append(out, arg)
	}
	return id, out
}

func runSwitchTransition(ctx context.Context, eng *engine.Engine, switchID string, dir engine.Direction, dryRun, yes, force bool) int {
	plan, err := eng.BuildSwitchPlan(ctx, switchID, dir)
	if err != nil {
		if errors.Is(err, engine.ErrUnsafeState) {
			fmt.Fprintf(os.Stderr, "Unsafe state: %v\n", err)
			return engine.ExitUnsafeOrLock
		}
		fmt.Fprintf(os.Stderr, "Error creating plan for %s %s: %v\n", switchID, dir, err)
		return engine.ExitConfigOrConnection
	}

	if len(plan.Missing) > 0 {
		fmt.Fprintf(os.Stderr, "Error: %d configured target(s) not found on cluster:\n", len(plan.Missing))
		for _, m := range plan.Missing {
			fmt.Fprintf(os.Stderr, "  - %s (%s/%s)\n", m.Resource.Key(), m.Switch, m.Step)
		}
		return engine.ExitConfigOrConnection
	}

	fmt.Printf("\n=== PLAN: %s %s (%d steps) ===\n\n", strings.ToUpper(switchID), strings.ToUpper(string(dir)), len(plan.Items))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ORDER\tSWITCH\tSTEP\tACTION\tTARGET\tCURRENT\tDESIRED\tCHANGE\tCONFLICT")
	for i, item := range plan.Items {
		changeStr := "no"
		if item.WillChange {
			changeStr = "yes"
		}
		confStr := "none"
		if item.Conflict {
			confStr = item.ConflictReason
		}
		cascade := ""
		if item.Cascade {
			cascade = " (cascade)"
		}
		fmt.Fprintf(w, "%d\t%s%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			i+1, item.Switch, cascade, item.Step, item.Action, item.Resource.Key(),
			formatValue(item.ObservedValue), formatValue(item.DesiredValue), changeStr, confStr)
	}
	_ = w.Flush()
	fmt.Println()

	if len(plan.Conflicts) > 0 && !force {
		fmt.Fprintf(os.Stderr, "Conflict detected: %d step(s) were modified outside killswitch.\n", len(plan.Conflicts))
		fmt.Fprintln(os.Stderr, "Run with --force to overwrite external modifications.")
		return engine.ExitConflict
	}

	if dryRun {
		fmt.Println("Dry-run requested. No changes made.")
		return engine.ExitSuccess
	}

	if !yes {
		fmt.Printf("Proceed with %s %s on %d steps? [y/N]: ", switchID, dir, len(plan.Items))
		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nInput error: %v\n", err)
			return engine.ExitConfigOrConnection
		}
		trimmed := strings.ToLower(strings.TrimSpace(input))
		if trimmed != "y" && trimmed != "yes" {
			fmt.Println("Operation canceled by user.")
			return engine.ExitSuccess
		}
	}

	fmt.Printf("\nExecuting %s %s...\n", switchID, dir)
	progress := func(step int, total int, res config.Resource, status string, execErr error) {
		if execErr != nil {
			fmt.Printf("[%d/%d] %s ... FAILED: %v\n", step, total, res.Key(), execErr)
		} else {
			fmt.Printf("[%d/%d] %s ... OK\n", step, total, res.Key())
		}
	}

	report, err := eng.ExecuteSwitch(ctx, switchID, dir, force, progress)
	if err != nil {
		if errors.Is(err, engine.ErrConflict) {
			fmt.Fprintf(os.Stderr, "\nConflict error: %v\n", err)
			return engine.ExitConflict
		}
		if errors.Is(err, engine.ErrLockContention) || errors.Is(err, engine.ErrUnsafeState) {
			fmt.Fprintf(os.Stderr, "\nState error: %v\n", err)
			return engine.ExitUnsafeOrLock
		}
		if errors.Is(err, engine.ErrPartialFailure) {
			printReportSummary(report)
			return engine.ExitPartialFailure
		}
		fmt.Fprintf(os.Stderr, "\nExecution error: %v\n", err)
		return engine.ExitConfigOrConnection
	}

	printReportSummary(report)
	return engine.ExitSuccess
}

func formatValue(v any) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%v", v)
}

func printReportSummary(report *engine.ExecutionReport) {
	if report == nil {
		return
	}
	fmt.Printf("\n=== OPERATION SUMMARY: %s ===\n", strings.ToUpper(report.Status))
	fmt.Printf("Operation ID:  %s\n", report.Op)
	fmt.Printf("Switch:        %s\n", report.Switch)
	fmt.Printf("Direction:     %s\n", report.Direction)
	fmt.Printf("Total:         %d\n", report.Total)
	fmt.Printf("Succeeded:     %d\n", report.OK)
	fmt.Printf("Failed:        %d\n", report.Failed)
	fmt.Printf("Switch State:  %s\n", report.SwitchState)
	fmt.Printf("Global State:  %s\n\n", report.GlobalStatus)

	if report.Failed > 0 {
		fmt.Fprintln(os.Stderr, "Failures:")
		for _, res := range report.Results {
			if res.Err != nil {
				fmt.Fprintf(os.Stderr, "  - %s (%s): %v\n", res.Resource.Key(), res.Step, res.Err)
			}
		}
		fmt.Fprintln(os.Stderr)
	}
}
