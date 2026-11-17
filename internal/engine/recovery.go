package engine

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

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
	// Only a fully successful operation establishes a trustworthy baseline.
	// After a partial or failed run some targets were intentionally left in a
	// different state, so comparing against them would produce false conflicts.
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

// recoveryDriftConflicts reports completed resources from an interrupted
// operation that are excluded from the execution subset (for example, targets
// already verified before a resume). Without this check, drift on those
// resources would go unnoticed and the resume would journal a clean completion.
func (e *Engine) recoveryDriftConflicts(ctx context.Context, planned map[string]bool) ([]PlanItem, error) {
	if e.state.ActiveOp == nil {
		return nil, nil
	}

	byKey := make(map[string]config.Resource, len(e.cfg.Flux.Resources))
	for _, res := range e.cfg.Flux.Resources {
		byKey[res.Key()] = res
	}

	var conflicts []PlanItem
	for key, rs := range e.state.ActiveOp.ResourceStates {
		if planned[key] || rs.Phase != state.PhaseOK {
			continue
		}
		res, ok := byKey[key]
		if !ok {
			continue
		}

		info, err := e.fluxClient.GetResource(ctx, res.Kind, res.Namespace, res.Name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("getting resource %s during recovery: %w", key, err)
		}
		if info.Suspended == rs.Target {
			continue
		}

		expected := rs.Target
		conflicts = append(conflicts, PlanItem{
			Resource:          res,
			Info:              info,
			CurrentSuspended:  info.Suspended,
			TargetSuspended:   rs.Target,
			Original:          info.Suspended,
			Exists:            true,
			ExpectedSuspended: &expected,
			Conflict:          true,
			ConflictReason: fmt.Sprintf(
				"resource was modified outside killswitch: expected spec.suspend=%t, observed %t",
				expected, info.Suspended,
			),
		})
	}

	return conflicts, nil
}
