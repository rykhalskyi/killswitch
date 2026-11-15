package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/rykhalskyi/killswitch/internal/engine"
)

func runStatus(ctx context.Context, eng *engine.Engine, jsonOutput bool) int {
	report, err := eng.Status(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error querying status: %v\n", err)
		return engine.ExitConfigOrConnection
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "Error formatting JSON: %v\n", err)
			return engine.ExitConfigOrConnection
		}
		return engine.ExitSuccess
	}

	// Text output
	fmt.Printf("\n=== KILL SWITCH STATUS ===\n")
	fmt.Printf("Context:       %s\n", report.Context)
	fmt.Printf("Global State:  %s\n", formatGlobalStatus(report.GlobalStatus))

	if report.Incomplete {
		fmt.Printf("WARNING:       %s\n", report.IncompleteMsg)
	}
	if report.LastCompletedOp != nil {
		fmt.Printf("Last Run:      %s\n", *report.LastCompletedOp)
	}
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KIND\tNAMESPACE\tNAME\tSTATUS\tORIGINAL\tREADY\tGEN/OBS\tPHASE")
	for _, r := range report.Resources {
		stateStr := "ACTIVE"
		if !r.Exists {
			stateStr = "MISSING"
		} else if r.CurrentSuspended {
			stateStr = "SUSPENDED"
		}

		origStr := "-"
		if r.OriginalSuspended != nil {
			if *r.OriginalSuspended {
				origStr = "SUSPENDED"
			} else {
				origStr = "ACTIVE"
			}
		}

		readyStr := r.Ready
		if r.ReadyReason != "" {
			readyStr = fmt.Sprintf("%s (%s)", r.Ready, r.ReadyReason)
		}

		genStr := fmt.Sprintf("%d/%d", r.Generation, r.ObservedGeneration)
		if r.LaggingObservation {
			genStr += " (!)"
		}

		phaseStr := r.ActivePhase
		if phaseStr == "" {
			phaseStr = "-"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Kind, r.Namespace, r.Name, stateStr, origStr, readyStr, genStr, phaseStr)
	}
	_ = w.Flush()
	fmt.Println()

	return engine.ExitSuccess
}

func formatGlobalStatus(status engine.GlobalStatus) string {
	switch status {
	case engine.GlobalStatusSuspended:
		return "SUSPENDED (all targets frozen)"
	case engine.GlobalStatusRestored:
		return "RESTORED (all targets match original states)"
	case engine.GlobalStatusPartial:
		return "PARTIAL (targets in mixed state)"
	default:
		return string(status)
	}
}
