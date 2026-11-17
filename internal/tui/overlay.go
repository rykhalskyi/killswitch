package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// overlayCenter draws modal centered over base. The base stays visible around
// the dialog but is muted so the modal reads as the focused layer. Bubble Tea
// has no window compositor, so the overlay is composed into one frame string.
func overlayCenter(base, modal string, width, height int) string {
	if width <= 0 || height <= 0 {
		return base
	}

	modalW, modalH := lipgloss.Size(modal)
	if modalW <= 0 || modalH <= 0 {
		return base
	}
	if modalW > width {
		modal = ansi.Truncate(modal, width, "")
		modalW = width
	}

	baseLines := strings.Split(base, "\n")
	modalLines := strings.Split(modal, "\n")
	if modalH > height {
		modalLines = modalLines[:height]
		modalH = height
	}

	x := (width - modalW) / 2
	y := (height - modalH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	lines := make([]string, 0, height)
	for row := 0; row < height; row++ {
		var baseLine string
		if row < len(baseLines) {
			baseLine = ansi.Strip(baseLines[row])
		}
		baseLine = fitVisible(baseLine, width)

		if row < y || row >= y+modalH {
			lines = append(lines, dimStyle.Render(baseLine))
			continue
		}

		left := ansi.Truncate(baseLine, x, "")
		right := ansi.TruncateLeft(baseLine, x+modalW, "")
		modalLine := fitVisible(modalLines[row-y], modalW)
		lines = append(lines, dimStyle.Render(left)+modalLine+dimStyle.Render(right))
	}

	return strings.Join(lines, "\n")
}

// fitVisible truncates or pads s to exactly width visible columns.
func fitVisible(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if w := ansi.StringWidth(s); w > width {
		return ansi.Truncate(s, width, "")
	} else if w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}
