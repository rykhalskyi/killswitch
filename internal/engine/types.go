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
	ErrPartialFailure = errors.New("one or more resources failed to reconcile")
	ErrConflict       = errors.New("conflict detected: resource modified outside killswitch (use --force to override)")
	ErrUnsafeState    = errors.New("unsafe or incomplete state detected")
	ErrLockContention = errors.New("state directory is locked by another running killswitch process")
)

type Action string

const (
	ActionSuspend Action = "suspend"
	ActionRestore Action = "restore"
)

type GlobalStatus string

const (
	GlobalStatusSuspended GlobalStatus = "suspended"
	GlobalStatusRestored  GlobalStatus = "restored"
	GlobalStatusPartial   GlobalStatus = "partial"
	GlobalStatusUnknown   GlobalStatus = "unknown"
)

// PlanItem describes the planned action on a single resource.
type PlanItem struct {
	Resource          config.Resource    `json:"resource"`
	Info              *flux.ResourceInfo `json:"info,omitempty"`
	CurrentSuspended  bool               `json:"currentSuspended"`
	TargetSuspended   bool               `json:"targetSuspended"`
	Original          bool               `json:"original"`
	Exists            bool               `json:"exists"`
	WillChange        bool               `json:"willChange"`
	ExpectedSuspended *bool              `json:"expectedSuspended,omitempty"`
	Conflict          bool               `json:"conflict"`
	ConflictReason    string             `json:"conflictReason,omitempty"`
}

// Plan represents the proposed changes before execution.
type Plan struct {
	Action    Action     `json:"action"`
	Items     []PlanItem `json:"items"`
	Conflicts []PlanItem `json:"conflicts"`
	Missing   []PlanItem `json:"missing"`
}

// ResourceExecutionResult captures the result of executing an action on one resource.
type ResourceExecutionResult struct {
	Resource           config.Resource `json:"resource"`
	TargetSuspended    bool            `json:"targetSuspended"`
	ObservedSuspended  bool            `json:"observedSuspended"`
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
	GlobalStatus GlobalStatus              `json:"globalStatus"`
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
