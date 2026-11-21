package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/rykhalskyi/killswitch/internal/engine"
)

// switchIconWidth is the fixed slot reserved for a switch's optional icon so
// rows stay aligned whether or not an icon is configured.
const switchIconWidth = 2

// switchNameText builds the name column with a fixed-width icon slot.
func switchNameText(marker, icon, name string) string {
	return marker + fitVisible(icon, switchIconWidth) + " " + name
}

func (m Model) renderList(bodyHeight int) string {
	usable := m.contentWidth()
	widths := allocWidths(usable-3, []int{32, 37, 15, 16})

	colHeader := cell("NAME", widths[0]) + " " +
		cell("DESCRIPTION", widths[1]) + " " +
		cell("CLUSTER", widths[2]) + " " +
		cellRight("SWITCH", widths[3])

	clusterLabel, _ := m.clusterState()
	defs := m.switchDefs()

	rows := make([]string, 0, len(defs))
	for i, def := range defs {
		marker := "  "
		if i == m.cursor {
			marker = "▸ "
		}
		sw := m.switchStateAt(i)
		isFlux := def.id == m.fluxSwitchID()
		rowCluster, clusterStyle := clusterLabel, dimStyle
		if isFlux {
			rowCluster, clusterStyle = m.clusterState()
		} else {
			rowCluster = "-"
		}

		nameText := switchNameText(marker, def.icon, def.name)
		nameStyleFor := nameStyle
		if isFlux {
			nameStyleFor = fluxNameStyle
		}
		name := cell(nameText, widths[0])
		desc := cell(def.description, widths[1])
		cluster := cell(rowCluster, widths[2])
		state := cellRight(sw.label, widths[3])

		if i == m.cursor {
			rows = append(rows, selectedRowStyle.Render(name+" "+desc+" "+cluster+" "+state))
		} else {
			rows = append(rows, nameStyleFor.Render(name)+" "+dimStyle.Render(desc)+" "+clusterStyle.Render(cluster)+" "+sw.style.Render(state))
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
	_, dir, ok := m.nextTransition()
	if !ok {
		return "", false
	}
	return engine.LegacyAction(dir), true
}

func (m Model) nextTransition() (string, engine.Direction, bool) {
	defs := m.switchDefs()
	idx := m.cursor
	if idx < 0 || idx >= len(defs) {
		return "", "", false
	}
	st := m.switchStateAt(idx)
	if !st.known {
		return "", "", false
	}
	dir := engine.DirectionOn
	if st.on {
		dir = engine.DirectionOff
	}
	return defs[idx].id, dir, true
}

func (m Model) fluxSwitchID() string {
	if m.eng != nil {
		if id := m.eng.FluxSwitchID(); id != "" {
			return id
		}
	}
	return "flux"
}

// switchStateAt derives the position of one switch row.
func (m Model) switchStateAt(idx int) switchState {
	defs := m.switchDefs()
	if idx < 0 || idx >= len(defs) || m.status == nil {
		return switchState{label: "… LOADING", style: dimStyle}
	}
	def := defs[idx]
	for _, st := range m.status.Switches {
		if st.ID == def.id {
			return mapSwitchState(st.State)
		}
	}
	if def.id == m.fluxSwitchID() && len(m.status.Resources) > 0 {
		return m.switchState()
	}
	return switchState{label: "? UNKNOWN", style: dimStyle}
}

func mapSwitchState(st engine.SwitchState) switchState {
	switch st {
	case engine.SwitchOn:
		return switchState{label: "● ON", style: onStyle, on: true, known: true}
	case engine.SwitchOff:
		return switchState{label: "○ OFF", style: offStyle, known: true}
	case engine.SwitchMixed:
		return switchState{label: "◐ MIXED", style: mixedStyle, known: true}
	case engine.SwitchDrift:
		return switchState{label: "⚠ DRIFT", style: mixedStyle, known: true}
	case engine.SwitchMissing:
		return switchState{label: "✕ MISSING", style: errorStyle, known: true}
	default:
		return switchState{label: "? UNKNOWN", style: dimStyle}
	}
}
