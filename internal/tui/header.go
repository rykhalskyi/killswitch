package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rykhalskyi/killswitch/internal/version"
)

func (m Model) renderHeader(height int) string {
	word := styledWordmark()
	wordWidth := lipgloss.Width(word[0])

	verText := version.Display()
	verWidth := 2 + lipgloss.Width(verText)
	verSuffix := "  " + versionStyle.Render(verText)

	needForWord := wordWidth
	if height < 5 {
		// No subtitle row: the version shares the last wordmark row.
		needForWord += verWidth
	}

	// The block wordmark needs four rows. Below that, use a compact one-liner.
	if height < 4 || m.width < needForWord {
		title := truncateText("KILL SWITCH  "+verText, m.width)
		return lipgloss.Place(m.width, height, lipgloss.Left, lipgloss.Center, bannerStyle.Render(title))
	}

	lines := make([]string, 0, height)
	lines = append(lines, word...)

	if height >= 5 {
		subtitle := subtitleStyle.Render("FLUX RECONCILIATION CONTROLLER") + "  ·  " + versionStyle.Render(verText)
		lines = append(lines, subtitle)
	} else {
		lines[len(lines)-1] += verSuffix
	}

	return lipgloss.Place(m.width, height, lipgloss.Left, lipgloss.Center, strings.Join(lines, "\n"))
}

// styledWordmark renders "KILL SWITCH" as block letters, red for KILL and
// white for SWITCH, mirroring the project logo.
func styledWordmark() []string {
	kill := renderBanner("KILL")
	sw := renderBanner("SWITCH")
	lines := make([]string, 4)
	for i := 0; i < 4; i++ {
		lines[i] = killWordStyle.Render(kill[i]) + "  " + switchWordStyle.Render(sw[i])
	}
	return lines
}

// renderBanner draws a 4-row block "pseudographic" banner.
func renderBanner(text string) []string {
	lines := make([]string, 4)
	for i, r := range text {
		glyph, ok := banner4[r]
		if !ok {
			glyph = banner4[' ']
		}
		for row := 0; row < 4; row++ {
			if i > 0 {
				lines[row] += " "
			}
			lines[row] += glyph[row]
		}
	}
	return lines
}

var banner4 = map[rune][]string{
	'K': {"█  █", "██  ", "█ █ ", "█  █"},
	'I': {"███", " █ ", " █ ", "███"},
	'L': {"█  ", "█  ", "█  ", "███"},
	'S': {"████", "███ ", "   █", "████"},
	'W': {"█   █", "█ █ █", "██ ██", "█   █"},
	'T': {"███", " █ ", " █ ", " █ "},
	'C': {"████", "█   ", "█   ", "████"},
	'H': {"█  █", "████", "█  █", "█  █"},
	' ': {"  ", "  ", "  ", "  "},
}
