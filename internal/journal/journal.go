package journal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const (
	Version = 2

	EventOpStart        = "op.start"
	EventResourceIntent = "resource.intent"
	EventResourceResult = "resource.result"
	EventConflict       = "conflict"
	EventOpEnd          = "op.end"

	StatusOk      = "ok"
	StatusError   = "error"
	StatusSuccess = "success"
	StatusPartial = "partial"
	StatusFailed  = "failed"

	ActionSuspend = "suspend"
	ActionRestore = "restore"
)

// Event represents an immutable journal record.
type Event struct {
	V          int    `json:"v"`
	Seq        int64  `json:"seq"`
	TS         string `json:"ts"`
	Op         string `json:"op"`
	Event      string `json:"event"`
	Action     string `json:"action,omitempty"`
	Switch     string `json:"switch,omitempty"`
	Workflow   string `json:"workflow,omitempty"`
	Step       string `json:"step,omitempty"`
	Direction  string `json:"direction,omitempty"`
	Phase      string `json:"phase,omitempty"`
	Context    string `json:"context,omitempty"`
	Targets    int    `json:"targets,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name,omitempty"`
	From       *bool  `json:"from,omitempty"`
	To         *bool  `json:"to,omitempty"`
	Status     string `json:"status,omitempty"`
	Observed   any    `json:"observed,omitempty"`
	Expected   any    `json:"expected,omitempty"`
	Original   any    `json:"original,omitempty"`
	Desired    any    `json:"desired,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	Error      string `json:"error,omitempty"`
	OK         int    `json:"ok,omitempty"`
	Failed     int    `json:"failed,omitempty"`
}

// BoolValue extracts a bool from a legacy bool/pointer or v2 any value.
func BoolValue(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case *bool:
		if t == nil {
			return false, false
		}
		return *t, true
	default:
		return false, false
	}
}

// IntValue extracts an int from a v2 JSON number.
func IntValue(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int32:
		return int(t), true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	default:
		return 0, false
	}
}

// Journal manages append-only logging to journal.jsonl.
type Journal struct {
	mu      sync.Mutex
	file    *os.File
	path    string
	nextSeq int64
}

// Open opens or creates the journal file at path, scanning for the highest sequence number.
func Open(path string) (*Journal, error) {
	events, err := ReadAll(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading existing journal: %w", err)
	}

	var lastSeq int64
	for _, e := range events {
		if e.Seq > lastSeq {
			lastSeq = e.Seq
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("opening journal file %s: %w", path, err)
	}

	return &Journal{
		file:    f,
		path:    path,
		nextSeq: lastSeq + 1,
	}, nil
}

// NextSeq returns the sequence number that will be assigned to the next appended event.
func (j *Journal) NextSeq() int64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.nextSeq
}

// Append writes an event to the journal, syncing it to disk before returning.
func (j *Journal) Append(e *Event) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	e.V = Version
	e.Seq = j.nextSeq
	if e.TS == "" {
		e.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}

	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshaling journal event: %w", err)
	}

	data = append(data, '\n')
	if _, err := j.file.Write(data); err != nil {
		return fmt.Errorf("writing journal entry: %w", err)
	}

	if err := j.file.Sync(); err != nil {
		return fmt.Errorf("syncing journal entry to disk: %w", err)
	}

	j.nextSeq++
	return nil
}

// Close flushes and closes the journal file.
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.file != nil {
		err := j.file.Close()
		j.file = nil
		return err
	}
	return nil
}

// ReadAll reads all complete events from a journal file at path.
// If the file ends with an incomplete line (missing trailing newline, e.g. from a crash/torn write),
// that incomplete trailing line is ignored.
func ReadAll(path string) ([]Event, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return Parse(bytes.NewReader(data))
}

// Parse reads events from an io.Reader. Unterminated lines at EOF are discarded.
func Parse(r io.Reader) ([]Event, error) {
	reader := bufio.NewReader(r)
	var events []Event

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("reading journal line: %w", err)
		}

		if err == io.EOF {
			// If EOF was reached and there is remaining data without a trailing newline,
			// this indicates a torn write. Ignore it as per spec.
			if len(line) > 0 && line[len(line)-1] != '\n' {
				break
			}
			if len(line) == 0 {
				break
			}
		}

		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			if err == io.EOF {
				break
			}
			continue
		}

		var event Event
		if err := json.Unmarshal(trimmed, &event); err != nil {
			return nil, fmt.Errorf("parsing journal event: %w (line: %s)", err, string(trimmed))
		}
		events = append(events, event)

		if err == io.EOF {
			break
		}
	}

	return events, nil
}
