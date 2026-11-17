package engine

import (
	"github.com/rykhalskyi/killswitch/internal/config"
	"github.com/rykhalskyi/killswitch/internal/journal"
	"github.com/rykhalskyi/killswitch/internal/state"
)

// planResources narrows an operation to resources recorded in an interrupted
// operation. Resources outside that operation were never touched by it.
func (e *Engine) planResources(action Action) []config.Resource {
	ordered := e.OrderedResources(action)
	if e.state.ActiveOp == nil || len(e.state.ActiveOp.ResourceStates) == 0 {
		return ordered
	}

	var filtered []config.Resource
	for _, res := range ordered {
		rs, touched := e.state.ActiveOp.ResourceStates[res.Key()]
		if !touched {
			continue
		}
		if action == ActionSuspend && rs.Phase == state.PhaseOK {
			continue
		}
		filtered = append(filtered, res)
	}
	return filtered
}

// expectedSuspended returns the suspension state killswitch believes a target
// should have. During an interrupted operation only completed resources have a
// reliable expectation: an intent without a result may or may not have reached
// the cluster, so it must not be treated as drift.
func (e *Engine) expectedSuspended(key string) (*bool, bool) {
	if e.state.ActiveOp != nil {
		rs, touched := e.state.ActiveOp.ResourceStates[key]
		if !touched || rs.Phase != state.PhaseOK {
			return nil, false
		}
		target := rs.Target
		return &target, true
	}

	last := e.state.LastCompletedOp
	if last == nil || last.Status != journal.StatusSuccess {
		return nil, false
	}
	original, recorded := e.state.OriginalSuspended[key]
	if !recorded {
		return nil, false
	}
	expected := original
	if last.Action == string(ActionSuspend) {
		expected = true
	}
	return &expected, true
}
