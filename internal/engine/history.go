package engine

import (
	"path/filepath"
	"time"

	"github.com/rykhalskyi/killswitch/internal/journal"
)

// HistoryItem represents an aggregated record of a past operation.
type HistoryItem struct {
	Op         string `json:"op"`
	Action     string `json:"action"`
	Switch     string `json:"switch,omitempty"`
	Direction  string `json:"direction,omitempty"`
	Context    string `json:"context"`
	StartedAt  string `json:"startedAt"`
	EndedAt    string `json:"endedAt,omitempty"`
	Status     string `json:"status"` // success, partial, failed, interrupted
	Targets    int    `json:"targets"`
	OK         int    `json:"ok"`
	Failed     int    `json:"failed"`
	DurationMs int64  `json:"durationMs"`
}

// History reads operations from the journal file, aggregating op.start and op.end events.
// Results are returned in reverse chronological order (newest first).
// If limit > 0, returns at most limit items.
func (e *Engine) History(limit int) ([]HistoryItem, error) {
	journalPath := filepath.Join(e.stateDir, "journal.jsonl")
	events, err := journal.ReadAll(journalPath)
	if err != nil {
		return nil, err
	}

	type opCollector struct {
		start     *journal.Event
		end       *journal.Event
		conflicts []journal.Event
	}

	opsOrder := []string{}
	opsMap := make(map[string]*opCollector)

	for _, ev := range events {
		if ev.Op == "" {
			continue
		}

		col, exists := opsMap[ev.Op]
		if !exists {
			col = &opCollector{}
			opsMap[ev.Op] = col
			opsOrder = append(opsOrder, ev.Op)
		}

		if ev.Event == journal.EventOpStart {
			col.start = &ev
		} else if ev.Event == journal.EventOpEnd {
			col.end = &ev
		} else if ev.Event == journal.EventConflict {
			col.conflicts = append(col.conflicts, ev)
		}
	}

	var items []HistoryItem
	// Reverse order (newest first)
	for i := len(opsOrder) - 1; i >= 0; i-- {
		opID := opsOrder[i]
		col := opsMap[opID]

		if col.start != nil {
			item := HistoryItem{
				Op:        opID,
				Action:    col.start.Action,
				Switch:    col.start.Switch,
				Direction: col.start.Direction,
				Context:   col.start.Context,
				StartedAt: col.start.TS,
				Targets:   col.start.Targets,
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
			item := HistoryItem{
				Op:        opID,
				Action:    firstConf.Action,
				Switch:    firstConf.Switch,
				Direction: firstConf.Direction,
				Context:   firstConf.Context,
				StartedAt: firstConf.TS,
				EndedAt:   firstConf.TS,
				Status:    "conflict",
				Targets:   len(col.conflicts),
				OK:        0,
				Failed:    len(col.conflicts),
			}
			items = append(items, item)
		}

		if limit > 0 && len(items) >= limit {
			break
		}
	}

	return items, nil
}
