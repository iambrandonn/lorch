package runstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/iambrandonn/lorch/internal/fsutil"
)

// Status represents the overall state of a run
type Status string

const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusAborted   Status = "aborted"
)

// Stage represents the current execution stage
type Stage string

const (
	StageIntake       Stage = "intake"
	StageImplement    Stage = "implement"
	StageReview       Stage = "review"
	StageSpecMaintain Stage = "spec_maintain"
	StageComplete     Stage = "complete"
)

// RunState represents the persisted state of a run
// Based on P1.3-REVIEW-ANSWERS #4
type RunState struct {
	RunID            string            `json:"run_id"`
	Status           Status            `json:"status"`
	TaskID           string            `json:"task_id"`
	CorrelationID    string            `json:"correlation_id,omitempty"`
	SnapshotID       string            `json:"snapshot_id"`
	CurrentStage     Stage             `json:"current_stage"`
	StartedAt        time.Time         `json:"started_at"`
	CompletedAt      *time.Time        `json:"completed_at,omitempty"`
	LastCommandID    string            `json:"last_command_id,omitempty"`
	LastEventID      string            `json:"last_event_id,omitempty"`
	TerminalEvents   map[string]string `json:"terminal_events,omitempty"`
	Intake           *IntakeState      `json:"intake,omitempty"`
	ActivatedTaskIDs []string          `json:"activated_task_ids,omitempty"`     // P2.4: tracks completed intake-derived tasks
	CurrentTaskInputs map[string]any   `json:"current_task_inputs,omitempty"` // P2.4: stores full command inputs for idempotent resume
}

// NewRunState creates a new run state
func NewRunState(runID, taskID, snapshotID string) *RunState {
	return &RunState{
		RunID:          runID,
		Status:         StatusRunning,
		TaskID:         taskID,
		SnapshotID:     snapshotID,
		CurrentStage:   StageImplement,
		StartedAt:      time.Now().UTC(),
		TerminalEvents: make(map[string]string),
	}
}

// NewIntakeState creates a run state for an intake session.
func NewIntakeState(runID, snapshotID, instruction string, baseInputs map[string]any) *RunState {
	return &RunState{
		RunID:          runID,
		Status:         StatusRunning,
		TaskID:         "",
		SnapshotID:     snapshotID,
		CurrentStage:   StageIntake,
		StartedAt:      time.Now().UTC(),
		TerminalEvents: make(map[string]string),
		Intake: &IntakeState{
			Instruction:         instruction,
			BaseInputs:          cloneGenericMap(baseInputs),
			LastClarifications:  []string{},
			ConflictResolutions: []string{},
		},
	}
}

// IntakeState captures the state of the intake negotiation flow.
type IntakeState struct {
	Instruction           string          `json:"instruction"`
	BaseInputs            map[string]any  `json:"base_inputs,omitempty"`
	LastClarifications    []string        `json:"last_clarifications,omitempty"`
	ConflictResolutions   []string        `json:"conflict_resolutions,omitempty"`
	LastDecision          *IntakeDecision `json:"last_decision,omitempty"`
	PendingAction         string          `json:"pending_action,omitempty"`
	PendingInputs         map[string]any  `json:"pending_inputs,omitempty"`
	PendingIdempotencyKey string          `json:"pending_idempotency_key,omitempty"`
	PendingCorrelationID  string          `json:"pending_correlation_id,omitempty"`
}

// IntakeDecision records the most recent user decision during intake.
type IntakeDecision struct {
	Status        string    `json:"status"`
	ApprovedPlan  string    `json:"approved_plan,omitempty"`
	ApprovedTasks []string  `json:"approved_tasks,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	Prompt        string    `json:"prompt,omitempty"`
	OccurredAt    time.Time `json:"occurred_at"`
	CorrelationID string    `json:"correlation_id,omitempty"`
}

// SaveRunState writes run state to disk atomically
func SaveRunState(state *RunState, path string) error {
	return fsutil.AtomicWriteJSON(path, state)
}

// LoadRunState reads run state from disk
func LoadRunState(path string) (*RunState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read run state: %w", err)
	}

	var state RunState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal run state: %w", err)
	}

	// Initialize map if nil
	if state.TerminalEvents == nil {
		state.TerminalEvents = make(map[string]string)
	}

	return &state, nil
}

// GetRunStatePath returns the standard path for run state
func GetRunStatePath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, "state", "run.json")
}

// MarkCompleted marks the run as completed
func (s *RunState) MarkCompleted() {
	s.Status = StatusCompleted
	s.CurrentStage = StageComplete
	now := time.Now().UTC()
	s.CompletedAt = &now
}

// MarkFailed marks the run as failed
func (s *RunState) MarkFailed() {
	s.Status = StatusFailed
	now := time.Now().UTC()
	s.CompletedAt = &now
}

// MarkAborted marks the run as aborted
func (s *RunState) MarkAborted() {
	s.Status = StatusAborted
	now := time.Now().UTC()
	s.CompletedAt = &now
}

// SetStage updates the current execution stage
func (s *RunState) SetStage(stage Stage) {
	s.CurrentStage = stage
}

// RecordCommand records a command being sent
func (s *RunState) RecordCommand(commandID, correlationID string) {
	s.LastCommandID = commandID
	if correlationID != "" {
		s.CorrelationID = correlationID
	}
}

// RecordEvent records an event being received
func (s *RunState) RecordEvent(eventID string) {
	s.LastEventID = eventID
}

// RecordTerminalEvent records a terminal event for an agent
func (s *RunState) RecordTerminalEvent(agentType, eventID string) {
	if s.TerminalEvents == nil {
		s.TerminalEvents = make(map[string]string)
	}
	s.TerminalEvents[agentType] = eventID
}

// SetIntakeClarifications records the latest clarification answers.
func (s *RunState) SetIntakeClarifications(clarifications []string) {
	if s.Intake == nil {
		s.Intake = &IntakeState{}
	}
	s.Intake.LastClarifications = append([]string(nil), clarifications...)
}

// RecordIntakeDecision stores the most recent intake decision.
func (s *RunState) RecordIntakeDecision(decision *IntakeDecision) {
	if s.Intake == nil {
		s.Intake = &IntakeState{}
	}
	s.Intake.LastDecision = decision

	// Clear pending command metadata now that a decision has been made.
	s.Intake.PendingAction = ""
	s.Intake.PendingInputs = nil
	s.Intake.PendingIdempotencyKey = ""
	s.Intake.PendingCorrelationID = ""
}

// SetIntakeConflictResolutions records conflict resolution notes supplied by the user.
func (s *RunState) SetIntakeConflictResolutions(resolutions []string) {
	if s.Intake == nil {
		s.Intake = &IntakeState{}
	}
	s.Intake.ConflictResolutions = append([]string(nil), resolutions...)
}

// RecordIntakeBaseInputs stores the canonical base inputs map for resumability.
func (s *RunState) RecordIntakeBaseInputs(inputs map[string]any) {
	if s.Intake == nil {
		s.Intake = &IntakeState{}
	}
	s.Intake.BaseInputs = cloneGenericMap(inputs)
}

// RecordIntakeCommand stores metadata about the most recent command dispatched to the orchestration agent.
func (s *RunState) RecordIntakeCommand(action string, inputs map[string]any, idempotencyKey, correlationID string) {
	if s.Intake == nil {
		s.Intake = &IntakeState{}
	}
	s.Intake.PendingAction = action
	s.Intake.PendingInputs = cloneGenericMap(inputs)
	s.Intake.PendingIdempotencyKey = idempotencyKey
	s.Intake.PendingCorrelationID = correlationID
}

// MarkTaskActivated records that a task has been successfully executed.
// Used by P2.4 activation flow to track which intake-derived tasks have completed.
func (s *RunState) MarkTaskActivated(taskID string) {
	// Check if already recorded
	for _, id := range s.ActivatedTaskIDs {
		if id == taskID {
			return
		}
	}
	s.ActivatedTaskIDs = append(s.ActivatedTaskIDs, taskID)
}

// IsTaskActivated checks if a task has already been executed.
// Used for idempotent resume after crash during multi-task activation.
func (s *RunState) IsTaskActivated(taskID string) bool {
	for _, id := range s.ActivatedTaskIDs {
		if id == taskID {
			return true
		}
	}
	return false
}

// SetCurrentTaskInputs stores the full command inputs for the current task.
// Used by P2.4 to ensure idempotent resume produces identical idempotency keys.
func (s *RunState) SetCurrentTaskInputs(inputs map[string]any) {
	s.CurrentTaskInputs = cloneGenericMap(inputs)
}

func cloneGenericMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = cloneGenericValue(v)
	}
	return dst
}

func cloneGenericValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return cloneGenericMap(val)
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = cloneGenericValue(item)
		}
		return result
	case []string:
		return append([]string(nil), val...)
	default:
		return val
	}
}
