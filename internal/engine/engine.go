package engine

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/rykhalskyi/killswitch/internal/config"
	"github.com/rykhalskyi/killswitch/internal/flux"
	"github.com/rykhalskyi/killswitch/internal/journal"
	"github.com/rykhalskyi/killswitch/internal/state"
)

// Engine orchestrates reading, planning, executing, and journaling Flux freeze operations.
type Engine struct {
	cfg        *config.Config
	fluxClient flux.Client
	journal    *journal.Journal
	state      *state.State
	lock       *state.Lock
	stateDir   string
	kubeCtx    string
	logger     *slog.Logger
}

// New creates an uninitialized Engine. Call Init() to acquire the lock and recover state.
func New(cfg *config.Config, client flux.Client, stateDir, kubeCtx string, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		cfg:        cfg,
		fluxClient: client,
		stateDir:   stateDir,
		kubeCtx:    kubeCtx,
		logger:     logger,
	}
}

// Init acquires the exclusive lock, opens the journal, and recovers or loads state.json.
func (e *Engine) Init(ctx context.Context) error {
	if e.stateDir == "" {
		defDir, err := state.DefaultStateDir()
		if err != nil {
			return fmt.Errorf("resolving default state dir: %w", err)
		}
		e.stateDir = defDir
	}

	if err := os.MkdirAll(e.stateDir, 0700); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	// Acquire lock
	l, err := state.AcquireLock(e.stateDir)
	if err != nil {
		if err == state.ErrLocked {
			return ErrLockContention
		}
		return fmt.Errorf("acquiring lock: %w", err)
	}
	e.lock = l

	// Open journal
	journalPath := filepath.Join(e.stateDir, "journal.jsonl")
	j, err := journal.Open(journalPath)
	if err != nil {
		_ = e.lock.Release()
		return fmt.Errorf("opening journal: %w", err)
	}
	e.journal = j

	// Read journal events for state recovery
	events, err := journal.ReadAll(journalPath)
	if err != nil && !os.IsNotExist(err) {
		_ = e.Close()
		return fmt.Errorf("reading journal events: %w", err)
	}

	// Recover or load state
	st, err := state.LoadOrRebuild(e.stateDir, events)
	if err != nil {
		_ = e.Close()
		return fmt.Errorf("loading or rebuilding state: %w", err)
	}
	e.state = st

	// Discover GVRs
	if err := e.fluxClient.DiscoverGVRs(ctx); err != nil {
		e.logger.Warn("gvr discovery failed, falling back to defaults", "error", err)
	}

	return nil
}

// Close releases resources (lock and journal).
func (e *Engine) Close() error {
	var errs []error
	if e.journal != nil {
		if err := e.journal.Close(); err != nil {
			errs = append(errs, err)
		}
		e.journal = nil
	}
	if e.lock != nil {
		if err := e.lock.Release(); err != nil {
			errs = append(errs, err)
		}
		e.lock = nil
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// State returns the current in-memory state snapshot.
func (e *Engine) State() *state.State {
	return e.state
}

// OrderedResources returns target resources ordered according to the operation:
// Suspend: Kustomizations in reverse config order, then HelmReleases in reverse config order.
// Restore: HelmReleases in forward config order, then Kustomizations in forward config order.
func (e *Engine) OrderedResources(action Action) []config.Resource {
	var kustomizations []config.Resource
	var helmReleases []config.Resource

	for _, r := range e.cfg.Flux.Resources {
		if r.Kind == config.KindKustomization {
			kustomizations = append(kustomizations, r)
		} else if r.Kind == config.KindHelmRelease {
			helmReleases = append(helmReleases, r)
		}
	}

	var ordered []config.Resource
	if action == ActionSuspend {
		// Reverse kustomizations
		for i := len(kustomizations) - 1; i >= 0; i-- {
			ordered = append(ordered, kustomizations[i])
		}
		// Reverse helmReleases
		for i := len(helmReleases) - 1; i >= 0; i-- {
			ordered = append(ordered, helmReleases[i])
		}
	} else {
		// Forward helmReleases
		ordered = append(ordered, helmReleases...)
		// Forward kustomizations
		ordered = append(ordered, kustomizations...)
	}

	return ordered
}

// BuildPlan queries the cluster and builds the execution plan for action.
// With an interrupted operation it plans only the resources that operation touched.
func (e *Engine) BuildPlan(ctx context.Context, action Action) (*Plan, error) {
	ordered := e.planResources(action)
	plan := &Plan{
		Action: action,
	}
	planned := make(map[string]bool, len(ordered))

	for _, res := range ordered {
		key := res.Key()
		planned[key] = true
		info, err := e.fluxClient.GetResource(ctx, res.Kind, res.Namespace, res.Name)
		item := PlanItem{
			Resource: res,
		}

		if err != nil {
			if apierrors.IsNotFound(err) {
				item.Exists = false
				plan.Missing = append(plan.Missing, item)
				plan.Items = append(plan.Items, item)
				continue
			}
			return nil, fmt.Errorf("getting resource %s: %w", key, err)
		}

		item.Exists = true
		item.Info = info
		item.CurrentSuspended = info.Suspended

		orig, hasOrig := e.state.OriginalSuspended[key]
		if action == ActionSuspend {
			item.TargetSuspended = true
			if info.Suspended && !hasOrig {
				item.Original = true // suspended outside the tool
			} else {
				item.Original = orig
			}
		} else {
			item.Original = orig
			item.TargetSuspended = orig
		}

		if expected, ok := e.expectedSuspended(key); ok {
			item.ExpectedSuspended = expected
			if info.Suspended != *expected {
				item.Conflict = true
				item.ConflictReason = fmt.Sprintf(
					"resource was modified outside killswitch: expected spec.suspend=%t, observed %t",
					*expected, info.Suspended,
				)
				plan.Conflicts = append(plan.Conflicts, item)
			}
		}

		item.WillChange = item.CurrentSuspended != item.TargetSuspended
		plan.Items = append(plan.Items, item)
	}

	drift, err := e.recoveryDriftConflicts(ctx, planned)
	if err != nil {
		return nil, err
	}
	plan.Conflicts = append(plan.Conflicts, drift...)

	return plan, nil
}

// Status queries all configured targets and returns a comprehensive StatusReport.
func (e *Engine) Status(ctx context.Context) (*StatusReport, error) {
	report := &StatusReport{
		Context: e.kubeCtx,
	}

	if e.state.ActiveOp != nil {
		report.Incomplete = true
		report.IncompleteMsg = fmt.Sprintf("Operation %s (%s) was interrupted and did not complete", e.state.ActiveOp.Op, e.state.ActiveOp.Action)
		opStr := fmt.Sprintf("%s (%s)", e.state.ActiveOp.Op, e.state.ActiveOp.Action)
		report.ActiveOp = &opStr
	}

	if e.state.LastCompletedOp != nil {
		lastStr := fmt.Sprintf("%s %s (status: %s, ok: %d, failed: %d)",
			e.state.LastCompletedOp.Action, e.state.LastCompletedOp.Op,
			e.state.LastCompletedOp.Status, e.state.LastCompletedOp.OK, e.state.LastCompletedOp.Failed)
		report.LastCompletedOp = &lastStr
	}

	allSuspended := true
	allMatchesOriginal := true
	anyMissing := false

	for _, res := range e.cfg.Flux.Resources {
		key := res.Key()
		item := ResourceStatusItem{
			Kind:      res.Kind,
			Namespace: res.Namespace,
			Name:      res.Name,
		}

		if orig, ok := e.state.OriginalSuspended[key]; ok {
			val := orig
			item.OriginalSuspended = &val
		}

		if e.state.ActiveOp != nil {
			if rs, ok := e.state.ActiveOp.ResourceStates[key]; ok {
				item.ActivePhase = string(rs.Phase)
			}
		}

		info, err := e.fluxClient.GetResource(ctx, res.Kind, res.Namespace, res.Name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				item.Exists = false
				item.Ready = "NotFound"
				anyMissing = true
				allSuspended = false
				allMatchesOriginal = false
			} else {
				return nil, fmt.Errorf("getting resource %s: %w", key, err)
			}
		} else {
			item.Exists = true
			item.CurrentSuspended = info.Suspended
			item.Generation = info.Generation
			item.ObservedGeneration = info.ObservedGeneration
			item.Ready = info.Ready
			item.ReadyReason = info.ReadyReason
			item.ReadyMessage = info.ReadyMessage
			if info.ObservedGeneration < info.Generation {
				item.LaggingObservation = true
			}

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

		report.Resources = append(report.Resources, item)
	}

	switch {
	case anyMissing:
		report.GlobalStatus = GlobalStatusPartial
	case allSuspended:
		report.GlobalStatus = GlobalStatusSuspended
	case allMatchesOriginal && e.state.LastCompletedOp != nil && e.state.LastCompletedOp.Action == string(ActionRestore):
		report.GlobalStatus = GlobalStatusRestored
	default:
		report.GlobalStatus = GlobalStatusPartial
	}

	return report, nil
}

// ProgressFunc is called on each resource execution step.
type ProgressFunc func(step int, total int, res config.Resource, status string, err error)

// Execute runs the full suspend or restore workflow with journaling, verification, and crash safety.
func (e *Engine) Execute(ctx context.Context, action Action, force bool, onProgress ProgressFunc) (*ExecutionReport, error) {
	// A narrowed recovery only touches a subset of configured resources, so its
	// own success is not enough to claim a clean global state.
	recovering := e.state.ActiveOp != nil

	// Build plan
	plan, err := e.BuildPlan(ctx, action)
	if err != nil {
		return nil, err
	}

	// Missing targets are a hard error at execution time
	if len(plan.Missing) > 0 {
		missingRes := plan.Missing[0].Resource
		return nil, fmt.Errorf("target resource %s not found on cluster (missing targets are a hard error)", missingRes.Key())
	}

	// Check conflicts
	if len(plan.Conflicts) > 0 && !force {
		opID := fmt.Sprintf("01%X", time.Now().UnixNano())
		// Append conflict events to journal under the same opID
		for _, conf := range plan.Conflicts {
			exp := true
			if conf.ExpectedSuspended != nil {
				exp = *conf.ExpectedSuspended
			}
			obs := conf.CurrentSuspended
			_ = e.journal.Append(&journal.Event{
				Op:        opID,
				Event:     journal.EventConflict,
				Action:    string(action),
				Context:   e.kubeCtx,
				Kind:      conf.Resource.Kind,
				Namespace: conf.Resource.Namespace,
				Name:      conf.Resource.Name,
				Expected:  &exp,
				Observed:  &obs,
				Error:     conf.ConflictReason,
			})
		}
		return nil, ErrConflict
	}

	// Check incomplete state safety: if restore is requested and originals are missing
	if action == ActionRestore && len(e.state.OriginalSuspended) == 0 && e.state.ActiveOp != nil {
		return nil, fmt.Errorf("%w: cannot restore without recorded original states", ErrUnsafeState)
	}

	opID := fmt.Sprintf("01%X", time.Now().UnixNano())

	// 6. Append op.start
	startEvent := &journal.Event{
		Op:      opID,
		Event:   journal.EventOpStart,
		Action:  string(action),
		Context: e.kubeCtx,
		Targets: len(plan.Items),
	}
	if err := e.journal.Append(startEvent); err != nil {
		return nil, fmt.Errorf("appending op.start to journal: %w", err)
	}
	e.state.ApplyEvent(startEvent)
	if err := e.state.Save(); err != nil {
		return nil, fmt.Errorf("saving state: %w", err)
	}

	report := &ExecutionReport{
		Op:     opID,
		Action: action,
		Total:  len(plan.Items),
	}

	okCount := 0
	failedCount := 0

	for i, item := range plan.Items {
		res := item.Resource
		key := res.Key()
		target := item.TargetSuspended
		current := item.CurrentSuspended

		// If suspend, ensure original is captured
		if action == ActionSuspend {
			if _, exists := e.state.OriginalSuspended[key]; !exists {
				e.state.OriginalSuspended[key] = item.Original
			}
		}

		// 9a. Append resource.intent + fsync BEFORE patching
		intentEvent := &journal.Event{
			Op:        opID,
			Event:     journal.EventResourceIntent,
			Action:    string(action),
			Kind:      res.Kind,
			Namespace: res.Namespace,
			Name:      res.Name,
			From:      &current,
			To:        &target,
		}
		if err := e.journal.Append(intentEvent); err != nil {
			return nil, fmt.Errorf("appending resource.intent for %s: %w", key, err)
		}
		e.state.ApplyEvent(intentEvent)
		_ = e.state.Save()

		// 9b. Patch spec.suspend and read back
		start := time.Now()
		info, patchErr := e.fluxClient.PatchSuspend(ctx, res.Kind, res.Namespace, res.Name, target)
		durationMs := time.Since(start).Milliseconds()

		resResult := ResourceExecutionResult{
			Resource:        res,
			TargetSuspended: target,
			DurationMs:      durationMs,
		}

		var resultEvent *journal.Event
		if patchErr != nil {
			resResult.Err = patchErr
			failedCount++
			resStatus := journal.StatusError
			resultEvent = &journal.Event{
				Op:         opID,
				Event:      journal.EventResourceResult,
				Kind:       res.Kind,
				Namespace:  res.Namespace,
				Name:       res.Name,
				Status:     resStatus,
				DurationMs: durationMs,
				Error:      patchErr.Error(),
			}
		} else {
			resResult.ObservedSuspended = info.Suspended
			resResult.Generation = info.Generation
			resResult.ObservedGeneration = info.ObservedGeneration
			resResult.Ready = info.Ready
			resResult.ReadyReason = info.ReadyReason
			resResult.ReadyMessage = info.ReadyMessage
			if info.ObservedGeneration < info.Generation {
				resResult.LaggingObservation = true
			}

			// Verify read-back matches target
			if info.Suspended != target {
				errVerification := fmt.Errorf("verification failed: expected spec.suspend=%t, observed %t", target, info.Suspended)
				resResult.Err = errVerification
				failedCount++
				resultEvent = &journal.Event{
					Op:         opID,
					Event:      journal.EventResourceResult,
					Kind:       res.Kind,
					Namespace:  res.Namespace,
					Name:       res.Name,
					Status:     journal.StatusError,
					Observed:   &info.Suspended,
					DurationMs: durationMs,
					Error:      errVerification.Error(),
				}
			} else {
				okCount++
				resultEvent = &journal.Event{
					Op:         opID,
					Event:      journal.EventResourceResult,
					Kind:       res.Kind,
					Namespace:  res.Namespace,
					Name:       res.Name,
					Status:     journal.StatusOk,
					Observed:   &info.Suspended,
					DurationMs: durationMs,
				}
			}
		}

		// 9c. Append resource.result + fsync
		if err := e.journal.Append(resultEvent); err != nil {
			return nil, fmt.Errorf("appending resource.result for %s: %w", key, err)
		}
		e.state.ApplyEvent(resultEvent)
		_ = e.state.Save()

		report.Results = append(report.Results, resResult)

		if onProgress != nil {
			stepStatus := "ok"
			if resResult.Err != nil {
				stepStatus = "error"
			}
			onProgress(i+1, len(plan.Items), res, stepStatus, resResult.Err)
		}
	}

	report.OK = okCount
	report.Failed = failedCount

	var opEndStatus string
	if failedCount == 0 {
		opEndStatus = journal.StatusSuccess
		if action == ActionSuspend {
			report.GlobalStatus = GlobalStatusSuspended
		} else {
			report.GlobalStatus = GlobalStatusRestored
		}
	} else if okCount > 0 {
		opEndStatus = journal.StatusPartial
		report.GlobalStatus = GlobalStatusPartial
	} else {
		opEndStatus = journal.StatusFailed
		report.GlobalStatus = GlobalStatusPartial
	}
	report.Status = opEndStatus

	// 10. Append op.end
	endEvent := &journal.Event{
		Op:     opID,
		Event:  journal.EventOpEnd,
		Status: opEndStatus,
		OK:     okCount,
		Failed: failedCount,
	}
	if err := e.journal.Append(endEvent); err != nil {
		return nil, fmt.Errorf("appending op.end: %w", err)
	}
	e.state.ApplyEvent(endEvent)
	if err := e.state.Save(); err != nil {
		return nil, fmt.Errorf("saving state: %w", err)
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
