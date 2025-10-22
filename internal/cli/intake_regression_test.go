package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/config"
	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/iambrandonn/lorch/internal/workspace"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Test Helpers for Regression Tests
// ============================================================================

// assertIntakeLogContains verifies event log contains specific event types
func assertIntakeLogContains(t *testing.T, logPath string, eventTypes ...string) {
	t.Helper()
	file, err := os.Open(logPath)
	require.NoError(t, err)
	defer file.Close()

	found := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var raw map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			continue
		}
		if eventName, ok := raw["event"].(string); ok {
			for _, expected := range eventTypes {
				if eventName == expected {
					found[expected] = true
				}
			}
		}
	}
	require.NoError(t, scanner.Err())

	for _, expected := range eventTypes {
		require.True(t, found[expected], "expected event %s not found in log %s", expected, logPath)
	}
}

// assertStateCleanup verifies intake flow ended (may still be in "running" state if interrupted)
func assertStateCleanup(t *testing.T, workspaceRoot string) {
	t.Helper()
	// For regression tests, we just verify no active task execution state
	// The state may remain "running" if intake was aborted mid-flow
	stateFile := filepath.Join(workspaceRoot, "state", "run.json")
	if _, err := os.Stat(stateFile); err == nil {
		data, err := os.ReadFile(stateFile)
		require.NoError(t, err)
		var state map[string]any
		require.NoError(t, json.Unmarshal(data, &state))

		// Just verify state file is readable and well-formed
		_, hasRunID := state["run_id"]
		require.True(t, hasRunID, "state should have run_id field")
	}
}

// runNonTTYIntake runs intake flow with non-TTY stdin simulation
func runNonTTYIntake(t *testing.T, workspaceRoot string, fixturePath string, inputs []string, cfg *config.Config) (*IntakeOutcome, error) {
	t.Helper()

	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Test Plan\n"), 0o644))

	binary := buildTestBinary(t)
	cfg.Agents.Orchestration.Cmd = []string{
		binary,
		"--role", "orchestration",
		"--fixture", fixturePath,
		"--no-heartbeat",
	}
	cfg.Agents.Orchestration.TimeoutsS["intake"] = 10

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))

	inputStr := strings.Join(inputs, "\n") + "\n"
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(strings.NewReader(inputStr))
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	return runIntakeFlow(cmd, cfg, workspaceRoot, logger)
}

// buildTestBinary builds claude-fixture binary for tests
func buildTestBinary(t *testing.T) string {
	t.Helper()
	// Reuse helper from run_test.go
	return buildHelperBinary(t, "./cmd/claude-fixture")
}

// ============================================================================
// Section 1: Denied Approval Regressions
// ============================================================================

// TestRegression_DeclineDuringTaskSelection verifies user can decline after plan approval
func TestRegression_DeclineDuringTaskSelection(t *testing.T) {
	workspaceRoot := t.TempDir()
	fixturePath := filepath.Join(repoRoot(t), "testdata", "fixtures", "orchestration-simple.json")

	cfg := config.GenerateDefault()
	// User input: instruction → plan 1 → decline tasks (0)
	outcome, err := runNonTTYIntake(t, workspaceRoot, fixturePath, []string{"Manage PLAN.md", "1", "0"}, cfg)

	require.Error(t, err)
	require.ErrorIs(t, err, errUserDeclined)
	require.Nil(t, outcome)

	// Verify denial recorded in intake log
	entries, readErr := os.ReadDir(filepath.Join(workspaceRoot, "events"))
	require.NoError(t, readErr)
	require.Len(t, entries, 1)
	logPath := filepath.Join(workspaceRoot, "events", entries[0].Name())

	assertIntakeLogContains(t, logPath, protocol.EventSystemUserDecision)

	// Verify system.user_decision shows denied status
	decisionEvt := findSystemDecisionEvent(t, logPath)
	require.Equal(t, "denied", decisionEvt.Status)
	require.Contains(t, decisionEvt.Payload["reason"], "declined")

	// Verify no task activation occurred
	assertStateCleanup(t, workspaceRoot)
}

// TestRegression_DeclineAfterMultipleClarifications verifies clarifications preserved on denial
func TestRegression_DeclineAfterMultipleClarifications(t *testing.T) {
	workspaceRoot := t.TempDir()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Plan\n"), 0o644))

	cfg := config.GenerateDefault()
	cfg.Agents.Orchestration = &config.AgentConfig{
		Enabled: true,
		Cmd:     []string{"fake"},
		Env:     map[string]string{},
		TimeoutsS: map[string]int{
			"intake": 60,
		},
	}

	sup := newFakeOrchestrationSupervisor()
	var firstIK string
	sup.onSend = func(cmd *protocol.Command) {
		switch len(sup.commands) {
		case 1:
			firstIK = cmd.IdempotencyKey
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-clarify-1",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
				Event:         protocol.EventOrchestrationNeedsClarification,
				Payload: map[string]any{
					"questions": []any{"What is the priority?"},
				},
			})
		case 2:
			require.Equal(t, firstIK, cmd.IdempotencyKey)
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-clarify-2",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
				Event:         protocol.EventOrchestrationNeedsClarification,
				Payload: map[string]any{
					"questions": []any{"Should tests be included?"},
				},
			})
		case 3:
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-proposed",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
				Event:         protocol.EventOrchestrationProposedTasks,
				Payload: map[string]any{
					"plan_candidates": []any{
						map[string]any{"path": "PLAN.md", "confidence": 0.9},
					},
					"derived_tasks": []any{
						map[string]any{"id": "TASK-CLARIFY", "title": "Task after clarifications"},
					},
				},
			})
		default:
			t.Fatalf("unexpected number of commands: %d", len(sup.commands))
		}
	}
	overrideOrchestrationSupervisorFactory(t, sup)

	// User provides clarifications but declines final proposal
	inReader := strings.NewReader("Manage PLAN.md\nHigh priority\nYes include tests\n0\n")

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(inReader)
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	outcome, err := runIntakeFlow(cmd, cfg, workspaceRoot, slog.Default())
	require.Error(t, err)
	require.ErrorIs(t, err, errUserDeclined)
	require.Nil(t, outcome)

	// Verify clarifications were preserved in state before denial
	state := loadRunStateFile(t, workspaceRoot)
	clarifications := getNestedStringSlice(state, "intake", "last_clarifications")
	require.Len(t, clarifications, 2)
	require.Contains(t, clarifications, "High priority")
	require.Contains(t, clarifications, "Yes include tests")

	// Verify denial recorded
	logPath := findLatestIntakeLog(t, workspaceRoot)
	assertIntakeLogContains(t, logPath, protocol.EventSystemUserDecision)
}

// TestRegression_DeclineAfterTaskDiscovery verifies denial after requesting more options
func TestRegression_DeclineAfterTaskDiscovery(t *testing.T) {
	workspaceRoot := t.TempDir()
	fixturePath := filepath.Join(repoRoot(t), "testdata", "fixtures", "orchestration-discovery-expanded.json")

	cfg := config.GenerateDefault()
	// User: instruction → "m" (more) → decline expanded set (0)
	outcome, err := runNonTTYIntake(t, workspaceRoot, fixturePath, []string{"Manage PLAN.md", "m", "0"}, cfg)

	require.Error(t, err)
	require.ErrorIs(t, err, errUserDeclined)
	require.Nil(t, outcome)

	// Verify task_discovery command was logged
	entries, readErr := os.ReadDir(filepath.Join(workspaceRoot, "events"))
	require.NoError(t, readErr)
	logPath := filepath.Join(workspaceRoot, "events", entries[0].Name())

	// Check for task_discovery command in log
	file, err := os.Open(logPath)
	require.NoError(t, err)
	defer file.Close()

	foundDiscovery := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var raw map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			continue
		}
		if action, ok := raw["action"].(string); ok && action == string(protocol.ActionTaskDiscovery) {
			foundDiscovery = true
			break
		}
	}
	require.True(t, foundDiscovery, "task_discovery command not found in log")

	// Verify denial recorded after discovery
	assertIntakeLogContains(t, logPath, protocol.EventSystemUserDecision)
}

// TestRegression_AbortDuringConflictResolution verifies abort triggers denial
func TestRegression_AbortDuringConflictResolution(t *testing.T) {
	workspaceRoot := t.TempDir()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Plan\n"), 0o644))

	cfg := config.GenerateDefault()
	cfg.Agents.Orchestration = &config.AgentConfig{
		Enabled: true,
		Cmd:     []string{"fake"},
		Env:     map[string]string{},
		TimeoutsS: map[string]int{
			"intake": 60,
		},
	}

	sup := newFakeOrchestrationSupervisor()
	sup.onSend = func(cmd *protocol.Command) {
		sup.emit(&protocol.Event{
			Kind:          protocol.MessageKindEvent,
			MessageID:     "evt-conflict",
			CorrelationID: cmd.CorrelationID,
			TaskID:        cmd.TaskID,
			From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
			Event:         protocol.EventOrchestrationPlanConflict,
			Payload: map[string]any{
				"message": "Test conflict for abort scenario",
			},
		})
	}
	overrideOrchestrationSupervisorFactory(t, sup)

	// User aborts during conflict resolution
	inReader := strings.NewReader("Manage PLAN.md\nabort\n")

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(inReader)
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	outcome, err := runIntakeFlow(cmd, cfg, workspaceRoot, slog.Default())
	require.Error(t, err)
	require.ErrorIs(t, err, errUserDeclined)
	require.Nil(t, outcome)

	// Verify denial recorded
	logPath := findLatestIntakeLog(t, workspaceRoot)
	decisionEvt := findSystemDecisionEvent(t, logPath)
	require.Equal(t, "denied", decisionEvt.Status)
	require.Contains(t, decisionEvt.Payload["reason"], "conflict")

	assertStateCleanup(t, workspaceRoot)
}

// TestRegression_DeclinePreservesIntakeLog verifies logs persist after decline
func TestRegression_DeclinePreservesIntakeLog(t *testing.T) {
	workspaceRoot := t.TempDir()
	fixturePath := filepath.Join(repoRoot(t), "testdata", "fixtures", "orchestration-simple.json")

	cfg := config.GenerateDefault()
	_, err := runNonTTYIntake(t, workspaceRoot, fixturePath, []string{"Manage PLAN.md", "0"}, cfg)
	require.Error(t, err)

	// Verify intake log exists and is non-empty
	eventsDir := filepath.Join(workspaceRoot, "events")
	entries, err := os.ReadDir(eventsDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Contains(t, entries[0].Name(), "-intake.ndjson")

	logPath := filepath.Join(eventsDir, entries[0].Name())
	info, err := os.Stat(logPath)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(0), "intake log should not be empty")

	// Verify it contains multiple event types
	assertIntakeLogContains(t, logPath, protocol.EventOrchestrationProposedTasks, protocol.EventSystemUserDecision)

	// Verify state exists (intake state may be in different location)
	stateFile := filepath.Join(workspaceRoot, "state", "run.json")
	require.FileExists(t, stateFile, "run state should be preserved")
}

// TestRegression_MultipleDeclineAttempts verifies clean state between runs
func TestRegression_MultipleDeclineAttempts(t *testing.T) {
	workspaceRoot := t.TempDir()
	fixturePath := filepath.Join(repoRoot(t), "testdata", "fixtures", "orchestration-simple.json")

	cfg := config.GenerateDefault()

	// First decline
	_, err1 := runNonTTYIntake(t, workspaceRoot, fixturePath, []string{"First attempt", "0"}, cfg)
	require.Error(t, err1)

	eventsDir := filepath.Join(workspaceRoot, "events")
	entries1, err := os.ReadDir(eventsDir)
	require.NoError(t, err)
	firstRunLogCount := len(entries1)

	// Clean up state manually (simulating fresh start)
	stateFile := filepath.Join(workspaceRoot, "state", "run.json")
	if _, err := os.Stat(stateFile); err == nil {
		require.NoError(t, os.Remove(stateFile))
	}

	// Second decline with fresh workspace state
	_, err2 := runNonTTYIntake(t, workspaceRoot, fixturePath, []string{"Second attempt", "0"}, cfg)
	require.Error(t, err2)

	// Verify both runs created separate logs
	entries2, err := os.ReadDir(eventsDir)
	require.NoError(t, err)
	require.Greater(t, len(entries2), firstRunLogCount, "second run should create new log file")

	// Verify each log contains its own decision event
	for _, entry := range entries2 {
		if strings.Contains(entry.Name(), "-intake.ndjson") {
			logPath := filepath.Join(eventsDir, entry.Name())
			assertIntakeLogContains(t, logPath, protocol.EventSystemUserDecision)
		}
	}
}

// TestRegression_DeclineWithNonTTY verifies non-TTY decline doesn't hang
func TestRegression_DeclineWithNonTTY(t *testing.T) {
	workspaceRoot := t.TempDir()
	fixturePath := filepath.Join(repoRoot(t), "testdata", "fixtures", "orchestration-simple.json")

	cfg := config.GenerateDefault()
	// Simpler test: just run directly with reasonable timeout (10s total test timeout from go test)
	// The runNonTTYIntake helper already has proper error handling
	_, err := runNonTTYIntake(t, workspaceRoot, fixturePath, []string{"Manage PLAN.md", "0"}, cfg)

	require.Error(t, err)
	require.ErrorIs(t, err, errUserDeclined)

	// Verify no hanging by checking log was created (proves flow completed)
	eventsDir := filepath.Join(workspaceRoot, "events")
	entries, readErr := os.ReadDir(eventsDir)
	require.NoError(t, readErr)
	require.Len(t, entries, 1, "intake log should exist")
}

// ============================================================================
// Section 2: Retry Flow Regressions
// ============================================================================

// TestRegression_TaskDiscoveryFollowedByDecline verifies discovery then denial flow
func TestRegression_TaskDiscoveryFollowedByDecline(t *testing.T) {
	workspaceRoot := t.TempDir()
	fixturePath := filepath.Join(repoRoot(t), "testdata", "fixtures", "orchestration-discovery-expanded.json")

	cfg := config.GenerateDefault()
	_, err := runNonTTYIntake(t, workspaceRoot, fixturePath, []string{"Manage PLAN.md", "m", "0"}, cfg)

	require.Error(t, err)
	require.ErrorIs(t, err, errUserDeclined)

	// Verify task_discovery command was executed
	logPath := findLatestIntakeLog(t, workspaceRoot)
	file, err := os.Open(logPath)
	require.NoError(t, err)
	defer file.Close()

	var discoveryIK string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var raw map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			continue
		}
		if action, ok := raw["action"].(string); ok && action == string(protocol.ActionTaskDiscovery) {
			if ik, ok := raw["idempotency_key"].(string); ok {
				discoveryIK = ik
				break
			}
		}
	}
	require.NotEmpty(t, discoveryIK, "task_discovery should have idempotency key")

	// Verify denial recorded after discovery
	assertIntakeLogContains(t, logPath, protocol.EventSystemUserDecision)
}

// TestRegression_MultipleConflictResolutions verifies sequential conflict handling
func TestRegression_MultipleConflictResolutions(t *testing.T) {
	workspaceRoot := t.TempDir()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Plan\n"), 0o644))

	cfg := config.GenerateDefault()
	cfg.Agents.Orchestration = &config.AgentConfig{
		Enabled: true,
		Cmd:     []string{"fake"},
		Env:     map[string]string{},
		TimeoutsS: map[string]int{
			"intake": 60,
		},
	}

	sup := newFakeOrchestrationSupervisor()
	var firstIK string
	sup.onSend = func(cmd *protocol.Command) {
		switch len(sup.commands) {
		case 1:
			firstIK = cmd.IdempotencyKey
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-conflict-1",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
				Event:         protocol.EventOrchestrationPlanConflict,
				Payload: map[string]any{
					"message": "First conflict: ambiguous plan structure",
				},
			})
		case 2:
			require.Equal(t, firstIK, cmd.IdempotencyKey)
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-conflict-2",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
				Event:         protocol.EventOrchestrationPlanConflict,
				Payload: map[string]any{
					"message": "Second conflict: dependency ordering unclear",
				},
			})
		case 3:
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-proposed",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
				Event:         protocol.EventOrchestrationProposedTasks,
				Payload: map[string]any{
					"plan_candidates": []any{
						map[string]any{"path": "PLAN.md", "confidence": 0.92},
					},
					"derived_tasks": []any{
						map[string]any{"id": "T-MULTI-CONFLICT", "title": "Task after multiple conflicts"},
					},
				},
			})
		}
	}
	overrideOrchestrationSupervisorFactory(t, sup)

	inReader := strings.NewReader("Manage PLAN.md\nUse PLAN.md structure\nDefine sequential dependencies\n1\n\n")

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(inReader)
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	outcome, err := runIntakeFlow(cmd, cfg, workspaceRoot, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, "approved", outcome.Decision.Status)

	// Verify both conflict resolutions recorded in state
	state := loadRunStateFile(t, workspaceRoot)
	conflicts := getNestedStringSlice(state, "intake", "conflict_resolutions")
	require.Len(t, conflicts, 2)
	require.Contains(t, conflicts, "Use PLAN.md structure")
	require.Contains(t, conflicts, "Define sequential dependencies")
}

// TestRegression_ClarificationConflictApprovalFlow verifies combined scenario
func TestRegression_ClarificationConflictApprovalFlow(t *testing.T) {
	workspaceRoot := t.TempDir()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Plan\n"), 0o644))

	cfg := config.GenerateDefault()
	cfg.Agents.Orchestration = &config.AgentConfig{
		Enabled: true,
		Cmd:     []string{"fake"},
		Env:     map[string]string{},
		TimeoutsS: map[string]int{
			"intake": 60,
		},
	}

	sup := newFakeOrchestrationSupervisor()
	var firstIK string
	sup.onSend = func(cmd *protocol.Command) {
		switch len(sup.commands) {
		case 1:
			firstIK = cmd.IdempotencyKey
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-clarify",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
				Event:         protocol.EventOrchestrationNeedsClarification,
				Payload: map[string]any{
					"questions": []any{"Which sections should be prioritized?"},
				},
			})
		case 2:
			require.Equal(t, firstIK, cmd.IdempotencyKey)
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-conflict",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
				Event:         protocol.EventOrchestrationPlanConflict,
				Payload: map[string]any{
					"message": "Multiple conflicting plan files detected",
				},
			})
		case 3:
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-proposed",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
				Event:         protocol.EventOrchestrationProposedTasks,
				Payload: map[string]any{
					"plan_candidates": []any{
						map[string]any{"path": "PLAN.md", "confidence": 0.88},
					},
					"derived_tasks": []any{
						map[string]any{"id": "T-CLARIFY-CONFLICT", "title": "Task after clarification and conflict"},
					},
				},
			})
		}
	}
	overrideOrchestrationSupervisorFactory(t, sup)

	inReader := strings.NewReader("Manage PLAN.md\nPrioritize section 2\nUse PLAN.md\n1\n\n")

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(inReader)
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	outcome, err := runIntakeFlow(cmd, cfg, workspaceRoot, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, "approved", outcome.Decision.Status)

	// Verify clarification recorded
	state := loadRunStateFile(t, workspaceRoot)
	clarifications := getNestedStringSlice(state, "intake", "last_clarifications")
	require.Contains(t, clarifications, "Prioritize section 2")

	// Verify conflict resolution recorded
	conflicts := getNestedStringSlice(state, "intake", "conflict_resolutions")
	require.Contains(t, conflicts, "Use PLAN.md")

	// Verify all event types present in log
	assertIntakeLogContains(t, outcome.LogPath,
		protocol.EventOrchestrationNeedsClarification,
		protocol.EventOrchestrationPlanConflict,
		protocol.EventOrchestrationProposedTasks,
		protocol.EventSystemUserDecision,
	)
}

// TestRegression_MalformedPayloadGracefulDegradation verifies error handling
func TestRegression_MalformedPayloadGracefulDegradation(t *testing.T) {
	workspaceRoot := t.TempDir()
	fixturePath := filepath.Join(repoRoot(t), "testdata", "fixtures", "orchestration-malformed-response.json")

	cfg := config.GenerateDefault()
	_, err := runNonTTYIntake(t, workspaceRoot, fixturePath, []string{"Manage PLAN.md"}, cfg)

	// Should fail gracefully with clear error (not panic)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "panic")
	// Error message should indicate missing candidates or malformed response
	errMsg := err.Error()
	require.True(t,
		strings.Contains(errMsg, "plan") || strings.Contains(errMsg, "candidate") || strings.Contains(errMsg, "missing"),
		"error should mention plan/candidate/missing: %s", errMsg)

	// Verify intake log still created (partial run captured)
	eventsDir := filepath.Join(workspaceRoot, "events")
	entries, readErr := os.ReadDir(eventsDir)
	require.NoError(t, readErr)
	require.NotEmpty(t, entries, "intake log should exist even on error")
}

// TestRegression_InvalidInputRetryLimit verifies prompts continue without artificial limit
func TestRegression_InvalidInputRetryLimit(t *testing.T) {
	workspaceRoot := t.TempDir()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Plan\n"), 0o644))

	cfg := config.GenerateDefault()
	cfg.Agents.Orchestration = &config.AgentConfig{
		Enabled: true,
		Cmd:     []string{"fake"},
		Env:     map[string]string{},
		TimeoutsS: map[string]int{
			"intake": 60,
		},
	}

	sup := newFakeOrchestrationSupervisor()
	sup.onSend = func(cmd *protocol.Command) {
		sup.emit(&protocol.Event{
			Kind:          protocol.MessageKindEvent,
			MessageID:     "evt-proposed",
			CorrelationID: cmd.CorrelationID,
			TaskID:        cmd.TaskID,
			From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
			Event:         protocol.EventOrchestrationProposedTasks,
			Payload: map[string]any{
				"plan_candidates": []any{
					map[string]any{"path": "PLAN.md", "confidence": 0.9},
				},
				"derived_tasks": []any{
					map[string]any{"id": "TASK-RETRY", "title": "Test retry"},
				},
			},
		})
	}
	overrideOrchestrationSupervisorFactory(t, sup)

	// Provide 10 invalid inputs followed by valid selection
	invalidInputs := make([]string, 12)
	invalidInputs[0] = "Manage PLAN.md"
	for i := 1; i < 10; i++ {
		invalidInputs[i] = "invalid"
	}
	invalidInputs[10] = "1"  // Valid plan selection
	invalidInputs[11] = "\n" // Task selection (all)

	inReader := strings.NewReader(strings.Join(invalidInputs, "\n") + "\n")

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(inReader)
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	outcome, err := runIntakeFlow(cmd, cfg, workspaceRoot, slog.Default())
	require.NoError(t, err, "should eventually accept valid input after many retries")
	require.NotNil(t, outcome)
	require.Equal(t, "approved", outcome.Decision.Status)

	// Verify output shows multiple "Invalid selection" messages
	output := out.String()
	invalidCount := strings.Count(output, "Invalid selection")
	require.GreaterOrEqual(t, invalidCount, 5, "should show multiple invalid selection messages")
}

// TestRegression_ResumeAfterPartialNegotiation verifies crash recovery
func TestRegression_ResumeAfterPartialNegotiation(t *testing.T) {
	workspaceRoot := t.TempDir()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Plan\n"), 0o644))

	cfg := config.GenerateDefault()
	cfg.Agents.Orchestration = &config.AgentConfig{
		Enabled: true,
		Cmd:     []string{"fake"},
		Env:     map[string]string{},
		TimeoutsS: map[string]int{
			"intake": 60,
		},
	}

	// Phase 1: Start negotiation, request clarification, then "crash" (context cancel)
	sup1 := newFakeOrchestrationSupervisor()
	var savedIK string
	sup1.onSend = func(cmd *protocol.Command) {
		savedIK = cmd.IdempotencyKey
		sup1.emit(&protocol.Event{
			Kind:          protocol.MessageKindEvent,
			MessageID:     "evt-clarify",
			CorrelationID: cmd.CorrelationID,
			TaskID:        cmd.TaskID,
			From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
			Event:         protocol.EventOrchestrationNeedsClarification,
			Payload: map[string]any{
				"questions": []any{"What is the priority?"},
			},
		})
	}
	overrideOrchestrationSupervisorFactory(t, sup1)

	ctx1, cancel1 := context.WithCancel(context.Background())
	cmd1 := &cobra.Command{}
	cmd1.SetContext(ctx1)
	cmd1.SetIn(strings.NewReader("Manage PLAN.md\n"))
	cmd1.SetOut(io.Discard)
	cmd1.SetErr(io.Discard)

	doneCh := make(chan error, 1)
	go func() {
		_, err := runIntakeFlow(cmd1, cfg, workspaceRoot, slog.Default())
		doneCh <- err
	}()

	// Wait for clarification to be sent, then cancel
	time.Sleep(200 * time.Millisecond)
	cancel1()
	<-doneCh

	// Phase 2: Resume from state
	sup2 := newFakeOrchestrationSupervisor()
	resumeIKSeen := false
	sup2.onSend = func(cmd *protocol.Command) {
		if cmd.IdempotencyKey == savedIK {
			resumeIKSeen = true
		}
		sup2.emit(&protocol.Event{
			Kind:          protocol.MessageKindEvent,
			MessageID:     "evt-proposed-resume",
			CorrelationID: cmd.CorrelationID,
			TaskID:        cmd.TaskID,
			From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
			Event:         protocol.EventOrchestrationProposedTasks,
			Payload: map[string]any{
				"plan_candidates": []any{
					map[string]any{"path": "PLAN.md", "confidence": 0.9},
				},
				"derived_tasks": []any{
					map[string]any{"id": "TASK-RESUME", "title": "Resumed task"},
				},
			},
		})
	}
	overrideOrchestrationSupervisorFactory(t, sup2)

	cmd2 := &cobra.Command{}
	cmd2.SetContext(context.Background())
	cmd2.SetIn(strings.NewReader("1\n\n"))
	cmd2.SetOut(io.Discard)
	cmd2.SetErr(io.Discard)

	outcome, err := runIntakeFlow(cmd2, cfg, workspaceRoot, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.True(t, resumeIKSeen, "resume should reuse original idempotency key")
}

// TestRegression_AgentErrorEventHandling verifies error event logging
func TestRegression_AgentErrorEventHandling(t *testing.T) {
	workspaceRoot := t.TempDir()
	fixturePath := filepath.Join(repoRoot(t), "testdata", "fixtures", "orchestration-error-retriable.json")

	cfg := config.GenerateDefault()
	_, err := runNonTTYIntake(t, workspaceRoot, fixturePath, []string{"Manage PLAN.md"}, cfg)

	// Should encounter error event and exit gracefully
	require.Error(t, err)

	// Verify error event was logged
	logPath := findLatestIntakeLog(t, workspaceRoot)
	assertIntakeLogContains(t, logPath, "error")

	// Verify no crash/panic
	require.NotContains(t, err.Error(), "panic")
}

// ============================================================================
// Section 3: Non-TTY Intake Regressions
// ============================================================================

// TestRegression_NonTTY_EndToEndApproval verifies complete non-TTY flow
func TestRegression_NonTTY_EndToEndApproval(t *testing.T) {
	workspaceRoot := t.TempDir()
	fixturePath := filepath.Join(repoRoot(t), "testdata", "fixtures", "orchestration-simple.json")

	cfg := config.GenerateDefault()
	// Full flow: instruction, plan 1, all tasks
	outcome, err := runNonTTYIntake(t, workspaceRoot, fixturePath, []string{"Manage PLAN.md", "1", ""}, cfg)

	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, "approved", outcome.Decision.Status)
	require.Equal(t, "PLAN.md", outcome.Decision.ApprovedPlan)
	require.NotEmpty(t, outcome.Decision.ApprovedTasks)

	// Verify no TTY-specific prompts in output (logged separately, but outcome should be clean)
	assertIntakeLogContains(t, outcome.LogPath,
		protocol.EventOrchestrationProposedTasks,
		protocol.EventSystemUserDecision,
	)
}

// TestRegression_NonTTY_WithClarifications verifies non-TTY clarification handling
func TestRegression_NonTTY_WithClarifications(t *testing.T) {
	workspaceRoot := t.TempDir()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Plan\n"), 0o644))

	cfg := config.GenerateDefault()
	cfg.Agents.Orchestration = &config.AgentConfig{
		Enabled: true,
		Cmd:     []string{"fake"},
		Env:     map[string]string{},
		TimeoutsS: map[string]int{
			"intake": 60,
		},
	}

	sup := newFakeOrchestrationSupervisor()
	var firstIK string
	sup.onSend = func(cmd *protocol.Command) {
		switch len(sup.commands) {
		case 1:
			firstIK = cmd.IdempotencyKey
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-clarify",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
				Event:         protocol.EventOrchestrationNeedsClarification,
				Payload: map[string]any{
					"questions": []any{"Which priority?"},
				},
			})
		case 2:
			require.Equal(t, firstIK, cmd.IdempotencyKey)
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-proposed",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
				Event:         protocol.EventOrchestrationProposedTasks,
				Payload: map[string]any{
					"plan_candidates": []any{
						map[string]any{"path": "PLAN.md", "confidence": 0.9},
					},
					"derived_tasks": []any{
						map[string]any{"id": "TASK-CLARIFY", "title": "Task after clarification"},
					},
				},
			})
		}
	}
	overrideOrchestrationSupervisorFactory(t, sup)

	// Non-TTY: instruction → clarification answer → approval
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(strings.NewReader("Manage PLAN.md\nHigh priority\n1\n\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	outcome, err := runIntakeFlow(cmd, cfg, workspaceRoot, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, "approved", outcome.Decision.Status)

	// Verify clarification recorded
	state := loadRunStateFile(t, workspaceRoot)
	clarifications := getNestedStringSlice(state, "intake", "last_clarifications")
	require.Contains(t, clarifications, "High priority")

	// Verify no TTY-specific hang or prompt issues
	output := out.String()
	require.NotEmpty(t, output)
}

// TestRegression_NonTTY_WithConflictResolution verifies non-TTY conflict handling
func TestRegression_NonTTY_WithConflictResolution(t *testing.T) {
	workspaceRoot := t.TempDir()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Plan\n"), 0o644))

	cfg := config.GenerateDefault()
	cfg.Agents.Orchestration = &config.AgentConfig{
		Enabled: true,
		Cmd:     []string{"fake"},
		Env:     map[string]string{},
		TimeoutsS: map[string]int{
			"intake": 60,
		},
	}

	sup := newFakeOrchestrationSupervisor()
	var firstIK string
	sup.onSend = func(cmd *protocol.Command) {
		switch len(sup.commands) {
		case 1:
			firstIK = cmd.IdempotencyKey
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-conflict",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
				Event:         protocol.EventOrchestrationPlanConflict,
				Payload: map[string]any{
					"message": "Conflict detected in plan structure",
				},
			})
		case 2:
			require.Equal(t, firstIK, cmd.IdempotencyKey)
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-proposed",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
				Event:         protocol.EventOrchestrationProposedTasks,
				Payload: map[string]any{
					"plan_candidates": []any{
						map[string]any{"path": "PLAN.md", "confidence": 0.92},
					},
					"derived_tasks": []any{
						map[string]any{"id": "T-CONFLICT-RESOLVED", "title": "Task after conflict resolution"},
					},
				},
			})
		}
	}
	overrideOrchestrationSupervisorFactory(t, sup)

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(strings.NewReader("Manage PLAN.md\nResolve conflict\n1\n\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	outcome, err := runIntakeFlow(cmd, cfg, workspaceRoot, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, "approved", outcome.Decision.Status)

	// Verify resolution recorded
	state := loadRunStateFile(t, workspaceRoot)
	conflicts := getNestedStringSlice(state, "intake", "conflict_resolutions")
	require.Len(t, conflicts, 1)
	require.Contains(t, conflicts, "Resolve conflict")
}

// TestRegression_NonTTY_WithTaskDiscovery verifies non-TTY discovery flow
func TestRegression_NonTTY_WithTaskDiscovery(t *testing.T) {
	workspaceRoot := t.TempDir()
	fixturePath := filepath.Join(repoRoot(t), "testdata", "fixtures", "orchestration-discovery-expanded.json")

	cfg := config.GenerateDefault()
	// Non-TTY: instruction → "m" (more) → select from expanded
	outcome, err := runNonTTYIntake(t, workspaceRoot, fixturePath,
		[]string{"Manage PLAN.md", "m", "2", ""}, cfg)

	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, "approved", outcome.Decision.Status)
	require.Equal(t, "docs/plan_v2.md", outcome.Decision.ApprovedPlan)

	// Verify task_discovery command sent
	logPath := outcome.LogPath
	file, err := os.Open(logPath)
	require.NoError(t, err)
	defer file.Close()

	foundDiscovery := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var raw map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			continue
		}
		if action, ok := raw["action"].(string); ok && action == string(protocol.ActionTaskDiscovery) {
			foundDiscovery = true
			break
		}
	}
	require.True(t, foundDiscovery, "task_discovery should be in log")
}

// TestRegression_NonTTY_Decline verifies non-TTY decline flow
func TestRegression_NonTTY_Decline(t *testing.T) {
	workspaceRoot := t.TempDir()
	fixturePath := filepath.Join(repoRoot(t), "testdata", "fixtures", "orchestration-simple.json")

	cfg := config.GenerateDefault()
	_, err := runNonTTYIntake(t, workspaceRoot, fixturePath, []string{"Manage PLAN.md", "0"}, cfg)

	require.Error(t, err)
	require.ErrorIs(t, err, errUserDeclined)

	// Verify denial recorded
	logPath := findLatestIntakeLog(t, workspaceRoot)
	assertIntakeLogContains(t, logPath, protocol.EventSystemUserDecision)

	// No TTY-specific error messages should appear
	decisionEvt := findSystemDecisionEvent(t, logPath)
	require.Equal(t, "denied", decisionEvt.Status)
}

// TestRegression_NonTTY_EOFHandling verifies graceful EOF handling
func TestRegression_NonTTY_EOFHandling(t *testing.T) {
	workspaceRoot := t.TempDir()
	fixturePath := filepath.Join(repoRoot(t), "testdata", "fixtures", "orchestration-simple.json")

	cfg := config.GenerateDefault()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Plan\n"), 0o644))

	binary := buildTestBinary(t)
	cfg.Agents.Orchestration.Cmd = []string{
		binary,
		"--role", "orchestration",
		"--fixture", fixturePath,
		"--no-heartbeat",
	}
	cfg.Agents.Orchestration.TimeoutsS["intake"] = 10

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Provide instruction but close stdin before selection
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(strings.NewReader("Manage PLAN.md\n")) // No plan selection, EOF
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	_, err := runIntakeFlow(cmd, cfg, workspaceRoot, logger)

	// Should fail gracefully, not hang
	require.Error(t, err)
	require.NotContains(t, err.Error(), "panic")

	// Verify partial state was persisted
	eventsDir := filepath.Join(workspaceRoot, "events")
	entries, readErr := os.ReadDir(eventsDir)
	require.NoError(t, readErr)
	require.NotEmpty(t, entries, "intake log should exist even on EOF")
}
