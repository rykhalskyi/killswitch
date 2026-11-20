package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rykhalskyi/killswitch/internal/journal"
)

type ResourcePhase string

const (
	PhasePending    ResourcePhase = "pending"
	PhaseInProgress ResourcePhase = "in_progress"
	PhaseOK         ResourcePhase = "ok"
	PhaseError      ResourcePhase = "error"
)

// ResourceState tracks the progress of a resource in the active operation.
type ResourceState struct {
	Phase      ResourcePhase `json:"phase"`
	Target     bool          `json:"target"`
	Observed   *bool         `json:"observed,omitempty"`
	DurationMs int64         `json:"durationMs,omitempty"`
	Error      string        `json:"error,omitempty"`
}

// StepState tracks the typed progress of a workflow step.
type StepState struct {
	Phase      ResourcePhase `json:"phase"`
	Desired    any           `json:"desired,omitempty"`
	Observed   any           `json:"observed,omitempty"`
	Original   any           `json:"original,omitempty"`
	DurationMs int64         `json:"durationMs,omitempty"`
	Error      string        `json:"error,omitempty"`
}

// OriginalValue is the typed captured original for a workflow step.
type OriginalValue struct {
	Action string `json:"action"`
	Value  any    `json:"value"`
}

// ActiveOperation holds state about an ongoing or interrupted operation.
type ActiveOperation struct {
	Op             string                   `json:"op"`
	Action         string                   `json:"action"`
	Direction      string                   `json:"direction,omitempty"`
	Switch         string                   `json:"switch,omitempty"`
	Workflow       string                   `json:"workflow,omitempty"`
	Context        string                   `json:"context"`
	StartedAt      string                   `json:"startedAt"`
	Targets        int                      `json:"targets"`
	ResourceStates map[string]ResourceState `json:"resourceStates"`
	StepStates     map[string]StepState     `json:"stepStates,omitempty"`
}

// CompletedOperation holds metadata about the last finished operation.
type CompletedOperation struct {
	Op        string `json:"op"`
	Action    string `json:"action"`
	Direction string `json:"direction,omitempty"`
	Switch    string `json:"switch,omitempty"`
	Workflow  string `json:"workflow,omitempty"`
	EndedAt   string `json:"endedAt"`
	Status    string `json:"status"`
	OK        int    `json:"ok"`
	Failed    int    `json:"failed"`
}

// State represents the atomic snapshot saved in state.json.
type State struct {
	LastSeq           int64                    `json:"lastSeq"`
	ActiveOp          *ActiveOperation         `json:"activeOp,omitempty"`
	OriginalSuspended map[string]bool          `json:"originalSuspended"`
	OriginalValues    map[string]OriginalValue `json:"originalValues,omitempty"`
	LastCompletedOp   *CompletedOperation      `json:"lastCompletedOp,omitempty"`

	dir string
}

// DefaultStateDir returns $XDG_STATE_HOME/killswitch or fallback ~/.local/state/killswitch.
func DefaultStateDir() (string, error) {
	xdg := os.Getenv("XDG_STATE_HOME")
	if xdg != "" {
		return filepath.Join(xdg, "killswitch"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting user home dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "killswitch"), nil
}

// New creates a clean, empty State structure for a state directory.
func New(dir string) *State {
	return &State{
		OriginalSuspended: make(map[string]bool),
		OriginalValues:    make(map[string]OriginalValue),
		dir:               dir,
	}
}

// Path returns the absolute path to state.json.
func (s *State) Path() string {
	return filepath.Join(s.dir, "state.json")
}

// Save writes the state snapshot atomically: temp file -> fsync -> rename -> dir fsync.
func (s *State) Save() error {
	if s.dir == "" {
		return fmt.Errorf("state directory is not set")
	}

	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	tmpFile, err := os.CreateTemp(s.dir, "state.*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp state file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		// Clean up temp file in case rename didn't happen
		_ = os.Remove(tmpName)
	}()

	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		return fmt.Errorf("chmodding temp state file: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("writing state data: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return fmt.Errorf("syncing temp state file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing temp state file: %w", err)
	}

	finalPath := s.Path()
	if err := os.Rename(tmpName, finalPath); err != nil {
		return fmt.Errorf("renaming temp state to %s: %w", finalPath, err)
	}

	// Fsync containing directory so directory entry is durable
	dirFile, err := os.Open(s.dir)
	if err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}

	return nil
}

// Load attempts to read state.json from dir.
func Load(dir string) (*State, error) {
	path := filepath.Join(dir, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	st := New(dir)
	if err := json.Unmarshal(data, st); err != nil {
		return nil, fmt.Errorf("unmarshaling state from %s: %w", path, err)
	}
	if st.OriginalSuspended == nil {
		st.OriginalSuspended = make(map[string]bool)
	}
	if st.OriginalValues == nil {
		st.OriginalValues = make(map[string]OriginalValue)
	}
	if st.ActiveOp != nil {
		if st.ActiveOp.StepStates == nil {
			st.ActiveOp.StepStates = make(map[string]StepState)
		}
		if st.ActiveOp.ResourceStates == nil {
			st.ActiveOp.ResourceStates = make(map[string]ResourceState)
		}
	}
	st.dir = dir

	return st, nil
}

// RebuildFromJournal reconstructs the state snapshot by replaying all journal events.
func RebuildFromJournal(dir string, events []journal.Event) *State {
	st := New(dir)

	for _, e := range events {
		st.ApplyEvent(&e)
	}

	return st
}

// ApplyEvent updates the state according to a single journal event.
func (s *State) ApplyEvent(e *journal.Event) {
	s.LastSeq = e.Seq

	key := ""
	if e.Kind != "" && e.Namespace != "" && e.Name != "" {
		key = fmt.Sprintf("%s/%s/%s", e.Kind, e.Namespace, e.Name)
	}

	stepKey := e.Step
	if stepKey == "" {
		stepKey = key
	} else if e.Workflow != "" {
		stepKey = e.Workflow + "/" + e.Step
	}
	offDirection := e.Direction == "off" || (e.Direction == "" && e.Action == journal.ActionSuspend)

	switch e.Event {
	case journal.EventOpStart:
		s.ActiveOp = &ActiveOperation{
			Op:             e.Op,
			Action:         e.Action,
			Direction:      e.Direction,
			Switch:         e.Switch,
			Workflow:       e.Workflow,
			Context:        e.Context,
			StartedAt:      e.TS,
			Targets:        e.Targets,
			ResourceStates: make(map[string]ResourceState),
			StepStates:     make(map[string]StepState),
		}

	case journal.EventResourceIntent:
		original := e.Original
		if original == nil {
			original = e.From
		}
		desired := e.Desired
		if desired == nil {
			desired = e.To
		}
		if s.ActiveOp != nil && key != "" {
			target := false
			if e.To != nil {
				target = *e.To
			} else if v, ok := journal.BoolValue(desired); ok {
				target = v
			}
			s.ActiveOp.ResourceStates[key] = ResourceState{
				Phase:  PhaseInProgress,
				Target: target,
			}
		}
		if s.ActiveOp != nil && stepKey != "" {
			s.ActiveOp.StepStates[stepKey] = StepState{
				Phase:    PhaseInProgress,
				Desired:  desired,
				Original: original,
			}
		}
		// Capture typed originals when entering the non-default (OFF) state.
		if offDirection && original != nil && stepKey != "" {
			if _, exists := s.OriginalValues[stepKey]; !exists {
				s.OriginalValues[stepKey] = OriginalValue{Action: e.Action, Value: original}
			}
		}
		// Keep the legacy Flux originals for v1 state compatibility.
		if (e.Action == journal.ActionSuspend || (s.ActiveOp != nil && s.ActiveOp.Action == journal.ActionSuspend)) && e.From != nil && key != "" {
			if _, exists := s.OriginalSuspended[key]; !exists {
				s.OriginalSuspended[key] = *e.From
			}
		}
		if v, ok := journal.BoolValue(original); ok && key != "" && (e.Action == "flux.suspend" || e.Action == "flux.resume") {
			if _, exists := s.OriginalSuspended[key]; !exists {
				s.OriginalSuspended[key] = v
			}
		}

	case journal.EventResourceResult:
		if s.ActiveOp != nil && key != "" {
			target := false
			if existing, ok := s.ActiveOp.ResourceStates[key]; ok {
				target = existing.Target
			}
			phase := PhaseOK
			if e.Status != journal.StatusOk {
				phase = PhaseError
			}
			observedBool, _ := journal.BoolValue(e.Observed)
			obs := observedBool
			s.ActiveOp.ResourceStates[key] = ResourceState{
				Phase:      phase,
				Target:     target,
				Observed:   &obs,
				DurationMs: e.DurationMs,
				Error:      e.Error,
			}
		}
		if s.ActiveOp != nil && stepKey != "" {
			phase := PhaseOK
			if e.Status != journal.StatusOk {
				phase = PhaseError
			}
			existing := s.ActiveOp.StepStates[stepKey]
			existing.Phase = phase
			existing.Observed = e.Observed
			existing.DurationMs = e.DurationMs
			existing.Error = e.Error
			s.ActiveOp.StepStates[stepKey] = existing
		}

	case journal.EventOpEnd:
		if s.ActiveOp != nil {
			s.LastCompletedOp = &CompletedOperation{
				Op:        e.Op,
				Action:    s.ActiveOp.Action,
				Direction: s.ActiveOp.Direction,
				Switch:    s.ActiveOp.Switch,
				Workflow:  s.ActiveOp.Workflow,
				EndedAt:   e.TS,
				Status:    e.Status,
				OK:        e.OK,
				Failed:    e.Failed,
			}
			s.ActiveOp = nil
		}
	}
}

// LoadOrRebuild loads state.json, verifies that state.LastSeq == journalLastSeq,
// and if missing, corrupt, or out of sync with journal, rebuilds state from the journal and saves it.
func LoadOrRebuild(dir string, events []journal.Event) (*State, error) {
	var journalLastSeq int64
	for _, e := range events {
		if e.Seq > journalLastSeq {
			journalLastSeq = e.Seq
		}
	}

	st, err := Load(dir)
	if err == nil && st != nil && st.LastSeq == journalLastSeq {
		return st, nil
	}

	// Rebuild
	rebuilt := RebuildFromJournal(dir, events)
	if err := rebuilt.Save(); err != nil {
		return nil, fmt.Errorf("saving rebuilt state: %w", err)
	}

	return rebuilt, nil
}
