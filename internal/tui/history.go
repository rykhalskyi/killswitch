package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/rykhalskyi/killswitch/internal/engine"
)

func (m Model) selectedHistory() *engine.HistoryItem {
	if m.historyCursor < 0 || m.historyCursor >= len(m.history) {
		return nil
	}
	return &m.history[m.historyCursor]
}

func (m Model) historyDetailSteps() []engine.HistoryStep {
	if item := m.selectedHistory(); item != nil {
		return item.Steps
	}
	return nil
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

// historyDirectionLabel shows ON/OFF for toggles and ONE-WAY for buttons.
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

func (m Model) renderHistory(bodyHeight int) string {
	usable := m.contentWidth()
	widths := allocWidths(usable-5, []int{15, 26, 9, 11, 8, 10})

	colHeader := cell("TIME", widths[0]) + " " +
		cell("SWITCH", widths[1]) + " " +
		cell("DIR", widths[2]) + " " +
		cell("RESULT", widths[3]) + " " +
		cellRight("STEPS", widths[4]) + " " +
		cellRight("DURATION", widths[5])

	rows := make([]string, 0, len(m.history))
	for i, item := range m.history {
		marker := "  "
		if i == m.historyCursor {
			marker = "▸ "
		}
		steps := "-"
		if item.Targets > 0 {
			steps = fmt.Sprintf("%d/%d", item.OK, item.Targets)
		}
		duration := formatDuration(item.DurationMs)
		if item.Status == "interrupted" || item.Status == "conflict" {
			duration = "-"
		}
		cells := []string{
			cell(marker+formatHistoryTime(item.StartedAt), widths[0]),
			cell(historySwitchLabel(item), widths[1]),
			cell(historyDirectionLabel(item), widths[2]),
			cell(strings.ToUpper(item.Status), widths[3]),
			cellRight(steps, widths[4]),
			cellRight(duration, widths[5]),
		}
		row := strings.Join(cells, " ")
		if i == m.historyCursor {
			row = selectedRowStyle.Render(row)
		} else {
			row = historyStatusStyle(item.Status).Render(row)
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		rows = append(rows, cell("No operations recorded yet.", usable))
	}

	innerH := bodyHeight - 2
	maxRows := innerH - 6
	if maxRows < 1 {
		maxRows = 1
	}
	visible, start := windowRows(rows, m.historyCursor, maxRows)

	title := m.breadcrumb()
	right := fmt.Sprintf("%d operations", len(m.history))
	if len(m.history) > len(visible) {
		title = fmt.Sprintf("%s  (%d-%d of %d)", title, start+1, start+len(visible), len(m.history))
	}

	return m.renderPanel(title, right, colHeader, visible, bodyHeight)
}

func (m Model) renderHistoryLog(bodyHeight int) string {
	usable := m.contentWidth()
	if len(m.historyLogLines) == 0 {
		return m.renderPanel(m.breadcrumb(), "", "",
			[]string{cell("No journal records found for this operation.", usable)}, bodyHeight)
	}

	innerH := bodyHeight - 2
	maxRows := innerH - 4
	if maxRows < 1 {
		maxRows = 1
	}

	visible, start := windowRows(m.historyLogLines, m.historyLogScroll, maxRows)
	rows := make([]string, 0, len(visible))
	for i, line := range visible {
		rows = append(rows, cell(fmt.Sprintf("%4d  %s", start+i+1, line), usable))
	}

	title := m.breadcrumb()
	right := fmt.Sprintf("%d lines", len(m.historyLogLines))
	if len(m.historyLogLines) > len(visible) {
		title = fmt.Sprintf("%s  (%d-%d of %d)", title, start+1, start+len(visible), len(m.historyLogLines))
	}

	return m.renderPanel(title, right, "", rows, bodyHeight)
}

func (m Model) renderHistoryDetail(bodyHeight int) string {
	item := m.selectedHistory()
	if item == nil {
		return m.renderPanel(m.breadcrumb(), "", "", []string{cell("No operation selected.", m.contentWidth())}, bodyHeight)
	}

	usable := m.contentWidth()
	widths := allocWidths(usable-4, []int{12, 16, 24, 9, 27})

	colHeader := cell("STEP", widths[0]) + " " +
		cell("ACTION", widths[1]) + " " +
		cell("TARGET", widths[2]) + " " +
		cell("RESULT", widths[3]) + " " +
		cell("DETAIL", widths[4])

	rows := make([]string, 0, len(item.Steps))
	for i, step := range item.Steps {
		stepID := step.Step
		if stepID == "" {
			stepID = "-"
		}
		target := step.Target
		if target == "" {
			target = "-"
		}
		result := strings.ToUpper(step.Status)
		if result == "" {
			result = "-"
		}
		row := strings.Join([]string{
			cell(stepID, widths[0]),
			cell(step.Action, widths[1]),
			cell(target, widths[2]),
			cell(result, widths[3]),
			cell(formatStepDetail(step), widths[4]),
		}, " ")
		if i == m.historyDetailCursor {
			row = selectedRowStyle.Render(row)
		} else {
			row = historyStepStatusStyle(step.Status).Render(row)
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		rows = append(rows, cell("No per-step records for this operation.", usable))
	}

	innerH := bodyHeight - 2
	maxRows := innerH - 6
	if maxRows < 1 {
		maxRows = 1
	}
	visible, start := windowRows(rows, m.historyDetailCursor, maxRows)

	title := m.breadcrumb()
	direction := historyDirectionLabel(*item)
	right := fmt.Sprintf("%s · %s · %s · %s", historySwitchLabel(*item), direction, strings.ToUpper(item.Status), formatDuration(item.DurationMs))
	if item.Status == "interrupted" || item.Status == "conflict" {
		right = fmt.Sprintf("%s · %s · %s", historySwitchLabel(*item), direction, strings.ToUpper(item.Status))
	}
	if len(item.Steps) > len(visible) {
		title = fmt.Sprintf("%s  (%d-%d of %d)", title, start+1, start+len(visible), len(item.Steps))
	}

	return m.renderPanel(title, right, colHeader, visible, bodyHeight)
}

func shortOpID(op string) string {
	if len(op) <= 10 {
		return op
	}
	return op[:2] + "…" + op[len(op)-6:]
}

func formatStepDetail(step engine.HistoryStep) string {
	if step.Error != "" {
		return step.Error
	}
	parts := make([]string, 0, 3)
	if step.Original != nil {
		parts = append(parts, "orig="+formatHistoryValue(step.Original))
	}
	if step.Desired != nil {
		parts = append(parts, "want="+formatHistoryValue(step.Desired))
	}
	if step.Observed != nil {
		parts = append(parts, "obs="+formatHistoryValue(step.Observed))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

func formatHistoryValue(v any) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%v", v)
}

func formatHistoryTime(raw string) string {
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return raw
	}
	return ts.Local().Format("01-02 15:04:05")
}

func formatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

func historyStatusStyle(status string) lipgloss.Style {
	switch status {
	case "success":
		return onStyle
	case "partial", "interrupted", "conflict":
		return mixedStyle
	case "failed":
		return errorStyle
	default:
		return dimStyle
	}
}

func historyStepStatusStyle(status string) lipgloss.Style {
	switch status {
	case "ok":
		return onStyle
	case "error":
		return errorStyle
	case "conflict", "pending":
		return mixedStyle
	default:
		return dimStyle
	}
}
