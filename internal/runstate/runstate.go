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
	StageImplement     Stage = "implement"
	StageReview        Stage = "review"
	StageSpecMaintain  Stage = "spec_maintain"
	StageComplete      Stage = "complete"
)

// RunState represents the persisted state of a run
// Based on P1.3-REVIEW-ANSWERS #4
type RunState struct {
	RunID          string            `json:"run_id"`
	Status         Status            `json:"status"`
	TaskID         string            `json:"task_id"`
	CorrelationID  string            `json:"correlation_id,omitempty"`
	SnapshotID     string            `json:"snapshot_id"`
	CurrentStage   Stage             `json:"current_stage"`
	StartedAt      time.Time         `json:"started_at"`
	CompletedAt    *time.Time        `json:"completed_at,omitempty"`
	LastCommandID  string            `json:"last_command_id,omitempty"`
	LastEventID    string            `json:"last_event_id,omitempty"`
	TerminalEvents map[string]string `json:"terminal_events,omitempty"`
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
