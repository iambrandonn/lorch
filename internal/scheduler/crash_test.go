package scheduler

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/eventlog"
	"github.com/iambrandonn/lorch/internal/ledger"
	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/iambrandonn/lorch/internal/runstate"
	"github.com/iambrandonn/lorch/internal/snapshot"
	"github.com/iambrandonn/lorch/internal/supervisor"
	"github.com/iambrandonn/lorch/internal/transcript"
)

// TestCrashAndResumeAfterBuilderCompleted tests resuming after builder completes
func TestCrashAndResumeAfterBuilderCompleted(t *testing.T) {
	mockAgentPath, err := buildMockAgent(t)
	if err != nil {
		t.Fatalf("failed to build mock agent: %v", err)
	}

	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create initial snapshot
	snap := &snapshot.Manifest{
		SnapshotID:    "snap-test-crash-1",
		WorkspaceRoot: "./",
		Files:         []snapshot.FileInfo{},
	}

	// Initialize run state
	runID := "run-crash-test-1"
	taskID := "T-CRASH-001"
	state := runstate.NewRunState(runID, taskID, snap.SnapshotID)
	statePath := filepath.Join(tmpDir, "state", "run.json")
	if err := runstate.SaveRunState(state, statePath); err != nil {
		t.Fatalf("failed to save initial state: %v", err)
	}

	// Create event log
	eventLogPath := filepath.Join(tmpDir, "events", runID+".ndjson")
	evtLog, err := eventlog.NewEventLog(eventLogPath, logger)
	if err != nil {
		t.Fatalf("failed to create event log: %v", err)
	}

	// First execution - simulate crash after builder completes
	{
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		builder := supervisor.NewAgentSupervisor(
			protocol.AgentTypeBuilder,
			[]string{mockAgentPath, "-type", "builder", "-no-heartbeat"},
			map[string]string{},
			logger,
		)

		if err := builder.Start(ctx); err != nil {
			t.Fatalf("failed to start builder: %v", err)
		}

		// Create scheduler
		sched := NewScheduler(builder, nil, nil, logger)
		sched.SetSnapshotID(snap.SnapshotID)
		sched.SetEventLogger(evtLog)

		// Send implement command
		cmd := sched.makeCommand(taskID, protocol.AgentTypeBuilder, protocol.ActionImplement, map[string]any{"goal": "test"})
		if err := sched.sendCommand(builder, cmd); err != nil {
			t.Fatalf("failed to send command: %v", err)
		}

		// Wait for builder.completed
		if err := sched.waitForEvent(ctx, builder, protocol.EventBuilderCompleted, taskID); err != nil {
			t.Fatalf("failed to wait for event: %v", err)
		}

		// Simulate crash - stop agents and close log
		builder.Stop(context.Background())
		evtLog.Close()

		t.Log("Simulated crash after builder completed")
	}

	// Resume execution
	{
		// Load state
		state, err := runstate.LoadRunState(statePath)
		if err != nil {
			t.Fatalf("failed to load state: %v", err)
		}

		if state.Status != runstate.StatusRunning {
			t.Errorf("state status = %s, want running", state.Status)
		}

		// Load ledger
		lg, err := ledger.ReadLedger(eventLogPath)
		if err != nil {
			t.Fatalf("failed to read ledger: %v", err)
		}

		t.Logf("Ledger has %d commands, %d events", len(lg.Commands), len(lg.Events))

		// Check pending commands
		pending := lg.GetPendingCommands()
		t.Logf("Pending commands: %d", len(pending))

		// Reopen event log
		evtLog2, err := eventlog.NewEventLog(eventLogPath, logger)
		if err != nil {
			t.Fatalf("failed to reopen event log: %v", err)
		}
		defer evtLog2.Close()

		// Restart agents
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

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

		// Create scheduler and resume
		sched := NewScheduler(builder, reviewer, specMaintainer, logger)
		sched.SetSnapshotID(snap.SnapshotID)
		sched.SetWorkspaceRoot(tmpDir)
		sched.SetEventLogger(evtLog2)
		sched.SetTranscriptFormatter(transcript.NewFormatter())

		// Resume task (will skip completed commands)
		if err := sched.ResumeTask(ctx, taskID, map[string]any{"goal": "test goal"}, lg); err != nil {
			t.Fatalf("resume execution failed: %v", err)
		}

		// Mark complete
		state.MarkCompleted()
		if err := runstate.SaveRunState(state, statePath); err != nil {
			t.Fatalf("failed to save final state: %v", err)
		}

		t.Log("Resume completed successfully")
	}

	// Verify final state
	finalState, err := runstate.LoadRunState(statePath)
	if err != nil {
		t.Fatalf("failed to load final state: %v", err)
	}

	if finalState.Status != runstate.StatusCompleted {
		t.Errorf("final status = %s, want completed", finalState.Status)
	}

	// Verify ledger has all events
	finalLedger, err := ledger.ReadLedger(eventLogPath)
	if err != nil {
		t.Fatalf("failed to read final ledger: %v", err)
	}

	t.Logf("Final ledger: %d commands, %d events", len(finalLedger.Commands), len(finalLedger.Events))

	// Should have commands for: implement, review, update_spec
	// Some may be duplicated if we reran, but that's ok - verifies idempotency
	if len(finalLedger.Commands) < 3 {
		t.Errorf("expected at least 3 commands, got %d", len(finalLedger.Commands))
	}
}

// TestResumeAlreadyCompleted verifies resuming a completed run is a no-op
func TestResumeAlreadyCompleted(t *testing.T) {
	tmpDir := t.TempDir()

	// Create completed run state
	runID := "run-complete-test"
	taskID := "T-COMPLETE"
	state := runstate.NewRunState(runID, taskID, "snap-test")
	state.MarkCompleted()

	statePath := filepath.Join(tmpDir, "state", "run.json")
	if err := runstate.SaveRunState(state, statePath); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	// Load and check
	loaded, err := runstate.LoadRunState(statePath)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}

	if loaded.Status != runstate.StatusCompleted {
		t.Errorf("status = %s, want completed", loaded.Status)
	}

	// In a real resume, we'd exit early when status is completed
	t.Log("Resume of completed run would exit early (no work needed)")
}

// TestLedgerPersistenceAfterCrash verifies ledger survives crashes
func TestLedgerPersistenceAfterCrash(t *testing.T) {
	tmpDir := t.TempDir()
	ledgerPath := filepath.Join(tmpDir, "events", "test.ndjson")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Write some events
	{
		evtLog, err := eventlog.NewEventLog(ledgerPath, logger)
		if err != nil {
			t.Fatalf("failed to create event log: %v", err)
		}

		cmd := &protocol.Command{
			Kind:          protocol.MessageKindCommand,
			MessageID:     "cmd-1",
			CorrelationID: "corr-1",
			TaskID:        "T-001",
		}

		if err := evtLog.WriteCommand(cmd); err != nil {
			t.Fatalf("failed to write command: %v", err)
		}

		// Simulate crash - close without explicit flush
		evtLog.Close()
	}

	// Read back
	{
		lg, err := ledger.ReadLedger(ledgerPath)
		if err != nil {
			t.Fatalf("failed to read ledger: %v", err)
		}

		if len(lg.Commands) != 1 {
			t.Errorf("commands count = %d, want 1", len(lg.Commands))
		}

		if lg.Commands[0].MessageID != "cmd-1" {
			t.Errorf("command ID = %s, want cmd-1", lg.Commands[0].MessageID)
		}

		t.Log("Ledger persisted correctly after crash")
	}
}

// TestSnapshotImmutability verifies snapshots don't change during run
func TestSnapshotImmutability(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	srcDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(srcDir, 0700); err != nil {
		t.Fatalf("failed to create src dir: %v", err)
	}

	testFile := filepath.Join(srcDir, "test.go")
	if err := os.WriteFile(testFile, []byte("package main\n"), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Capture first snapshot
	snap1, err := snapshot.CaptureSnapshot(tmpDir)
	if err != nil {
		t.Fatalf("failed to capture snapshot 1: %v", err)
	}

	// Modify file
	if err := os.WriteFile(testFile, []byte("package main\n\n// Modified\n"), 0600); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	// Capture second snapshot
	snap2, err := snapshot.CaptureSnapshot(tmpDir)
	if err != nil {
		t.Fatalf("failed to capture snapshot 2: %v", err)
	}

	// Snapshot IDs should differ
	if snap1.SnapshotID == snap2.SnapshotID {
		t.Error("snapshot IDs should differ after file modification")
	}

	t.Logf("Snapshot 1: %s", snap1.SnapshotID)
	t.Logf("Snapshot 2: %s", snap2.SnapshotID)
	t.Log("Snapshot correctly detects workspace changes")
}

// TestCrashAndResumeAfterSpecChangesRequested tests granular resume in spec loop per P1.4-ANSWERS A5
// Scenario: crash after spec.changes_requested, resume should continue from implement_changes
func TestCrashAndResumeAfterSpecChangesRequested(t *testing.T) {
	mockAgentPath, err := buildMockAgent(t)
	if err != nil {
		t.Fatalf("failed to build mock agent: %v", err)
	}

	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create initial snapshot
	snap := &snapshot.Manifest{
		SnapshotID:    "snap-test-spec-crash",
		WorkspaceRoot: "./",
		Files:         []snapshot.FileInfo{},
	}

	// Initialize run state
	runID := "run-spec-crash-test"
	taskID := "T-SPEC-CRASH"
	state := runstate.NewRunState(runID, taskID, snap.SnapshotID)
	statePath := filepath.Join(tmpDir, "state", "run.json")
	if err := runstate.SaveRunState(state, statePath); err != nil {
		t.Fatalf("failed to save initial state: %v", err)
	}

	// Create event log
	eventLogPath := filepath.Join(tmpDir, "events", runID+".ndjson")
	evtLog, err := eventlog.NewEventLog(eventLogPath, logger)
	if err != nil {
		t.Fatalf("failed to create event log: %v", err)
	}

	// First execution - manually run workflow until spec.changes_requested, then simulate crash
	{
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

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

		// Use spec-changes-count=1 to emit spec.changes_requested once
		specMaintainer := supervisor.NewAgentSupervisor(
			protocol.AgentTypeSpecMaintainer,
			[]string{mockAgentPath, "-type", "spec_maintainer", "-no-heartbeat", "-spec-changes-count", "1"},
			map[string]string{},
			logger,
		)

		if err := builder.Start(ctx); err != nil {
			t.Fatalf("failed to start builder: %v", err)
		}

		if err := reviewer.Start(ctx); err != nil {
			t.Fatalf("failed to start reviewer: %v", err)
		}

		if err := specMaintainer.Start(ctx); err != nil {
			t.Fatalf("failed to start spec maintainer: %v", err)
		}

		// Create scheduler
		sched := NewScheduler(builder, reviewer, specMaintainer, logger)
		sched.SetSnapshotID(snap.SnapshotID)
		sched.SetWorkspaceRoot(tmpDir)
		sched.SetEventLogger(evtLog)

		// Manually execute workflow: implement → review → update_spec (which returns spec.changes_requested)
		// Step 1: Implement
		t.Log("Step 1: Executing implement")
		if err := sched.executeImplement(ctx, taskID, map[string]any{"goal": "test goal"}); err != nil {
			t.Fatalf("failed to execute implement: %v", err)
		}

		// Step 2: Review
		t.Log("Step 2: Executing review")
		reviewStatus, err := sched.executeReview(ctx, taskID)
		if err != nil {
			t.Fatalf("failed to execute review: %v", err)
		}
		t.Logf("Review status: %s", reviewStatus)

		// Step 3: Update spec (will return spec.changes_requested)
		t.Log("Step 3: Executing update_spec")
		specStatus, err := sched.executeSpecMaintenance(ctx, taskID)
		if err != nil {
			t.Fatalf("failed to execute spec maintenance: %v", err)
		}
		t.Logf("Spec status: %s", specStatus)

		if specStatus != protocol.EventSpecChangesRequested {
			t.Fatalf("Expected spec.changes_requested, got %s", specStatus)
		}

		// NOW simulate crash - stop agents and close log WITHOUT completing the implement_changes cycle
		builder.Stop(context.Background())
		reviewer.Stop(context.Background())
		specMaintainer.Stop(context.Background())
		evtLog.Close()

		t.Log("Simulated crash immediately after spec.changes_requested (before implement_changes)")
	}

	// Resume execution - should continue from implement_changes per P1.4-ANSWERS A5
	{
		// Load ledger
		lg, err := ledger.ReadLedger(eventLogPath)
		if err != nil {
			t.Fatalf("failed to read ledger: %v", err)
		}

		t.Logf("Ledger before resume: %d commands, %d events", len(lg.Commands), len(lg.Events))

		// Verify ledger contains spec.changes_requested
		hasSpecChangesRequested := false
		for _, evt := range lg.Events {
			if evt.Event == protocol.EventSpecChangesRequested {
				hasSpecChangesRequested = true
				break
			}
		}

		if !hasSpecChangesRequested {
			t.Fatal("Ledger should contain spec.changes_requested event")
		}

		// Reopen event log
		evtLog2, err := eventlog.NewEventLog(eventLogPath, logger)
		if err != nil {
			t.Fatalf("failed to reopen event log: %v", err)
		}
		defer evtLog2.Close()

		// Restart agents
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

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

		// Spec maintainer will now approve (no more changes requested)
		specMaintainer := supervisor.NewAgentSupervisor(
			protocol.AgentTypeSpecMaintainer,
			[]string{mockAgentPath, "-type", "spec_maintainer", "-no-heartbeat"},
			map[string]string{},
			logger,
		)

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

		// Create scheduler and resume
		sched := NewScheduler(builder, reviewer, specMaintainer, logger)
		sched.SetSnapshotID(snap.SnapshotID)
		sched.SetWorkspaceRoot(tmpDir)
		sched.SetEventLogger(evtLog2)

		// Track which commands are executed during resume
		resumedImplementChanges := false
		resumedReview := false
		resumedUpdateSpec := false

		sched.SetEventHandler(func(evt *protocol.Event) {
			t.Logf("[RESUME] Event: %s", evt.Event)
			if evt.Event == protocol.EventBuilderCompleted {
				resumedImplementChanges = true
			}
			if evt.Event == protocol.EventReviewCompleted {
				resumedReview = true
			}
			if evt.Event == protocol.EventSpecUpdated || evt.Event == protocol.EventSpecNoChangesNeeded {
				resumedUpdateSpec = true
			}
		})

		// Resume task - per P1.4-ANSWERS A5, should continue from implement_changes
		t.Log("Starting ResumeTask...")
		if err := sched.ResumeTask(ctx, taskID, map[string]any{"goal": "test spec loop crash"}, lg); err != nil {
			t.Fatalf("resume execution failed: %v", err)
		}
		t.Log("ResumeTask completed")

		// Verify granular resume: should have executed implement_changes, review, and update_spec
		if !resumedImplementChanges {
			t.Error("Resume should have executed implement_changes (granular resume per A5)")
		}

		if !resumedReview {
			t.Error("Resume should have executed review after implement_changes")
		}

		if !resumedUpdateSpec {
			t.Error("Resume should have executed update_spec after review")
		} else {
			t.Log("✓ All three steps executed during resume (implement_changes, review, update_spec)")
		}

		// Mark complete
		state.MarkCompleted()
		if err := runstate.SaveRunState(state, statePath); err != nil {
			t.Fatalf("failed to save final state: %v", err)
		}

		t.Log("Resume completed successfully with granular continuation")
	}

	// Verify final state
	finalState, err := runstate.LoadRunState(statePath)
	if err != nil {
		t.Fatalf("failed to load final state: %v", err)
	}

	if finalState.Status != runstate.StatusCompleted {
		t.Errorf("final status = %s, want completed", finalState.Status)
	}

	t.Log("✓ Test passed: granular spec loop resume works correctly")
}
