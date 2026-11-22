package engine

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rykhalskyi/killswitch/internal/journal"
)

// HistoryStep is one workflow step recorded inside an operation.
type HistoryStep struct {
	Step       string `json:"step,omitempty"`
	Action     string `json:"action,omitempty"`
	Direction  string `json:"direction,omitempty"`
	Target     string `json:"target,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name,omitempty"`
	Status     string `json:"status,omitempty"`
	Original   any    `json:"original,omitempty"`
	Desired    any    `json:"desired,omitempty"`
	Observed   any    `json:"observed,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	Error      string `json:"error,omitempty"`
}

// HistoryItem represents an aggregated record of a past operation.
type HistoryItem struct {
	Op         string        `json:"op"`
	Action     string        `json:"action"`
	Switch     string        `json:"switch,omitempty"`
	SwitchName string        `json:"switchName,omitempty"`
	Workflow   string        `json:"workflow,omitempty"`
	Direction  string        `json:"direction,omitempty"`
	OneWay     bool          `json:"oneWay,omitempty"`
	Context    string        `json:"context"`
	StartedAt  string        `json:"startedAt"`
	EndedAt    string        `json:"endedAt,omitempty"`
	Status     string        `json:"status"` // success, partial, failed, interrupted, conflict
	Targets    int           `json:"targets"`
	OK         int           `json:"ok"`
	Failed     int           `json:"failed"`
	DurationMs int64         `json:"durationMs"`
	Steps      []HistoryStep `json:"steps,omitempty"`
}

type historyStepCollector struct {
	step      HistoryStep
	hasResult bool
}

type historyOpCollector struct {
	start     *journal.Event
	end       *journal.Event
	conflicts []journal.Event
	order     []string
	steps     map[string]*historyStepCollector
}

func historyTargetKey(kind, namespace, name string) string {
	if namespace == "" {
		return kind + "/" + name
	}
	return kind + "/" + namespace + "/" + name
}

func historyEventKey(ev journal.Event) string {
	if ev.Step != "" {
		if ev.Workflow != "" {
			return ev.Workflow + "/" + ev.Step
		}
		return ev.Step
	}
	return historyTargetKey(ev.Kind, ev.Namespace, ev.Name)
}

func (c *historyOpCollector) stepFor(ev journal.Event) *historyStepCollector {
	key := historyEventKey(ev)
	if key == "" {
		return nil
	}
	sc, ok := c.steps[key]
	if !ok {
		sc = &historyStepCollector{}
		c.steps[key] = sc
		c.order = append(c.order, key)
	}
	if sc.step.Step == "" {
		sc.step.Step = ev.Step
	}
	if sc.step.Action == "" {
		sc.step.Action = ev.Action
	}
	if sc.step.Direction == "" {
		sc.step.Direction = ev.Direction
	}
	if sc.step.Target == "" && ev.Kind != "" {
		sc.step.Target = historyTargetKey(ev.Kind, ev.Namespace, ev.Name)
		sc.step.Kind = ev.Kind
		sc.step.Namespace = ev.Namespace
		sc.step.Name = ev.Name
	}
	return sc
}

func (c *historyOpCollector) recordedSteps() []HistoryStep {
	steps := make([]HistoryStep, 0, len(c.order))
	for _, key := range c.order {
		sc := c.steps[key]
		if !sc.hasResult && sc.step.Status == "" {
			sc.step.Status = "pending"
		}
		steps = append(steps, sc.step)
	}
	return steps
}

// History reads operations from the journal file, aggregating op.start,
// per-step resource events, and op.end. Results are returned in reverse
// chronological order (newest first). If limit > 0, returns at most limit items.
func (e *Engine) History(limit int) ([]HistoryItem, error) {
	journalPath := filepath.Join(e.stateDir, "journal.jsonl")
	events, err := journal.ReadAll(journalPath)
	if err != nil {
		return nil, err
	}

	opsOrder := []string{}
	opsMap := make(map[string]*historyOpCollector)

	for i := range events {
		ev := &events[i]
		if ev.Op == "" {
			continue
		}

		col, exists := opsMap[ev.Op]
		if !exists {
			col = &historyOpCollector{steps: make(map[string]*historyStepCollector)}
			opsMap[ev.Op] = col
			opsOrder = append(opsOrder, ev.Op)
		}

		switch ev.Event {
		case journal.EventOpStart:
			col.start = ev
		case journal.EventOpEnd:
			col.end = ev
		case journal.EventConflict:
			col.conflicts = append(col.conflicts, *ev)
			if sc := col.stepFor(*ev); sc != nil {
				sc.step.Status = "conflict"
				sc.step.Observed = ev.Observed
				sc.step.Desired = ev.Expected
				sc.step.Error = ev.Error
			}
		case journal.EventResourceIntent:
			if sc := col.stepFor(*ev); sc != nil {
				if ev.Original != nil {
					sc.step.Original = ev.Original
				}
				if ev.Desired != nil {
					sc.step.Desired = ev.Desired
				}
			}
		case journal.EventResourceResult:
			if sc := col.stepFor(*ev); sc != nil {
				sc.hasResult = true
				status := ev.Status
				if status == "" {
					switch ev.Phase {
					case "ok":
						status = journal.StatusOk
					case "error":
						status = journal.StatusError
					}
				}
				sc.step.Status = status
				if ev.Observed != nil {
					sc.step.Observed = ev.Observed
				}
				if ev.Original != nil {
					sc.step.Original = ev.Original
				}
				if ev.Desired != nil {
					sc.step.Desired = ev.Desired
				}
				sc.step.DurationMs = ev.DurationMs
				sc.step.Error = ev.Error
			}
		}
	}

	var items []HistoryItem
	// Reverse order (newest first)
	for i := len(opsOrder) - 1; i >= 0; i-- {
		opID := opsOrder[i]
		col := opsMap[opID]

		if col.start != nil {
			switchID, switchName, oneWay := e.resolveHistorySwitch(col.start.Switch)
			item := HistoryItem{
				Op:         opID,
				Action:     col.start.Action,
				Switch:     switchID,
				SwitchName: switchName,
				Workflow:   col.start.Workflow,
				Direction:  col.start.Direction,
				OneWay:     oneWay,
				Context:    col.start.Context,
				StartedAt:  col.start.TS,
				Targets:    col.start.Targets,
				Steps:      col.recordedSteps(),
			}

			if col.end != nil {
				item.EndedAt = col.end.TS
				item.Status = col.end.Status
				item.OK = col.end.OK
				item.Failed = col.end.Failed

				startTime, err1 := time.Parse(time.RFC3339Nano, col.start.TS)
				endTime, err2 := time.Parse(time.RFC3339Nano, col.end.TS)
				if err1 == nil && err2 == nil {
					item.DurationMs = endTime.Sub(startTime).Milliseconds()
				}
			} else {
				item.Status = "interrupted"
			}

			items = append(items, item)
		} else if len(col.conflicts) > 0 {
			firstConf := col.conflicts[0]
			switchID, switchName, oneWay := e.resolveHistorySwitch(firstConf.Switch)
			item := HistoryItem{
				Op:         opID,
				Action:     firstConf.Action,
				Switch:     switchID,
				SwitchName: switchName,
				Workflow:   firstConf.Workflow,
				Direction:  firstConf.Direction,
				OneWay:     oneWay,
				Context:    firstConf.Context,
				StartedAt:  firstConf.TS,
				EndedAt:    firstConf.TS,
				Status:     "conflict",
				Targets:    len(col.conflicts),
				OK:         0,
				Failed:     len(col.conflicts),
				Steps:      col.recordedSteps(),
			}
			items = append(items, item)
		}

		if limit > 0 && len(items) >= limit {
			break
		}
	}

	return items, nil
}

// OperationLog returns the raw journal records for one operation, pretty-printed
// and split into lines for the scrollable log view.
func (e *Engine) OperationLog(op string) ([]string, error) {
	journalPath := filepath.Join(e.stateDir, "journal.jsonl")
	data, err := os.ReadFile(journalPath)
	if err != nil {
		return nil, err
	}

	var lines []string
	for _, raw := range strings.Split(string(data), "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var ev journal.Event
		if err := json.Unmarshal([]byte(raw), &ev); err != nil || ev.Op != op {
			continue
		}
		var buf bytes.Buffer
		if err := json.Indent(&buf, []byte(raw), "", "  "); err != nil {
			lines = append(lines, raw)
		} else {
			lines = append(lines, strings.Split(buf.String(), "\n")...)
		}
		lines = append(lines, "")
	}
	return lines, nil
}

// resolveHistorySwitch maps a journaled switch id to its configured id, name,
// and whether it is a one-way button.
func (e *Engine) resolveHistorySwitch(id string) (string, string, bool) {
	if id == "" {
		id = e.FluxSwitchID()
		if id == "" {
			id = "flux"
		}
	}
	name := id
	oneWay := false
	if e.cfg != nil {
		if sw := e.cfg.SwitchByID(id); sw != nil {
			name = sw.Name
			if wf := e.cfg.Workflows[sw.Workflow]; wf != nil {
				oneWay = wf.OneWay
			}
		}
	}
	return id, name, oneWay
}
