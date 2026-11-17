package tui

import "github.com/charmbracelet/lipgloss"

func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return "Loading..."
	}
	if m.width < 12 || m.height < 6 {
		msg := truncateText("too small — resize", m.width)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	}

	headerHeight := m.headerHeight()
	bodyHeight := m.height - headerHeight

	header := m.renderHeader(headerHeight)
	if bodyHeight <= 0 {
		return header
	}
	if bodyHeight < 3 {
		msg := truncateText("too small — resize", m.width)
		return header + "\n" + lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center, msg)
	}

	var body string
	switch {
	case m.view == viewDetail:
		body = m.renderDetail(bodyHeight)
	case m.view == viewHistory:
		body = m.renderHistory(bodyHeight)
	default:
		body = m.renderList(bodyHeight)
	}

	switch {
	case m.confirmOpen && m.pendingPlan != nil:
		body = overlayCenter(body, m.renderConfirm(bodyHeight), m.width, bodyHeight)
	case m.executing:
		body = m.renderProgress(bodyHeight)
	}

	return header + "\n" + body
}

func (m Model) headerHeight() int {
	if m.height <= 0 {
		return 4
	}
	h := m.height / 5
	if h < 4 {
		h = 4
	}
	if h > m.height-4 {
		h = m.height / 5
	}
	if h < 1 {
		h = 1
	}
	return h
}
