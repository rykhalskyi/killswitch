package engine

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/rykhalskyi/killswitch/internal/actions"
	"github.com/rykhalskyi/killswitch/internal/config"
	"github.com/rykhalskyi/killswitch/internal/flux"
	"github.com/rykhalskyi/killswitch/internal/journal"
	"github.com/rykhalskyi/killswitch/internal/state"
)

// Engine orchestrates reading, planning, executing, and journaling switch
// transitions over layered workflows.
type Engine struct {
	cfg        *config.Config
	fluxClient flux.Client
	registry   *actions.Registry
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
	if cfg != nil {
		cfg.Normalize()
	}
	registry := actions.NewRegistry()
	if client != nil {
		fh := &actions.FluxHandler{Client: client}
		registry.Register(config.ActionFluxSuspend, fh)
		registry.Register(config.ActionFluxResume, fh)
	}
	return &Engine{
		cfg:        cfg,
		fluxClient: client,
		registry:   registry,
		stateDir:   stateDir,
		kubeCtx:    kubeCtx,
		logger:     logger,
	}
}

// SetScaleClient wires the Kubernetes workload and CronJob handlers.
func (e *Engine) SetScaleClient(c any) {
	if c == nil {
		return
	}
	if scale, ok := c.(actions.ScaleClient); ok {
		e.registry.Register(config.ActionKubeScale, &actions.ScaleHandler{Client: scale})
	}
	if cron, ok := c.(actions.CronJobClient); ok {
		e.registry.Register(config.ActionKubeSuspend, &actions.CronJobSuspendHandler{Client: cron})
	}
}

// SetScaleHandler registers a scale handler directly (used by tests).
func (e *Engine) SetScaleHandler(h actions.Handler) {
	e.registry.Register(config.ActionKubeScale, h)
}

// SetCronJobHandler registers a CronJob suspend handler directly (used by tests).
func (e *Engine) SetCronJobHandler(h actions.Handler) {
	e.registry.Register(config.ActionKubeSuspend, h)
}

// Config returns the normalized configuration.
func (e *Engine) Config() *config.Config {
	return e.cfg
}

// Switches returns configured switches in declaration order.
func (e *Engine) Switches() []config.Switch {
	if e.cfg == nil {
		return nil
	}
	return e.cfg.Switches
}

// Switch returns one switch by id.
func (e *Engine) Switch(id string) *config.Switch {
	if e.cfg == nil {
		return nil
	}
	return e.cfg.SwitchByID(id)
}

// FluxSwitchID returns the Flux switch id.
func (e *Engine) FluxSwitchID() string {
	if e.cfg == nil {
		return ""
	}
	return e.cfg.FluxSwitchID()
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

	l, err := state.AcquireLock(e.stateDir)
	if err != nil {
		if err == state.ErrLocked {
			return ErrLockContention
		}
		return fmt.Errorf("acquiring lock: %w", err)
	}
	e.lock = l

	journalPath := filepath.Join(e.stateDir, "journal.jsonl")
	j, err := journal.Open(journalPath)
	if err != nil {
		_ = e.lock.Release()
		return fmt.Errorf("opening journal: %w", err)
	}
	e.journal = j

	events, err := journal.ReadAll(journalPath)
	if err != nil && !os.IsNotExist(err) {
		_ = e.Close()
		return fmt.Errorf("reading journal events: %w", err)
	}

	st, err := state.LoadOrRebuild(e.stateDir, events)
	if err != nil {
		_ = e.Close()
		return fmt.Errorf("loading or rebuilding state: %w", err)
	}
	e.state = st

	if e.fluxClient != nil {
		if err := e.fluxClient.DiscoverGVRs(ctx); err != nil {
			e.logger.Warn("gvr discovery failed, falling back to defaults", "error", err)
		}
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

// OrderedResources returns Flux targets in workflow order for an action.
func (e *Engine) OrderedResources(action Action) []config.Resource {
	wf := e.cfg.WorkflowForSwitch(e.FluxSwitchID())
	if wf == nil {
		return nil
	}
	dir := DirectionOn
	if action == ActionSuspend {
		dir = DirectionOff
	}
	var out []config.Resource
	for _, step := range orderedSteps(wf, dir) {
		out = append(out, step.Target)
	}
	return out
}

// BuildPlan is the legacy Flux plan entry point.
func (e *Engine) BuildPlan(ctx context.Context, action Action) (*Plan, error) {
	dir := DirectionOn
	if action == ActionSuspend {
		dir = DirectionOff
	}
	return e.BuildSwitchPlan(ctx, e.FluxSwitchID(), dir)
}

// Execute is the legacy Flux execution entry point.
func (e *Engine) Execute(ctx context.Context, action Action, force bool, onProgress ProgressFunc) (*ExecutionReport, error) {
	dir := DirectionOn
	if action == ActionSuspend {
		dir = DirectionOff
	}
	return e.ExecuteSwitch(ctx, e.FluxSwitchID(), dir, force, onProgress)
}

// ProgressFunc is called on each step execution step.
type ProgressFunc func(step int, total int, res config.Resource, status string, err error)
