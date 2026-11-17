package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderHistory(bodyHeight int) string {
	usable := m.contentWidth()
	widths := allocWidths(usable-6, []int{17, 9, 12, 8, 6, 7, 10})

	colHeader := cell("TIME", widths[0]) + " " +
		cell("ACTION", widths[1]) + " " +
		cell("STATUS", widths[2]) + " " +
		cellRight("TARGETS", widths[3]) + " " +
		cellRight("OK", widths[4]) + " " +
		cellRight("FAILED", widths[5]) + " " +
		cellRight("DURATION", widths[6])

	rows := make([]string, 0, len(m.history))
	for i, item := range m.history {
		marker := "  "
		if i == m.historyCursor {
			marker = "▸ "
		}
		cells := []string{
			cell(marker+formatHistoryTime(item.StartedAt), widths[0]),
			cell(item.Action, widths[1]),
			cell(strings.ToUpper(item.Status), widths[2]),
			cellRight(fmt.Sprintf("%d", item.Targets), widths[3]),
			cellRight(fmt.Sprintf("%d", item.OK), widths[4]),
			cellRight(fmt.Sprintf("%d", item.Failed), widths[5]),
			cellRight(formatDuration(item.DurationMs), widths[6]),
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
