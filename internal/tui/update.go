package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rykhalskyi/killswitch/internal/engine"
)

func (m Model) pendingTransition() (string, engine.Direction) {
	if m.pendingSwitch != "" {
		return m.pendingSwitch, m.pendingDir
	}
	sw := m.selectedSwitch()
	dir := engine.DirectionOn
	if m.pendingAction == engine.ActionSuspend {
		dir = engine.DirectionOff
	}
	return sw.id, dir
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
			if m.status != nil && m.detailCursor >= len(m.status.Resources) {
				m.detailCursor = len(m.status.Resources) - 1
				if m.detailCursor < 0 {
					m.detailCursor = 0
				}
			}
		}
		return m, nil

	case planMsg:
		m.planning = false
		if msg.err != nil {
			m.errorMessage = fmt.Sprintf("Error building plan: %v", msg.err)
			m.confirmOpen = false
			return m, nil
		}
		m.pendingPlan = msg.plan
		m.pendingAction = msg.action
		m.pendingSwitch = msg.switchID
		m.pendingDir = msg.direction
		m.pendingForce = false
		m.confirmOpen = true
		return m, nil

	case historyMsg:
		if msg.err != nil {
			m.errorMessage = fmt.Sprintf("Error loading history: %v", msg.err)
		} else {
			m.history = msg.items
			m.errorMessage = ""
			if m.historyCursor >= len(m.history) {
				m.historyCursor = len(m.history) - 1
				if m.historyCursor < 0 {
					m.historyCursor = 0
				}
			}
		}
		return m, nil

	case progressMsg:
		m.execStep = msg.step
		m.execTotal = msg.total
		if msg.err != nil {
			m.execFailed++
			m.execProgress = fmt.Sprintf("[%d/%d] %s (FAILED: %v)", msg.step, msg.total, msg.res.Name, msg.err)
		} else {
			m.execOK++
			m.execProgress = fmt.Sprintf("[%d/%d] %s (OK)", msg.step, msg.total, msg.res.Name)
		}
		return m, waitForProgress(m.msgChan)

	case execDoneMsg:
		m.executing = false
		m.msgChan = nil
		m.confirmOpen = false
		m.pendingPlan = nil
		m.pendingForce = false
		m.planning = false
		m.execProgress = ""
		if msg.err != nil {
			m.errorMessage = fmt.Sprintf("Execution error: %v", msg.err)
			m.infoMessage = ""
		} else {
			m.infoMessage = fmt.Sprintf("Operation %s completed (%d ok, %d failed)", msg.report.Op, msg.report.OK, msg.report.Failed)
			m.errorMessage = ""
		}
		return m, tea.Batch(m.fetchStatusCmd(), m.fetchHistoryCmd())

	case tea.KeyMsg:
		if m.executing {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
		}

		if m.planning {
			// The plan request is already in flight; ignore further input so
			// repeated Ctrl+T presses cannot stack dialogs.
			if msg.String() == "ctrl+c" || msg.String() == "q" {
				return m, tea.Quit
			}
			return m, nil
		}

		if m.confirmOpen {
			switch msg.String() {
			case "y", "Y", "enter":
				m.confirmOpen = false
				switchID, dir := m.pendingTransition()
				return m, m.startExecution(switchID, dir, false)
			case "f", "F":
				if m.pendingPlan == nil || len(m.pendingPlan.Conflicts) == 0 {
					return m, nil
				}
				m.confirmOpen = false
				m.pendingForce = true
				switchID, dir := m.pendingTransition()
				return m, m.startExecution(switchID, dir, true)
			case "n", "N", "esc", "q":
				m.confirmOpen = false
				m.pendingPlan = nil
				m.pendingForce = false
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "ctrl+t", "t", " ":
			if m.view == viewHistory {
				return m, nil
			}
			switchID, dir, ok := m.nextTransition()
			if !ok {
				m.errorMessage = "Status not loaded yet"
				return m, nil
			}
			m.errorMessage = ""
			m.infoMessage = ""
			m.planning = true
			m.planSwitch = switchID
			m.planDir = dir
			return m, m.fetchPlanCmd(switchID, dir, engine.LegacyAction(dir))

		case "r", "u":
			if m.view == viewHistory {
				return m, tea.Batch(m.fetchStatusCmd(), m.fetchHistoryCmd())
			}
			return m, m.fetchStatusCmd()

		case "h":
			if m.view == viewHistory {
				return m, nil
			}
			m.historyReturn = m.view
			m.view = viewHistory
			m.historyCursor = 0
			m.errorMessage = ""
			return m, m.fetchHistoryCmd()

		case "up", "k":
			switch m.view {
			case viewHistory:
				if m.historyCursor > 0 {
					m.historyCursor--
				}
			case viewDetail:
				if m.detailCursor > 0 {
					m.detailCursor--
				}
			default:
				if m.cursor > 0 {
					m.cursor--
				}
			}
			return m, nil

		case "down", "j":
			switch m.view {
			case viewHistory:
				if m.historyCursor < len(m.history)-1 {
					m.historyCursor++
				}
			case viewDetail:
				max := 0
				if m.status != nil {
					max = len(m.status.Resources) - 1
				}
				if m.detailCursor < max {
					m.detailCursor++
				}
			default:
				if m.cursor < len(m.switchDefs())-1 {
					m.cursor++
				}
			}
			return m, nil

		case "enter":
			if m.view == viewList {
				m.view = viewDetail
				m.detailCursor = 0
			}
			return m, nil

		case "esc":
			switch m.view {
			case viewHistory:
				m.view = m.historyReturn
			case viewDetail:
				m.view = viewList
			}
			return m, nil
		}
	}

	return m, nil
}
