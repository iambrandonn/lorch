package activation

import (
	"fmt"

	"github.com/iambrandonn/lorch/internal/protocol"
)

// Input captures everything derived from intake that the activation
// pipeline needs to translate user approvals into concrete work items.
type Input struct {
	// Run identifiers
	RunID      string
	SnapshotID string

	// Workspace context
	WorkspaceRoot string

	// Intake context
	Instruction         string
	ApprovedPlan        string
	ApprovedTaskIDs     []string
	Clarifications      []string
	ConflictResolutions []string
	DerivedTasks        []DerivedTask
	DecisionStatus      string
	IntakeCorrelationID string

	// Previously activated task IDs (for idempotent resumes)
	AlreadyActivated map[string]struct{}
}

// DerivedTask mirrors orchestration `derived_tasks` payload entries.
type DerivedTask struct {
	ID    string
	Title string
	Files []string
}

// Task represents a concrete piece of work ready for the scheduler layer.
type Task struct {
	ID                  string
	Title               string
	Files               []string
	Instruction         string
	ApprovedPlan        string
	Clarifications      []string
	ConflictResolutions []string
	SnapshotID          string
	RunID               string
	IntakeCorrelationID string
}

// ToCommandInputs produces the canonical command inputs map that will be
// passed to the builder implement command (future Task B wiring).
func (t Task) ToCommandInputs() map[string]any {
	return map[string]any{
		"instruction":          t.Instruction,
		"task_title":           t.Title,
		"task_files":           t.Files,
		"goal":                 t.Title,
		"approved_plan":        t.ApprovedPlan,
		"clarifications":       t.Clarifications,
		"conflict_resolutions": t.ConflictResolutions,
	}
}

// ToActivationMetadata captures traceability fields for receipts/events.
func (t Task) ToActivationMetadata() map[string]any {
	return map[string]any{
		"intake_run_id":        t.RunID,
		"approved_plan":        t.ApprovedPlan,
		"approved_task_id":     t.ID,
		"clarifications":       t.Clarifications,
		"conflict_resolutions": t.ConflictResolutions,
	}
}

// ToExpectedOutputs is a placeholder helper for future integration.
// It returns an empty slice for now; Task B will populate expected outputs.
func (t Task) ToExpectedOutputs() []protocol.ExpectedOutput {
	outputs := make([]protocol.ExpectedOutput, 0, len(t.Files))
	for _, path := range t.Files {
		outputs = append(outputs, protocol.ExpectedOutput{
			Path:        path,
			Description: fmt.Sprintf("task %s artifact: %s", t.ID, path),
			Required:    true,
		})
	}
	return outputs
}
