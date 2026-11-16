package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func windowRows(rows []string, cursor, maxVisible int) ([]string, int) {
	if maxVisible <= 0 || len(rows) == 0 {
		return nil, 0
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	if len(rows) <= maxVisible {
		return rows, 0
	}
	start := cursor - maxVisible + 1
	if start < 0 {
		start = 0
	}
	end := start + maxVisible
	if end > len(rows) {
		end = len(rows)
		start = end - maxVisible
	}
	return rows[start:end], start
}

func allocWidths(total int, weights []int) []int {
	n := len(weights)
	widths := make([]int, n)
	if n == 0 {
		return widths
	}
	sum := 0
	for _, w := range weights {
		sum += w
	}
	if sum <= 0 {
		sum = n
	}

	assigned := 0
	for i := 0; i < n; i++ {
		if i == n-1 {
			widths[i] = total - assigned
		} else {
			widths[i] = total * weights[i] / sum
		}
		if widths[i] < 1 {
			widths[i] = 1
		}
		assigned += widths[i]
	}

	for assigned > total {
		maxIdx := 0
		for i := range widths {
			if widths[i] > widths[maxIdx] {
				maxIdx = i
			}
		}
		if widths[maxIdx] <= 1 {
			break
		}
		widths[maxIdx]--
		assigned--
	}
	if assigned < total {
		widths[n-1] += total - assigned
	}
	return widths
}

func padBetween(left, right string, width int) string {
	left = truncateText(left, width)
	right = truncateText(right, width)
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	if lw+rw+1 > width {
		return cell(left, width)
	}
	return left + strings.Repeat(" ", width-lw-rw) + right
}

func cell(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = truncateText(s, width)
	if pad := width - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

func cellRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = truncateText(s, width)
	if pad := width - lipgloss.Width(s); pad > 0 {
		s = strings.Repeat(" ", pad) + s
	}
	return s
}

func truncateText(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	r := []rune(s)
	for len(r) > 0 {
		r = r[:len(r)-1]
		if lipgloss.Width(string(r))+1 <= width {
			break
		}
	}
	if width == 1 {
		return "…"
	}
	return string(r) + "…"
}
