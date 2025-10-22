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
