package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderPanel(title, right, colHeader string, rows []string, bodyHeight int) string {
	if m.width < 6 {
		return lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center, "terminal too small")
	}

	usable := m.contentWidth()
	innerH := bodyHeight - 2
	if innerH < 1 {
		innerH = 1
	}

	chrome := 4 // title, blank, status, footer
	if colHeader != "" {
		chrome += 2 // column header + separator
	}
	maxRows := innerH - chrome
	if maxRows < 0 {
		maxRows = 0
	}
	if len(rows) > maxRows {
		rows = rows[:maxRows]
	}

	lines := make([]string, 0, innerH)
	lines = append(lines, " "+panelTitleStyle.Render(padBetween(title, right, usable))+" ")
	lines = append(lines, strings.Repeat(" ", usable+2))
	if colHeader != "" {
		lines = append(lines, " "+colHeaderStyle.Render(cell(colHeader, usable))+" ")
		lines = append(lines, " "+borderStyle.Render(strings.Repeat("─", usable))+" ")
	}
	for _, row := range rows {
		lines = append(lines, " "+row+" ")
	}

	// Keep the status line and key help pinned to the bottom of the panel.
	for len(lines) < innerH-2 {
		lines = append(lines, strings.Repeat(" ", usable+2))
	}

	statusText, statusStyle := m.panelStatus()
	lines = append(lines, " "+statusStyle.Render(cell(statusText, usable))+" ")
	lines = append(lines, " "+footerStyle.Render(cell(m.footerText(), usable))+" ")

	if len(lines) > innerH {
		tail := 2
		if innerH <= tail {
			lines = lines[:innerH]
		} else {
			lines = append(lines[:innerH-tail], lines[len(lines)-tail:]...)
		}
	}
	for len(lines) < innerH {
		lines = append(lines, strings.Repeat(" ", usable+2))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor)

	return box.Render(strings.Join(lines, "\n"))
}

func (m Model) contentWidth() int {
	w := m.width - 4
	if w < 1 {
		w = 1
	}
	return w
}

func (m Model) panelStatus() (string, lipgloss.Style) {
	if m.errorMessage != "" {
		return "Error: " + m.errorMessage, errorStyle
	}
	if m.infoMessage != "" {
		return m.infoMessage, infoStyle
	}
	return "", footerStyle
}

func (m Model) footerText() string {
	switch m.view {
	case viewHistory:
		return "[↑/↓] scroll   [esc] back   [r] refresh   [q] quit"
	case viewYAML:
		return "[↑/↓] scroll   [g/G] top/bottom   [esc] back   [q] quit"
	case viewDetail:
		return "[↑/↓] scroll   [y] YAML   [esc] back   [ctrl+t] toggle   [h] history   [r] refresh   [q] quit"
	default:
		return "[↑/↓] select   [enter] inspect   [ctrl+t] toggle   [h] history   [r] refresh   [q] quit"
	}
}
