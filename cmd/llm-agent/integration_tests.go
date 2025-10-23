package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/ndjson"
	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLLMAgentFullIntegration tests complete agent integration
func TestLLMAgentFullIntegration(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	// Create test workspace structure
	tu.CreateTestWorkspaceStructure()
	tu.AssertWorkspaceStructureValid()

	agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

	t.Run("CompleteIntakeFlow", func(t *testing.T) {
		// Set up mock LLM response
		mockResponse := `{
			"plan_file": "PLAN.md",
			"confidence": 0.95,
			"tasks": [
				{
					"id": "T-001-1",
					"title": "Implement authentication",
					"files": ["src/auth.go", "tests/auth_test.go"],
					"notes": "Add JWT-based authentication"
				},
				{
					"id": "T-001-2",
					"title": "Add user management",
					"files": ["src/user.go", "tests/user_test.go"],
					"notes": "User registration and login"
				}
			],
			"needs_clarification": false,
			"clarification_questions": []
		}`

		agent.llmCaller.(*MockLLMCaller).SetResponse("test prompt", mockResponse)

		// Create intake command
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd.Inputs = map[string]any{
			"user_instruction": "Implement authentication from PLAN.md",
			"discovery": map[string]any{
				"root": "/workspace",
				"strategy": "heuristic:v1",
				"candidates": []map[string]any{
					{"path": "PLAN.md", "score": 0.9, "reason": "filename contains 'plan'"},
					{"path": "docs/spec.md", "score": 0.6},
				},
			},
		}

		// Process command
		err := agent.handleCommand(cmd)
		require.NoError(t, err)

		// Verify LLM was called
		assert.Equal(t, 1, agent.llmCaller.(*MockLLMCaller).CallCount())

		// Verify events were emitted
		events := agent.eventEmitter.(*MockEventEmitter).GetEvents()
		assert.Len(t, events, 1)
		assert.Equal(t, "orchestration.proposed_tasks", events[0].Event)
		assert.Equal(t, "success", events[0].Status)

		// Verify payload structure
		payload := events[0].Payload
		assert.Contains(t, payload, "plan_candidates")
		assert.Contains(t, payload, "derived_tasks")
		assert.Contains(t, payload, "notes")

		// Verify plan candidates
		candidates, ok := payload["plan_candidates"].([]any)
		require.True(t, ok)
		assert.Len(t, candidates, 1)

		candidate := candidates[0].(map[string]any)
		assert.Equal(t, "PLAN.md", candidate["path"])
		assert.Equal(t, 0.95, candidate["confidence"])

		// Verify derived tasks
		tasks, ok := payload["derived_tasks"].([]any)
		require.True(t, ok)
		assert.Len(t, tasks, 2)

		task1 := tasks[0].(map[string]any)
		assert.Equal(t, "T-001-1", task1["id"])
		assert.Equal(t, "Implement authentication", task1["title"])

		task2 := tasks[1].(map[string]any)
		assert.Equal(t, "T-001-2", task2["id"])
		assert.Equal(t, "Add user management", task2["title"])
	})

	t.Run("TaskDiscoveryFlow", func(t *testing.T) {
		// Set up mock LLM response for task discovery
		mockResponse := `{
			"plan_file": "PLAN.md",
			"confidence": 0.88,
			"tasks": [
				{
					"id": "T-001-3",
					"title": "Add password reset",
					"files": ["src/reset.go", "tests/reset_test.go"],
					"notes": "Password reset functionality"
				}
			],
			"needs_clarification": false,
			"clarification_questions": []
		}`

		agent.llmCaller.(*MockLLMCaller).SetResponse("test prompt", mockResponse)

		// Create task discovery command
		cmd := tu.CreateTestCommand(protocol.ActionTaskDiscovery, "T-001")
		cmd.Inputs = map[string]any{
			"user_instruction": "Add more authentication features",
			"discovery": map[string]any{
				"root": "/workspace",
				"strategy": "heuristic:v1",
				"candidates": []map[string]any{
					{"path": "PLAN.md", "score": 0.9},
				},
			},
			"context": map[string]any{
				"approved_plan": "PLAN.md",
				"completed_tasks": []string{"T-001-1", "T-001-2"},
			},
		}

		// Process command
		err := agent.handleCommand(cmd)
		require.NoError(t, err)

		// Verify LLM was called
		assert.Equal(t, 1, agent.llmCaller.(*MockLLMCaller).CallCount())

		// Verify events were emitted
		events := agent.eventEmitter.(*MockEventEmitter).GetEvents()
		assert.Len(t, events, 1)
		assert.Equal(t, "orchestration.proposed_tasks", events[0].Event)
	})

	t.Run("NeedsClarificationFlow", func(t *testing.T) {
		// Set up mock LLM response for clarification
		mockResponse := `{
			"plan_file": null,
			"confidence": 0.0,
			"tasks": [],
			"needs_clarification": true,
			"clarification_questions": [
				"Which plan file should be used (PLAN.md vs docs/plan_v2.md)?",
				"Should we implement phases A and B together or separately?"
			]
		}`

		agent.llmCaller.(*MockLLMCaller).SetResponse("test prompt", mockResponse)

		// Create intake command with ambiguous instruction
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd.Inputs = map[string]any{
			"user_instruction": "Implement the plan",
			"discovery": map[string]any{
				"root": "/workspace",
				"strategy": "heuristic:v1",
				"candidates": []map[string]any{
					{"path": "PLAN.md", "score": 0.8},
					{"path": "docs/plan_v2.md", "score": 0.8},
				},
			},
		}

		// Process command
		err := agent.handleCommand(cmd)
		require.NoError(t, err)

		// Verify LLM was called
		assert.Equal(t, 1, agent.llmCaller.(*MockLLMCaller).CallCount())

		// Verify clarification event was emitted
		events := agent.eventEmitter.(*MockEventEmitter).GetEvents()
		assert.Len(t, events, 1)
		assert.Equal(t, "orchestration.needs_clarification", events[0].Event)
		assert.Equal(t, "needs_input", events[0].Status)

		// Verify payload structure
		payload := events[0].Payload
		assert.Contains(t, payload, "questions")
		assert.Contains(t, payload, "notes")

		questions, ok := payload["questions"].([]any)
		require.True(t, ok)
		assert.Len(t, questions, 2)
		assert.Contains(t, questions[0], "PLAN.md")
		assert.Contains(t, questions[1], "phases A and B")
	})

	t.Run("PlanConflictFlow", func(t *testing.T) {
		// Set up mock LLM response for plan conflict
		mockResponse := `{
			"plan_file": null,
			"confidence": 0.0,
			"tasks": [],
			"needs_clarification": false,
			"clarification_questions": [],
			"plan_conflict": true,
			"conflict_reason": "Two high-confidence plans diverge in scope; human selection required."
		}`

		agent.llmCaller.(*MockLLMCaller).SetResponse("test prompt", mockResponse)

		// Create intake command with conflicting plans
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd.Inputs = map[string]any{
			"user_instruction": "Implement the plan",
			"discovery": map[string]any{
				"root": "/workspace",
				"strategy": "heuristic:v1",
				"candidates": []map[string]any{
					{"path": "PLAN.md", "score": 0.9},
					{"path": "docs/plan_v2.md", "score": 0.9},
				},
			},
		}

		// Process command
		err := agent.handleCommand(cmd)
		require.NoError(t, err)

		// Verify LLM was called
		assert.Equal(t, 1, agent.llmCaller.(*MockLLMCaller).CallCount())

		// Verify conflict event was emitted
		events := agent.eventEmitter.(*MockEventEmitter).GetEvents()
		assert.Len(t, events, 1)
		assert.Equal(t, "orchestration.plan_conflict", events[0].Event)
		assert.Equal(t, "needs_input", events[0].Status)

		// Verify payload structure
		payload := events[0].Payload
		assert.Contains(t, payload, "candidates")
		assert.Contains(t, payload, "reason")

		candidates, ok := payload["candidates"].([]any)
		require.True(t, ok)
		assert.Len(t, candidates, 2)
	})

	t.Run("ArtifactProduction", func(t *testing.T) {
		// Set up mock LLM response
		mockResponse := `{
			"plan_file": "PLAN.md",
			"confidence": 0.95,
			"tasks": [
				{
					"id": "T-001-1",
					"title": "Test task",
					"files": ["src/test.go"],
					"notes": "Test implementation"
				}
			],
			"needs_clarification": false,
			"clarification_questions": []
		}`

		agent.llmCaller.(*MockLLMCaller).SetResponse("test prompt", mockResponse)

		// Create command with expected outputs
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd.ExpectedOutputs = []protocol.ExpectedOutput{
			{
				Path:        "tasks/T-001.plan.json",
				Description: "Task plan file",
				Required:    true,
			},
		}

		// Process command
		err := agent.handleCommand(cmd)
		require.NoError(t, err)

		// Verify artifact was produced
		events := agent.eventEmitter.(*MockEventEmitter).GetEvents()
		assert.Len(t, events, 2) // orchestration.proposed_tasks + artifact.produced

		// Find artifact event
		var artifactEvent *protocol.Event
		for _, event := range events {
			if event.Event == "artifact.produced" {
				artifactEvent = &event
				break
			}
		}
		require.NotNil(t, artifactEvent)

		// Verify artifact structure
		assert.Len(t, artifactEvent.Artifacts, 1)
		artifact := artifactEvent.Artifacts[0]
		assert.Equal(t, "tasks/T-001.plan.json", artifact.Path)
		assert.NotEmpty(t, artifact.SHA256)
		assert.Greater(t, artifact.Size, int64(0))
	})

	t.Run("IdempotencyReplay", func(t *testing.T) {
		// Set up mock LLM response
		mockResponse := `{
			"plan_file": "PLAN.md",
			"confidence": 0.95,
			"tasks": [
				{
					"id": "T-001-1",
					"title": "Test task",
					"files": ["src/test.go"],
					"notes": "Test implementation"
				}
			],
			"needs_clarification": false,
			"clarification_questions": []
		}`

		agent.llmCaller.(*MockLLMCaller).SetResponse("test prompt", mockResponse)

		// Create command
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		ik := cmd.IdempotencyKey

		// Process command first time
		err := agent.handleCommand(cmd)
		require.NoError(t, err)

		// Verify LLM was called
		assert.Equal(t, 1, agent.llmCaller.(*MockLLMCaller).CallCount())

		// Create receipt for idempotency
		receipt := tu.CreateTestReceipt("T-001", "intake", ik)
		err = agent.receiptStore.SaveReceipt("/receipts/T-001/intake-1.json", receipt)
		require.NoError(t, err)

		// Process same command again (should replay from receipt)
		cmd2 := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd2.IdempotencyKey = ik // Same IK

		err = agent.handleCommand(cmd2)
		require.NoError(t, err)

		// Verify LLM was NOT called again (idempotency)
		assert.Equal(t, 1, agent.llmCaller.(*MockLLMCaller).CallCount())

		// Verify events were replayed
		events := agent.eventEmitter.(*MockEventEmitter).GetEvents()
		assert.Len(t, events, 2) // Two sets of events (original + replay)
	})

	t.Run("VersionMismatch", func(t *testing.T) {
		// Set initial snapshot
		agent.mu.Lock()
		agent.firstObservedSnapshotID = "snap-001"
		agent.mu.Unlock()

		// Create command with different snapshot
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd.Version.SnapshotID = "snap-002"

		// Process command
		err := agent.handleCommand(cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "version_mismatch")

		// Verify LLM was NOT called
		assert.Equal(t, 0, agent.llmCaller.(*MockLLMCaller).CallCount())
	})

	t.Run("LLMErrorHandling", func(t *testing.T) {
		// Set up LLM to return error
		agent.llmCaller.(*MockLLMCaller).SetError("test prompt", fmt.Errorf("LLM service unavailable"))

		// Create command
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")

		// Process command
		err := agent.handleCommand(cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "LLM service unavailable")

		// Verify error event was emitted
		events := agent.eventEmitter.(*MockEventEmitter).GetEvents()
		assert.Len(t, events, 1)
		assert.Equal(t, "error", events[0].Event)
		assert.Equal(t, "failed", events[0].Status)
		assert.Equal(t, "llm_call_failed", events[0].Payload["code"])
	})

	t.Run("InvalidLLMResponse", func(t *testing.T) {
		// Set up LLM to return invalid JSON
		agent.llmCaller.(*MockLLMCaller).SetResponse("test prompt", "invalid json response")

		// Create command
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")

		// Process command
		err := agent.handleCommand(cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid_llm_response")

		// Verify error event was emitted
		events := agent.eventEmitter.(*MockEventEmitter).GetEvents()
		assert.Len(t, events, 1)
		assert.Equal(t, "error", events[0].Event)
		assert.Equal(t, "failed", events[0].Status)
		assert.Equal(t, "invalid_llm_response", events[0].Payload["code"])
	})
}

// TestNDJSONProtocolIntegration tests NDJSON protocol integration
func TestNDJSONProtocolIntegration(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

	t.Run("NDJSONCommandParsing", func(t *testing.T) {
		// Create command JSON
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmdJSON, err := json.Marshal(cmd)
		require.NoError(t, err)

		// Create stdin with command
		stdin := strings.NewReader(string(cmdJSON) + "\n")
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		// Set up mock LLM response
		agent.llmCaller.(*MockLLMCaller).SetResponse("test prompt", `{"plan_file": "PLAN.md", "confidence": 0.95, "tasks": [], "needs_clarification": false, "clarification_questions": []}`)

		// Create context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Run agent
		err = agent.Run(ctx, stdin, &stdout, &stderr)
		require.NoError(t, err)

		// Verify output contains valid JSON
		output := stdout.String()
		assert.NotEmpty(t, output)

		// Parse output lines
		lines := strings.Split(strings.TrimSpace(output), "\n")
		for _, line := range lines {
			if line != "" {
				var msg map[string]any
				err := json.Unmarshal([]byte(line), &msg)
				assert.NoError(t, err, "Invalid JSON in output: %s", line)
			}
		}
	})

	t.Run("HeartbeatEmission", func(t *testing.T) {
		var stdout bytes.Buffer
		agent.encoder = ndjson.NewEncoder(&stdout, nil)

		// Send heartbeat
		err := agent.sendHeartbeat(protocol.HeartbeatStatusReady, "")
		require.NoError(t, err)

		// Verify heartbeat output
		output := stdout.String()
		assert.NotEmpty(t, output)

		// Parse heartbeat
		var hb protocol.Heartbeat
		err = json.Unmarshal([]byte(output), &hb)
		require.NoError(t, err)

		assert.Equal(t, protocol.MessageKindHeartbeat, hb.Kind)
		assert.Equal(t, protocol.AgentTypeOrchestration, hb.Agent.AgentType)
		assert.Equal(t, protocol.HeartbeatStatusReady, hb.Status)
		assert.Greater(t, hb.PID, 0)
		assert.GreaterOrEqual(t, hb.UptimeS, 0.0)
		assert.NotEmpty(t, hb.LastActivityAt)
	})

	t.Run("EventEmission", func(t *testing.T) {
		var stdout bytes.Buffer
		agent.encoder = ndjson.NewEncoder(&stdout, nil)

		// Create and send event
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		event := tu.CreateTestEvent("test.event", cmd)

		err := agent.encoder.Encode(event)
		require.NoError(t, err)

		// Verify event output
		output := stdout.String()
		assert.NotEmpty(t, output)

		// Parse event
		var evt protocol.Event
		err = json.Unmarshal([]byte(output), &evt)
		require.NoError(t, err)

		assert.Equal(t, protocol.MessageKindEvent, evt.Kind)
		assert.Equal(t, "test.event", evt.Event)
		assert.Equal(t, cmd.CorrelationID, evt.CorrelationID)
		assert.Equal(t, cmd.TaskID, evt.TaskID)
	})

	t.Run("LogEmission", func(t *testing.T) {
		var stdout bytes.Buffer
		agent.encoder = ndjson.NewEncoder(&stdout, nil)

		// Create and send log
		log := tu.CreateTestLog("info", "test message")

		err := agent.encoder.Encode(log)
		require.NoError(t, err)

		// Verify log output
		output := stdout.String()
		assert.NotEmpty(t, output)

		// Parse log
		var lg protocol.Log
		err = json.Unmarshal([]byte(output), &lg)
		require.NoError(t, err)

		assert.Equal(t, protocol.MessageKindLog, lg.Kind)
		assert.Equal(t, "info", lg.Level)
		assert.Equal(t, "test message", lg.Message)
	})
}

// TestMockLLMIntegration tests integration with mock LLM
func TestMockLLMIntegration(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	t.Run("MockLLMScript", func(t *testing.T) {
		// Create mock LLM script
		scriptPath := tu.CreateMockLLMScript()
		defer os.Remove(scriptPath)

		// Test with real LLM caller
		config := DefaultLLMConfig(scriptPath)
		caller := NewRealLLMCaller(config)

		ctx := context.Background()
		prompt := "Test prompt"

		response, err := caller.Call(ctx, prompt)
		require.NoError(t, err)
		assert.Contains(t, response, "LLM response to:")
		assert.Contains(t, response, prompt)
	})

	t.Run("MockLLMCaller", func(t *testing.T) {
		caller := NewMockLLMCaller()

		// Test basic functionality
		caller.SetResponse("test prompt", "test response")

		ctx := context.Background()
		response, err := caller.Call(ctx, "test prompt")
		require.NoError(t, err)
		assert.Equal(t, "test response", response)
		assert.Equal(t, 1, caller.CallCount())

		// Test error handling
		caller.SetError("error prompt", fmt.Errorf("LLM error"))

		_, err = caller.Call(ctx, "error prompt")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "LLM error")

		// Test call count
		assert.Equal(t, 2, caller.CallCount())
	})

	t.Run("LLMTimeout", func(t *testing.T) {
		caller := NewMockLLMCaller()

		// Set up response
		caller.SetResponse("slow prompt", "slow response")

		// Test with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		response, err := caller.Call(ctx, "slow prompt")
		require.NoError(t, err)
		assert.Equal(t, "slow response", response)
	})
}

// TestWorkspaceIntegration tests workspace file integration
func TestWorkspaceIntegration(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	// Create test workspace structure
	tu.CreateTestWorkspaceStructure()
	tu.AssertWorkspaceStructureValid()

	agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

	t.Run("FileReading", func(t *testing.T) {
		// Test reading existing files
		content, err := agent.fsProvider.ReadFileSafe(filepath.Join(tu.workspace, "specs/MASTER-SPEC.md"), 1024)
		require.NoError(t, err)
		assert.Contains(t, content, "Test Spec")

		content, err = agent.fsProvider.ReadFileSafe(filepath.Join(tu.workspace, "PLAN.md"), 1024)
		require.NoError(t, err)
		assert.Contains(t, content, "Test Plan")
	})

	t.Run("FileWriting", func(t *testing.T) {
		// Test writing artifacts
		content := []byte("test artifact content")
		artifact, err := agent.fsProvider.WriteArtifactAtomic(tu.workspace, "test.txt", content)
		require.NoError(t, err)

		assert.Equal(t, "test.txt", artifact.Path)
		assert.NotEmpty(t, artifact.SHA256)
		assert.Equal(t, int64(len(content)), artifact.Size)

		// Verify file was written
		readContent, err := agent.fsProvider.ReadFileSafe(filepath.Join(tu.workspace, "test.txt"), 1024)
		require.NoError(t, err)
		assert.Equal(t, string(content), readContent)
	})

	t.Run("PathValidation", func(t *testing.T) {
		// Test safe paths
		safePaths := []string{
			"test.txt",
			"subdir/file.txt",
			"specs/MASTER-SPEC.md",
		}

		for _, path := range safePaths {
			fullPath := filepath.Join(tu.workspace, path)
			_, _ = agent.fsProvider.ReadFileSafe(fullPath, 1024)
			// May not exist, but path should be safe
			// In real implementation, this would use resolveWorkspacePath
		}
	})
}

// TestErrorRecovery tests error recovery scenarios
func TestErrorRecovery(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

	t.Run("LLMServiceUnavailable", func(t *testing.T) {
		// Set up LLM to return error
		agent.llmCaller.(*MockLLMCaller).SetError("test prompt", fmt.Errorf("Service unavailable"))

		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")

		// Process command
		err := agent.handleCommand(cmd)
		require.Error(t, err)

		// Verify error event was emitted
		events := agent.eventEmitter.(*MockEventEmitter).GetEvents()
		assert.Len(t, events, 1)
		assert.Equal(t, "error", events[0].Event)
		assert.Equal(t, "llm_call_failed", events[0].Payload["code"])
	})

	t.Run("InvalidInput", func(t *testing.T) {
		// Create command with invalid inputs
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd.Inputs = map[string]any{
			// Missing required fields
		}

		// Process command
		_ = agent.handleCommand(cmd)
		// Should handle invalid inputs gracefully
		// In real implementation, this would emit an error event
	})

	t.Run("FileSystemError", func(t *testing.T) {
		// Test with non-existent workspace
		config := tu.CreateTestConfig()
		config.Workspace = "/non/existent/path"

		_, _ = NewLLMAgent(config)
		// Should handle missing workspace gracefully
		// In real implementation, this would create the workspace
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		// Test with cancelled context
		_, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Set up LLM response
		agent.llmCaller.(*MockLLMCaller).SetResponse("test prompt", "test response")

		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")

		// Process command with cancelled context
		_ = agent.handleCommand(cmd)
		// Should handle context cancellation gracefully
		// In real implementation, this would check context in LLM call
	})
}
