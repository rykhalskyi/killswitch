package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rykhalskyi/killswitch/internal/config"
	"github.com/rykhalskyi/killswitch/internal/engine"
	"github.com/rykhalskyi/killswitch/internal/version"
)

// Colors follow a k9s / flux9s inspired palette.
var (
	accent      = lipgloss.Color("#A78BFA")
	accentDark  = lipgloss.Color("#5A56E0")
	muted       = lipgloss.Color("#9CA3AF")
	borderColor = lipgloss.Color("#4B5563")
	green       = lipgloss.Color("#10B981")
	red         = lipgloss.Color("#EF4444")
	yellow      = lipgloss.Color("#F59E0B")
	cyan        = lipgloss.Color("#22D3EE")

	bannerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accent)

	killWordStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(red)

	switchWordStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#F9FAFB"))

	logoRedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(red)

	logoWhiteStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#E5E7EB"))

	subtitleStyle = lipgloss.NewStyle().
			Foreground(muted)

	versionStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(cyan)

	panelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(cyan)

	colHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accent)

	borderStyle = lipgloss.NewStyle().
			Foreground(borderColor)

	selectedRowStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(accentDark)

	nameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F9FAFB"))

	dimStyle = lipgloss.NewStyle().
			Foreground(muted)

	onStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(green)

	offStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(red)

	mixedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(yellow)

	errorStyle = lipgloss.NewStyle().
			Foreground(red)

	infoStyle = lipgloss.NewStyle().
			Foreground(green)

	footerStyle = lipgloss.NewStyle().
			Foreground(muted)

	modalBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(1, 2)

	modalTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accent)

	modalWarnStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(yellow)
)

type viewMode int

const (
	viewList viewMode = iota
	viewDetail
)

// switchDef describes one top-level switch in the main table.
// Phase 1 only ships the Flux reconciliation switch; more can be appended later.
type switchDef struct {
	name        string
	description string
}

var switchDefs = []switchDef{
	{
		name:        "Flux Reconciliation",
		description: "Suspend / restore all configured Flux Kustomizations and HelmReleases",
	},
}

type statusMsg struct {
	report *engine.StatusReport
	err    error
}

type planMsg struct {
	action engine.Action
	plan   *engine.Plan
	err    error
}

type execDoneMsg struct {
	report *engine.ExecutionReport
	err    error
}

type progressMsg struct {
	step   int
	total  int
	res    config.Resource
	status string
	err    error
}

type Model struct {
	eng    *engine.Engine
	status *engine.StatusReport

	// layout
	width  int
	height int

	// navigation
	view         viewMode
	cursor       int
	detailCursor int

	// confirmation / execution
	pendingPlan   *engine.Plan
	pendingAction engine.Action
	confirmOpen   bool
	executing     bool
	execProgress  string
	msgChan       chan tea.Msg

	errorMessage string
	infoMessage  string
}

func NewModel(eng *engine.Engine) Model {
	return Model{
		eng: eng,
	}
}

func (m Model) Init() tea.Cmd {
	return m.fetchStatusCmd()
}

func (m Model) fetchStatusCmd() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		rep, err := m.eng.Status(ctx)
		return statusMsg{report: rep, err: err}
	}
}

func (m Model) fetchPlanCmd(action engine.Action) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		plan, err := m.eng.BuildPlan(ctx, action)
		return planMsg{action: action, plan: plan, err: err}
	}
}

func waitForProgress(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *Model) startExecution(action engine.Action) tea.Cmd {
	ch := make(chan tea.Msg, 64)
	m.executing = true
	m.msgChan = ch
	m.execProgress = fmt.Sprintf("Starting %s...", action)

	go func() {
		defer close(ch)
		ctx := context.Background()
		report, err := m.eng.Execute(ctx, action, false, func(step int, total int, res config.Resource, status string, execErr error) {
			ch <- progressMsg{step: step, total: total, res: res, status: status, err: execErr}
		})
		ch <- execDoneMsg{report: report, err: err}
	}()

	return waitForProgress(ch)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case statusMsg:
		if msg.err != nil {
			m.errorMessage = fmt.Sprintf("Error fetching status: %v", msg.err)
		} else {
			m.status = msg.report
			m.errorMessage = ""
			if m.status != nil && m.detailCursor >= len(m.status.Resources) {
				m.detailCursor = len(m.status.Resources) - 1
				if m.detailCursor < 0 {
					m.detailCursor = 0
				}
			}
		}
		return m, nil

	case planMsg:
		if msg.err != nil {
			m.errorMessage = fmt.Sprintf("Error building plan: %v", msg.err)
			m.confirmOpen = false
			return m, nil
		}
		m.pendingPlan = msg.plan
		m.pendingAction = msg.action
		m.confirmOpen = true
		return m, nil

	case progressMsg:
		if msg.err != nil {
			m.execProgress = fmt.Sprintf("[%d/%d] %s (FAILED: %v)", msg.step, msg.total, msg.res.Name, msg.err)
		} else {
			m.execProgress = fmt.Sprintf("[%d/%d] %s (OK)", msg.step, msg.total, msg.res.Name)
		}
		return m, waitForProgress(m.msgChan)

	case execDoneMsg:
		m.executing = false
		m.msgChan = nil
		m.confirmOpen = false
		m.pendingPlan = nil
		if msg.err != nil {
			m.errorMessage = fmt.Sprintf("Execution error: %v", msg.err)
			m.infoMessage = ""
		} else {
			m.infoMessage = fmt.Sprintf("Operation %s completed (%d ok, %d failed)", msg.report.Op, msg.report.OK, msg.report.Failed)
			m.errorMessage = ""
		}
		return m, m.fetchStatusCmd()

	case tea.KeyMsg:
		if m.executing {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
		}

		if m.confirmOpen {
			switch msg.String() {
			case "y", "Y", "enter":
				m.confirmOpen = false
				return m, m.startExecution(m.pendingAction)
			case "n", "N", "esc", "q":
				m.confirmOpen = false
				m.pendingPlan = nil
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "ctrl+t", "t", " ":
			action, ok := m.nextAction()
			if !ok {
				m.errorMessage = "Status not loaded yet"
				return m, nil
			}
			m.errorMessage = ""
			m.infoMessage = ""
			return m, m.fetchPlanCmd(action)

		case "r", "u":
			return m, m.fetchStatusCmd()

		case "up", "k":
			if m.view == viewDetail {
				if m.detailCursor > 0 {
					m.detailCursor--
				}
			} else if m.cursor > 0 {
				m.cursor--
			}
			return m, nil

		case "down", "j":
			if m.view == viewDetail {
				max := 0
				if m.status != nil {
					max = len(m.status.Resources) - 1
				}
				if m.detailCursor < max {
					m.detailCursor++
				}
			} else if m.cursor < len(switchDefs)-1 {
				m.cursor++
			}
			return m, nil

		case "enter":
			if m.view == viewList {
				m.view = viewDetail
				m.detailCursor = 0
			}
			return m, nil

		case "esc":
			if m.view == viewDetail {
				m.view = viewList
			}
			return m, nil
		}
	}

	return m, nil
}

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
	case m.confirmOpen && m.pendingPlan != nil:
		body = m.renderConfirm(bodyHeight)
	case m.executing:
		body = m.renderProgress(bodyHeight)
	case m.view == viewDetail:
		body = m.renderDetail(bodyHeight)
	default:
		body = m.renderList(bodyHeight)
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

func (m Model) renderHeader(height int) string {
	word := styledWordmark()
	wordWidth := lipgloss.Width(word[0])

	logo := styledLogo()
	logoWidth := lipgloss.Width(logo[0])
	combinedWidth := wordWidth + 2 + logoWidth

	verText := version.Display()
	verWidth := 2 + lipgloss.Width(verText)
	verSuffix := "  " + versionStyle.Render(verText)

	needForWord := wordWidth
	needForLogo := combinedWidth
	if height < 5 {
		// No subtitle row: the version shares the last wordmark row.
		needForWord += verWidth
		needForLogo += verWidth
	}

	// The block wordmark needs four rows. Below that, use a compact one-liner.
	if height < 4 || m.width < needForWord {
		title := truncateText("KILL SWITCH  "+verText, m.width)
		return lipgloss.Place(m.width, height, lipgloss.Left, lipgloss.Center, bannerStyle.Render(title))
	}

	lines := make([]string, 0, 5)
	if m.width >= needForLogo {
		for i := 0; i < 4; i++ {
			lines = append(lines, word[i]+"  "+logo[i])
		}
	} else {
		lines = append(lines, word...)
	}

	if height >= 5 {
		subtitle := subtitleStyle.Render("FLUX RECONCILIATION CONTROLLER") + "  ·  " + versionStyle.Render(verText)
		lines = append(lines, subtitle)
	} else {
		lines[len(lines)-1] += verSuffix
	}

	return lipgloss.Place(m.width, height, lipgloss.Left, lipgloss.Center, strings.Join(lines, "\n"))
}

// styledWordmark renders "KILL SWITCH" as block letters, red for KILL and
// white for SWITCH, mirroring the project logo.
func styledWordmark() []string {
	kill := renderBanner("KILL")
	sw := renderBanner("SWITCH")
	lines := make([]string, 4)
	for i := 0; i < 4; i++ {
		lines[i] = killWordStyle.Render(kill[i]) + "  " + switchWordStyle.Render(sw[i])
	}
	return lines
}

// styledLogo renders a compact pseudographic version of the kill-switch logo:
// a vertical toggle with red rays and a red "on" knob.
func styledLogo() []string {
	return []string{
		logoRedStyle.Render(`  \│/    `),
		logoWhiteStyle.Render(`╭─────╮  `),
		logoWhiteStyle.Render(`│`) + logoRedStyle.Render(`█████`) + logoWhiteStyle.Render(`│  `),
		logoWhiteStyle.Render(`╰─────╯  `),
	}
}

// renderBanner draws a 4-row block "pseudographic" banner.
func renderBanner(text string) []string {
	lines := make([]string, 4)
	for i, r := range text {
		glyph, ok := banner4[r]
		if !ok {
			glyph = banner4[' ']
		}
		for row := 0; row < 4; row++ {
			if i > 0 {
				lines[row] += " "
			}
			lines[row] += glyph[row]
		}
	}
	return lines
}

var banner4 = map[rune][]string{
	'K': {"█  █", "██  ", "█ █ ", "█  █"},
	'I': {"███", " █ ", " █ ", "███"},
	'L': {"█  ", "█  ", "█  ", "███"},
	'S': {"████", "███ ", "   █", "████"},
	'W': {"█   █", "█ █ █", "██ ██", "█   █"},
	'T': {"███", " █ ", " █ ", " █ "},
	'C': {"████", "█   ", "█   ", "████"},
	'H': {"█  █", "████", "█  █", "█  █"},
	' ': {"  ", "  ", "  ", "  "},
}

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

	title := "FLUX CONTROLS"
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

	if len(plan.Items) > show {
		lines = append(lines, cell(fmt.Sprintf("… and %d more", len(plan.Items)-show), contentW))
	} else if len(plan.Conflicts) > 0 {
		lines = append(lines, modalWarnStyle.Render(cell("⚠ conflicts detected — execution may fail", contentW)))
	}

	lines = append(lines, "")
	lines = append(lines, cell("[y/enter] Confirm    [n/esc] Cancel", contentW))

	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = cell("[y/enter] Confirm    [n/esc] Cancel", contentW)
	}

	return lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center, modalBox.Render(strings.Join(lines, "\n")))
}

func (m Model) renderPanel(title, right, colHeader string, rows []string, bodyHeight int) string {
	if m.width < 6 {
		return lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center, "terminal too small")
	}

	usable := m.contentWidth()
	innerH := bodyHeight - 2
	if innerH < 1 {
		innerH = 1
	}

	chrome := 4 // title, blank, status, footer
	if colHeader != "" {
		chrome += 2 // column header + separator
	}
	maxRows := innerH - chrome
	if maxRows < 0 {
		maxRows = 0
	}
	if len(rows) > maxRows {
		rows = rows[:maxRows]
	}

	lines := make([]string, 0, innerH)
	lines = append(lines, " "+panelTitleStyle.Render(padBetween(title, right, usable))+" ")
	lines = append(lines, strings.Repeat(" ", usable+2))
	if colHeader != "" {
		lines = append(lines, " "+colHeaderStyle.Render(cell(colHeader, usable))+" ")
		lines = append(lines, " "+borderStyle.Render(strings.Repeat("─", usable))+" ")
	}
	for _, row := range rows {
		lines = append(lines, " "+row+" ")
	}

	// Keep the status line and key help pinned to the bottom of the panel.
	for len(lines) < innerH-2 {
		lines = append(lines, strings.Repeat(" ", usable+2))
	}

	statusText, statusStyle := m.panelStatus()
	lines = append(lines, " "+statusStyle.Render(cell(statusText, usable))+" ")
	lines = append(lines, " "+footerStyle.Render(cell(m.footerText(), usable))+" ")

	if len(lines) > innerH {
		tail := 2
		if innerH <= tail {
			lines = lines[:innerH]
		} else {
			lines = append(lines[:innerH-tail], lines[len(lines)-tail:]...)
		}
	}
	for len(lines) < innerH {
		lines = append(lines, strings.Repeat(" ", usable+2))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor)

	return box.Render(strings.Join(lines, "\n"))
}

func (m Model) contentWidth() int {
	w := m.width - 4
	if w < 1 {
		w = 1
	}
	return w
}

func (m Model) panelStatus() (string, lipgloss.Style) {
	if m.errorMessage != "" {
		return "Error: " + m.errorMessage, errorStyle
	}
	if m.infoMessage != "" {
		return m.infoMessage, infoStyle
	}
	return "", footerStyle
}

func (m Model) footerText() string {
	if m.view == viewDetail {
		return "[↑/↓] scroll   [esc] back   [ctrl+t] toggle   [r] refresh   [q] quit"
	}
	return "[↑/↓] select   [enter] inspect   [ctrl+t] toggle   [r] refresh   [q] quit"
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

func windowRows(rows []string, cursor, maxVisible int) ([]string, int) {
	if maxVisible <= 0 || len(rows) == 0 {
		return nil, 0
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	if len(rows) <= maxVisible {
		return rows, 0
	}
	start := cursor - maxVisible + 1
	if start < 0 {
		start = 0
	}
	end := start + maxVisible
	if end > len(rows) {
		end = len(rows)
		start = end - maxVisible
	}
	return rows[start:end], start
}

func allocWidths(total int, weights []int) []int {
	n := len(weights)
	widths := make([]int, n)
	if n == 0 {
		return widths
	}
	sum := 0
	for _, w := range weights {
		sum += w
	}
	if sum <= 0 {
		sum = n
	}

	assigned := 0
	for i := 0; i < n; i++ {
		if i == n-1 {
			widths[i] = total - assigned
		} else {
			widths[i] = total * weights[i] / sum
		}
		if widths[i] < 1 {
			widths[i] = 1
		}
		assigned += widths[i]
	}

	for assigned > total {
		maxIdx := 0
		for i := range widths {
			if widths[i] > widths[maxIdx] {
				maxIdx = i
			}
		}
		if widths[maxIdx] <= 1 {
			break
		}
		widths[maxIdx]--
		assigned--
	}
	if assigned < total {
		widths[n-1] += total - assigned
	}
	return widths
}

func padBetween(left, right string, width int) string {
	left = truncateText(left, width)
	right = truncateText(right, width)
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	if lw+rw+1 > width {
		return cell(left, width)
	}
	return left + strings.Repeat(" ", width-lw-rw) + right
}

func cell(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = truncateText(s, width)
	if pad := width - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

func cellRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = truncateText(s, width)
	if pad := width - lipgloss.Width(s); pad > 0 {
		s = strings.Repeat(" ", pad) + s
	}
	return s
}

func truncateText(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	r := []rune(s)
	for len(r) > 0 {
		r = r[:len(r)-1]
		if lipgloss.Width(string(r))+1 <= width {
			break
		}
	}
	if width == 1 {
		return "…"
	}
	return string(r) + "…"
}

// Run launches the Bubble Tea TUI program.
func Run(eng *engine.Engine) error {
	p := tea.NewProgram(NewModel(eng), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
