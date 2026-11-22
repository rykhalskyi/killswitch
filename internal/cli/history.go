package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/rykhalskyi/killswitch/internal/engine"
)

func runHistory(eng *engine.Engine, limit int, jsonOutput bool, switchID string, details bool) int {
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
	fmt.Fprintln(w, "TIMESTAMP\tOP ID\tSWITCH\tDIRECTION\tSTATUS\tSTEPS\tDURATION")
	for _, item := range items {
		durStr := fmt.Sprintf("%dms", item.DurationMs)
		if item.Status == "interrupted" || item.Status == "conflict" {
			durStr = "-"
		}
		steps := "-"
		if item.Targets > 0 {
			steps = fmt.Sprintf("%d/%d", item.OK, item.Targets)
		}
		direction := historyDirectionLabel(item)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			item.StartedAt, item.Op, historySwitchLabel(item), direction,
			strings.ToUpper(item.Status), steps, durStr)
	}
	_ = w.Flush()
	fmt.Println()

	if details {
		printHistoryDetails(items)
	}

	return engine.ExitSuccess
}

func printHistoryDetails(items []engine.HistoryItem) {
	for _, item := range items {
		fmt.Printf("=== %s · %s · %s ===\n", item.Op, historySwitchLabel(item), strings.ToUpper(item.Status))
		if item.StartedAt != "" {
			fmt.Printf("Started: %s", item.StartedAt)
			if item.EndedAt != "" {
				fmt.Printf("   Ended: %s", item.EndedAt)
			}
			fmt.Println()
		}
		if item.Context != "" {
			fmt.Printf("Context: %s\n", item.Context)
		}
		if len(item.Steps) == 0 {
			fmt.Println("No per-step records for this operation.")
			fmt.Println()
			continue
		}
		fmt.Println()
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  STEP\tACTION\tTARGET\tRESULT\tDETAIL")
		for _, step := range item.Steps {
			result := strings.ToUpper(step.Status)
			if result == "" {
				result = "-"
			}
			target := step.Target
			if target == "" {
				target = "-"
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n",
				stepValue(step.Step), stepValue(step.Action), target, result, historyStepDetail(step))
		}
		_ = w.Flush()
		fmt.Println()
	}
}

func historySwitchLabel(item engine.HistoryItem) string {
	if item.SwitchName != "" {
		return item.SwitchName
	}
	if item.Switch != "" {
		return item.Switch
	}
	return "flux"
}

func historyDirectionLabel(item engine.HistoryItem) string {
	if item.OneWay {
		return "ONE-WAY"
	}
	direction := item.Direction
	if direction == "" {
		direction = item.Action
	}
	switch direction {
	case "suspend":
		return "OFF"
	case "restore":
		return "ON"
	case "":
		return "-"
	default:
		return strings.ToUpper(direction)
	}
}

func stepValue(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

func historyStepDetail(step engine.HistoryStep) string {
	if step.Error != "" {
		return step.Error
	}
	parts := make([]string, 0, 3)
	if step.Original != nil {
		parts = append(parts, "orig="+formatValue(step.Original))
	}
	if step.Desired != nil {
		parts = append(parts, "want="+formatValue(step.Desired))
	}
	if step.Observed != nil {
		parts = append(parts, "obs="+formatValue(step.Observed))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}
