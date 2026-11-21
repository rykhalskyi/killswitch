package engine

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/rykhalskyi/killswitch/internal/actions"
	"github.com/rykhalskyi/killswitch/internal/config"
)

// Status queries every configured switch and returns a comprehensive report.
func (e *Engine) Status(ctx context.Context) (*StatusReport, error) {
	report := &StatusReport{Context: e.kubeCtx}

	if e.state.ActiveOp != nil {
		report.Incomplete = true
		report.IncompleteMsg = fmt.Sprintf("Operation %s (%s) was interrupted and did not complete", e.state.ActiveOp.Op, e.state.ActiveOp.Action)
		opStr := fmt.Sprintf("%s (%s)", e.state.ActiveOp.Op, e.state.ActiveOp.Action)
		report.ActiveOp = &opStr
	}
	if e.state.LastCompletedOp != nil {
		last := e.state.LastCompletedOp
		lastStr := fmt.Sprintf("%s %s (status: %s, ok: %d, failed: %d)", last.Action, last.Op, last.Status, last.OK, last.Failed)
		report.LastCompletedOp = &lastStr
	}

	for i := range e.cfg.Switches {
		st, err := e.switchStatus(ctx, &e.cfg.Switches[i])
		if err != nil {
			return nil, err
		}
		report.Switches = append(report.Switches, st)
	}

	fluxID := e.FluxSwitchID()
	if fluxID != "" {
		resources, global, err := e.fluxResources(ctx, fluxID)
		if err != nil {
			return nil, err
		}
		report.Resources = resources
		report.GlobalStatus = global
	} else {
		report.GlobalStatus = GlobalStatusUnknown
	}

	return report, nil
}

// StatusForSwitch returns the live status of one switch.
func (e *Engine) StatusForSwitch(ctx context.Context, switchID string) (*SwitchStatus, error) {
	sw := e.cfg.SwitchByID(switchID)
	if sw == nil {
		return nil, fmt.Errorf("unknown switch %q", switchID)
	}
	st, err := e.switchStatus(ctx, sw)
	if err != nil {
		return nil, err
	}
	return &st, nil
}

func (e *Engine) switchStatus(ctx context.Context, sw *config.Switch) (SwitchStatus, error) {
	wf := e.cfg.Workflows[sw.Workflow]
	st := SwitchStatus{
		ID:          sw.ID,
		Name:        sw.Name,
		Description: sw.Description,
		Workflow:    sw.Workflow,
	}
	if wf == nil {
		st.State = SwitchUnknown
		return st, nil
	}
	st.Requires = wf.Requires.Flux

	stateNow, err := e.switchState(ctx, sw.ID)
	if err != nil {
		return st, err
	}
	st.State = stateNow

	for _, step := range wf.Steps {
		handler, err := e.registry.Get(step.Action)
		if err != nil {
			st.Steps = append(st.Steps, StepStatus{Step: step.ID, Action: step.Action, Target: step.Target, State: string(SwitchUnknown), Error: err.Error()})
			continue
		}
		observed, exists, err := handler.Observe(ctx, step)
		if err != nil {
			st.Steps = append(st.Steps, StepStatus{Step: step.ID, Action: step.Action, Target: step.Target, State: string(SwitchUnknown), Error: err.Error()})
			continue
		}
		ss := StepStatus{
			Step:     step.ID,
			Action:   step.Action,
			Target:   step.Target,
			Exists:   exists,
			Observed: observed,
			Original: e.originalFor(sw.ID, wf.ID, step),
		}
		ss.State = e.stepState(step, handler, observed, exists, ss.Original)
		st.Steps = append(st.Steps, ss)
	}
	return st, nil
}

func (e *Engine) stepState(step config.Step, handler actions.Handler, observed any, exists bool, original any) string {
	if !exists {
		return string(SwitchMissing)
	}
	offDesired, offErr := handler.Desired(step, string(DirectionOff), original)
	onDesired, onErr := handler.Desired(step, string(DirectionOn), original)
	offMatch := offErr == nil && handler.Equal(observed, offDesired)
	onMatch := onErr == nil && handler.Equal(observed, onDesired)
	if onErr != nil {
		onMatch = !offMatch
	}
	switch {
	case offMatch && onMatch:
		return string(SwitchOn)
	case offMatch:
		return string(SwitchOff)
	case onMatch:
		return string(SwitchOn)
	default:
		return string(SwitchMixed)
	}
}

// switchState derives the ON/OFF/MIXED/DRIFT position from live observation.
func (e *Engine) switchState(ctx context.Context, switchID string) (SwitchState, error) {
	sw := e.cfg.SwitchByID(switchID)
	if sw == nil {
		return SwitchUnknown, fmt.Errorf("unknown switch %q", switchID)
	}
	wf := e.cfg.Workflows[sw.Workflow]
	if wf == nil || len(wf.Steps) == 0 {
		return SwitchUnknown, nil
	}

	offMatches, onMatches, expectedMatches, expectedKnown := 0, 0, 0, 0
	for _, step := range wf.Steps {
		handler, err := e.registry.Get(step.Action)
		if err != nil {
			return SwitchUnknown, err
		}
		observed, exists, err := handler.Observe(ctx, step)
		if err != nil {
			return SwitchUnknown, err
		}
		if !exists {
			return SwitchMissing, nil
		}
		original := e.originalFor(sw.ID, wf.ID, step)
		offDesired, offErr := handler.Desired(step, string(DirectionOff), original)
		onDesired, onErr := handler.Desired(step, string(DirectionOn), original)
		off := offErr == nil && handler.Equal(observed, offDesired)
		on := onErr == nil && handler.Equal(observed, onDesired)
		if onErr != nil {
			on = !off
		}
		if off {
			offMatches++
		}
		if on {
			onMatches++
		}
		if expected, ok := e.expectedFor(sw.ID, wf.ID, step, handler); ok {
			expectedKnown++
			if handler.Equal(observed, expected) {
				expectedMatches++
			}
		}
	}

	if offMatches == len(wf.Steps) {
		return SwitchOff, nil
	}
	if onMatches == len(wf.Steps) {
		if expectedKnown > 0 && expectedMatches == 0 {
			return SwitchDrift, nil
		}
		return SwitchOn, nil
	}
	return SwitchMixed, nil
}

// legacyGlobalStatus preserves the Phase 1/2 Flux global status semantics.
func (e *Engine) legacyGlobalStatus(ctx context.Context) GlobalStatus {
	fluxID := e.FluxSwitchID()
	if fluxID == "" {
		return GlobalStatusUnknown
	}
	_, global, err := e.fluxResources(ctx, fluxID)
	if err != nil {
		return GlobalStatusPartial
	}
	return global
}

func (e *Engine) fluxResources(ctx context.Context, switchID string) ([]ResourceStatusItem, GlobalStatus, error) {
	sw := e.cfg.SwitchByID(switchID)
	if sw == nil {
		return nil, GlobalStatusUnknown, nil
	}
	wf := e.cfg.Workflows[sw.Workflow]
	if wf == nil {
		return nil, GlobalStatusUnknown, nil
	}

	allSuspended := true
	allMatchesOriginal := true
	anyMissing := false
	var resources []ResourceStatusItem

	for _, step := range wf.Steps {
		item := ResourceStatusItem{
			Kind:      step.Target.Kind,
			Namespace: step.Target.Namespace,
			Name:      step.Target.Name,
		}
		if orig, ok := e.state.OriginalSuspended[step.Target.Key()]; ok {
			v := orig
			item.OriginalSuspended = &v
		}
		if e.state.ActiveOp != nil {
			if rs, ok := e.state.ActiveOp.ResourceStates[step.Target.Key()]; ok {
				item.ActivePhase = string(rs.Phase)
			}
		}

		if e.fluxClient == nil {
			item.Exists = false
			anyMissing = true
			resources = append(resources, item)
			continue
		}
		info, err := e.fluxClient.GetResource(ctx, step.Target.Kind, step.Target.Namespace, step.Target.Name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				item.Exists = false
				item.Ready = "NotFound"
				anyMissing = true
				allSuspended = false
				allMatchesOriginal = false
			} else {
				return nil, GlobalStatusPartial, fmt.Errorf("getting resource %s: %w", step.Target.Key(), err)
			}
		} else {
			item.Exists = true
			item.CurrentSuspended = info.Suspended
			item.Generation = info.Generation
			item.ObservedGeneration = info.ObservedGeneration
			item.Ready = info.Ready
			item.ReadyReason = info.ReadyReason
			item.ReadyMessage = info.ReadyMessage
			item.LaggingObservation = info.ObservedGeneration < info.Generation
			if !info.Suspended {
				allSuspended = false
			}
			orig := false
			if item.OriginalSuspended != nil {
				orig = *item.OriginalSuspended
			}
			if info.Suspended != orig {
				allMatchesOriginal = false
			}
		}
		resources = append(resources, item)
	}

	last := e.state.LastCompletedOp
	switch {
	case anyMissing:
		return resources, GlobalStatusPartial, nil
	case allSuspended:
		return resources, GlobalStatusSuspended, nil
	case allMatchesOriginal && last != nil && last.Action == string(ActionRestore):
		return resources, GlobalStatusRestored, nil
	default:
		return resources, GlobalStatusPartial, nil
	}
}
