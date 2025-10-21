package protocol

import (
	"time"
)

// MessageKind represents the envelope type
type MessageKind string

const (
	MessageKindCommand   MessageKind = "command"
	MessageKindEvent     MessageKind = "event"
	MessageKindHeartbeat MessageKind = "heartbeat"
	MessageKindLog       MessageKind = "log"
)

// AgentType represents the role of an agent
type AgentType string

const (
	AgentTypeBuilder        AgentType = "builder"
	AgentTypeReviewer       AgentType = "reviewer"
	AgentTypeSpecMaintainer AgentType = "spec_maintainer"
	AgentTypeOrchestration  AgentType = "orchestration"
	AgentTypeSystem         AgentType = "system"
)

// Action represents a command action
type Action string

const (
	// ActionImplement instructs the builder to produce an initial implementation for the task.
	ActionImplement Action = "implement"
	// ActionImplementChanges instructs the builder to respond to review/spec feedback.
	ActionImplementChanges Action = "implement_changes"
	// ActionReview instructs the reviewer to examine the latest builder output.
	ActionReview Action = "review"
	// ActionUpdateSpec instructs the spec maintainer to reconcile implementation with SPEC.md.
	ActionUpdateSpec Action = "update_spec"
	// ActionIntake asks the orchestration agent to translate an initial natural-language instruction
	// into concrete plan candidates and derived task objects.
	ActionIntake Action = "intake"
	// ActionTaskDiscovery asks the orchestration agent to perform incremental task expansion mid-run,
	// leveraging existing context (e.g., approved plans, current task state) to suggest next actions.
	ActionTaskDiscovery Action = "task_discovery"
)

// AgentRef identifies an agent
type AgentRef struct {
	AgentType AgentType `json:"agent_type"`
	AgentID   string    `json:"agent_id,omitempty"`
}

// Version captures version/snapshot information
type Version struct {
	SnapshotID string `json:"snapshot_id"`
	SpecsHash  string `json:"specs_hash,omitempty"`
	CodeHash   string `json:"code_hash,omitempty"`
}

// Retry contains retry attempt information
type Retry struct {
	Attempt     int `json:"attempt"`
	MaxAttempts int `json:"max_attempts"`
}

// ExpectedOutput describes an artifact the agent should produce
type ExpectedOutput struct {
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
}

// Command is sent from lorch to agents
type Command struct {
	Kind            MessageKind      `json:"kind"`
	MessageID       string           `json:"message_id"`
	CorrelationID   string           `json:"correlation_id"`
	TaskID          string           `json:"task_id"`
	IdempotencyKey  string           `json:"idempotency_key"`
	To              AgentRef         `json:"to"`
	Action          Action           `json:"action"`
	Inputs          map[string]any   `json:"inputs"`
	ExpectedOutputs []ExpectedOutput `json:"expected_outputs"`
	Version         Version          `json:"version"`
	Deadline        time.Time        `json:"deadline"`
	Retry           Retry            `json:"retry"`
	Priority        int              `json:"priority"`
}

// Artifact describes a produced file
type Artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Event is sent from agents to lorch
type Event struct {
	Kind            MessageKind    `json:"kind"`
	MessageID       string         `json:"message_id"`
	CorrelationID   string         `json:"correlation_id"`
	TaskID          string         `json:"task_id"`
	From            AgentRef       `json:"from"`
	Event           string         `json:"event"`
	Status          string         `json:"status,omitempty"`
	Payload         map[string]any `json:"payload"`
	Artifacts       []Artifact     `json:"artifacts,omitempty"`
	ObservedVersion *Version       `json:"observed_version,omitempty"`
	OccurredAt      time.Time      `json:"occurred_at"`
}

// HeartbeatStatus represents agent health status
type HeartbeatStatus string

const (
	HeartbeatStatusStarting HeartbeatStatus = "starting"
	HeartbeatStatusReady    HeartbeatStatus = "ready"
	HeartbeatStatusBusy     HeartbeatStatus = "busy"
	HeartbeatStatusStopping HeartbeatStatus = "stopping"
	HeartbeatStatusBackoff  HeartbeatStatus = "backoff"
)

// HeartbeatStats contains optional resource usage statistics
type HeartbeatStats struct {
	CPUPercent float64 `json:"cpu_pct,omitempty"`
	RSSBytes   int64   `json:"rss_bytes,omitempty"`
}

// Heartbeat is sent from agents to lorch for liveness
type Heartbeat struct {
	Kind           MessageKind     `json:"kind"`
	Agent          AgentRef        `json:"agent"`
	Seq            int64           `json:"seq"`
	Status         HeartbeatStatus `json:"status"`
	PID            int             `json:"pid"`
	PPID           int             `json:"ppid,omitempty"`
	UptimeS        float64         `json:"uptime_s"`
	LastActivityAt time.Time       `json:"last_activity_at"`
	Stats          *HeartbeatStats `json:"stats,omitempty"`
	TaskID         string          `json:"task_id,omitempty"`
}

// LogLevel represents log severity
type LogLevel string

const (
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// Log is a diagnostic message
type Log struct {
	Kind      MessageKind    `json:"kind"`
	Level     LogLevel       `json:"level"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// Well-known event types
const (
	// Builder events
	EventBuilderProgress  = "builder.progress"
	EventBuilderCompleted = "builder.completed"

	// Reviewer events
	EventReviewCompleted = "review.completed"

	// Spec maintainer events
	EventSpecUpdated          = "spec.updated"
	EventSpecNoChangesNeeded  = "spec.no_changes_needed"
	EventSpecChangesRequested = "spec.changes_requested"

	// Orchestration events
	EventOrchestrationProposedTasks      = "orchestration.proposed_tasks"
	EventOrchestrationNeedsClarification = "orchestration.needs_clarification"
	EventOrchestrationPlanConflict       = "orchestration.plan_conflict"

	// Generic events
	EventArtifactProduced = "artifact.produced"
	EventError            = "error"

	// System events
	EventSystemUserDecision = "system.user_decision"
)

// Review statuses
const (
	ReviewStatusApproved         = "approved"
	ReviewStatusChangesRequested = "changes_requested"
)
