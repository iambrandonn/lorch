package runstate

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewRunState(t *testing.T) {
	runID := "run-20251019-abc123"
	taskID := "T-0042"
	snapshotID := "snap-xyz789"

	state := NewRunState(runID, taskID, snapshotID)

	if state.RunID != runID {
		t.Errorf("RunID = %s, want %s", state.RunID, runID)
	}

	if state.TaskID != taskID {
		t.Errorf("TaskID = %s, want %s", state.TaskID, taskID)
	}

	if state.SnapshotID != snapshotID {
		t.Errorf("SnapshotID = %s, want %s", state.SnapshotID, snapshotID)
	}

	if state.Status != StatusRunning {
		t.Errorf("Status = %s, want %s", state.Status, StatusRunning)
	}

	if state.CurrentStage != StageImplement {
		t.Errorf("CurrentStage = %s, want %s", state.CurrentStage, StageImplement)
	}

	if state.StartedAt.IsZero() {
		t.Error("StartedAt is zero")
	}
}

func TestSaveAndLoadRunState(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state", "run.json")

	// Create test state
	original := &RunState{
		RunID:          "run-001",
		Status:         StatusRunning,
		TaskID:         "T-0042",
		CorrelationID:  "corr-001",
		SnapshotID:     "snap-abc123",
		CurrentStage:   StageReview,
		StartedAt:      time.Now().UTC(),
		LastCommandID:  "cmd-5",
		LastEventID:    "evt-42",
		TerminalEvents: map[string]string{
			"builder": "evt-10",
		},
	}

	// Save
	if err := SaveRunState(original, statePath); err != nil {
		t.Fatalf("SaveRunState() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		t.Fatal("state file not created")
	}

	// Load back
	loaded, err := LoadRunState(statePath)
	if err != nil {
		t.Fatalf("LoadRunState() error = %v", err)
	}

	// Verify fields
	if loaded.RunID != original.RunID {
		t.Errorf("RunID = %s, want %s", loaded.RunID, original.RunID)
	}

	if loaded.Status != original.Status {
		t.Errorf("Status = %s, want %s", loaded.Status, original.Status)
	}

	if loaded.CurrentStage != original.CurrentStage {
		t.Errorf("CurrentStage = %s, want %s", loaded.CurrentStage, original.CurrentStage)
	}

	if len(loaded.TerminalEvents) != len(original.TerminalEvents) {
		t.Errorf("TerminalEvents count = %d, want %d", len(loaded.TerminalEvents), len(original.TerminalEvents))
	}
}

func TestMarkCompleted(t *testing.T) {
	state := &RunState{
		RunID:        "run-001",
		Status:       StatusRunning,
		CurrentStage: StageReview,
		StartedAt:    time.Now().UTC().Add(-1 * time.Hour),
	}

	state.MarkCompleted()

	if state.Status != StatusCompleted {
		t.Errorf("Status = %s, want %s", state.Status, StatusCompleted)
	}

	if state.CurrentStage != StageComplete {
		t.Errorf("CurrentStage = %s, want %s", state.CurrentStage, StageComplete)
	}

	if state.CompletedAt == nil {
		t.Error("CompletedAt is nil")
	}

	if !state.CompletedAt.After(state.StartedAt) {
		t.Error("CompletedAt should be after StartedAt")
	}
}

func TestMarkFailed(t *testing.T) {
	state := &RunState{
		RunID:  "run-001",
		Status: StatusRunning,
	}

	state.MarkFailed()

	if state.Status != StatusFailed {
		t.Errorf("Status = %s, want %s", state.Status, StatusFailed)
	}

	if state.CompletedAt == nil {
		t.Error("CompletedAt should be set on failure")
	}
}

func TestSetStage(t *testing.T) {
	state := &RunState{
		RunID:        "run-001",
		CurrentStage: StageImplement,
	}

	state.SetStage(StageReview)

	if state.CurrentStage != StageReview {
		t.Errorf("CurrentStage = %s, want %s", state.CurrentStage, StageReview)
	}
}

func TestRecordCommand(t *testing.T) {
	state := &RunState{
		RunID: "run-001",
	}

	state.RecordCommand("cmd-123", "corr-456")

	if state.LastCommandID != "cmd-123" {
		t.Errorf("LastCommandID = %s, want cmd-123", state.LastCommandID)
	}

	if state.CorrelationID != "corr-456" {
		t.Errorf("CorrelationID = %s, want corr-456", state.CorrelationID)
	}
}

func TestRecordEvent(t *testing.T) {
	state := &RunState{
		RunID: "run-001",
	}

	state.RecordEvent("evt-789")

	if state.LastEventID != "evt-789" {
		t.Errorf("LastEventID = %s, want evt-789", state.LastEventID)
	}
}

func TestRecordTerminalEvent(t *testing.T) {
	state := &RunState{
		RunID:          "run-001",
		TerminalEvents: make(map[string]string),
	}

	state.RecordTerminalEvent("builder", "evt-100")
	state.RecordTerminalEvent("reviewer", "evt-200")

	if len(state.TerminalEvents) != 2 {
		t.Errorf("TerminalEvents count = %d, want 2", len(state.TerminalEvents))
	}

	if state.TerminalEvents["builder"] != "evt-100" {
		t.Errorf("builder terminal event = %s, want evt-100", state.TerminalEvents["builder"])
	}

	if state.TerminalEvents["reviewer"] != "evt-200" {
		t.Errorf("reviewer terminal event = %s, want evt-200", state.TerminalEvents["reviewer"])
	}
}

func TestGetRunStatePath(t *testing.T) {
	tests := []struct {
		name          string
		workspaceRoot string
		expected      string
	}{
		{
			name:          "simple case",
			workspaceRoot: "/workspace",
			expected:      "/workspace/state/run.json",
		},
		{
			name:          "relative path",
			workspaceRoot: ".",
			expected:      "state/run.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetRunStatePath(tt.workspaceRoot)
			if result != tt.expected {
				t.Errorf("GetRunStatePath() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestStatusConstants(t *testing.T) {
	// Verify status constants are defined
	statuses := []Status{StatusRunning, StatusCompleted, StatusFailed, StatusAborted}
	if len(statuses) != 4 {
		t.Error("expected 4 status constants")
	}
}

func TestStageConstants(t *testing.T) {
	// Verify stage constants are defined
	stages := []Stage{StageImplement, StageReview, StageSpecMaintain, StageComplete}
	if len(stages) != 4 {
		t.Error("expected 4 stage constants")
	}
}
