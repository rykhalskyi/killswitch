package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/rykhalskyi/killswitch/internal/engine"
)

func (m Model) renderList(bodyHeight int) string {
	usable := m.contentWidth()
	widths := allocWidths(usable-3, []int{32, 37, 15, 16})

	colHeader := cell("NAME", widths[0]) + " " +
		cell("DESCRIPTION", widths[1]) + " " +
		cell("CLUSTER", widths[2]) + " " +
		cellRight("SWITCH", widths[3])

	sw := m.switchState()
	clusterLabel, clusterStyle := m.clusterState()

	rows := make([]string, 0, len(switchDefs))
	for i, def := range switchDefs {
		marker := "  "
		if i == m.cursor {
			marker = "▸ "
		}

		name := cell(marker+def.name, widths[0])
		desc := cell(def.description, widths[1])
		cluster := cell(clusterLabel, widths[2])
		state := cellRight(sw.label, widths[3])

		if i == m.cursor {
			rows = append(rows, selectedRowStyle.Render(name+" "+desc+" "+cluster+" "+state))
		} else {
			rows = append(rows, nameStyle.Render(name)+" "+dimStyle.Render(desc)+" "+clusterStyle.Render(cluster)+" "+sw.style.Render(state))
		}
	}

	innerH := bodyHeight - 2
	maxRows := innerH - 6
	if maxRows < 1 {
		maxRows = 1
	}
	visible, _ := windowRows(rows, m.cursor, maxRows)

	title := m.breadcrumb()
	right := "connecting…"
	if m.status != nil {
		ctx := m.status.Context
		if ctx == "" {
			ctx = "cluster"
		}
		right = ctx + " · " + clusterLabel
	}

	return m.renderPanel(title, right, colHeader, visible, bodyHeight)
}

type switchState struct {
	label string
	style lipgloss.Style
	on    bool
	known bool
}

// switchState is the switch position itself: ON while reconciliation is
// active, OFF only when every reachable target is fully frozen.
func (m Model) switchState() switchState {
	if m.status == nil || len(m.status.Resources) == 0 {
		return switchState{label: "… LOADING", style: dimStyle}
	}

	existing, suspended := 0, 0
	for _, res := range m.status.Resources {
		if !res.Exists {
			continue
		}
		existing++
		if res.CurrentSuspended {
			suspended++
		}
	}

	if existing == 0 {
		return switchState{label: "? UNKNOWN", style: dimStyle}
	}
	if suspended == existing {
		return switchState{label: "○ OFF", style: offStyle, known: true}
	}
	return switchState{label: "● ON", style: onStyle, on: true, known: true}
}

// clusterState is the observed condition of the Flux resources. It is shown
// separately from the switch position so MIXED/DRIFT never masquerade as a
// switch state.
func (m Model) clusterState() (string, lipgloss.Style) {
	if m.status == nil || len(m.status.Resources) == 0 {
		return "… LOADING", dimStyle
	}

	existing, suspended, missing := 0, 0, 0
	drifted := false
	for _, res := range m.status.Resources {
		if !res.Exists {
			missing++
			continue
		}
		existing++
		if res.CurrentSuspended {
			suspended++
		}
		if res.OriginalSuspended != nil && res.CurrentSuspended != *res.OriginalSuspended {
			drifted = true
		}
	}

	switch {
	case existing == 0 || missing > 0:
		return "MISSING", errorStyle
	case suspended == existing:
		return "SUSPENDED", offStyle
	case suspended == 0 && drifted:
		return "DRIFT", mixedStyle
	case suspended == 0:
		return "RESTORED", onStyle
	default:
		return "MIXED", mixedStyle
	}
}

func (m Model) nextAction() (engine.Action, bool) {
	st := m.switchState()
	if !st.known {
		return "", false
	}
	if st.on {
		return engine.ActionSuspend, true
	}
	return engine.ActionRestore, true
}
