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
	case viewHistory:
		if m.historyReturn == viewDetail {
			if name := m.switchName(m.cursor); name != "" {
				parts = append(parts, name)
			}
		}
		parts = append(parts, "Operation History")
	}
	return strings.Join(parts, " › ")
}

func (m Model) switchName(idx int) string {
	if idx < 0 || idx >= len(switchDefs) {
		return ""
	}
	return switchDefs[idx].name
}
