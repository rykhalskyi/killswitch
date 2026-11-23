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
	viewHistoryDetail
	viewHistoryLog
	viewYAML
)

// switchDef describes one top-level switch in the main table.
type switchDef struct {
	id          string
	name        string
	description string
	icon        string
	workflow    string
	oneWay      bool
}

var defaultSwitchDefs = []switchDef{
	{
		id:          "flux",
		name:        "Flux Reconciliation",
		description: "Suspend / restore all configured Flux Kustomizations and HelmReleases",
		icon:        "⭐",
		workflow:    "flux",
	},
}

type statusMsg struct {
	report *engine.StatusReport
	err    error
}

type planMsg struct {
	action    engine.Action
	switchID  string
	direction engine.Direction
	plan      *engine.Plan
	err       error
}

type execDoneMsg struct {
	report *engine.ExecutionReport
	err    error
}

type historyMsg struct {
	items []engine.HistoryItem
	err   error
}

type historyLogMsg struct {
	op    string
	lines []string
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
	eng      *engine.Engine
	status   *engine.StatusReport
	switches []switchDef

	// layout
	width  int
	height int

	// navigation
	view          viewMode
	historyReturn viewMode
	cursor        int
	detailCursor  int
	historyCursor int

	historyDetailCursor int

	historyLogLines  []string
	historyLogScroll int
	historyLogOp     string
	historyLogReturn viewMode

	// switch YAML
	yamlLines  []string
	yamlScroll int

	// history
	history []engine.HistoryItem

	// confirmation / execution
	pendingPlan   *engine.Plan
	pendingAction engine.Action
	pendingSwitch string
	pendingDir    engine.Direction
	pendingForce  bool
	confirmOpen   bool
	planning      bool
	planSwitch    string
	planDir       engine.Direction
	executing     bool
	execProgress  string
	execStep      int
	execTotal     int
	execOK        int
	execFailed    int
	msgChan       chan tea.Msg

	errorMessage string
	infoMessage  string
}

func NewModel(eng *engine.Engine) Model {
	m := Model{eng: eng}
	if eng != nil {
		for _, sw := range eng.Switches() {
			oneWay := false
			if cfg := eng.Config(); cfg != nil {
				if wf := cfg.WorkflowForSwitch(sw.ID); wf != nil {
					oneWay = wf.OneWay
				}
			}
			m.switches = append(m.switches, switchDef{
				id:          sw.ID,
				name:        sw.Name,
				description: sw.Description,
				icon:        sw.Icon,
				workflow:    sw.Workflow,
				oneWay:      oneWay,
			})
		}
	}
	m.switches = fluxFirst(m.switches, m.fluxSwitchID())
	return m
}

// fluxFirst pins the main reconciliation switch to the top of the table
// regardless of its declaration order in switches.yaml.
func fluxFirst(defs []switchDef, fluxID string) []switchDef {
	if fluxID == "" {
		return defs
	}
	for i, def := range defs {
		if def.id != fluxID {
			continue
		}
		if i == 0 {
			return defs
		}
		out := make([]switchDef, 0, len(defs))
		out = append(out, def)
		out = append(out, defs[:i]...)
		out = append(out, defs[i+1:]...)
		return out
	}
	return defs
}

// switchDefs returns configured switches, falling back to the Flux default.
func (m Model) switchDefs() []switchDef {
	if len(m.switches) > 0 {
		return m.switches
	}
	return defaultSwitchDefs
}

// selectedSwitch returns the switch at the cursor.
func (m Model) selectedSwitch() switchDef {
	defs := m.switchDefs()
	if len(defs) == 0 {
		return switchDef{}
	}
	idx := m.cursor
	if idx < 0 {
		idx = 0
	}
	if idx >= len(defs) {
		idx = len(defs) - 1
	}
	return defs[idx]
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

func (m Model) fetchPlanCmd(switchID string, dir engine.Direction, action engine.Action) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		plan, err := m.eng.BuildSwitchPlan(ctx, switchID, dir)
		return planMsg{action: action, switchID: switchID, direction: dir, plan: plan, err: err}
	}
}

func (m Model) fetchHistoryCmd() tea.Cmd {
	return func() tea.Msg {
		items, err := m.eng.History(50)
		return historyMsg{items: items, err: err}
	}
}

func (m Model) fetchHistoryLogCmd(op string) tea.Cmd {
	return func() tea.Msg {
		if m.eng == nil {
			return historyLogMsg{op: op, err: fmt.Errorf("engine not available")}
		}
		lines, err := m.eng.OperationLog(op)
		return historyLogMsg{op: op, lines: lines, err: err}
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

func (m *Model) startExecution(switchID string, dir engine.Direction, force bool) tea.Cmd {
	ch := make(chan tea.Msg, 64)
	m.executing = true
	m.msgChan = ch
	m.execStep = 0
	m.execTotal = 0
	m.execOK = 0
	m.execFailed = 0
	m.execProgress = fmt.Sprintf("Starting %s %s...", switchID, dir)

	go func() {
		defer close(ch)
		ctx := context.Background()
		report, err := m.eng.ExecuteSwitch(ctx, switchID, dir, force, func(step int, total int, res config.Resource, status string, execErr error) {
			ch <- progressMsg{step: step, total: total, res: res, status: status, err: execErr}
		})
		ch <- execDoneMsg{report: report, err: err}
	}()

	return waitForProgress(ch)
}
