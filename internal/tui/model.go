package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rykhalskyi/killswitch/internal/config"
	"github.com/rykhalskyi/killswitch/internal/engine"
)

type viewMode int

const (
	viewList viewMode = iota
	viewDetail
	viewHistory
)

// switchDef describes one top-level switch in the main table.
// Phase 1 only ships the Flux reconciliation switch; more can be appended later.
type switchDef struct {
	name        string
	description string
}

var switchDefs = []switchDef{
	{
		name:        "Flux Reconciliation",
		description: "Suspend / restore all configured Flux Kustomizations and HelmReleases",
	},
}

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

type historyMsg struct {
	items []engine.HistoryItem
	err   error
}

type progressMsg struct {
	step   int
	total  int
	res    config.Resource
	status string
	err    error
}

type Model struct {
	eng    *engine.Engine
	status *engine.StatusReport

	// layout
	width  int
	height int

	// navigation
	view          viewMode
	historyReturn viewMode
	cursor        int
	detailCursor  int
	historyCursor int

	// history
	history []engine.HistoryItem

	// confirmation / execution
	pendingPlan   *engine.Plan
	pendingAction engine.Action
	pendingForce  bool
	confirmOpen   bool
	executing     bool
	execProgress  string
	msgChan       chan tea.Msg

	errorMessage string
	infoMessage  string
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

func (m Model) fetchHistoryCmd() tea.Cmd {
	return func() tea.Msg {
		items, err := m.eng.History(50)
		return historyMsg{items: items, err: err}
	}
}

func waitForProgress(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *Model) startExecution(action engine.Action, force bool) tea.Cmd {
	ch := make(chan tea.Msg, 64)
	m.executing = true
	m.msgChan = ch
	m.execProgress = fmt.Sprintf("Starting %s...", action)

	go func() {
		defer close(ch)
		ctx := context.Background()
		report, err := m.eng.Execute(ctx, action, force, func(step int, total int, res config.Resource, status string, execErr error) {
			ch <- progressMsg{step: step, total: total, res: res, status: status, err: execErr}
		})
		ch <- execDoneMsg{report: report, err: err}
	}()

	return waitForProgress(ch)
}
