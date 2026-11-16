package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rykhalskyi/killswitch/internal/engine"
)

// Run launches the Bubble Tea TUI program.
func Run(eng *engine.Engine) error {
	p := tea.NewProgram(NewModel(eng), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
