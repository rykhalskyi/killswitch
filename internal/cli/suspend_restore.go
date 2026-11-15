package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/rykhalskyi/killswitch/internal/config"
	"github.com/rykhalskyi/killswitch/internal/engine"
)

func runAction(ctx context.Context, eng *engine.Engine, action engine.Action, dryRun, yes, force bool) int {
	plan, err := eng.BuildPlan(ctx, action)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating plan for %s: %v\n", action, err)
		return engine.ExitConfigOrConnection
	}

	// Missing targets check
	if len(plan.Missing) > 0 {
		fmt.Fprintf(os.Stderr, "Error: %d configured resource(s) not found on cluster:\n", len(plan.Missing))
		for _, m := range plan.Missing {
			fmt.Fprintf(os.Stderr, "  - %s\n", m.Resource.Key())
		}
		return engine.ExitConfigOrConnection
	}

	// Print execution plan
	fmt.Printf("\n=== PLAN: %s (%d resources) ===\n\n", strings.ToUpper(string(action)), len(plan.Items))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ORDER\tKIND\tNAMESPACE\tNAME\tCURRENT\tTARGET\tWILL CHANGE\tCONFLICT")
	for i, item := range plan.Items {
		currStr := "ACTIVE"
		if item.CurrentSuspended {
			currStr = "SUSPENDED"
		}
		targetStr := "ACTIVE"
		if item.TargetSuspended {
			targetStr = "SUSPENDED"
		}
		changeStr := "no"
		if item.WillChange {
			changeStr = "yes"
		}
		confStr := "none"
		if item.Conflict {
			confStr = item.ConflictReason
		}

		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			i+1, item.Resource.Kind, item.Resource.Namespace, item.Resource.Name, currStr, targetStr, changeStr, confStr)
	}
	_ = w.Flush()
	fmt.Println()

	// Conflict check
	if len(plan.Conflicts) > 0 && !force {
		fmt.Fprintf(os.Stderr, "Conflict detected: %d resource(s) were modified outside killswitch.\n", len(plan.Conflicts))
		fmt.Fprintf(os.Stderr, "Run with --force to overwrite external modifications.\n")
		return engine.ExitConflict
	}

	// If dry run, stop here
	if dryRun {
		fmt.Println("Dry-run requested. No changes made.")
		return engine.ExitSuccess
	}

	// Confirm unless --yes
	if !yes {
		fmt.Printf("Proceed with %s on %d resources? [y/N]: ", action, len(plan.Items))
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

	fmt.Printf("\nExecuting %s...\n", action)
	progress := func(step int, total int, res config.Resource, status string, execErr error) {
		if execErr != nil {
			fmt.Printf("[%d/%d] %s %s ... FAILED: %v\n", step, total, action, res.Key(), execErr)
		} else {
			fmt.Printf("[%d/%d] %s %s ... OK\n", step, total, action, res.Key())
		}
	}

	report, err := eng.Execute(ctx, action, force, progress)
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

func printReportSummary(report *engine.ExecutionReport) {
	if report == nil {
		return
	}
	fmt.Printf("\n=== OPERATION SUMMARY: %s ===\n", strings.ToUpper(report.Status))
	fmt.Printf("Operation ID:  %s\n", report.Op)
	fmt.Printf("Action:        %s\n", report.Action)
	fmt.Printf("Total:         %d\n", report.Total)
	fmt.Printf("Succeeded:     %d\n", report.OK)
	fmt.Printf("Failed:        %d\n", report.Failed)
	fmt.Printf("Global State:  %s\n\n", report.GlobalStatus)

	if report.Failed > 0 {
		fmt.Fprintln(os.Stderr, "Failures:")
		for _, res := range report.Results {
			if res.Err != nil {
				fmt.Fprintf(os.Stderr, "  - %s: %v\n", res.Resource.Key(), res.Err)
			}
		}
		fmt.Fprintln(os.Stderr)
	}
}
