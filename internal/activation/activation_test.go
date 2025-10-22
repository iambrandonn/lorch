package activation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/stretchr/testify/require"
)

// --- Task Activation Tests ---

func TestTaskActivationSingleTask(t *testing.T) {
	workspace := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "PLAN.md"), []byte("# Plan"), 0o644))

	input := Input{
		RunID:           "intake-1",
		SnapshotID:      "snap-1",
		WorkspaceRoot:   workspace,
		Instruction:     "Implement feature",
		ApprovedPlan:    "PLAN.md",
		ApprovedTaskIDs: []string{"TASK-1"},
		DerivedTasks: []DerivedTask{
			{ID: "TASK-1", Title: "Build foo", Files: []string{"src/foo.go", "src/foo_test.go"}},
		},
		Clarifications:      []string{"Focus on API"},
		ConflictResolutions: []string{"Use docs/plan_v2.md"},
		DecisionStatus:      "approved",
	}

	tasks, err := PrepareTasks(input)
	require.NoError(t, err)
	require.Len(t, tasks, 1)

	task := tasks[0]
	require.Equal(t, "TASK-1", task.ID)
	require.Equal(t, "Build foo", task.Title)
	require.Equal(t, input.ApprovedPlan, task.ApprovedPlan)
	require.Equal(t, input.Clarifications, task.Clarifications)
	require.Equal(t, input.ConflictResolutions, task.ConflictResolutions)
	require.Equal(t, input.SnapshotID, task.SnapshotID)
	require.Equal(t, input.RunID, task.RunID)

	inputs := task.ToCommandInputs()
	require.Equal(t, input.Instruction, inputs["instruction"])
	require.Equal(t, task.Files, inputs["task_files"])

	metadata := task.ToActivationMetadata()
	require.Equal(t, input.RunID, metadata["intake_run_id"])
	require.Equal(t, input.ApprovedPlan, metadata["approved_plan"])
	require.Equal(t, task.ID, metadata["approved_task_id"])
}

func TestTaskActivationMultipleTasksOrder(t *testing.T) {
	workspace := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "PLAN.md"), []byte("# Plan"), 0o644))

	input := Input{
		RunID:           "intake-1",
		SnapshotID:      "snap-1",
		WorkspaceRoot:   workspace,
		Instruction:     "Implement feature",
		ApprovedPlan:    "PLAN.md",
		ApprovedTaskIDs: []string{"TASK-1", "TASK-2"},
		DerivedTasks: []DerivedTask{
			{ID: "TASK-1", Title: "First", Files: []string{"src/first.go"}},
			{ID: "TASK-2", Title: "Second", Files: []string{"src/second.go"}},
		},
		DecisionStatus: "approved",
	}

	tasks, err := PrepareTasks(input)
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	require.Equal(t, "TASK-1", tasks[0].ID)
	require.Equal(t, "TASK-2", tasks[1].ID)
}

func TestTaskActivationDiscoveryExpansion(t *testing.T) {
	workspace := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "PLAN.md"), []byte("# Plan"), 0o644))

	input := Input{
		RunID:           "intake-1",
		SnapshotID:      "snap-1",
		WorkspaceRoot:   workspace,
		Instruction:     "Implement feature",
		ApprovedPlan:    "PLAN.md",
		ApprovedTaskIDs: []string{"TASK-1", "TASK-2"},
		DerivedTasks: []DerivedTask{
			{ID: "TASK-1", Title: "Existing", Files: []string{"src/existing.go"}},
			{ID: "TASK-2", Title: "New", Files: []string{"src/new.go"}},
		},
		AlreadyActivated: map[string]struct{}{"TASK-1": {}},
		DecisionStatus:   "approved",
	}

	tasks, err := PrepareTasks(input)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, "TASK-2", tasks[0].ID)
}

func TestTaskActivationDuplicateGuard(t *testing.T) {
	workspace := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "PLAN.md"), []byte("# Plan"), 0o644))

	input := Input{
		WorkspaceRoot:   workspace,
		Instruction:     "Implement",
		SnapshotID:      "snap-1",
		ApprovedPlan:    "PLAN.md",
		ApprovedTaskIDs: []string{"TASK-1"},
		DerivedTasks: []DerivedTask{
			{ID: "TASK-1", Title: "Existing"},
		},
		AlreadyActivated: map[string]struct{}{"TASK-1": {}},
		DecisionStatus:   "approved",
	}

	tasks, err := PrepareTasks(input)
	require.NoError(t, err)
	require.Empty(t, tasks)
}

func TestTaskActivationZeroTasks(t *testing.T) {
	workspace := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "PLAN.md"), []byte("# Plan"), 0o644))

	input := Input{
		WorkspaceRoot:   workspace,
		Instruction:     "Implement",
		SnapshotID:      "snap-1",
		ApprovedPlan:    "PLAN.md",
		ApprovedTaskIDs: nil,
		DecisionStatus:  "approved",
	}

	tasks, err := PrepareTasks(input)
	require.NoError(t, err)
	require.Empty(t, tasks)
}

func TestTaskActivationPlanFileFailure(t *testing.T) {
	workspace := t.TempDir()
	input := Input{
		WorkspaceRoot:   workspace,
		SnapshotID:      "snap-1",
		ApprovedPlan:    "PLAN.md",
		ApprovedTaskIDs: []string{"TASK-1"},
		DerivedTasks: []DerivedTask{
			{ID: "TASK-1", Title: "Missing plan"},
		},
		DecisionStatus: "approved",
		Instruction:    "Implement",
	}

	tasks, err := PrepareTasks(input)
	require.Error(t, err)
	require.Nil(t, tasks)
}

func TestTaskActivationMetadataCompleteness(t *testing.T) {
	workspace := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "PLAN.md"), []byte("# Plan"), 0o644))

	input := Input{
		RunID:               "intake-123",
		SnapshotID:          "snap-abc",
		WorkspaceRoot:       workspace,
		Instruction:         "Implement feature",
		ApprovedPlan:        "PLAN.md",
		ApprovedTaskIDs:     []string{"TASK-1"},
		Clarifications:      []string{"Focus on API"},
		ConflictResolutions: []string{"Use docs/plan_v2.md"},
		IntakeCorrelationID: "corr-intake-1",
		DerivedTasks: []DerivedTask{
			{ID: "TASK-1", Title: "Implement", Files: []string{"src/impl.go"}},
		},
		DecisionStatus: "approved",
	}

	tasks, err := PrepareTasks(input)
	require.NoError(t, err)
	require.Len(t, tasks, 1)

	task := tasks[0]
	require.Equal(t, input.RunID, task.RunID)
	require.Equal(t, input.SnapshotID, task.SnapshotID)
	require.Equal(t, input.Clarifications, task.Clarifications)
	require.Equal(t, input.ConflictResolutions, task.ConflictResolutions)
	require.Equal(t, input.IntakeCorrelationID, task.IntakeCorrelationID)
}

func TestBuildImplementCommand(t *testing.T) {
	task := Task{
		ID:                  "TASK-1",
		Title:               "Build foo",
		Files:               []string{"src/foo.go", "src/foo_test.go"},
		Instruction:         "Implement feature",
		ApprovedPlan:        "PLAN.md",
		Clarifications:      []string{"Focus on API"},
		ConflictResolutions: []string{"Use docs/plan_v2.md"},
		SnapshotID:          "snap-1",
		RunID:               "intake-1",
		IntakeCorrelationID: "corr-intake-1",
	}

	cmd, err := BuildImplementCommand(task)
	require.NoError(t, err)
	require.Equal(t, protocol.ActionImplement, cmd.Action)
	require.Equal(t, protocol.AgentTypeBuilder, cmd.To.AgentType)
	require.Equal(t, task.ID, cmd.TaskID)
	require.Equal(t, task.SnapshotID, cmd.Version.SnapshotID)
	require.NotEmpty(t, cmd.IdempotencyKey)
	require.NotEmpty(t, cmd.CorrelationID)
	require.WithinDuration(t, time.Now().Add(DefaultCommandTimeout), cmd.Deadline, DefaultCommandTimeout)

	require.Equal(t, task.Title, cmd.Inputs["goal"])
	require.Equal(t, task.ApprovedPlan, cmd.Inputs["approved_plan"])
	require.Equal(t, task.Instruction, cmd.Inputs["instruction"])
	if len(task.Files) > 0 {
		expected := task.ToExpectedOutputs()
		require.Equal(t, expected, cmd.ExpectedOutputs)
	}
}

func TestBuildImplementCommandIdempotencyDeterministic(t *testing.T) {
	task := Task{
		ID:          "TASK-1",
		Title:       "Build foo",
		SnapshotID:  "snap-1",
		Instruction: "Implement feature",
		Files:       []string{"src/foo.go"},
	}

	cmd1, err := BuildImplementCommand(task)
	require.NoError(t, err)
	cmd2, err := BuildImplementCommand(task)
	require.NoError(t, err)

	require.Equal(t, cmd1.IdempotencyKey, cmd2.IdempotencyKey)
}

func TestTaskActivationRequiresApprovedDecision(t *testing.T) {
	workspace := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "PLAN.md"), []byte("# Plan"), 0o644))

	input := Input{
		WorkspaceRoot:   workspace,
		Instruction:     "Implement",
		ApprovedPlan:    "PLAN.md",
		ApprovedTaskIDs: []string{"TASK-1"},
		DerivedTasks:    []DerivedTask{{ID: "TASK-1", Title: "Task"}},
		DecisionStatus:  "",
	}

	_, err := PrepareTasks(input)
	require.Error(t, err)
}

func TestTaskActivationRequiresInstruction(t *testing.T) {
	workspace := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "PLAN.md"), []byte("# Plan"), 0o644))

	input := Input{
		WorkspaceRoot:   workspace,
		ApprovedPlan:    "PLAN.md",
		ApprovedTaskIDs: []string{"TASK-1"},
		DerivedTasks:    []DerivedTask{{ID: "TASK-1", Title: "Task"}},
		DecisionStatus:  "approved",
	}

	_, err := PrepareTasks(input)
	require.Error(t, err)
}

// --- Execution Ordering Tests ---

func TestExecutionOrderingHappyPath(t *testing.T) {
	t.Skip("TODO: EO-001 – implement → review → spec pipeline succeeds")
}

func TestExecutionOrderingReviewerChanges(t *testing.T) {
	t.Skip("TODO: EO-002 – reviewer change requests trigger implement_changes loop")
}

func TestExecutionOrderingSpecChanges(t *testing.T) {
	t.Skip("TODO: EO-003 – spec maintainer change requests trigger full round-trip")
}

func TestExecutionOrderingMultipleTasksSequential(t *testing.T) {
	tasks := []Task{
		{ID: "TASK-1", Title: "First"},
		{ID: "TASK-2", Title: "Second"},
		{ID: "TASK-3", Title: "Third"},
	}

	exec := &recordingExecutor{}
	err := Activate(context.Background(), exec, tasks)
	require.NoError(t, err)
	require.Equal(t, []string{"TASK-1", "TASK-2", "TASK-3"}, exec.sequence)
}

func TestExecutionOrderingSnapshotMismatch(t *testing.T) {
	t.Skip("TODO: EO-005 – snapshot/version mismatch detected and surfaced")
}

func TestExecutionOrderingBuilderTestEnforcement(t *testing.T) {
	t.Skip("TODO: EO-006 – builder must report passing tests or fail activation")
}

// --- Traceability Tests ---

func TestTraceabilityReceipts(t *testing.T) {
	t.Skip("TODO: TR-001 – receipts capture intake lineage metadata")
}

func TestTraceabilityRunState(t *testing.T) {
	t.Skip("TODO: TR-002 – run state tracks per-task lineage")
}

func TestTraceabilityEventLog(t *testing.T) {
	t.Skip("TODO: TR-003 – event log records activation command traceability")
}

func TestTraceabilityClarificationMetadata(t *testing.T) {
	t.Skip("TODO: TR-004 – clarifications/conflicts propagate into task commands")
}

// --- Resume Tests ---

func TestResumeAfterBuilderStart(t *testing.T) {
	t.Skip("TODO: RS-001 – resume after builder command avoids duplicates")
}

func TestResumeAfterReviewerChangeRequest(t *testing.T) {
	t.Skip("TODO: RS-002 – resume after reviewer change request retains state")
}

func TestResumeAfterStackedIntakeActivation(t *testing.T) {
	t.Skip("TODO: RS-003 – intake + activation resume sequence works end-to-end")
}

func TestTaskActivationRunStateTransitions(t *testing.T) {
	t.Skip("TODO: RS-004 – state transitions remain valid across crash points")
}

// --- Intake Interaction Tests ---

func TestIntakeDeniedProducesNoActivation(t *testing.T) {
	t.Skip("TODO: INT-001 – denied intake should not enqueue tasks")
}

func TestIntakePartialApproval(t *testing.T) {
	t.Skip("TODO: INT-002 – partial approval activates only selected tasks")
}

func TestIntakeMoreOptionsFlow(t *testing.T) {
	t.Skip("TODO: INT-003 – discovery adds tasks while preserving existing approvals")
}

// --- Error Handling Tests ---

func TestErrorHandlingBuilderFailure(t *testing.T) {
	t.Skip("TODO: ERR-001 – builder failure surfaces error and halts task")
}

func TestErrorHandlingReviewerSpecFailure(t *testing.T) {
	t.Skip("TODO: ERR-002 – reviewer/spec failure handled without corrupting queue")
}

func TestErrorHandlingHeartbeatTimeout(t *testing.T) {
	t.Skip("TODO: ERR-003 – heartbeat timeout aborts task with resumable state")
}

func TestTaskActivationAbortDuringExecution(t *testing.T) {
	t.Skip("TODO: ERR-004 – user abort leaves resumable activation state")
}

// --- Performance Tests ---

func TestPerformanceLargeTaskSet(t *testing.T) {
	t.Skip("TODO: PERF-001 – large task set activates efficiently")
}

type recordingExecutor struct {
	sequence []string
}

func (r *recordingExecutor) ExecuteTask(ctx context.Context, taskID string, inputs map[string]any) error {
	r.sequence = append(r.sequence, taskID)
	return nil
}
