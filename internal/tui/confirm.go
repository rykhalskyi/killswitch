package tui

import (
	"fmt"
	"strings"

	"github.com/rykhalskyi/killswitch/internal/engine"
)

func (m Model) dialogWidth() int {
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
	if modalWidth < 1 {
		modalWidth = 1
	}
	return modalWidth
}

func (m Model) dialogContentWidth(modalWidth int) int {
	contentW := modalWidth - 6
	if contentW > m.width-6 {
		contentW = m.width - 6
	}
	if contentW < 4 {
		contentW = 4
	}
	return contentW
}

func (m Model) renderPlanning(bodyHeight int) string {
	if bodyHeight < 6 {
		return truncateText("Building switch plan...", m.width)
	}
	modalWidth := m.dialogWidth()
	contentW := m.dialogContentWidth(modalWidth)

	target := m.selectedSwitch().name
	if m.planSwitch != "" {
		target = m.planSwitch
		for _, def := range m.switchDefs() {
			if def.id == m.planSwitch {
				target = def.name
				break
			}
		}
	}
	dir := strings.ToUpper(string(m.planDir))
	if dir == "" {
		dir = "SWITCH"
	}

	lines := []string{
		modalTitleStyle.Render(cell("BUILDING SWITCH PLAN", contentW)),
		"",
		cell(fmt.Sprintf("Preparing %s %s...", target, dir), contentW),
		cell("Observing live cluster state", contentW),
		"",
		cell("[ctrl+c] quit", contentW),
	}

	return modalBox.Render(strings.Join(lines, "\n"))
}

func (m Model) renderProgress(bodyHeight int) string {
	action := strings.ToUpper(string(m.pendingAction))
	if action == "" {
		action = "SWITCH"
	}
	if bodyHeight < 6 {
		return truncateText("Executing "+action+"...", m.width)
	}

	modalWidth := m.dialogWidth()
	contentW := m.dialogContentWidth(modalWidth)

	progress := m.execProgress
	if progress == "" {
		progress = "Starting..."
	}
	counter := fmt.Sprintf("Step %d/%d  ·  ok %d  ·  failed %d", m.execStep, m.execTotal, m.execOK, m.execFailed)

	lines := []string{
		modalTitleStyle.Render(cell("EXECUTING "+action, contentW)),
		"",
		cell(progress, contentW),
		cell(counter, contentW),
		"",
		cell("[ctrl+c] quit", contentW),
	}

	return modalBox.Render(strings.Join(lines, "\n"))
}

func (m Model) renderConfirm(bodyHeight int) string {
	plan := m.pendingPlan

	modalWidth := m.dialogWidth()
	contentW := m.dialogContentWidth(modalWidth)
	maxLines := bodyHeight - 4
	if maxLines < 1 {
		return truncateText("Confirm switch change? [y] yes / [n] no", m.width)
	}

	sw := m.selectedSwitch()
	current, target := "ON", "OFF"
	if m.pendingAction == engine.ActionRestore || m.pendingDir == engine.DirectionOn {
		current, target = "OFF", "ON"
	}

	lines := []string{
		modalTitleStyle.Render("CONFIRM SWITCH CHANGE"),
		"",
		cell(sw.name+": "+current+" → "+target, contentW),
		cell(fmt.Sprintf("Action: %s · %d steps", m.pendingAction, len(plan.Items)), contentW),
		"",
	}

	// Reserve lines for the overflow notice, any conflict warnings, a blank
	// line, and the footer.
	noticeLines := 1
	if len(plan.Conflicts) > 0 {
		noticeLines += 2
	}
	available := maxLines - len(lines) - 2 - noticeLines
	if available < 1 {
		available = 1
	}
	show := len(plan.Items)
	if show > available {
		show = available
	}
	for i := 0; i < show; i++ {
		item := plan.Items[i]
		targetState := fmt.Sprintf("%v", item.DesiredValue)
		if item.Action == "flux.suspend" || item.Action == "flux.resume" || item.Action == "kubernetes.suspend" {
			targetState = "ACTIVE"
			if b, ok := item.DesiredValue.(bool); ok && b {
				targetState = "SUSPENDED"
			}
		}
		label := item.Resource.Kind + "/" + item.Resource.Name
		if item.Switch != "" && item.Switch != sw.id {
			label = item.Switch + " " + label
		}
		lines = append(lines, cell(fmt.Sprintf("%2d. %s → %s", i+1, label, targetState), contentW))
	}

	footer := "[y/enter] Confirm    [n/esc] Cancel"
	if len(plan.Conflicts) > 0 {
		footer = "[y/enter] Confirm    [f] Force override    [n/esc] Cancel"
	}

	if len(plan.Items) > show {
		lines = append(lines, cell(fmt.Sprintf("… and %d more", len(plan.Items)-show), contentW))
	}
	if len(plan.Conflicts) > 0 {
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

	return modalBox.Render(strings.Join(lines, "\n"))
}
