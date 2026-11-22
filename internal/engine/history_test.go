package engine

import (
	"strings"
	"testing"

	"github.com/rykhalskyi/killswitch/internal/journal"
)

func TestHistoryIncludesSwitchNameAndSteps(t *testing.T) {
	dir := t.TempDir()
	seedJournal(t, dir, []journal.Event{
		{Op: "01HIST", Event: journal.EventOpStart, Action: "suspend", Switch: "cluster-shutdown", Workflow: "cluster-shutdown", Direction: "off", Context: "test", Targets: 2},
		{Op: "01HIST", Event: journal.EventResourceIntent, Action: "talos.shutdown", Switch: "cluster-shutdown", Workflow: "cluster-shutdown", Step: "01-235", Direction: "off", Kind: "Node", Name: "192.168.2.235", Desired: "down"},
		{Op: "01HIST", Event: journal.EventResourceResult, Action: "talos.shutdown", Switch: "cluster-shutdown", Workflow: "cluster-shutdown", Step: "01-235", Direction: "off", Status: journal.StatusOk, Observed: "down", DurationMs: 100},
		{Op: "01HIST", Event: journal.EventResourceIntent, Action: "talos.shutdown", Switch: "cluster-shutdown", Workflow: "cluster-shutdown", Step: "02-234", Direction: "off", Kind: "Node", Name: "192.168.2.234", Desired: "down"},
		{Op: "01HIST", Event: journal.EventResourceResult, Action: "talos.shutdown", Switch: "cluster-shutdown", Workflow: "cluster-shutdown", Step: "02-234", Direction: "off", Status: journal.StatusError, Error: "boom"},
		{Op: "01HIST", Event: journal.EventOpEnd, Action: "suspend", Switch: "cluster-shutdown", Direction: "off", Status: journal.StatusPartial, OK: 1, Failed: 1},
	})

	eng := New(oneWayConfig(), nil, dir, "test", nil)
	items, err := eng.History(10)
	if err != nil {
		t.Fatalf("History failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one item, got %d", len(items))
	}
	item := items[0]
	if item.SwitchName != "Cluster Shutdown" {
		t.Fatalf("SwitchName = %q, want Cluster Shutdown", item.SwitchName)
	}
	if !item.OneWay || item.Direction != "off" {
		t.Fatalf("expected one-way off operation, got oneWay=%v direction=%q", item.OneWay, item.Direction)
	}
	if item.Status != journal.StatusPartial || item.OK != 1 || item.Failed != 1 {
		t.Fatalf("unexpected summary: %+v", item)
	}
	if len(item.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %+v", item.Steps)
	}
	first := item.Steps[0]
	if first.Step != "01-235" || first.Status != "ok" || first.Target != "Node/192.168.2.235" {
		t.Fatalf("unexpected first step: %+v", first)
	}
	if first.Desired != "down" || first.Observed != "down" {
		t.Fatalf("expected captured desired/observed values, got %+v", first)
	}
	second := item.Steps[1]
	if second.Status != "error" || second.Error != "boom" {
		t.Fatalf("unexpected second step: %+v", second)
	}
}

func TestOperationLogFiltersAndFormats(t *testing.T) {
	dir := t.TempDir()
	seedJournal(t, dir, []journal.Event{
		{Op: "01HIST", Event: journal.EventOpStart, Action: "suspend", Switch: "cluster-shutdown", Direction: "off", Targets: 1},
		{Op: "01HIST", Event: journal.EventResourceResult, Step: "01-235", Status: journal.StatusOk, Observed: "down"},
		{Op: "01OTHER", Event: journal.EventOpStart, Action: "restore", Switch: "media"},
	})

	eng := New(oneWayConfig(), nil, dir, "test", nil)
	lines, err := eng.OperationLog("01HIST")
	if err != nil {
		t.Fatalf("OperationLog failed: %v", err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, `"op": "01HIST"`) {
		t.Fatalf("expected the selected operation, got:\n%s", joined)
	}
	if strings.Contains(joined, "01OTHER") {
		t.Fatalf("log must exclude other operations, got:\n%s", joined)
	}
	if !strings.Contains(joined, `"01-235"`) {
		t.Fatalf("expected step records in the log, got:\n%s", joined)
	}
}
