package tui

import "strings"

// breadcrumb returns the navigation path to the current view so the user can
// see where they are relative to the main switch list.
func (m Model) breadcrumb() string {
	parts := []string{"Flux Controls"}
	switch m.view {
	case viewDetail:
		if name := m.switchName(m.cursor); name != "" {
			parts = append(parts, name)
		}
		parts = append(parts, "Properties")
	case viewHistory, viewHistoryDetail, viewHistoryLog:
		if m.historyReturn == viewDetail {
			if name := m.switchName(m.cursor); name != "" {
				parts = append(parts, name)
			}
		}
		parts = append(parts, "Operation History")
		if m.view == viewHistoryDetail || m.view == viewHistoryLog {
			op := m.historyLogOp
			if m.view == viewHistoryDetail {
				if item := m.selectedHistory(); item != nil {
					op = item.Op
				}
			}
			if op != "" {
				parts = append(parts, shortOpID(op))
			}
		}
		if m.view == viewHistoryLog {
			parts = append(parts, "Journal")
		}
	case viewYAML:
		if name := m.switchName(m.cursor); name != "" {
			parts = append(parts, name)
		}
		parts = append(parts, "YAML")
	}
	return strings.Join(parts, " › ")
}

func (m Model) switchName(idx int) string {
	defs := m.switchDefs()
	if idx < 0 || idx >= len(defs) {
		return ""
	}
	return defs[idx].name
}
