package activation

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/iambrandonn/lorch/internal/receipt"
	"github.com/iambrandonn/lorch/internal/scheduler"
	"github.com/iambrandonn/lorch/internal/supervisor"
)

func TestActivationEndToEnd(t *testing.T) {
	mockAgentPath, err := buildMockAgent(t)
	if err != nil {
		t.Fatalf("failed to build mock agent: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	builder := supervisor.NewAgentSupervisor(
		protocol.AgentTypeBuilder,
		[]string{mockAgentPath, "-type", "builder", "-no-heartbeat"},
		map[string]string{},
		logger,
	)

	reviewer := supervisor.NewAgentSupervisor(
		protocol.AgentTypeReviewer,
		[]string{mockAgentPath, "-type", "reviewer", "-no-heartbeat"},
		map[string]string{},
		logger,
	)

	specMaintainer := supervisor.NewAgentSupervisor(
		protocol.AgentTypeSpecMaintainer,
		[]string{mockAgentPath, "-type", "spec_maintainer", "-no-heartbeat"},
		map[string]string{},
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := builder.Start(ctx); err != nil {
		t.Fatalf("failed to start builder: %v", err)
	}
	defer builder.Stop(context.Background())

	if err := reviewer.Start(ctx); err != nil {
		t.Fatalf("failed to start reviewer: %v", err)
	}
	defer reviewer.Stop(context.Background())

	if err := specMaintainer.Start(ctx); err != nil {
		t.Fatalf("failed to start spec maintainer: %v", err)
	}
	defer specMaintainer.Stop(context.Background())

	sched := scheduler.NewScheduler(builder, reviewer, specMaintainer, logger)
	sched.SetSnapshotID("snap-1")

	var events []string
	sched.SetEventHandler(func(evt *protocol.Event) {
		events = append(events, evt.Event)
	})

	workspace := t.TempDir()
	planPath := filepath.Join(workspace, "PLAN.md")
	if err := os.WriteFile(planPath, []byte("# Plan"), 0o644); err != nil {
		t.Fatalf("failed to write plan: %v", err)
	}

	input := Input{
		RunID:           "intake-run",
		SnapshotID:      "snap-1",
		WorkspaceRoot:   workspace,
		Instruction:     "Implement feature",
		ApprovedPlan:    "PLAN.md",
		ApprovedTaskIDs: []string{"TASK-1"},
		DerivedTasks: []DerivedTask{
			{ID: "TASK-1", Title: "Build foo", Files: []string{"src/foo.go"}},
		},
		DecisionStatus:      "approved",
		IntakeCorrelationID: "corr-intake-1",
	}

	tasks, err := PrepareTasks(input)
	if err != nil {
		t.Fatalf("PrepareTasks failed: %v", err)
	}

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	if err := Activate(ctx, sched, tasks); err != nil {
		t.Fatalf("Activate failed: %v", err)
	}

	if len(events) == 0 {
		t.Fatalf("expected events, got none")
	}

	var gotBuilderCompleted, gotReviewCompleted, gotSpecEvent bool
	for _, evt := range events {
		switch evt {
		case protocol.EventBuilderCompleted:
			gotBuilderCompleted = true
		case protocol.EventReviewCompleted:
			gotReviewCompleted = true
		case protocol.EventSpecUpdated, protocol.EventSpecNoChangesNeeded:
			gotSpecEvent = true
		}
	}

	if !gotBuilderCompleted {
		t.Error("builder.completed not observed")
	}
	if !gotReviewCompleted {
		t.Error("review.completed not observed")
	}
	if !gotSpecEvent {
		t.Error("spec event not observed")
	}
}

func buildMockAgent(t *testing.T) (string, error) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "mockagent")
	cmd := execCommand("go", "build", "-o", path, "./cmd/mockagent")
	cmd.Dir = filepath.Join("..", "..")
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build mockagent: %w\n%s", err, string(output))
	}
	return path, nil
}

func execCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()
	return cmd
}

// TestReceiptTraceability validates TR-001 from Phase 2.4 test plan:
// Receipts must include intake_run_id, approved_plan, approved_task_id, and related metadata
// for full traceability back to the natural language intake conversation.
func TestReceiptTraceability(t *testing.T) {
	// Setup
	mockAgentPath, err := buildMockAgent(t)
	if err != nil {
		t.Fatalf("failed to build mock agent: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	builder := supervisor.NewAgentSupervisor(
		protocol.AgentTypeBuilder,
		[]string{mockAgentPath, "-type", "builder", "-no-heartbeat"},
		map[string]string{},
		logger,
	)

	reviewer := supervisor.NewAgentSupervisor(
		protocol.AgentTypeReviewer,
		[]string{mockAgentPath, "-type", "reviewer", "-no-heartbeat"},
		map[string]string{},
		logger,
	)

	specMaintainer := supervisor.NewAgentSupervisor(
		protocol.AgentTypeSpecMaintainer,
		[]string{mockAgentPath, "-type", "spec_maintainer", "-no-heartbeat"},
		map[string]string{},
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := builder.Start(ctx); err != nil {
		t.Fatalf("failed to start builder: %v", err)
	}
	defer builder.Stop(context.Background())

	if err := reviewer.Start(ctx); err != nil {
		t.Fatalf("failed to start reviewer: %v", err)
	}
	defer reviewer.Stop(context.Background())

	if err := specMaintainer.Start(ctx); err != nil {
		t.Fatalf("failed to start spec maintainer: %v", err)
	}
	defer specMaintainer.Stop(context.Background())

	// Setup workspace and scheduler
	workspace := t.TempDir()
	planPath := filepath.Join(workspace, "PLAN.md")
	if err := os.WriteFile(planPath, []byte("# Plan"), 0o644); err != nil {
		t.Fatalf("failed to write plan: %v", err)
	}

	sched := scheduler.NewScheduler(builder, reviewer, specMaintainer, logger)
	sched.SetSnapshotID("snap-test-123")
	sched.SetWorkspaceRoot(workspace)

	// Create activation input with rich traceability metadata
	input := Input{
		RunID:           "intake-run-456",
		SnapshotID:      "snap-test-123",
		WorkspaceRoot:   workspace,
		Instruction:     "Add user authentication with OAuth2",
		ApprovedPlan:    "PLAN.md",
		ApprovedTaskIDs: []string{"AUTH-1"},
		DerivedTasks: []DerivedTask{
			{
				ID:    "AUTH-1",
				Title: "Implement OAuth2 login flow",
				Files: []string{"src/auth.go", "tests/auth_test.go"},
			},
		},
		Clarifications:      []string{"Use Google OAuth provider", "Store tokens in Redis"},
		ConflictResolutions: []string{"Keep existing session management"},
		DecisionStatus:      "approved",
		IntakeCorrelationID: "corr-intake-tr001",
	}

	// Execute activation pipeline
	tasks, err := PrepareTasks(input)
	if err != nil {
		t.Fatalf("PrepareTasks failed: %v", err)
	}

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	if err := Activate(ctx, sched, tasks); err != nil {
		t.Fatalf("Activate failed: %v", err)
	}

	// Verify receipts were written and contain traceability metadata
	receipts, err := receipt.ListReceipts(workspace, "AUTH-1")
	if err != nil {
		t.Fatalf("failed to list receipts: %v", err)
	}

	if len(receipts) == 0 {
		t.Fatal("expected at least one receipt, got none")
	}

	// Check the first receipt (builder implement step)
	rec := receipts[0]

	// Validate traceability fields are populated correctly
	if rec.TaskTitle != "Implement OAuth2 login flow" {
		t.Errorf("TaskTitle = %q, want %q", rec.TaskTitle, "Implement OAuth2 login flow")
	}

	if rec.Instruction != "Add user authentication with OAuth2" {
		t.Errorf("Instruction = %q, want %q", rec.Instruction, "Add user authentication with OAuth2")
	}

	if rec.ApprovedPlan != "PLAN.md" {
		t.Errorf("ApprovedPlan = %q, want %q", rec.ApprovedPlan, "PLAN.md")
	}

	// IntakeCorrelationID should be extracted from command correlation ID
	if rec.IntakeCorrelationID == "" {
		t.Error("IntakeCorrelationID is empty, expected intake correlation to be preserved")
	}

	if len(rec.Clarifications) != 2 {
		t.Errorf("Clarifications count = %d, want 2", len(rec.Clarifications))
	} else {
		if rec.Clarifications[0] != "Use Google OAuth provider" {
			t.Errorf("Clarifications[0] = %q, want %q", rec.Clarifications[0], "Use Google OAuth provider")
		}
		if rec.Clarifications[1] != "Store tokens in Redis" {
			t.Errorf("Clarifications[1] = %q, want %q", rec.Clarifications[1], "Store tokens in Redis")
		}
	}

	if len(rec.ConflictResolutions) != 1 {
		t.Errorf("ConflictResolutions count = %d, want 1", len(rec.ConflictResolutions))
	} else {
		if rec.ConflictResolutions[0] != "Keep existing session management" {
			t.Errorf("ConflictResolutions[0] = %q", rec.ConflictResolutions[0])
		}
	}

	// Validate all receipts (implement, review, spec) preserve traceability
	for i, r := range receipts {
		if r.TaskTitle == "" {
			t.Errorf("receipt %d: TaskTitle is empty", i)
		}
		if r.Instruction == "" {
			t.Errorf("receipt %d: Instruction is empty", i)
		}
		if r.ApprovedPlan == "" {
			t.Errorf("receipt %d: ApprovedPlan is empty", i)
		}
		// IntakeCorrelationID may be empty for non-implement commands (review, spec)
		// but at least the first receipt should have it
	}

	t.Logf("TR-001 validated: %d receipts with complete traceability metadata", len(receipts))
}
