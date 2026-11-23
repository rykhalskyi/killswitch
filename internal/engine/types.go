package engine

import (
	"errors"

	"github.com/rykhalskyi/killswitch/internal/config"
	"github.com/rykhalskyi/killswitch/internal/flux"
)

// Exit codes defined by the spec:
// 0: success
// 1: partial failure
// 2: config/connection error
// 3: conflict/needs --force
// 4: unsafe/incomplete state or lock contention
const (
	ExitSuccess            = 0
	ExitPartialFailure     = 1
	ExitConfigOrConnection = 2
	ExitConflict           = 3
	ExitUnsafeOrLock       = 4
)

var (
	ErrPartialFailure = errors.New("one or more steps failed to reconcile")
	ErrConflict       = errors.New("conflict detected: resource modified outside killswitch (use --force to override)")
	ErrUnsafeState    = errors.New("unsafe or incomplete state detected")
	ErrLockContention = errors.New("state directory is locked by another running killswitch process")
)

type Action string

const (
	ActionSuspend Action = "suspend"
	ActionRestore Action = "restore"
)

// Direction is the switch transition: ON is the operational default, OFF is
// the non-default state where originals are captured.
type Direction string

const (
	DirectionOn  Direction = "on"
	DirectionOff Direction = "off"
)

// LegacyAction maps an old CLI action to a switch direction.
func LegacyAction(d Direction) Action {
	if d == DirectionOff {
		return ActionSuspend
	}
	return ActionRestore
}

type GlobalStatus string

const (
	GlobalStatusSuspended GlobalStatus = "suspended"
	GlobalStatusRestored  GlobalStatus = "restored"
	GlobalStatusPartial   GlobalStatus = "partial"
	GlobalStatusUnknown   GlobalStatus = "unknown"
)

// SwitchState is the switch position derived from live observation.
type SwitchState string

const (
	SwitchOn      SwitchState = "ON"
	SwitchOff     SwitchState = "OFF"
	SwitchMixed   SwitchState = "MIXED"
	SwitchDrift   SwitchState = "DRIFT"
	SwitchMissing SwitchState = "MISSING"
	SwitchUnknown SwitchState = "UNKNOWN"
)

// PlanItem describes the planned action on one workflow step.
type PlanItem struct {
	Switch            string             `json:"switch,omitempty"`
	Workflow          string             `json:"workflow,omitempty"`
	Step              string             `json:"step,omitempty"`
	Action            string             `json:"action,omitempty"`
	Direction         Direction          `json:"direction,omitempty"`
	Resource          config.Resource    `json:"resource"`
	Info              *flux.ResourceInfo `json:"info,omitempty"`
	CurrentSuspended  bool               `json:"currentSuspended"`
	TargetSuspended   bool               `json:"targetSuspended"`
	Original          bool               `json:"original"`
	OriginalValue     any                `json:"originalValue,omitempty"`
	DesiredValue      any                `json:"desiredValue,omitempty"`
	ObservedValue     any                `json:"observedValue,omitempty"`
	Exists            bool               `json:"exists"`
	WillChange        bool               `json:"willChange"`
	ExpectedSuspended *bool              `json:"expectedSuspended,omitempty"`
	ExpectedValue     any                `json:"expectedValue,omitempty"`
	Conflict          bool               `json:"conflict"`
	ConflictReason    string             `json:"conflictReason,omitempty"`
	Cascade           bool               `json:"cascade,omitempty"`
	ContinueOnError   bool               `json:"continueOnError,omitempty"`
	Timeout           config.Duration    `json:"timeout,omitempty"`
}

// Plan represents the proposed changes before execution.
type Plan struct {
	Action    Action     `json:"action"`
	Switch    string     `json:"switch,omitempty"`
	Workflow  string     `json:"workflow,omitempty"`
	Direction Direction  `json:"direction,omitempty"`
	Items     []PlanItem `json:"items"`
	Conflicts []PlanItem `json:"conflicts"`
	Missing   []PlanItem `json:"missing"`
}

// ResourceExecutionResult captures the result of executing one step.
type ResourceExecutionResult struct {
	Switch             string          `json:"switch,omitempty"`
	Step               string          `json:"step,omitempty"`
	Action             string          `json:"action,omitempty"`
	Resource           config.Resource `json:"resource"`
	TargetSuspended    bool            `json:"targetSuspended"`
	ObservedSuspended  bool            `json:"observedSuspended"`
	DesiredValue       any             `json:"desiredValue,omitempty"`
	ObservedValue      any             `json:"observedValue,omitempty"`
	DurationMs         int64           `json:"durationMs"`
	Generation         int64           `json:"generation"`
	ObservedGeneration int64           `json:"observedGeneration"`
	Ready              string          `json:"ready"`
	ReadyReason        string          `json:"readyReason"`
	ReadyMessage       string          `json:"readyMessage"`
	LaggingObservation bool            `json:"laggingObservation"`
	Err                error           `json:"err,omitempty"`
}

// ExecutionReport summarizes the entire run.
type ExecutionReport struct {
	Op           string                    `json:"op"`
	Action       Action                    `json:"action"`
	Switch       string                    `json:"switch,omitempty"`
	Direction    Direction                 `json:"direction,omitempty"`
	GlobalStatus GlobalStatus              `json:"globalStatus"`
	SwitchState  SwitchState               `json:"switchState,omitempty"`
	Results      []ResourceExecutionResult `json:"results"`
	Total        int                       `json:"total"`
	OK           int                       `json:"ok"`
	Failed       int                       `json:"failed"`
	Status       string                    `json:"status"` // success, partial, failed
}

// StatusReport holds cluster and state information for 'killswitch status'.
type StatusReport struct {
	GlobalStatus    GlobalStatus         `json:"globalStatus"`
	Context         string               `json:"context"`
	ActiveOp        *string              `json:"activeOp,omitempty"`
	Incomplete      bool                 `json:"incomplete"`
	IncompleteMsg   string               `json:"incompleteMsg,omitempty"`
	LastCompletedOp *string              `json:"lastCompletedOp,omitempty"`
	Resources       []ResourceStatusItem `json:"resources"`
	Switches        []SwitchStatus       `json:"switches,omitempty"`
}

type ResourceStatusItem struct {
	Kind               string `json:"kind"`
	Namespace          string `json:"namespace"`
	Name               string `json:"name"`
	Exists             bool   `json:"exists"`
	CurrentSuspended   bool   `json:"currentSuspended"`
	OriginalSuspended  *bool  `json:"originalSuspended,omitempty"`
	Generation         int64  `json:"generation"`
	ObservedGeneration int64  `json:"observedGeneration"`
	Ready              string `json:"ready"`
	ReadyReason        string `json:"readyReason,omitempty"`
	ReadyMessage       string `json:"readyMessage,omitempty"`
	LaggingObservation bool   `json:"laggingObservation"`
	ActivePhase        string `json:"activePhase,omitempty"`
}

// SwitchStatus is the live state of one configured switch.
type SwitchStatus struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Workflow    string       `json:"workflow"`
	State       SwitchState  `json:"state"`
	Error       string       `json:"error,omitempty"`
	Requires    string       `json:"requiresFlux,omitempty"`
	Steps       []StepStatus `json:"steps,omitempty"`
}

// StepStatus is the live state of one workflow step.
type StepStatus struct {
	Step     string          `json:"step"`
	Action   string          `json:"action"`
	Target   config.Resource `json:"target"`
	Exists   bool            `json:"exists"`
	Observed any             `json:"observed,omitempty"`
	Original any             `json:"original,omitempty"`
	State    string          `json:"state"`
	Error    string          `json:"error,omitempty"`
}
