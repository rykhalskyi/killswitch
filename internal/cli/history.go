package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/rykhalskyi/killswitch/internal/engine"
)

func runHistory(eng *engine.Engine, limit int, jsonOutput bool, switchID string) int {
	fetchLimit := limit
	if switchID != "" && fetchLimit > 0 {
		fetchLimit = 0
	}
	items, err := eng.History(fetchLimit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading history: %v\n", err)
		return engine.ExitConfigOrConnection
	}

	if switchID != "" {
		filtered := items[:0]
		for _, item := range items {
			if item.Switch == switchID {
				filtered = append(filtered, item)
			}
		}
		items = filtered
		if limit > 0 && len(items) > limit {
			items = items[:limit]
		}
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(items); err != nil {
			fmt.Fprintf(os.Stderr, "Error formatting JSON: %v\n", err)
			return engine.ExitConfigOrConnection
		}
		return engine.ExitSuccess
	}

	if len(items) == 0 {
		fmt.Println("No past operations found in journal.")
		return engine.ExitSuccess
	}

	fmt.Printf("\n=== OPERATION HISTORY (%d recorded) ===\n\n", len(items))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIMESTAMP\tOP ID\tSWITCH\tACTION\tSTATUS\tTARGETS\tOK\tFAILED\tDURATION")
	for _, item := range items {
		durStr := fmt.Sprintf("%dms", item.DurationMs)
		if item.Status == "interrupted" {
			durStr = "-"
		}
		switchLabel := item.Switch
		if switchLabel == "" {
			switchLabel = "flux"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%s\n",
			item.StartedAt, item.Op, switchLabel, item.Action, item.Status, item.Targets, item.OK, item.Failed, durStr)
	}
	_ = w.Flush()
	fmt.Println()

	return engine.ExitSuccess
}
