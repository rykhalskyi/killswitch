package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/rykhalskyi/killswitch/internal/config"
	"github.com/rykhalskyi/killswitch/internal/engine"
	"github.com/rykhalskyi/killswitch/internal/flux"
	"github.com/rykhalskyi/killswitch/internal/journal"
)

type tuiFluxClient struct {
	observed map[string]bool
}

func (tuiFluxClient) DiscoverGVRs(context.Context) error { return nil }

func (tuiFluxClient) GetGVR(string) (schema.GroupVersionResource, error) {
	return schema.GroupVersionResource{}, nil
}

func (c tuiFluxClient) GetResource(_ context.Context, kind, namespace, name string) (*flux.ResourceInfo, error) {
	suspended := c.observed[kind+"/"+namespace+"/"+name]
	return &flux.ResourceInfo{Kind: kind, Namespace: namespace, Name: name, Suspended: suspended, Ready: "True"}, nil
}

func (tuiFluxClient) PatchSuspend(_ context.Context, kind, namespace, name string, suspend bool) (*flux.ResourceInfo, error) {
	return &flux.ResourceInfo{Kind: kind, Namespace: namespace, Name: name, Suspended: suspend, Ready: "True"}, nil
}

func sampleStatus(count int) *engine.StatusReport {
	orig := false
	res := make([]engine.ResourceStatusItem, 0, count)
	for i := 0; i < count; i++ {
		kind := "Kustomization"
		if i%2 == 1 {
			kind = "HelmRelease"
		}
		res = append(res, engine.ResourceStatusItem{
			Kind:              kind,
			Namespace:         "flux-system",
			Name:              fmt.Sprintf("resource-%02d", i),
			Exists:            true,
			OriginalSuspended: &orig,
			Ready:             "True",
		})
	}
	return &engine.StatusReport{
		GlobalStatus: engine.GlobalStatusRestored,
		Context:      "kind-homelab",
		Resources:    res,
	}
}

func samplePlan(count int) *engine.Plan {
	plan := &engine.Plan{Action: engine.ActionSuspend}
	for i := 0; i < count; i++ {
		plan.Items = append(plan.Items, engine.PlanItem{
			Resource: config.Resource{
				Kind:      "Kustomization",
				Namespace: "flux-system",
				Name:      fmt.Sprintf("resource-%02d", i),
			},
			Exists:          true,
			TargetSuspended: true,
		})
	}
	return plan
}

func sampleHistory(count int) []engine.HistoryItem {
	items := make([]engine.HistoryItem, 0, count)
	for i := 0; i < count; i++ {
		status := "success"
		if i%3 == 1 {
			status = "partial"
		} else if i%3 == 2 {
			status = "interrupted"
		}
		items = append(items, engine.HistoryItem{
			Op:         fmt.Sprintf("01TEST%02d", i),
			Action:     "suspend",
			StartedAt:  "2026-11-17T12:00:00Z",
			Status:     status,
			Targets:    15,
			OK:         14,
			Failed:     1,
			DurationMs: 4200,
		})
	}
	return items
}

func checkDimensions(t *testing.T, m Model, label string) {
	t.Helper()
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Fatalf("%s: got %d lines, want %d", label, len(lines), m.height)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > m.width {
			t.Fatalf("%s: line %d width %d exceeds %d", label, i, w, m.width)
		}
	}
}

func TestViewDimensions(t *testing.T) {
	sizes := [][2]int{
		{80, 24},
		{100, 30},
		{120, 40},
		{60, 20},
		{40, 12},
		{24, 8},
	}

	for _, size := range sizes {
		w, h := size[0], size[1]

		m := NewModel(nil)
		m.width, m.height = w, h
		m.status = sampleStatus(15)
		checkDimensions(t, m, fmt.Sprintf("list %dx%d", w, h))

		m.view = viewDetail
		m.detailCursor = 12
		checkDimensions(t, m, fmt.Sprintf("detail %dx%d", w, h))

		m.pendingPlan = samplePlan(20)
		m.pendingAction = engine.ActionSuspend
		m.confirmOpen = true
		checkDimensions(t, m, fmt.Sprintf("confirm %dx%d", w, h))

		m.confirmOpen = false
		m.planning = true
		m.planSwitch = "pullini"
		m.planDir = engine.DirectionOff
		checkDimensions(t, m, fmt.Sprintf("planning %dx%d", w, h))

		m.planning = false
		m.executing = true
		m.execProgress = "[3/20] resource-03 (OK)"
		checkDimensions(t, m, fmt.Sprintf("progress %dx%d", w, h))

		m.executing = false
		m.view = viewHistory
		m.history = sampleHistory(20)
		m.historyCursor = 19
		checkDimensions(t, m, fmt.Sprintf("history %dx%d", w, h))
	}
}

func TestConfirmOverlayKeepsBackground(t *testing.T) {
	m := NewModel(nil)
	m.width, m.height = 100, 30
	m.status = sampleStatus(15)
	m.pendingPlan = samplePlan(3)
	m.pendingAction = engine.ActionSuspend
	m.confirmOpen = true

	view := m.View()
	if !strings.Contains(view, "CONFIRM SWITCH CHANGE") {
		t.Fatalf("expected confirm dialog, got:\n%s", view)
	}
	if !strings.Contains(view, "DESCRIPTION") {
		t.Fatalf("expected switch table to remain visible behind the dialog, got:\n%s", view)
	}
	if !strings.Contains(view, "Flux Reconciliation") {
		t.Fatalf("expected switch name in the background table, got:\n%s", view)
	}
	checkDimensions(t, m, "confirm overlay")
}

func TestPlanningOverlayShowsImmediately(t *testing.T) {
	m := NewModel(nil)
	m.width, m.height = 100, 30
	m.status = sampleStatus(15)
	m.cursor = 2
	m.planning = true
	m.planSwitch = "pullini"
	m.planDir = engine.DirectionOff

	view := m.View()
	if got := strings.Count(view, "BUILDING SWITCH PLAN"); got != 1 {
		t.Fatalf("expected one planning dialog, got %d:\n%s", got, view)
	}
	if !strings.Contains(view, "DESCRIPTION") {
		t.Fatalf("expected switch table to remain visible behind the dialog, got:\n%s", view)
	}
	checkDimensions(t, m, "planning overlay")
}

func TestProgressOverlayKeepsBackground(t *testing.T) {
	m := NewModel(nil)
	m.width, m.height = 100, 30
	m.status = sampleStatus(15)
	m.executing = true
	m.pendingAction = engine.ActionSuspend
	m.execStep = 3
	m.execTotal = 16
	m.execOK = 3
	m.execProgress = "[3/16] Kustomization/apps (OK)"

	view := m.View()
	if got := strings.Count(view, "EXECUTING SUSPEND"); got != 1 {
		t.Fatalf("expected one progress dialog, got %d:\n%s", got, view)
	}
	if !strings.Contains(view, "DESCRIPTION") {
		t.Fatalf("expected switch table to remain visible behind the dialog, got:\n%s", view)
	}
	checkDimensions(t, m, "progress overlay")
}

func TestToggleShowsPlanDialogImmediately(t *testing.T) {
	m := NewModel(nil)
	m.width, m.height = 100, 30
	m.status = statusFromStates(false, false)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = next.(Model)
	if cmd == nil || !m.planning || m.confirmOpen {
		t.Fatalf("toggle should start planning, got cmd=%v planning=%v confirm=%v", cmd != nil, m.planning, m.confirmOpen)
	}
	if view := m.View(); !strings.Contains(view, "BUILDING SWITCH PLAN") {
		t.Fatalf("planning dialog should appear before the plan arrives:\n%s", view)
	}

	next, _ = m.Update(planMsg{
		action: engine.ActionSuspend, switchID: "flux",
		direction: engine.DirectionOff, plan: samplePlan(2),
	})
	m = next.(Model)
	if m.planning || !m.confirmOpen {
		t.Fatalf("plan should replace planning with the confirm dialog, got planning=%v confirm=%v", m.planning, m.confirmOpen)
	}
}

func TestHistoryView(t *testing.T) {
	m := NewModel(nil)
	m.width, m.height = 100, 24
	m.view = viewHistory
	m.history = sampleHistory(3)

	view := m.View()
	if !strings.Contains(view, "Operation History") {
		t.Fatalf("expected history breadcrumb, got:\n%s", view)
	}
	if !strings.Contains(view, "SUCCESS") {
		t.Fatalf("expected operation status in history, got:\n%s", view)
	}
	if !strings.Contains(view, "4.2s") {
		t.Fatalf("expected formatted duration in history, got:\n%s", view)
	}
}

func TestBreadcrumb(t *testing.T) {
	m := NewModel(nil)
	m.width, m.height = 120, 40

	if got := m.breadcrumb(); got != "Flux Controls" {
		t.Fatalf("list breadcrumb = %q", got)
	}

	m.view = viewDetail
	if got := m.breadcrumb(); got != "Flux Controls › Flux Reconciliation › Properties" {
		t.Fatalf("detail breadcrumb = %q", got)
	}
	if view := m.View(); !strings.Contains(view, "Flux Controls › Flux Reconciliation › Properties") {
		t.Fatalf("detail view missing breadcrumb:\n%s", view)
	}

	m.view = viewHistory
	m.historyReturn = viewList
	if got := m.breadcrumb(); got != "Flux Controls › Operation History" {
		t.Fatalf("history breadcrumb = %q", got)
	}

	m.historyReturn = viewDetail
	if got := m.breadcrumb(); got != "Flux Controls › Flux Reconciliation › Operation History" {
		t.Fatalf("history-from-detail breadcrumb = %q", got)
	}
}

func TestHistoryNavigation(t *testing.T) {
	m := NewModel(nil)
	m.width, m.height = 100, 24
	m.view = viewDetail

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = next.(Model)
	if m.view != viewHistory || m.historyReturn != viewDetail {
		t.Fatalf("h should open history from detail, got view=%v return=%v", m.view, m.historyReturn)
	}

	m.status = sampleStatus(4)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(Model)
	if cmd != nil || m.confirmOpen || m.view != viewHistory {
		t.Fatalf("toggle must not run from history, got cmd=%v confirm=%v view=%v", cmd != nil, m.confirmOpen, m.view)
	}

	m.history = sampleHistory(3)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = next.(Model)
	if m.historyCursor != 1 {
		t.Fatalf("down should move history cursor, got %d", m.historyCursor)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.view != viewDetail {
		t.Fatalf("esc should return to detail, got view=%v", m.view)
	}
}

func TestTUIForceOverride(t *testing.T) {
	cfg := &config.Config{Flux: config.FluxConfig{
		Resources: []config.Resource{{Kind: config.KindKustomization, Namespace: "flux-system", Name: "apps"}},
	}}

	stateDir := t.TempDir()
	j, err := journal.Open(filepath.Join(stateDir, "journal.jsonl"))
	if err != nil {
		t.Fatalf("opening seed journal: %v", err)
	}
	from, to := false, true
	events := []journal.Event{
		{Op: "01SEED", Event: journal.EventOpStart, Action: journal.ActionSuspend, Context: "test", Targets: 1},
		{Op: "01SEED", Event: journal.EventResourceIntent, Action: journal.ActionSuspend, Kind: config.KindKustomization, Namespace: "flux-system", Name: "apps", From: &from, To: &to},
		{Op: "01SEED", Event: journal.EventResourceResult, Kind: config.KindKustomization, Namespace: "flux-system", Name: "apps", Status: journal.StatusOk, Observed: &to},
		{Op: "01SEED", Event: journal.EventOpEnd, Status: journal.StatusSuccess, OK: 1},
		{Op: "01RESTORE", Event: journal.EventOpStart, Action: journal.ActionRestore, Context: "test", Targets: 1},
		{Op: "01RESTORE", Event: journal.EventOpEnd, Status: journal.StatusSuccess, OK: 1},
	}
	for i := range events {
		if err := j.Append(&events[i]); err != nil {
			_ = j.Close()
			t.Fatalf("appending seed event: %v", err)
		}
	}
	if err := j.Close(); err != nil {
		t.Fatalf("closing seed journal: %v", err)
	}

	// External suspend after a clean restore: expected false, observed true.
	client := tuiFluxClient{observed: map[string]bool{"Kustomization/flux-system/apps": true}}
	eng := engine.New(cfg, client, stateDir, "test", nil)
	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer eng.Close()

	if _, err := eng.Execute(ctx, engine.ActionSuspend, false, nil); err != engine.ErrConflict {
		t.Fatalf("expected a real conflict without force, got %v", err)
	}

	m := NewModel(eng)
	m.pendingPlan = samplePlan(1)
	m.pendingPlan.Conflicts = []engine.PlanItem{m.pendingPlan.Items[0]}
	m.pendingAction = engine.ActionSuspend
	m.confirmOpen = true

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = next.(Model)
	if cmd == nil || !m.pendingForce || !m.executing || m.confirmOpen {
		t.Fatalf("f should confirm a forced execution, got force=%v executing=%v confirm=%v", m.pendingForce, m.executing, m.confirmOpen)
	}

	done := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-m.msgChan:
			if !ok {
				report, err := eng.History(1)
				if err != nil || len(report) == 0 || report[0].Status != "success" {
					t.Fatalf("forced execution was not journaled: %v %+v", err, report)
				}
				if report[0].Action != string(engine.ActionSuspend) {
					t.Fatalf("forced execution journaled wrong action: %s", report[0].Action)
				}
				return
			}
		case <-done:
			t.Fatal("timed out waiting for forced execution")
		}
	}
}

func TestDetailScrolling(t *testing.T) {
	m := NewModel(nil)
	m.width, m.height = 100, 24
	m.status = sampleStatus(50)
	m.view = viewDetail
	m.detailCursor = 49

	view := m.View()
	if !strings.Contains(view, "of 50") {
		t.Fatalf("expected scroll range indicator, got:\n%s", view)
	}
	if !strings.Contains(view, "resource-49") {
		t.Fatalf("expected selected resource to be visible")
	}
}

func statusFromStates(states ...bool) *engine.StatusReport {
	orig := false
	res := make([]engine.ResourceStatusItem, 0, len(states))
	for i, suspended := range states {
		res = append(res, engine.ResourceStatusItem{
			Kind:              "Kustomization",
			Namespace:         "flux-system",
			Name:              fmt.Sprintf("resource-%02d", i),
			Exists:            true,
			CurrentSuspended:  suspended,
			OriginalSuspended: &orig,
			Ready:             "True",
		})
	}
	return &engine.StatusReport{Resources: res}
}

func TestNextAction(t *testing.T) {
	m := NewModel(nil)
	if _, ok := m.nextAction(); ok {
		t.Fatal("expected no action without status")
	}

	m.status = statusFromStates(false, false)
	if action, ok := m.nextAction(); !ok || action != engine.ActionSuspend {
		t.Fatalf("switch ON should suspend, got %q ok=%v", action, ok)
	}

	m.status = statusFromStates(true, true)
	if action, ok := m.nextAction(); !ok || action != engine.ActionRestore {
		t.Fatalf("switch OFF should restore, got %q ok=%v", action, ok)
	}

	// A partially frozen cluster still leaves reconciliation active somewhere,
	// so the switch is ON and toggling turns it fully OFF.
	m.status = statusFromStates(true, false)
	if action, ok := m.nextAction(); !ok || action != engine.ActionSuspend {
		t.Fatalf("mixed cluster should suspend, got %q ok=%v", action, ok)
	}
}

func TestSwitchPosition(t *testing.T) {
	m := NewModel(nil)
	if got := m.switchState(); got.label != "… LOADING" || got.known {
		t.Fatalf("nil status switch = %+v", got)
	}

	m.status = statusFromStates(false, false)
	if got := m.switchState(); !got.known || !got.on || got.label != "● ON" {
		t.Fatalf("all active switch = %+v", got)
	}

	m.status = statusFromStates(true, true)
	if got := m.switchState(); !got.known || got.on || got.label != "○ OFF" {
		t.Fatalf("all suspended switch = %+v", got)
	}

	m.status = statusFromStates(true, false)
	if got := m.switchState(); !got.known || !got.on || got.label != "● ON" {
		t.Fatalf("mixed cluster switch should stay ON, got %+v", got)
	}
}

func TestClusterState(t *testing.T) {
	m := NewModel(nil)
	if label, _ := m.clusterState(); label != "… LOADING" {
		t.Fatalf("nil status cluster = %q", label)
	}

	cases := []struct {
		name   string
		status *engine.StatusReport
		want   string
	}{
		{"all active", statusFromStates(false, false), "RESTORED"},
		{"all suspended", statusFromStates(true, true), "SUSPENDED"},
		{"mixed", statusFromStates(true, false), "MIXED"},
		{
			"missing",
			&engine.StatusReport{Resources: []engine.ResourceStatusItem{{Kind: "Kustomization", Exists: false}}},
			"MISSING",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m.status = tc.status
			if got, _ := m.clusterState(); got != tc.want {
				t.Fatalf("cluster state = %q, want %q", got, tc.want)
			}
		})
	}

	// Everything is active, but it no longer matches the recorded baseline.
	orig := true
	m.status = &engine.StatusReport{Resources: []engine.ResourceStatusItem{{
		Kind: "Kustomization", Exists: true, CurrentSuspended: false, OriginalSuspended: &orig,
	}}}
	if got, _ := m.clusterState(); got != "DRIFT" {
		t.Fatalf("cluster state = %q, want DRIFT", got)
	}
}
