package tui

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/rykhalskyi/killswitch/internal/config"
)

// switchYAMLDoc is the effective configuration of one switch: its entry from
// switches.yaml plus the workflow it references.
type switchYAMLDoc struct {
	ID          string           `yaml:"id"`
	Name        string           `yaml:"name"`
	Description string           `yaml:"description,omitempty"`
	Icon        string           `yaml:"icon,omitempty"`
	Workflow    string           `yaml:"workflow"`
	Requires    *config.Requires `yaml:"requires,omitempty"`
	Steps       []config.Step    `yaml:"steps,omitempty"`
	OnError     string           `yaml:"on_error,omitempty"`
}

// openSwitchYAML loads the YAML view for the switch at the cursor.
func (m *Model) openSwitchYAML() {
	def := m.selectedSwitch()
	m.yamlLines = m.switchYAMLLines(def)
	m.yamlScroll = 0
	m.view = viewYAML
}

func (m Model) switchYAMLLines(def switchDef) []string {
	doc := switchYAMLDoc{
		ID:          def.id,
		Name:        def.name,
		Description: def.description,
		Icon:        def.icon,
		Workflow:    def.workflow,
	}
	if m.eng != nil {
		if cfg := m.eng.Config(); cfg != nil {
			if wf := cfg.Workflows[def.workflow]; wf != nil {
				if wf.Requires.Flux != "" {
					req := wf.Requires
					doc.Requires = &req
				}
				doc.Steps = wf.Steps
				doc.OnError = wf.OnError
			}
		}
	}

	data, err := yaml.Marshal(doc)
	if err != nil {
		return []string{fmt.Sprintf("error rendering YAML: %v", err)}
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

func (m Model) renderYAML(bodyHeight int) string {
	if len(m.yamlLines) == 0 {
		return m.renderPanel(m.breadcrumb(), "", "", []string{"No configuration available"}, bodyHeight)
	}

	innerH := bodyHeight - 2
	maxRows := innerH - 4
	if maxRows < 1 {
		maxRows = 1
	}

	visible, start := windowRows(m.yamlLines, m.yamlScroll, maxRows)

	title := m.breadcrumb()
	right := fmt.Sprintf("%d lines", len(m.yamlLines))
	if len(m.yamlLines) > len(visible) {
		title = fmt.Sprintf("%s  (%d-%d of %d)", title, start+1, start+len(visible), len(m.yamlLines))
	}

	return m.renderPanel(title, right, "", visible, bodyHeight)
}
