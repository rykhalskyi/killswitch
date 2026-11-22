package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rykhalskyi/killswitch/internal/actions"
	"github.com/rykhalskyi/killswitch/internal/config"
	"github.com/rykhalskyi/killswitch/internal/journal"
)

// ExecuteSwitch runs a switch transition with journaling, verification, and
// crash safety. Cascaded prerequisite/dependent transitions are journaled as
// one operation.
func (e *Engine) ExecuteSwitch(ctx context.Context, switchID string, dir Direction, force bool, onProgress ProgressFunc) (*ExecutionReport, error) {
	recovering := e.state.ActiveOp != nil

	plan, err := e.BuildSwitchPlan(ctx, switchID, dir)
	if err != nil {
		return nil, err
	}

	if len(plan.Missing) > 0 {
		missingRes := plan.Missing[0].Resource
		return nil, fmt.Errorf("target resource %s not found on cluster (missing targets are a hard error)", missingRes.Key())
	}

	legacyAction := string(dir)
	if switchID == e.FluxSwitchID() {
		legacyAction = string(LegacyAction(dir))
	}

	if len(plan.Conflicts) > 0 && !force {
		opID := fmt.Sprintf("01%X", time.Now().UnixNano())
		for _, conf := range plan.Conflicts {
			_ = e.journal.Append(&journal.Event{
				Op:        opID,
				Event:     journal.EventConflict,
				Action:    legacyAction,
				Switch:    conf.Switch,
				Workflow:  conf.Workflow,
				Step:      conf.Step,
				Direction: string(conf.Direction),
				Context:   e.kubeCtx,
				Kind:      conf.Resource.Kind,
				Namespace: conf.Resource.Namespace,
				Name:      conf.Resource.Name,
				Expected:  conf.ExpectedValue,
				Observed:  conf.ObservedValue,
				Error:     conf.ConflictReason,
			})
		}
		return nil, ErrConflict
	}

	if dir == DirectionOn && e.state.ActiveOp != nil && len(e.state.OriginalValues) == 0 && len(e.state.OriginalSuspended) == 0 {
		return nil, fmt.Errorf("%w: cannot restore without recorded original states", ErrUnsafeState)
	}

	opID := fmt.Sprintf("01%X", time.Now().UnixNano())
	primaryWorkflow := ""
	if sw := e.cfg.SwitchByID(switchID); sw != nil {
		primaryWorkflow = sw.Workflow
	}
	startEvent := &journal.Event{
		Op:        opID,
		Event:     journal.EventOpStart,
		Action:    legacyAction,
		Switch:    switchID,
		Workflow:  primaryWorkflow,
		Direction: string(dir),
		Context:   e.kubeCtx,
		Targets:   len(plan.Items),
	}
	if err := e.journal.Append(startEvent); err != nil {
		return nil, fmt.Errorf("appending op.start to journal: %w", err)
	}
	e.state.ApplyEvent(startEvent)
	if err := e.state.Save(); err != nil {
		return nil, fmt.Errorf("saving state: %w", err)
	}

	report := &ExecutionReport{
		Op:        opID,
		Action:    LegacyAction(dir),
		Switch:    switchID,
		Direction: dir,
		Total:     len(plan.Items),
	}

	okCount := 0
	failedCount := 0
	aborted := make(map[string]bool)

	for i, item := range plan.Items {
		if aborted[item.Switch] {
			continue
		}
		handler, err := e.registry.Get(item.Action)
		if err != nil {
			return nil, err
		}
		original := item.OriginalValue
		if item.Direction == DirectionOff && original == nil {
			original, err = handler.Capture(ctx, stepFromItem(item))
			if err != nil {
				failedCount++
				report.Results = append(report.Results, ResourceExecutionResult{
					Switch: item.Switch, Step: item.Step, Action: item.Action,
					Resource: item.Resource, Err: err,
				})
				if !item.ContinueOnError {
					break
				}
				aborted[item.Switch] = true
				continue
			}
			item.OriginalValue = original
		}

		intentEvent := &journal.Event{
			Op:        opID,
			Event:     journal.EventResourceIntent,
			Action:    item.Action,
			Switch:    item.Switch,
			Workflow:  item.Workflow,
			Step:      item.Step,
			Direction: string(item.Direction),
			Phase:     string(statePhaseInProgress),
			Kind:      item.Resource.Kind,
			Namespace: item.Resource.Namespace,
			Name:      item.Resource.Name,
			Original:  item.OriginalValue,
			Desired:   item.DesiredValue,
		}
		if strings.HasPrefix(item.Action, "flux.") {
			if v, ok := actions.Bool(original); ok {
				intentEvent.From = &v
			}
			if v, ok := actions.Bool(item.DesiredValue); ok {
				intentEvent.To = &v
			}
		}
		if err := e.journal.Append(intentEvent); err != nil {
			return nil, fmt.Errorf("appending resource.intent for %s: %w", item.Resource.Key(), err)
		}
		e.state.ApplyEvent(intentEvent)
		_ = e.state.Save()

		start := time.Now()
		var observed any
		var applyErr error
		useRestore := item.Direction == DirectionOn && item.OriginalValue != nil &&
			handler.Equal(item.DesiredValue, item.OriginalValue)
		if useRestore {
			observed, applyErr = handler.Restore(ctx, stepFromItem(item), item.OriginalValue)
		} else {
			observed, applyErr = handler.Apply(ctx, stepFromItem(item), item.DesiredValue)
		}
		durationMs := time.Since(start).Milliseconds()

		resResult := ResourceExecutionResult{
			Switch:        item.Switch,
			Step:          item.Step,
			Action:        item.Action,
			Resource:      item.Resource,
			DesiredValue:  item.DesiredValue,
			ObservedValue: observed,
			DurationMs:    durationMs,
		}

		resultEvent := &journal.Event{
			Op:         opID,
			Event:      journal.EventResourceResult,
			Action:     item.Action,
			Switch:     item.Switch,
			Workflow:   item.Workflow,
			Step:       item.Step,
			Direction:  string(item.Direction),
			Phase:      string(statePhaseOK),
			Kind:       item.Resource.Kind,
			Namespace:  item.Resource.Namespace,
			Name:       item.Resource.Name,
			Original:   item.OriginalValue,
			Desired:    item.DesiredValue,
			Observed:   observed,
			DurationMs: durationMs,
		}

		if applyErr == nil {
			verified, exists, verifyErr := handler.Observe(ctx, stepFromItem(item))
			if verifyErr != nil {
				applyErr = fmt.Errorf("verifying %s: %w", item.Resource.Key(), verifyErr)
			} else if !exists {
				applyErr = fmt.Errorf("verification failed: %s disappeared", item.Resource.Key())
			} else if !handler.Equal(verified, item.DesiredValue) {
				applyErr = fmt.Errorf("verification failed: expected %v, observed %v", item.DesiredValue, verified)
				observed = verified
				resResult.ObservedValue = verified
				resultEvent.Observed = verified
			}
		}

		e.fillResultLegacy(ctx, &resResult, item, observed)

		if applyErr != nil {
			resResult.Err = applyErr
			failedCount++
			resultEvent.Status = journal.StatusError
			resultEvent.Phase = string(statePhaseError)
			resultEvent.Error = applyErr.Error()
		} else {
			okCount++
			resultEvent.Status = journal.StatusOk
		}

		if err := e.journal.Append(resultEvent); err != nil {
			return nil, fmt.Errorf("appending resource.result for %s: %w", item.Resource.Key(), err)
		}
		e.state.ApplyEvent(resultEvent)
		_ = e.state.Save()

		report.Results = append(report.Results, resResult)

		if onProgress != nil {
			stepStatus := "ok"
			if resResult.Err != nil {
				stepStatus = "error"
			}
			onProgress(i+1, len(plan.Items), item.Resource, stepStatus, resResult.Err)
		}

		if applyErr != nil {
			if !item.ContinueOnError {
				break
			}
			// Stop this cascaded workflow on its first failure but let the
			// prerequisite transition continue (D-17).
			aborted[item.Switch] = true
		}
	}

	report.OK = okCount
	report.Failed = failedCount

	var opEndStatus string
	if failedCount == 0 && okCount == len(plan.Items) {
		opEndStatus = journal.StatusSuccess
	} else if okCount > 0 {
		opEndStatus = journal.StatusPartial
	} else {
		opEndStatus = journal.StatusFailed
	}
	report.Status = opEndStatus

	endEvent := &journal.Event{
		Op:        opID,
		Event:     journal.EventOpEnd,
		Switch:    switchID,
		Workflow:  primaryWorkflow,
		Direction: string(dir),
		Status:    opEndStatus,
		OK:        okCount,
		Failed:    failedCount,
	}
	if err := e.journal.Append(endEvent); err != nil {
		return nil, fmt.Errorf("appending op.end: %w", err)
	}
	e.state.ApplyEvent(endEvent)
	if err := e.state.Save(); err != nil {
		return nil, fmt.Errorf("saving state: %w", err)
	}

	report.SwitchState, _ = e.switchState(ctx, switchID)
	if switchID == e.FluxSwitchID() {
		report.GlobalStatus = e.legacyGlobalStatus(ctx)
	} else {
		report.GlobalStatus = GlobalStatusPartial
		if opEndStatus == journal.StatusSuccess {
			switch report.SwitchState {
			case SwitchOff:
				report.GlobalStatus = GlobalStatusSuspended
			case SwitchOn:
				report.GlobalStatus = GlobalStatusRestored
			}
		}
	}

	if recovering {
		if st, statusErr := e.Status(ctx); statusErr == nil {
			report.GlobalStatus = st.GlobalStatus
		} else {
			report.GlobalStatus = GlobalStatusPartial
		}
	}

	if opEndStatus != journal.StatusSuccess {
		return report, ErrPartialFailure
	}

	return report, nil
}

func stepFromItem(item PlanItem) config.Step {
	return config.Step{
		ID:      item.Step,
		Action:  item.Action,
		Target:  item.Resource,
		Timeout: item.Timeout,
	}
}

func (e *Engine) fillResultLegacy(ctx context.Context, res *ResourceExecutionResult, item PlanItem, observed any) {
	if !strings.HasPrefix(item.Action, "flux.") {
		return
	}
	if v, ok := actions.Bool(observed); ok {
		res.ObservedSuspended = v
	}
	if v, ok := actions.Bool(item.DesiredValue); ok {
		res.TargetSuspended = v
	}
	if e.fluxClient != nil {
		if info, err := e.fluxClient.GetResource(ctx, item.Resource.Kind, item.Resource.Namespace, item.Resource.Name); err == nil {
			res.Generation = info.Generation
			res.ObservedGeneration = info.ObservedGeneration
			res.Ready = info.Ready
			res.ReadyReason = info.ReadyReason
			res.ReadyMessage = info.ReadyMessage
			res.LaggingObservation = info.ObservedGeneration < info.Generation
		}
	}
}

// statePhase aliases avoid importing state just for string conversion.
type statePhase string

const (
	statePhaseInProgress statePhase = "in_progress"
	statePhaseOK         statePhase = "ok"
	statePhaseError      statePhase = "error"
)
