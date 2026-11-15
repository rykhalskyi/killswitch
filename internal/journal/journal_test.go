package journal

import (
	"path/filepath"
	"strings"
	"testing"
)

func boolPtr(b bool) *bool {
	return &b
}

func TestJournalAppendAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.jsonl")

	j, err := Open(path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	e1 := &Event{
		Op:      "op-1",
		Event:   EventOpStart,
		Action:  ActionSuspend,
		Context: "talos",
		Targets: 2,
	}
	if err := j.Append(e1); err != nil {
		t.Fatalf("Append e1 failed: %v", err)
	}
	if e1.Seq != 1 {
		t.Errorf("expected e1.Seq == 1, got %d", e1.Seq)
	}

	e2 := &Event{
		Op:        "op-1",
		Event:     EventResourceIntent,
		Kind:      "Kustomization",
		Namespace: "flux-system",
		Name:      "apps",
		From:      boolPtr(false),
		To:        boolPtr(true),
	}
	if err := j.Append(e2); err != nil {
		t.Fatalf("Append e2 failed: %v", err)
	}
	if e2.Seq != 2 {
		t.Errorf("expected e2.Seq == 2, got %d", e2.Seq)
	}

	if err := j.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Read all events
	events, err := ReadAll(path)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Event != EventOpStart || events[0].Seq != 1 {
		t.Errorf("unexpected event 0: %+v", events[0])
	}
	if events[1].Event != EventResourceIntent || events[1].Seq != 2 {
		t.Errorf("unexpected event 1: %+v", events[1])
	}

	// Reopen and append e3
	j2, err := Open(path)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}
	defer j2.Close()

	if j2.NextSeq() != 3 {
		t.Errorf("expected j2.NextSeq == 3, got %d", j2.NextSeq())
	}

	e3 := &Event{
		Op:        "op-1",
		Event:     EventResourceResult,
		Kind:      "Kustomization",
		Namespace: "flux-system",
		Name:      "apps",
		Status:    StatusOk,
		Observed:  boolPtr(true),
	}
	if err := j2.Append(e3); err != nil {
		t.Fatalf("Append e3 failed: %v", err)
	}
	if e3.Seq != 3 {
		t.Errorf("expected e3.Seq == 3, got %d", e3.Seq)
	}
}

func TestTornTrailingLineHandling(t *testing.T) {
	content := `{"v":1,"seq":1,"ts":"2026-11-08T18:00:00Z","op":"01J","event":"op.start","action":"suspend","targets":1}
{"v":1,"seq":2,"ts":"2026-11-08T18:00:01Z","op":"01J","event":"resource.intent","kind":"Kustomization","namespace":"flux-system","name":"apps","from":false,"to":true}
{"v":1,"seq":3,"ts":"2026-11-08T18:00:02Z","op":"01J","event":"resource.result","kind":"Kustomiz` // incomplete line without \n

	events, err := Parse(strings.NewReader(content))
	if err != nil {
		t.Fatalf("Parse with torn line failed: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected exactly 2 valid events, got %d", len(events))
	}
	if events[0].Seq != 1 || events[1].Seq != 2 {
		t.Errorf("unexpected event sequences: %d, %d", events[0].Seq, events[1].Seq)
	}
}
