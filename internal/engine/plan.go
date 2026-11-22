package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rykhalskyi/killswitch/internal/actions"
	"github.com/rykhalskyi/killswitch/internal/config"
	"github.com/rykhalskyi/killswitch/internal/journal"
	"github.com/rykhalskyi/killswitch/internal/state"
)

// orderedSteps returns steps in ON order and reversed for OFF. One-way
// workflows have no ON side, so their buttons always run in declared order.
func orderedSteps(wf *config.Workflow, dir Direction) []config.Step {
	steps := append([]config.Step(nil), wf.Steps...)
	if dir == DirectionOff && !wf.OneWay {
		for i, j := 0, len(steps)-1; i < j; i, j = i+1, j-1 {
			steps[i], steps[j] = steps[j], steps[i]
		}
	}
	return steps
}

func stepKey(workflowID, stepID string) string {
	return workflowID + "/" + stepID
}

// BuildSwitchPlan builds a plan for one switch transition, including any
// prerequisite or dependent cascade.
func (e *Engine) BuildSwitchPlan(ctx context.Context, switchID string, dir Direction) (*Plan, error) {
	if e.cfg == nil {
		return nil, errors.New("no configuration loaded")
	}
	sw := e.cfg.SwitchByID(switchID)
	if sw == nil {
		return nil, fmt.Errorf("unknown switch %q", switchID)
	}
	wf := e.cfg.Workflows[sw.Workflow]
	if wf == nil {
		return nil, fmt.Errorf("switch %q references unknown workflow %q", switchID, sw.Workflow)
	}
	if wf.OneWay && dir != DirectionOff {
		return nil, fmt.Errorf("switch %q is one-way; only the OFF transition is supported", switchID)
	}

	plan := &Plan{
		Action:    LegacyAction(dir),
		Switch:    switchID,
		Workflow:  wf.ID,
		Direction: dir,
	}
	seen := make(map[string]bool)
	if err := e.addSwitchSegment(ctx, plan, sw, wf, dir, false, false, seen); err != nil {
		return nil, err
	}

	drift, err := e.recoveryDriftConflicts(ctx, plan)
	if err != nil {
		return nil, err
	}
	plan.Conflicts = append(plan.Conflicts, drift...)
	return plan, nil
}

// addSwitchSegment prepends prerequisite transitions and appends the segment's
// own steps. Prerequisites run before the primary transition.
func (e *Engine) addSwitchSegment(ctx context.Context, plan *Plan, sw *config.Switch, wf *config.Workflow, dir Direction, cascade, continueOnError bool, seen map[string]bool) error {
	key := sw.ID + "/" + string(dir)
	if seen[key] {
		return nil
	}
	seen[key] = true

	fluxID := e.FluxSwitchID()
	// Prerequisites are enforced when a dependent enters its non-default (OFF)
	// state: scaling a Flux-managed workload down must suspend Flux first or
	// reconciliation immediately brings it back.
	if dir == DirectionOff && wf.Requires.Flux != "" && wf.Requires.Flux != config.RequiresAny && fluxID != "" && fluxID != sw.ID {
		reqDir := DirectionOff
		if wf.Requires.Flux == config.RequiresActive {
			reqDir = DirectionOn
		}
		stateNow, _ := e.switchState(ctx, fluxID)
		need := (wf.Requires.Flux == config.RequiresSuspended && stateNow != SwitchOff) ||
			(wf.Requires.Flux == config.RequiresActive && stateNow != SwitchOn)
		if need {
			reqSw := e.cfg.SwitchByID(fluxID)
			reqWf := e.cfg.Workflows[reqSw.Workflow]
			if reqWf != nil {
				if err := e.addSwitchSegment(ctx, plan, reqSw, reqWf, reqDir, true, false, seen); err != nil {
					return err
				}
			}
		}
	}

	items, conflicts, missing, err := e.buildSteps(ctx, sw, wf, dir, cascade, continueOnError)
	if err != nil {
		return err
	}
	plan.Items = append(plan.Items, items...)
	plan.Conflicts = append(plan.Conflicts, conflicts...)
	plan.Missing = append(plan.Missing, missing...)
	return nil
}

func (e *Engine) buildSteps(ctx context.Context, sw *config.Switch, wf *config.Workflow, dir Direction, cascade, continueOnError bool) ([]PlanItem, []PlanItem, []PlanItem, error) {
	steps := orderedSteps(wf, dir)
	narrow := e.state != nil && e.state.ActiveOp != nil &&
		(e.state.ActiveOp.Switch == sw.ID ||
			(e.state.ActiveOp.Switch == "" && sw.ID == e.FluxSwitchID()) ||
			e.workflowTouched(e.state.ActiveOp, wf.ID))
	var items []PlanItem
	var conflicts []PlanItem
	var missing []PlanItem

	for _, step := range steps {
		sk := stepKey(wf.ID, step.ID)
		if narrow && !e.stepTouched(step, sk, dir) {
			continue
		}

		handler, err := e.registry.Get(step.Action)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := handler.Validate(step); err != nil {
			return nil, nil, nil, fmt.Errorf("workflow %q step %q: %w", wf.ID, step.ID, err)
		}

		observed, exists, err := handler.Observe(ctx, step)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("observing %s: %w", step.Target.Key(), err)
		}

		original := e.originalFor(sw.ID, wf.ID, step)
		desired, err := handler.Desired(step, string(dir), original)
		if err != nil {
			if errors.Is(err, actions.ErrMissingOriginal) {
				return nil, nil, nil, fmt.Errorf("%w: step %q (%s) has no recorded original; set states.on.default to allow it", ErrUnsafeState, step.ID, step.Target.Key())
			}
			return nil, nil, nil, fmt.Errorf("planning %s: %w", step.Target.Key(), err)
		}

		item := PlanItem{
			Switch:          sw.ID,
			Workflow:        wf.ID,
			Step:            step.ID,
			Action:          step.Action,
			Direction:       dir,
			Resource:        step.Target,
			Exists:          exists,
			ObservedValue:   observed,
			DesiredValue:    desired,
			OriginalValue:   original,
			Cascade:         cascade,
			ContinueOnError: continueOnError,
		}

		if !exists {
			missing = append(missing, item)
			items = append(items, item)
			continue
		}

		if expected, ok := e.expectedFor(sw.ID, wf.ID, step, handler); ok {
			item.ExpectedValue = expected
			item.ExpectedSuspended = boolPtrIf(handler, expected)
			if !handler.Equal(observed, expected) {
				item.Conflict = true
				item.ConflictReason = fmt.Sprintf("resource was modified outside killswitch: expected %v, observed %v", expected, observed)
				conflicts = append(conflicts, item)
			}
		}

		item.WillChange = !handler.Equal(observed, desired)
		e.fillLegacyFluxFields(&item, step, observed, original, desired)
		items = append(items, item)
	}

	return items, conflicts, missing, nil
}

func boolPtrIf(h actions.Handler, v any) *bool {
	if b, ok := actions.Bool(v); ok {
		_ = h
		return &b
	}
	return nil
}

// fillLegacyFluxFields keeps the Phase 1/2 Flux report fields populated.
func (e *Engine) fillLegacyFluxFields(item *PlanItem, step config.Step, observed, original, desired any) {
	if !strings.HasPrefix(step.Action, "flux.") {
		return
	}
	if v, ok := actions.Bool(observed); ok {
		item.CurrentSuspended = v
	}
	if v, ok := actions.Bool(desired); ok {
		item.TargetSuspended = v
	}
	if v, ok := actions.Bool(original); ok {
		item.Original = v
	}
}

// originalFor resolves the typed original recorded for a step.
func (e *Engine) originalFor(switchID, workflowID string, step config.Step) any {
	sk := stepKey(workflowID, step.ID)
	if e.state != nil {
		if ov, ok := e.state.OriginalValues[sk]; ok {
			return ov.Value
		}
		if strings.HasPrefix(step.Action, "flux.") {
			if v, ok := e.state.OriginalSuspended[step.Target.Key()]; ok {
				return v
			}
		}
	}
	return nil
}

// expectedFor derives the state killswitch believes a step should have.
func (e *Engine) expectedFor(switchID, workflowID string, step config.Step, handler actions.Handler) (any, bool) {
	if e.state == nil {
		return nil, false
	}
	sk := stepKey(workflowID, step.ID)
	if e.state.ActiveOp != nil {
		if ss, ok := e.state.ActiveOp.StepStates[sk]; ok && ss.Phase == state.PhaseOK {
			return ss.Desired, true
		}
		if rs, ok := e.state.ActiveOp.ResourceStates[step.Target.Key()]; ok && rs.Phase == state.PhaseOK {
			return rs.Target, true
		}
		return nil, false
	}

	last := e.state.LastCompletedOp
	if last == nil || last.Status != journal.StatusSuccess {
		return nil, false
	}
	if last.Switch != "" && last.Switch != switchID {
		return nil, false
	}
	lastDir := DirectionOn
	if last.Direction == string(DirectionOff) || last.Action == string(ActionSuspend) {
		lastDir = DirectionOff
	}
	original := e.originalFor(switchID, workflowID, step)
	expected, err := handler.Desired(step, string(lastDir), original)
	if err != nil {
		return nil, false
	}
	return expected, true
}

// workflowTouched reports whether an interrupted cascade journaled steps for
// this workflow.
func (e *Engine) workflowTouched(active *state.ActiveOperation, workflowID string) bool {
	prefix := workflowID + "/"
	for key := range active.StepStates {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// stepTouched reports whether an interrupted operation touched the step.
func (e *Engine) stepTouched(step config.Step, sk string, dir Direction) bool {
	active := e.state.ActiveOp
	if active == nil {
		return true
	}
	if ss, ok := active.StepStates[sk]; ok {
		if dir == DirectionOff && ss.Phase == state.PhaseOK {
			return false
		}
		return true
	}
	rs, ok := active.ResourceStates[step.Target.Key()]
	if !ok {
		return false
	}
	if dir == DirectionOff && rs.Phase == state.PhaseOK {
		return false
	}
	return true
}

// recoveryDriftConflicts reports verified steps that drift while a narrowed
// recovery excludes them from the planned subset.
func (e *Engine) recoveryDriftConflicts(ctx context.Context, plan *Plan) ([]PlanItem, error) {
	active := e.state.ActiveOp
	if active == nil {
		return nil, nil
	}

	plannedSteps := make(map[string]bool)
	plannedResources := make(map[string]bool)
	for _, item := range plan.Items {
		plannedSteps[stepKey(item.Workflow, item.Step)] = true
		plannedResources[item.Resource.Key()] = true
	}

	activeSwitchID := active.Switch
	if activeSwitchID == "" {
		activeSwitchID = e.FluxSwitchID()
	}
	sw := e.cfg.SwitchByID(activeSwitchID)
	if sw == nil {
		return nil, nil
	}
	wf := e.cfg.Workflows[sw.Workflow]
	if wf == nil {
		return nil, nil
	}

	var conflicts []PlanItem
	hasStepStates := false
	for _, step := range wf.Steps {
		if _, ok := active.StepStates[stepKey(wf.ID, step.ID)]; ok {
			hasStepStates = true
			break
		}
	}
	if hasStepStates {
		for _, step := range wf.Steps {
			sk := stepKey(wf.ID, step.ID)
			ss, ok := active.StepStates[sk]
			if !ok || plannedSteps[sk] || ss.Phase != state.PhaseOK {
				continue
			}
			item, err := e.driftItem(ctx, sw.ID, wf.ID, step, ss.Desired)
			if err != nil {
				return nil, err
			}
			if item != nil {
				conflicts = append(conflicts, *item)
			}
		}
		return conflicts, nil
	}

	// v1 journals key recovery by resource.
	for _, step := range wf.Steps {
		rs, ok := active.ResourceStates[step.Target.Key()]
		if !ok || plannedResources[step.Target.Key()] || rs.Phase != state.PhaseOK {
			continue
		}
		item, err := e.driftItem(ctx, sw.ID, wf.ID, step, rs.Target)
		if err != nil {
			return nil, err
		}
		if item != nil {
			conflicts = append(conflicts, *item)
		}
	}
	return conflicts, nil
}

func (e *Engine) driftItem(ctx context.Context, switchID, workflowID string, step config.Step, desired any) (*PlanItem, error) {
	handler, err := e.registry.Get(step.Action)
	if err != nil {
		return nil, err
	}
	observed, exists, err := handler.Observe(ctx, step)
	if err != nil {
		return nil, fmt.Errorf("getting resource %s during recovery: %w", step.Target.Key(), err)
	}
	if !exists || handler.Equal(observed, desired) {
		return nil, nil
	}
	item := PlanItem{
		Switch:         switchID,
		Workflow:       workflowID,
		Step:           step.ID,
		Action:         step.Action,
		Resource:       step.Target,
		Exists:         true,
		ObservedValue:  observed,
		ExpectedValue:  desired,
		Conflict:       true,
		ConflictReason: fmt.Sprintf("resource was modified outside killswitch: expected %v, observed %v", desired, observed),
	}
	e.fillLegacyFluxFields(&item, step, observed, nil, desired)
	item.ExpectedSuspended = boolPtrIf(handler, desired)
	return &item, nil
}
