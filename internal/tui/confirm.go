package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rykhalskyi/killswitch/internal/engine"
)

func (m Model) renderProgress(bodyHeight int) string {
	action := strings.ToUpper(string(m.pendingAction))
	line := cell("⏳ "+m.execProgress, m.contentWidth())
	return m.renderPanel("EXECUTING "+action, "in progress", "", []string{line}, bodyHeight)
}

func (m Model) renderConfirm(bodyHeight int) string {
	plan := m.pendingPlan

	modalWidth := m.width * 3 / 4
	if modalWidth > 84 {
		modalWidth = 84
	}
	if modalWidth < 44 {
		modalWidth = m.width - 2
	}
	if modalWidth > m.width {
		modalWidth = m.width
	}
	contentW := modalWidth - 6
	if contentW > m.width-6 {
		contentW = m.width - 6
	}
	if contentW < 4 {
		contentW = 4
	}
	maxLines := bodyHeight - 4
	if maxLines < 1 {
		msg := truncateText("Confirm switch change? [y] yes / [n] no", m.width)
		return lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center, msg)
	}

	sw := switchDefs[m.cursor]
	current, target := "ON", "OFF"
	if m.pendingAction == engine.ActionRestore {
		current, target = "OFF", "ON"
	}

	lines := []string{
		modalTitleStyle.Render("CONFIRM SWITCH CHANGE"),
		"",
		cell(sw.name+": "+current+" → "+target, contentW),
		cell(fmt.Sprintf("Action: %s · %d resources", m.pendingAction, len(plan.Items)), contentW),
		"",
	}

	// Reserve a line for a possible overflow/warning notice, plus a blank line and the footer.
	available := maxLines - len(lines) - 3
	if available < 1 {
		available = 1
	}
	show := len(plan.Items)
	if show > available {
		show = available
	}
	for i := 0; i < show; i++ {
		item := plan.Items[i]
		targetState := "ACTIVE"
		if item.TargetSuspended {
			targetState = "SUSPENDED"
		}
		lines = append(lines, cell(fmt.Sprintf("%2d. %s/%s → %s", i+1, item.Resource.Kind, item.Resource.Name, targetState), contentW))
	}

	footer := "[y/enter] Confirm    [n/esc] Cancel"
	if len(plan.Conflicts) > 0 {
		footer = "[y/enter] Confirm    [f] Force override    [n/esc] Cancel"
	}

	if len(plan.Items) > show {
		lines = append(lines, cell(fmt.Sprintf("… and %d more", len(plan.Items)-show), contentW))
	} else if len(plan.Conflicts) > 0 {
		reason := plan.Conflicts[0].ConflictReason
		if reason == "" {
			reason = "conflicts detected — execution may fail"
		}
		lines = append(lines, modalWarnStyle.Render(cell("⚠ "+reason, contentW)))
		if len(plan.Conflicts) > 1 {
			lines = append(lines, modalWarnStyle.Render(cell(fmt.Sprintf("⚠ and %d more conflicts", len(plan.Conflicts)-1), contentW)))
		}
	}

	lines = append(lines, "")
	lines = append(lines, cell(footer, contentW))

	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = cell(footer, contentW)
	}

	return lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center, modalBox.Render(strings.Join(lines, "\n")))
}
