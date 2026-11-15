package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rykhalskyi/killswitch/internal/engine"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#5A56E0")).
			Padding(0, 1)

	bannerGreen = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#047857")).
			Padding(0, 1)

	bannerRed = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#B91C1C")).
			Padding(0, 1)

	bannerYellow = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#F59E0B")).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7C3AED")).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true)

	tableRowStyle = lipgloss.NewStyle().
			Padding(0, 1)

	modalBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#8B5CF6")).
			Padding(1, 2)

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9CA3AF"))
)

type statusMsg struct {
	report *engine.StatusReport
	err    error
}

type planMsg struct {
	action engine.Action
	plan   *engine.Plan
	err    error
}

type execDoneMsg struct {
	report *engine.ExecutionReport
	err    error
}

type Model struct {
	eng           *engine.Engine
	status        *engine.StatusReport
	pendingPlan   *engine.Plan
	pendingAction engine.Action
	isModalOpen   bool
	isDryRunModal bool
	executing     bool
	execProgress  string
	errorMessage  string
	infoMessage   string
	width         int
	height        int
}

func NewModel(eng *engine.Engine) Model {
	return Model{
		eng: eng,
	}
}

func (m Model) Init() tea.Cmd {
	return m.fetchStatusCmd()
}

func (m Model) fetchStatusCmd() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		rep, err := m.eng.Status(ctx)
		return statusMsg{report: rep, err: err}
	}
}

func (m Model) fetchPlanCmd(action engine.Action) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		plan, err := m.eng.BuildPlan(ctx, action)
		return planMsg{action: action, plan: plan, err: err}
	}
}

func (m Model) executeCmd(action engine.Action) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		report, err := m.eng.Execute(ctx, action, false, nil)
		return execDoneMsg{report: report, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case statusMsg:
		if msg.err != nil {
			m.errorMessage = fmt.Sprintf("Error fetching status: %v", msg.err)
		} else {
			m.status = msg.report
			m.errorMessage = ""
		}
		return m, nil

	case planMsg:
		if msg.err != nil {
			m.errorMessage = fmt.Sprintf("Error building plan: %v", msg.err)
			m.isModalOpen = false
		} else {
			m.pendingPlan = msg.plan
			m.pendingAction = msg.action
			m.isModalOpen = true
		}
		return m, nil

	case execDoneMsg:
		m.executing = false
		m.isModalOpen = false
		if msg.err != nil {
			m.errorMessage = fmt.Sprintf("Execution error: %v", msg.err)
		} else {
			m.infoMessage = fmt.Sprintf("Operation %s completed (%d ok, %d failed)", msg.report.Op, msg.report.OK, msg.report.Failed)
		}
		return m, m.fetchStatusCmd()

	case tea.KeyMsg:
		if m.executing {
			return m, nil
		}

		// Handle Modal keys
		if m.isModalOpen {
			switch msg.String() {
			case "y", "Y":
				if !m.isDryRunModal {
					m.executing = true
					m.execProgress = fmt.Sprintf("Executing %s...", m.pendingAction)
					return m, m.executeCmd(m.pendingAction)
				}
				m.isModalOpen = false
				return m, nil
			case "n", "N", "esc", "q":
				m.isModalOpen = false
				m.isDryRunModal = false
				return m, nil
			default:
				return m, nil
			}
		}

		// Main screen keys
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "s":
			m.isDryRunModal = false
			return m, m.fetchPlanCmd(engine.ActionSuspend)

		case "r":
			m.isDryRunModal = false
			return m, m.fetchPlanCmd(engine.ActionRestore)

		case "d":
			m.isDryRunModal = true
			return m, m.fetchPlanCmd(engine.ActionSuspend)

		case "u":
			return m, m.fetchStatusCmd()
		}
	}

	return m, nil
}

func (m Model) View() string {
	var b strings.Builder

	// Title
	b.WriteString(titleStyle.Render(" KILL SWITCH — FLUX CONTROLLER ") + "\n\n")

	// Global Banner
	if m.status != nil {
		switch m.status.GlobalStatus {
		case engine.GlobalStatusSuspended:
			b.WriteString(bannerGreen.Render(" GLOBAL STATE: SUSPENDED (All Flux resources frozen) ") + "\n")
		case engine.GlobalStatusRestored:
			b.WriteString(bannerGreen.Render(" GLOBAL STATE: RESTORED (Reconciliation active) ") + "\n")
		case engine.GlobalStatusPartial:
			b.WriteString(bannerRed.Render(" GLOBAL STATE: PARTIAL / MIXED (Intervention required) ") + "\n")
		default:
			b.WriteString(bannerYellow.Render(" GLOBAL STATE: UNKNOWN ") + "\n")
		}
		if m.status.Incomplete {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Render(" ⚠ "+m.status.IncompleteMsg) + "\n")
		}
	} else {
		b.WriteString(bannerYellow.Render(" CONNECTING TO CLUSTER... ") + "\n")
	}

	b.WriteString("\n")

	if m.errorMessage != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#DC2626")).Bold(true).Render("Error: "+m.errorMessage) + "\n\n")
	}
	if m.infoMessage != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Render(m.infoMessage) + "\n\n")
	}

	// Resource Table
	if m.status != nil {
		b.WriteString(headerStyle.Render(fmt.Sprintf("%-16s %-20s %-26s %-12s %-12s %-16s",
			"KIND", "NAMESPACE", "NAME", "STATUS", "ORIGINAL", "READY")) + "\n")

		for _, res := range m.status.Resources {
			stStr := "ACTIVE"
			if !res.Exists {
				stStr = "MISSING"
			} else if res.CurrentSuspended {
				stStr = "SUSPENDED"
			}

			origStr := "-"
			if res.OriginalSuspended != nil {
				if *res.OriginalSuspended {
					origStr = "SUSPENDED"
				} else {
					origStr = "ACTIVE"
				}
			}

			readyStr := res.Ready
			if res.LaggingObservation {
				readyStr += " (!)"
			}

			row := fmt.Sprintf("%-16s %-20s %-26s %-12s %-12s %-16s",
				truncate(res.Kind, 15),
				truncate(res.Namespace, 19),
				truncate(res.Name, 25),
				stStr,
				origStr,
				truncate(readyStr, 15),
			)
			b.WriteString(tableRowStyle.Render(row) + "\n")
		}
	}

	b.WriteString("\n")

	// Modal Overlay if open
	if m.isModalOpen && m.pendingPlan != nil {
		var modal strings.Builder
		if m.isDryRunModal {
			modal.WriteString(lipgloss.NewStyle().Bold(true).Render("DRY RUN PLAN:") + "\n\n")
		} else {
			modal.WriteString(lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("CONFIRM %s PLAN:", strings.ToUpper(string(m.pendingAction)))) + "\n\n")
		}

		for i, item := range m.pendingPlan.Items {
			target := "ACTIVE"
			if item.TargetSuspended {
				target = "SUSPENDED"
			}
			modal.WriteString(fmt.Sprintf("%2d. %s/%s -> %s\n", i+1, item.Resource.Kind, item.Resource.Name, target))
		}

		if len(m.pendingPlan.Conflicts) > 0 {
			modal.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Render("\n⚠ Conflicts detected (external modifications)!\n"))
		}

		if m.isDryRunModal {
			modal.WriteString("\nPress [Esc] or [q] to close.")
		} else {
			modal.WriteString(fmt.Sprintf("\nProceed with %s? [y] Yes  /  [n] Cancel", m.pendingAction))
		}

		b.WriteString(modalBox.Render(modal.String()) + "\n\n")
	}

	if m.executing {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F59E0B")).Render("⏳ "+m.execProgress) + "\n\n")
	}

	// Footer Actions
	b.WriteString(footerStyle.Render("[s] Suspend   [r] Restore   [d] Dry-run   [u] Refresh   [q] Quit") + "\n")

	return b.String()
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}

// Run launches the Bubble Tea TUI program.
func Run(eng *engine.Engine) error {
	p := tea.NewProgram(NewModel(eng), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
