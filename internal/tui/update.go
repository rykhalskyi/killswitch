package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

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
		if msg.err != nil {
			m.errorMessage = fmt.Sprintf("Error building plan: %v", msg.err)
			m.confirmOpen = false
			return m, nil
		}
		m.pendingPlan = msg.plan
		m.pendingAction = msg.action
		m.confirmOpen = true
		return m, nil

	case progressMsg:
		if msg.err != nil {
			m.execProgress = fmt.Sprintf("[%d/%d] %s (FAILED: %v)", msg.step, msg.total, msg.res.Name, msg.err)
		} else {
			m.execProgress = fmt.Sprintf("[%d/%d] %s (OK)", msg.step, msg.total, msg.res.Name)
		}
		return m, waitForProgress(m.msgChan)

	case execDoneMsg:
		m.executing = false
		m.msgChan = nil
		m.confirmOpen = false
		m.pendingPlan = nil
		if msg.err != nil {
			m.errorMessage = fmt.Sprintf("Execution error: %v", msg.err)
			m.infoMessage = ""
		} else {
			m.infoMessage = fmt.Sprintf("Operation %s completed (%d ok, %d failed)", msg.report.Op, msg.report.OK, msg.report.Failed)
			m.errorMessage = ""
		}
		return m, m.fetchStatusCmd()

	case tea.KeyMsg:
		if m.executing {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
		}

		if m.confirmOpen {
			switch msg.String() {
			case "y", "Y", "enter":
				m.confirmOpen = false
				return m, m.startExecution(m.pendingAction)
			case "n", "N", "esc", "q":
				m.confirmOpen = false
				m.pendingPlan = nil
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "ctrl+t", "t", " ":
			action, ok := m.nextAction()
			if !ok {
				m.errorMessage = "Status not loaded yet"
				return m, nil
			}
			m.errorMessage = ""
			m.infoMessage = ""
			return m, m.fetchPlanCmd(action)

		case "r", "u":
			return m, m.fetchStatusCmd()

		case "up", "k":
			if m.view == viewDetail {
				if m.detailCursor > 0 {
					m.detailCursor--
				}
			} else if m.cursor > 0 {
				m.cursor--
			}
			return m, nil

		case "down", "j":
			if m.view == viewDetail {
				max := 0
				if m.status != nil {
					max = len(m.status.Resources) - 1
				}
				if m.detailCursor < max {
					m.detailCursor++
				}
			} else if m.cursor < len(switchDefs)-1 {
				m.cursor++
			}
			return m, nil

		case "enter":
			if m.view == viewList {
				m.view = viewDetail
				m.detailCursor = 0
			}
			return m, nil

		case "esc":
			if m.view == viewDetail {
				m.view = viewList
			}
			return m, nil
		}
	}

	return m, nil
}
