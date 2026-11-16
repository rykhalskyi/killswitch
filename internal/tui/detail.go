package tui

import (
	"fmt"
	"strings"

	"github.com/rykhalskyi/killswitch/internal/engine"
)

func (m Model) renderDetail(bodyHeight int) string {
	sw := switchDefs[m.cursor]
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
	title := sw.name + " — properties"
	right := fmt.Sprintf("%d items", len(resources))
	if m.status != nil {
		right = clusterLabel + " · " + right
	}
	if len(resources) > len(visible) {
		title = fmt.Sprintf("%s  (%d-%d of %d)", sw.name, start+1, start+len(visible), len(resources))
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
