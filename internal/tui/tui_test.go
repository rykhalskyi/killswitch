package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/rykhalskyi/killswitch/internal/config"
	"github.com/rykhalskyi/killswitch/internal/engine"
)

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
		m.executing = true
		m.execProgress = "[3/20] resource-03 (OK)"
		checkDimensions(t, m, fmt.Sprintf("progress %dx%d", w, h))
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
