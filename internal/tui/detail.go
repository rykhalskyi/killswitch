package tui

import (
	"fmt"
	"strings"

	"github.com/rykhalskyi/killswitch/internal/engine"
)

func (m Model) renderDetail(bodyHeight int) string {
	if st, ok := m.selectedSwitchStatus(); ok && len(st.Steps) > 0 {
		return m.renderStepDetail(bodyHeight, st)
	}

	var resources []engine.ResourceStatusItem
	if m.status != nil {
		resources = m.status.Resources
	}
	if m.detailCursor >= len(resources) {
		m.detailCursor = len(resources) - 1
	}
	if m.detailCursor < 0 {
		m.detailCursor = 0
	}

	usable := m.contentWidth()
	widths := allocWidths(usable-5, []int{15, 18, 24, 14, 13, 16})
	colHeader := cell("KIND", widths[0]) + " " +
		cell("NAMESPACE", widths[1]) + " " +
		cell("NAME", widths[2]) + " " +
		cell("STATUS", widths[3]) + " " +
		cell("ORIGINAL", widths[4]) + " " +
		cell("READY", widths[5])

	rows := make([]string, 0, len(resources))
	for i, res := range resources {
		row := resourceRow(res, widths)
		if i == m.detailCursor {
			row = selectedRowStyle.Render(row)
		}
		rows = append(rows, row)
	}

	innerH := bodyHeight - 2
	maxRows := innerH - 6
	if maxRows < 1 {
		maxRows = 1
	}
	visible, start := windowRows(rows, m.detailCursor, maxRows)

	clusterLabel, _ := m.clusterState()
	title := m.breadcrumb()
	right := fmt.Sprintf("%d items", len(resources))
	if m.status != nil {
		right = clusterLabel + " · " + right
	}
	if len(resources) > len(visible) {
		title = fmt.Sprintf("%s  (%d-%d of %d)", title, start+1, start+len(visible), len(resources))
	}

	return m.renderPanel(title, right, colHeader, visible, bodyHeight)
}

// selectedSwitchStatus returns the live status of the selected switch.
func (m Model) selectedSwitchStatus() (engine.SwitchStatus, bool) {
	if m.status == nil {
		return engine.SwitchStatus{}, false
	}
	def := m.selectedSwitch()
	for _, st := range m.status.Switches {
		if st.ID == def.id {
			return st, true
		}
	}
	return engine.SwitchStatus{}, false
}

func (m Model) renderStepDetail(bodyHeight int, st engine.SwitchStatus) string {
	steps := st.Steps
	if m.detailCursor >= len(steps) {
		m.detailCursor = len(steps) - 1
	}
	if m.detailCursor < 0 {
		m.detailCursor = 0
	}

	usable := m.contentWidth()
	widths := allocWidths(usable-5, []int{14, 18, 24, 10, 12, 12})
	colHeader := cell("STEP", widths[0]) + " " +
		cell("ACTION", widths[1]) + " " +
		cell("TARGET", widths[2]) + " " +
		cell("STATE", widths[3]) + " " +
		cell("OBSERVED", widths[4]) + " " +
		cell("ORIGINAL", widths[5])

	rows := make([]string, 0, len(steps))
	for i, step := range steps {
		observed := fmt.Sprintf("%v", step.Observed)
		if !step.Exists {
			observed = "MISSING"
		}
		row := strings.Join([]string{
			cell(step.Step, widths[0]),
			cell(step.Action, widths[1]),
			cell(step.Target.Kind+"/"+step.Target.Name, widths[2]),
			cell(step.State, widths[3]),
			cell(observed, widths[4]),
			cell(fmt.Sprintf("%v", step.Original), widths[5]),
		}, " ")
		if i == m.detailCursor {
			row = selectedRowStyle.Render(row)
		}
		rows = append(rows, row)
	}

	innerH := bodyHeight - 2
	maxRows := innerH - 6
	if maxRows < 1 {
		maxRows = 1
	}
	visible, start := windowRows(rows, m.detailCursor, maxRows)

	title := m.breadcrumb()
	right := fmt.Sprintf("%d steps", len(steps))
	if len(steps) > len(visible) {
		title = fmt.Sprintf("%s  (%d-%d of %d)", title, start+1, start+len(visible), len(steps))
	}
	return m.renderPanel(title, right, colHeader, visible, bodyHeight)
}

func resourceRow(res engine.ResourceStatusItem, widths []int) string {
	status := "ACTIVE"
	if !res.Exists {
		status = "MISSING"
	} else if res.CurrentSuspended {
		status = "SUSPENDED"
	}

	original := "-"
	if res.OriginalSuspended != nil {
		if *res.OriginalSuspended {
			original = "SUSPENDED"
		} else {
			original = "ACTIVE"
		}
	}

	ready := res.Ready
	if res.LaggingObservation {
		ready += " (!)"
	}

	cells := []string{
		cell(res.Kind, widths[0]),
		cell(res.Namespace, widths[1]),
		cell(res.Name, widths[2]),
		cell(status, widths[3]),
		cell(original, widths[4]),
		cell(ready, widths[5]),
	}
	return strings.Join(cells, " ")
}
