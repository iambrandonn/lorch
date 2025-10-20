package scheduler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/iambrandonn/lorch/internal/supervisor"
)

func TestSchedulerBasicFlow(t *testing.T) {
	mockAgentPath, err := buildMockAgent(t)
	if err != nil {
		t.Fatalf("failed to build mock agent: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create supervisors for all agents
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

	// Start all agents
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

	// Create scheduler
	scheduler := NewScheduler(builder, reviewer, specMaintainer, logger)

	// Track events
	var events []*protocol.Event
	scheduler.SetEventHandler(func(evt *protocol.Event) {
		events = append(events, evt)
		t.Logf("Event: %s (status: %s, task: %s)", evt.Event, evt.Status, evt.TaskID)
	})

	// Execute task
	taskID := "T-TEST-001"
	goal := "test implementation"

	if err := scheduler.ExecuteTask(ctx, taskID, goal); err != nil {
		t.Fatalf("ExecuteTask failed: %v", err)
	}

	// Verify we got the expected events
	if len(events) < 3 {
		t.Fatalf("expected at least 3 events, got %d", len(events))
	}

	// Check for builder.completed
	var gotBuilderCompleted bool
	for _, evt := range events {
		if evt.Event == protocol.EventBuilderCompleted {
			gotBuilderCompleted = true
			break
		}
	}
	if !gotBuilderCompleted {
		t.Error("did not receive builder.completed event")
	}

	// Check for review.completed
	var gotReviewCompleted bool
	for _, evt := range events {
		if evt.Event == protocol.EventReviewCompleted {
			gotReviewCompleted = true
			break
		}
	}
	if !gotReviewCompleted {
		t.Error("did not receive review.completed event")
	}

	// Check for spec.updated or spec.no_changes_needed
	var gotSpecEvent bool
	for _, evt := range events {
		if evt.Event == protocol.EventSpecUpdated || evt.Event == protocol.EventSpecNoChangesNeeded {
			gotSpecEvent = true
			break
		}
	}
	if !gotSpecEvent {
		t.Error("did not receive spec.updated or spec.no_changes_needed event")
	}
}

func TestSchedulerReviewLoop(t *testing.T) {
	mockAgentPath, err := buildMockAgent(t)
	if err != nil {
		t.Fatalf("failed to build mock agent: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create builder (normal behavior)
	builder := supervisor.NewAgentSupervisor(
		protocol.AgentTypeBuilder,
		[]string{mockAgentPath, "-type", "builder", "-no-heartbeat"},
		map[string]string{},
		logger,
	)

	// Create reviewer that requests changes 2 times before approving
	reviewer := supervisor.NewAgentSupervisor(
		protocol.AgentTypeReviewer,
		[]string{mockAgentPath, "-type", "reviewer", "-no-heartbeat", "-review-changes-count", "2"},
		map[string]string{},
		logger,
	)

	// Create spec maintainer (normal behavior)
	specMaintainer := supervisor.NewAgentSupervisor(
		protocol.AgentTypeSpecMaintainer,
		[]string{mockAgentPath, "-type", "spec_maintainer", "-no-heartbeat"},
		map[string]string{},
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start all agents
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

	// Create scheduler
	scheduler := NewScheduler(builder, reviewer, specMaintainer, logger)

	// Track events
	var events []*protocol.Event
	scheduler.SetEventHandler(func(evt *protocol.Event) {
		events = append(events, evt)
		t.Logf("Event: %s (status: %s, task: %s)", evt.Event, evt.Status, evt.TaskID)
	})

	// Execute task
	taskID := "T-TEST-REVIEW-LOOP"
	goal := "test review iteration"

	if err := scheduler.ExecuteTask(ctx, taskID, goal); err != nil {
		t.Fatalf("ExecuteTask failed: %v", err)
	}

	// Verify we got multiple review iterations
	reviewCount := 0
	changesRequestedCount := 0
	approvedCount := 0

	for _, evt := range events {
		if evt.Event == protocol.EventReviewCompleted {
			reviewCount++
			if evt.Status == protocol.ReviewStatusChangesRequested {
				changesRequestedCount++
			} else if evt.Status == protocol.ReviewStatusApproved {
				approvedCount++
			}
		}
	}

	// We expect: 2 changes_requested + 1 approved = 3 total reviews
	if reviewCount != 3 {
		t.Errorf("expected 3 review events, got %d", reviewCount)
	}

	if changesRequestedCount != 2 {
		t.Errorf("expected 2 changes_requested, got %d", changesRequestedCount)
	}

	if approvedCount != 1 {
		t.Errorf("expected 1 approved, got %d", approvedCount)
	}

	// We should also see multiple builder.completed events (one per iteration)
	builderCompletedCount := 0
	for _, evt := range events {
		if evt.Event == protocol.EventBuilderCompleted {
			builderCompletedCount++
		}
	}

	// Initial implement + 2 implement_changes = 3 total
	if builderCompletedCount != 3 {
		t.Errorf("expected 3 builder.completed events, got %d", builderCompletedCount)
	}
}

func TestSchedulerSpecLoop(t *testing.T) {
	mockAgentPath, err := buildMockAgent(t)
	if err != nil {
		t.Fatalf("failed to build mock agent: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create builder (normal behavior)
	builder := supervisor.NewAgentSupervisor(
		protocol.AgentTypeBuilder,
		[]string{mockAgentPath, "-type", "builder", "-no-heartbeat"},
		map[string]string{},
		logger,
	)

	// Create reviewer (normal behavior - always approves)
	reviewer := supervisor.NewAgentSupervisor(
		protocol.AgentTypeReviewer,
		[]string{mockAgentPath, "-type", "reviewer", "-no-heartbeat"},
		map[string]string{},
		logger,
	)

	// Create spec maintainer that requests changes 1 time before updating
	specMaintainer := supervisor.NewAgentSupervisor(
		protocol.AgentTypeSpecMaintainer,
		[]string{mockAgentPath, "-type", "spec_maintainer", "-no-heartbeat", "-spec-changes-count", "1"},
		map[string]string{},
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start all agents
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

	// Create scheduler
	scheduler := NewScheduler(builder, reviewer, specMaintainer, logger)

	// Track events
	var events []*protocol.Event
	scheduler.SetEventHandler(func(evt *protocol.Event) {
		events = append(events, evt)
		t.Logf("Event: %s (status: %s, task: %s)", evt.Event, evt.Status, evt.TaskID)
	})

	// Execute task
	taskID := "T-TEST-SPEC-LOOP"
	goal := "test spec iteration"

	if err := scheduler.ExecuteTask(ctx, taskID, goal); err != nil {
		t.Fatalf("ExecuteTask failed: %v", err)
	}

	// Verify we got multiple spec maintenance iterations
	specChangesRequestedCount := 0
	specUpdatedCount := 0

	for _, evt := range events {
		if evt.Event == protocol.EventSpecChangesRequested {
			specChangesRequestedCount++
		} else if evt.Event == protocol.EventSpecUpdated {
			specUpdatedCount++
		}
	}

	// We expect: 1 spec.changes_requested + 1 spec.updated = 2 total
	if specChangesRequestedCount != 1 {
		t.Errorf("expected 1 spec.changes_requested, got %d", specChangesRequestedCount)
	}

	if specUpdatedCount != 1 {
		t.Errorf("expected 1 spec.updated, got %d", specUpdatedCount)
	}

	// After spec.changes_requested, we should see:
	// - implement_changes (builder.completed)
	// - review (review.completed: approved)
	// - update_spec again (spec.updated)
	//
	// So total builder.completed should be: initial implement + 1 implement_changes = 2
	builderCompletedCount := 0
	for _, evt := range events {
		if evt.Event == protocol.EventBuilderCompleted {
			builderCompletedCount++
		}
	}

	if builderCompletedCount != 2 {
		t.Errorf("expected 2 builder.completed events, got %d", builderCompletedCount)
	}

	// Total reviews should be: initial review + 1 re-review = 2
	reviewCount := 0
	for _, evt := range events {
		if evt.Event == protocol.EventReviewCompleted {
			reviewCount++
		}
	}

	if reviewCount != 2 {
		t.Errorf("expected 2 review.completed events, got %d", reviewCount)
	}
}

func buildMockAgent(t *testing.T) (string, error) {
	t.Helper()

	mockAgentPath := filepath.Join(t.TempDir(), "mockagent")

	cmd := exec.Command("go", "build", "-o", mockAgentPath, "../../cmd/mockagent")
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to build mockagent: %w", err)
	}

	return mockAgentPath, nil
}
