package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rykhalskyi/killswitch/internal/journal"
)

func boolPtr(b bool) *bool {
	return &b
}

func TestAtomicSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	st := New(dir)
	st.LastSeq = 42
	st.OriginalSuspended["Kustomization/flux-system/apps"] = false

	if err := st.Save(); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if loaded.LastSeq != 42 {
		t.Errorf("expected LastSeq 42, got %d", loaded.LastSeq)
	}
	if val, ok := loaded.OriginalSuspended["Kustomization/flux-system/apps"]; !ok || val != false {
		t.Errorf("expected original suspended false, got %v (exists: %v)", val, ok)
	}
}

func TestRebuildFromJournal(t *testing.T) {
	dir := t.TempDir()
	events := []journal.Event{
		{
			V:       1,
			Seq:     1,
			TS:      "2026-11-08T18:00:00Z",
			Op:      "op-test",
			Event:   journal.EventOpStart,
			Action:  journal.ActionSuspend,
			Context: "talos",
			Targets: 1,
		},
		{
			V:         1,
			Seq:       2,
			TS:        "2026-11-08T18:00:01Z",
			Op:        "op-test",
			Event:     journal.EventResourceIntent,
			Kind:      "Kustomization",
			Namespace: "flux-system",
			Name:      "apps",
			From:      boolPtr(false),
			To:        boolPtr(true),
		},
		{
			V:          1,
			Seq:        3,
			TS:         "2026-11-08T18:00:02Z",
			Op:         "op-test",
			Event:      journal.EventResourceResult,
			Kind:       "Kustomization",
			Namespace:  "flux-system",
			Name:       "apps",
			Status:     journal.StatusOk,
			Observed:   boolPtr(true),
			DurationMs: 35,
		},
	}

	// State was missing or out-of-date
	st, err := LoadOrRebuild(dir, events)
	if err != nil {
		t.Fatalf("LoadOrRebuild failed: %v", err)
	}

	if st.LastSeq != 3 {
		t.Errorf("expected LastSeq 3, got %d", st.LastSeq)
	}
	if st.ActiveOp == nil {
		t.Fatal("expected active op to still be present (incomplete op)")
	}
	if st.ActiveOp.Op != "op-test" {
		t.Errorf("expected op 'op-test', got %s", st.ActiveOp.Op)
	}

	key := "Kustomization/flux-system/apps"
	if orig, ok := st.OriginalSuspended[key]; !ok || orig != false {
		t.Errorf("expected original false, got %v (exists: %v)", orig, ok)
	}
	rState := st.ActiveOp.ResourceStates[key]
	if rState.Phase != PhaseOK {
		t.Errorf("expected PhaseOK, got %s", rState.Phase)
	}
}

func TestLockContention(t *testing.T) {
	dir := t.TempDir()

	l1, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("first AcquireLock failed: %v", err)
	}
	defer l1.Release()

	// Second lock attempt in the same directory should fail with ErrLocked
	_, err = AcquireLock(dir)
	if err != ErrLocked {
		t.Fatalf("expected ErrLocked, got %v", err)
	}

	// Release first lock
	if err := l1.Release(); err != nil {
		t.Fatalf("l1.Release failed: %v", err)
	}

	// Now second lock should succeed
	l2, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("second AcquireLock after release failed: %v", err)
	}
	defer l2.Release()
}

func TestCorruptStateRecovery(t *testing.T) {
	dir := t.TempDir()
	// Write corrupt state.json
	_ = os.WriteFile(filepath.Join(dir, "state.json"), []byte("{invalid-json"), 0600)

	events := []journal.Event{
		{
			V:       1,
			Seq:     1,
			TS:      "2026-11-08T18:00:00Z",
			Op:      "op-1",
			Event:   journal.EventOpStart,
			Action:  journal.ActionSuspend,
			Targets: 0,
		},
		{
			V:      1,
			Seq:    2,
			TS:     "2026-11-08T18:00:05Z",
			Op:     "op-1",
			Event:  journal.EventOpEnd,
			Status: journal.StatusSuccess,
			OK:     0,
		},
	}

	st, err := LoadOrRebuild(dir, events)
	if err != nil {
		t.Fatalf("LoadOrRebuild should recover from corrupt state: %v", err)
	}
	if st.LastSeq != 2 {
		t.Errorf("expected LastSeq 2, got %d", st.LastSeq)
	}
	if st.LastCompletedOp == nil || st.LastCompletedOp.Status != journal.StatusSuccess {
		t.Errorf("expected completed op success, got %+v", st.LastCompletedOp)
	}
}
