package scheduler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestSchedulerIdempotencyKeyGeneration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create scheduler with minimal setup (no actual agents needed for this test)
	scheduler := NewScheduler(nil, nil, nil, logger)
	scheduler.SetSnapshotID("snap-test123")

	// Create two commands with same parameters
	cmd1 := scheduler.makeCommand(
		"T-0042",
		protocol.AgentTypeBuilder,
		protocol.ActionImplement,
		map[string]any{"goal": "test goal"},
	)

	cmd2 := scheduler.makeCommand(
		"T-0042",
		protocol.AgentTypeBuilder,
		protocol.ActionImplement,
		map[string]any{"goal": "test goal"},
	)

	// Verify IK format
	if !strings.HasPrefix(cmd1.IdempotencyKey, "ik:") {
		t.Errorf("IK should start with 'ik:', got: %s", cmd1.IdempotencyKey)
	}

	if len(cmd1.IdempotencyKey) != 67 { // "ik:" (3) + 64 hex chars
		t.Errorf("IK length = %d, want 67", len(cmd1.IdempotencyKey))
	}

	// IKs should be identical for same inputs
	if cmd1.IdempotencyKey != cmd2.IdempotencyKey {
		t.Errorf("IKs should be identical for same inputs:\n  %s\n  %s",
			cmd1.IdempotencyKey, cmd2.IdempotencyKey)
	}

	// Different action should produce different IK
	cmd3 := scheduler.makeCommand(
		"T-0042",
		protocol.AgentTypeBuilder,
		protocol.ActionImplementChanges,
		map[string]any{"goal": "test goal"},
	)

	if cmd1.IdempotencyKey == cmd3.IdempotencyKey {
		t.Error("Different actions should produce different IKs")
	}

	// Different inputs should produce different IK
	cmd4 := scheduler.makeCommand(
		"T-0042",
		protocol.AgentTypeBuilder,
		protocol.ActionImplement,
		map[string]any{"goal": "different goal"},
	)

	if cmd1.IdempotencyKey == cmd4.IdempotencyKey {
		t.Error("Different inputs should produce different IKs")
	}

	// Different snapshot should produce different IK
	scheduler.SetSnapshotID("snap-different")
	cmd5 := scheduler.makeCommand(
		"T-0042",
		protocol.AgentTypeBuilder,
		protocol.ActionImplement,
		map[string]any{"goal": "test goal"},
	)

	if cmd1.IdempotencyKey == cmd5.IdempotencyKey {
		t.Error("Different snapshots should produce different IKs")
	}

	// Verify snapshot ID is set correctly
	if cmd5.Version.SnapshotID != "snap-different" {
		t.Errorf("Snapshot ID = %s, want snap-different", cmd5.Version.SnapshotID)
	}
}

// TestSchedulerBuilderMissingTests verifies scheduler rejects builder.completed without tests payload
func TestSchedulerBuilderMissingTests(t *testing.T) {
	mockAgentPath, err := buildMockAgent(t)
	if err != nil {
		t.Fatalf("failed to build mock agent: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create builder with script that omits tests payload
	scriptPath := "../../testdata/fixtures/builder-missing-tests.json"
	builder := supervisor.NewAgentSupervisor(
		protocol.AgentTypeBuilder,
		[]string{mockAgentPath, "-type", "builder", "-no-heartbeat", "-script", scriptPath},
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	// Execute task - should fail
	taskID := "T-TEST-MISSING-TESTS"
	goal := "test missing tests payload"

	err = scheduler.ExecuteTask(ctx, taskID, goal)
	if err == nil {
		t.Fatal("Expected ExecuteTask to fail with missing tests payload, but it succeeded")
	}

	// Verify error message mentions missing tests
	errMsg := err.Error()
	if !strings.Contains(errMsg, "tests") {
		t.Errorf("Error message should mention 'tests', got: %s", errMsg)
	}
}

// TestSchedulerBuilderInvalidTests verifies scheduler rejects builder.completed with invalid tests payload
func TestSchedulerBuilderInvalidTests(t *testing.T) {
	mockAgentPath, err := buildMockAgent(t)
	if err != nil {
		t.Fatalf("failed to build mock agent: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create builder with script that has tests payload missing status field
	scriptPath := "../../testdata/fixtures/builder-invalid-tests.json"
	builder := supervisor.NewAgentSupervisor(
		protocol.AgentTypeBuilder,
		[]string{mockAgentPath, "-type", "builder", "-no-heartbeat", "-script", scriptPath},
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	// Execute task - should fail
	taskID := "T-TEST-INVALID-TESTS"
	goal := "test invalid tests payload"

	err = scheduler.ExecuteTask(ctx, taskID, goal)
	if err == nil {
		t.Fatal("Expected ExecuteTask to fail with invalid tests payload, but it succeeded")
	}

	// Verify error message mentions status field
	errMsg := err.Error()
	if !strings.Contains(errMsg, "status") {
		t.Errorf("Error message should mention 'status', got: %s", errMsg)
	}
}

// TestSchedulerBuilderTestsFailed verifies scheduler rejects builder.completed with failing tests
func TestSchedulerBuilderTestsFailed(t *testing.T) {
	mockAgentPath, err := buildMockAgent(t)
	if err != nil {
		t.Fatalf("failed to build mock agent: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create builder with script that reports failing tests
	scriptPath := "../../testdata/fixtures/builder-tests-failed.json"
	builder := supervisor.NewAgentSupervisor(
		protocol.AgentTypeBuilder,
		[]string{mockAgentPath, "-type", "builder", "-no-heartbeat", "-script", scriptPath},
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	// Execute task - should fail
	taskID := "T-TEST-TESTS-FAILED"
	goal := "test failed tests"

	err = scheduler.ExecuteTask(ctx, taskID, goal)
	if err == nil {
		t.Fatal("Expected ExecuteTask to fail with failing tests, but it succeeded")
	}

	// Verify error message mentions test failure
	errMsg := err.Error()
	if !strings.Contains(errMsg, "fail") {
		t.Errorf("Error message should mention test 'fail', got: %s", errMsg)
	}
}

// TestSchedulerBuilderTestsFailedAllowed verifies scheduler accepts builder.completed with allowed_failures
func TestSchedulerBuilderTestsFailedAllowed(t *testing.T) {
	mockAgentPath, err := buildMockAgent(t)
	if err != nil {
		t.Fatalf("failed to build mock agent: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create builder with script that reports failing tests with allowed_failures: true
	scriptPath := "../../testdata/fixtures/builder-tests-failed-allowed.json"
	builder := supervisor.NewAgentSupervisor(
		protocol.AgentTypeBuilder,
		[]string{mockAgentPath, "-type", "builder", "-no-heartbeat", "-script", scriptPath},
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

	// Track events to verify warning is logged
	var events []*protocol.Event
	scheduler.SetEventHandler(func(evt *protocol.Event) {
		events = append(events, evt)
		t.Logf("Event: %s (status: %s, payload: %+v)", evt.Event, evt.Status, evt.Payload)
	})

	// Execute task - should succeed despite failing tests due to allowed_failures
	taskID := "T-TEST-TESTS-FAILED-ALLOWED"
	goal := "test failed tests with allowed failures"

	err = scheduler.ExecuteTask(ctx, taskID, goal)
	if err != nil {
		t.Fatalf("ExecuteTask failed unexpectedly: %v", err)
	}

	// Verify we got builder.completed event
	var gotBuilderCompleted bool
	for _, evt := range events {
		if evt.Event == protocol.EventBuilderCompleted {
			gotBuilderCompleted = true
			// Verify tests payload has allowed_failures
			if testsRaw, ok := evt.Payload["tests"]; ok {
				if testsMap, ok := testsRaw.(map[string]any); ok {
					if allowed, ok := testsMap["allowed_failures"]; !ok || allowed != true {
						t.Error("Expected allowed_failures: true in tests payload")
					}
				}
			}
			break
		}
	}
	if !gotBuilderCompleted {
		t.Error("did not receive builder.completed event")
	}
}

// TestSchedulerSpecNotesArtifacts verifies spec.changes_requested includes spec_notes artifact
func TestSchedulerSpecNotesArtifacts(t *testing.T) {
	mockAgentPath, err := buildMockAgent(t)
	if err != nil {
		t.Fatalf("failed to build mock agent: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Use simple-success script for builder/reviewer
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

	// Use script that emits spec.changes_requested with spec_notes artifact
	scriptPath := "../../testdata/fixtures/spec-changes-requested.json"
	specMaintainer := supervisor.NewAgentSupervisor(
		protocol.AgentTypeSpecMaintainer,
		[]string{mockAgentPath, "-type", "spec_maintainer", "-no-heartbeat", "-script", scriptPath},
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

	// Create scheduler with temporary workspace for receipts
	scheduler := NewScheduler(builder, reviewer, specMaintainer, logger)
	scheduler.SetWorkspaceRoot(t.TempDir())
	scheduler.SetSnapshotID("snap-test-spec-notes")

	// Track events
	var events []*protocol.Event
	scheduler.SetEventHandler(func(evt *protocol.Event) {
		events = append(events, evt)
		t.Logf("Event: %s (status: %s, artifacts: %d)", evt.Event, evt.Status, len(evt.Artifacts))
	})

	// Execute task - will go through: implement → review → spec.changes_requested → implement_changes → review → update_spec
	taskID := "T-TEST-SPEC-NOTES"
	goal := "test spec notes artifacts"

	// The task will fail at spec.changes_requested, but that's expected for this test
	// We're testing the first iteration where spec.changes_requested is emitted
	err = scheduler.ExecuteTask(ctx, taskID, goal)
	// Note: This will actually succeed because the fixture includes implement_changes/review responses
	if err != nil {
		t.Logf("Task execution error (expected for spec loop): %v", err)
	}

	// Find the spec.changes_requested event
	var specChangesEvent *protocol.Event
	for _, evt := range events {
		if evt.Event == protocol.EventSpecChangesRequested {
			specChangesEvent = evt
			break
		}
	}

	if specChangesEvent == nil {
		t.Fatal("Expected spec.changes_requested event, but did not receive it")
	}

	// Verify spec.changes_requested has artifacts (spec_notes)
	if len(specChangesEvent.Artifacts) == 0 {
		t.Error("spec.changes_requested should include spec_notes artifact")
	}

	// Verify artifact path is spec_notes/<task>.json
	foundSpecNotes := false
	for _, artifact := range specChangesEvent.Artifacts {
		if strings.Contains(artifact.Path, "spec_notes/") && strings.HasSuffix(artifact.Path, ".json") {
			foundSpecNotes = true
			t.Logf("Found spec_notes artifact: %s (%d bytes)", artifact.Path, artifact.Size)
			break
		}
	}

	if !foundSpecNotes {
		t.Error("spec.changes_requested artifact should be in spec_notes/ directory")
	}
}
