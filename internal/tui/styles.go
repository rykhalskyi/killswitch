package tui

import "github.com/charmbracelet/lipgloss"

// Colors follow a k9s / flux9s inspired palette.
var (
	accent      = lipgloss.Color("#A78BFA")
	accentDark  = lipgloss.Color("#5A56E0")
	muted       = lipgloss.Color("#9CA3AF")
	borderColor = lipgloss.Color("#4B5563")
	green       = lipgloss.Color("#10B981")
	red         = lipgloss.Color("#EF4444")
	yellow      = lipgloss.Color("#F59E0B")
	cyan        = lipgloss.Color("#22D3EE")

	bannerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accent)

	killWordStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(red)

	switchWordStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#F9FAFB"))

	logoRedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(red)

	logoWhiteStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#E5E7EB"))

	subtitleStyle = lipgloss.NewStyle().
			Foreground(muted)

	versionStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(cyan)

	panelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(cyan)

	colHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accent)

	borderStyle = lipgloss.NewStyle().
			Foreground(borderColor)

	selectedRowStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(accentDark)

	nameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F9FAFB"))

	// The Flux reconciliation switch is the main control, so its row is
	// visually distinct from workload switches.
	fluxNameStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accent)

	dimStyle = lipgloss.NewStyle().
			Foreground(muted)

	onStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(green)

	offStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(red)

	mixedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(yellow)

	errorStyle = lipgloss.NewStyle().
			Foreground(red)

	infoStyle = lipgloss.NewStyle().
			Foreground(green)

	footerStyle = lipgloss.NewStyle().
			Foreground(muted)

	modalBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(1, 2)

	modalTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accent)

	modalWarnStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(yellow)
)
