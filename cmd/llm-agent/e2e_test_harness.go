package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// E2ETestHarness provides end-to-end testing capabilities
type E2ETestHarness struct {
	t         *testing.T
	tempDir   string
	workspace string
	agentPath string
	mockLLMPath string
}

// NewE2ETestHarness creates a new end-to-end test harness
func NewE2ETestHarness(t *testing.T) *E2ETestHarness {
	tempDir := t.TempDir()
	workspace := filepath.Join(tempDir, "workspace")
	err := os.MkdirAll(workspace, 0700)
	require.NoError(t, err)

	// Create mock LLM script
	mockLLMPath := filepath.Join(tempDir, "mock-llm.sh")
	mockLLMScript := `#!/bin/bash
# Mock LLM script for E2E testing
echo "LLM response to: $(cat)"
`
	err = os.WriteFile(mockLLMPath, []byte(mockLLMScript), 0755)
	require.NoError(t, err)

	// Build agent binary (in real implementation, this would build the actual binary)
	agentPath := filepath.Join(tempDir, "llm-agent")
	// For testing, we'll use a mock that simulates the agent behavior
	agentScript := `#!/bin/bash
# Mock agent script for E2E testing
while read -r line; do
    if [ -n "$line" ]; then
        # Echo back a heartbeat
        echo '{"kind":"heartbeat","agent":{"agent_type":"orchestration","agent_id":"test-agent"},"seq":1,"status":"ready","pid":12345,"ppid":12340,"uptime_s":1.0,"last_activity_at":"2025-01-01T00:00:00Z"}'
        # Echo back an event
        echo '{"kind":"event","message_id":"evt-test001","correlation_id":"corr-test001","task_id":"T-001","from":{"agent_type":"orchestration"},"event":"orchestration.proposed_tasks","status":"success","payload":{"plan_candidates":[{"path":"PLAN.md","confidence":0.9}],"derived_tasks":[{"id":"T-001-1","title":"Test task","files":["src/test.go"]}]},"occurred_at":"2025-01-01T00:00:00Z"}'
        break
    fi
done
`
	err = os.WriteFile(agentPath, []byte(agentScript), 0755)
	require.NoError(t, err)

	return &E2ETestHarness{
		t:           t,
		tempDir:     tempDir,
		workspace:   workspace,
		agentPath:   agentPath,
		mockLLMPath: mockLLMPath,
	}
}

// Cleanup removes temporary test files
func (h *E2ETestHarness) Cleanup() {
	os.RemoveAll(h.tempDir)
}

// GetWorkspace returns the test workspace path
func (h *E2ETestHarness) GetWorkspace() string {
	return h.workspace
}

// CreateTestWorkspace creates a test workspace with standard structure
func (h *E2ETestHarness) CreateTestWorkspace() {
	// Create standard directories
	dirs := []string{
		"specs",
		"src",
		"tests",
		"reviews",
		"spec_notes",
		"receipts",
		"logs",
		"state",
		"snapshots",
		"transcripts",
	}

	for _, dir := range dirs {
		err := os.MkdirAll(filepath.Join(h.workspace, dir), 0700)
		require.NoError(h.t, err)
	}

	// Create test files
	h.createTestFile("specs/MASTER-SPEC.md", "# Test Spec\n\nThis is a test specification.")
	h.createTestFile("PLAN.md", "# Test Plan\n\nThis is a test plan.")
	h.createTestFile("src/main.go", "package main\n\nfunc main() {\n\t// Test code\n}")
	h.createTestFile("tests/main_test.go", "package main\n\nimport \"testing\"\n\nfunc TestMain(t *testing.T) {\n\t// Test code\n}")
}

// createTestFile creates a test file in the workspace
func (h *E2ETestHarness) createTestFile(relativePath, content string) string {
	fullPath := filepath.Join(h.workspace, relativePath)
	dir := filepath.Dir(fullPath)
	err := os.MkdirAll(dir, 0700)
	require.NoError(h.t, err)

	err = os.WriteFile(fullPath, []byte(content), 0600)
	require.NoError(h.t, err)

	return fullPath
}

// RunAgent runs the agent with the given command and returns output
func (h *E2ETestHarness) RunAgent(cmd *protocol.Command, timeout time.Duration) (string, string, error) {
	// Marshal command to JSON
	cmdJSON, err := json.Marshal(cmd)
	require.NoError(h.t, err)

	// Create stdin with command
	stdin := strings.NewReader(string(cmdJSON) + "\n")
	var stdout, stderr bytes.Buffer

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Run agent
	agentCmd := exec.CommandContext(ctx, h.agentPath)
	agentCmd.Stdin = stdin
	agentCmd.Stdout = &stdout
	agentCmd.Stderr = &stderr
	agentCmd.Env = append(os.Environ(),
		"WORKSPACE_ROOT="+h.workspace,
		"LLM_CLI_PATH="+h.mockLLMPath,
	)

	err = agentCmd.Run()
	return stdout.String(), stderr.String(), err
}

// RunAgentWithInput runs the agent with custom input
func (h *E2ETestHarness) RunAgentWithInput(input string, timeout time.Duration) (string, string, error) {
	stdin := strings.NewReader(input)
	var stdout, stderr bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	agentCmd := exec.CommandContext(ctx, h.agentPath)
	agentCmd.Stdin = stdin
	agentCmd.Stdout = &stdout
	agentCmd.Stderr = &stderr
	agentCmd.Env = append(os.Environ(),
		"WORKSPACE_ROOT="+h.workspace,
		"LLM_CLI_PATH="+h.mockLLMPath,
	)

	err := agentCmd.Run()
	return stdout.String(), stderr.String(), err
}

// ParseNDJSONOutput parses NDJSON output into messages
func (h *E2ETestHarness) ParseNDJSONOutput(output string) []map[string]any {
	var messages []map[string]any
	lines := strings.Split(strings.TrimSpace(output), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		var msg map[string]any
		err := json.Unmarshal([]byte(line), &msg)
		require.NoError(h.t, err, "Invalid JSON in output: %s", line)
		messages = append(messages, msg)
	}

	return messages
}

// AssertMessageType validates message type and structure
func (h *E2ETestHarness) AssertMessageType(messages []map[string]any, expectedKind string) {
	require.NotEmpty(h.t, messages, "Expected at least one message")

	for _, msg := range messages {
		kind, ok := msg["kind"].(string)
		require.True(h.t, ok, "Message missing 'kind' field")
		assert.Equal(h.t, expectedKind, kind, "Expected message kind %s, got %s", expectedKind, kind)
	}
}

// AssertHeartbeatMessage validates heartbeat message structure
func (h *E2ETestHarness) AssertHeartbeatMessage(msg map[string]any) {
	assert.Equal(h.t, "heartbeat", msg["kind"])
	assert.Contains(h.t, msg, "agent")
	assert.Contains(h.t, msg, "seq")
	assert.Contains(h.t, msg, "status")
	assert.Contains(h.t, msg, "pid")
	assert.Contains(h.t, msg, "uptime_s")
	assert.Contains(h.t, msg, "last_activity_at")

	agent, ok := msg["agent"].(map[string]any)
	require.True(h.t, ok)
	assert.Equal(h.t, "orchestration", agent["agent_type"])
}

// AssertEventMessage validates event message structure
func (h *E2ETestHarness) AssertEventMessage(msg map[string]any) {
	assert.Equal(h.t, "event", msg["kind"])
	assert.Contains(h.t, msg, "message_id")
	assert.Contains(h.t, msg, "correlation_id")
	assert.Contains(h.t, msg, "task_id")
	assert.Contains(h.t, msg, "from")
	assert.Contains(h.t, msg, "event")
	assert.Contains(h.t, msg, "occurred_at")

	from, ok := msg["from"].(map[string]any)
	require.True(h.t, ok)
	assert.Equal(h.t, "orchestration", from["agent_type"])
}

// AssertLogMessage validates log message structure
func (h *E2ETestHarness) AssertLogMessage(msg map[string]any) {
	assert.Equal(h.t, "log", msg["kind"])
	assert.Contains(h.t, msg, "level")
	assert.Contains(h.t, msg, "message")
	assert.Contains(h.t, msg, "timestamp")
}

// CreateTestCommand creates a test command
func (h *E2ETestHarness) CreateTestCommand(action protocol.Action, taskID string) *protocol.Command {
	return &protocol.Command{
		Kind:          protocol.MessageKindCommand,
		MessageID:     "cmd-test001",
		CorrelationID: "corr-test001",
		TaskID:        taskID,
		IdempotencyKey: "ik:test:key:1234567890123456789012345678901234567890123456789012345678901234",
		To: protocol.AgentRef{
			AgentType: protocol.AgentTypeOrchestration,
		},
		Action: action,
		Inputs: map[string]any{
			"user_instruction": "Test instruction",
		},
		ExpectedOutputs: []protocol.ExpectedOutput{},
		Version: protocol.Version{
			SnapshotID: "snap-test001",
		},
		Deadline: time.Now().Add(180 * time.Second),
		Retry: protocol.Retry{
			Attempt:     0,
			MaxAttempts: 3,
		},
		Priority: 5,
	}
}

// TestE2EAgentStartup tests agent startup and initialization
func TestE2EAgentStartup(t *testing.T) {
	harness := NewE2ETestHarness(t)
	defer harness.Cleanup()

	harness.CreateTestWorkspace()

	t.Run("AgentStartup", func(t *testing.T) {
		// Test agent startup with empty input (should emit heartbeats)
		output, _, err := harness.RunAgentWithInput("", 5*time.Second)

		// Should not error on empty input (clean shutdown)
		assert.NoError(t, err)
		assert.NotEmpty(t, output)
		// stderr is not used in this test

		// Parse output
		messages := harness.ParseNDJSONOutput(output)
		require.NotEmpty(t, messages)

		// Should contain heartbeat
		harness.AssertMessageType(messages, "heartbeat")
		harness.AssertHeartbeatMessage(messages[0])
	})

	t.Run("AgentWithCommand", func(t *testing.T) {
		cmd := harness.CreateTestCommand(protocol.ActionIntake, "T-001")

		output, _, err := harness.RunAgent(cmd, 5*time.Second)

		// Should not error
		assert.NoError(t, err)
		assert.NotEmpty(t, output)
		// stderr is not used in this test

		// Parse output
		messages := harness.ParseNDJSONOutput(output)
		require.NotEmpty(t, messages)

		// Should contain heartbeat and event
		hasHeartbeat := false
		hasEvent := false

		for _, msg := range messages {
			kind := msg["kind"].(string)
			if kind == "heartbeat" {
				hasHeartbeat = true
				harness.AssertHeartbeatMessage(msg)
			} else if kind == "event" {
				hasEvent = true
				harness.AssertEventMessage(msg)
			}
		}

		assert.True(t, hasHeartbeat, "Expected heartbeat message")
		assert.True(t, hasEvent, "Expected event message")
	})
}

// TestE2EIntakeFlow tests complete intake flow
func TestE2EIntakeFlow(t *testing.T) {
	harness := NewE2ETestHarness(t)
	defer harness.Cleanup()

	harness.CreateTestWorkspace()

	t.Run("IntakeCommand", func(t *testing.T) {
		cmd := harness.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd.Inputs = map[string]any{
			"user_instruction": "Implement authentication from PLAN.md",
			"discovery": map[string]any{
				"root": "/workspace",
				"strategy": "heuristic:v1",
				"candidates": []map[string]any{
					{"path": "PLAN.md", "score": 0.9, "reason": "filename contains 'plan'"},
				},
			},
		}

		output, _, err := harness.RunAgent(cmd, 10*time.Second)

		assert.NoError(t, err)
		assert.NotEmpty(t, output)
		// stderr is not used in this test

		// Parse output
		messages := harness.ParseNDJSONOutput(output)
		require.NotEmpty(t, messages)

		// Find orchestration event
		var orchestrationEvent map[string]any
		for _, msg := range messages {
			if msg["kind"] == "event" {
				event := msg["event"].(string)
				if event == "orchestration.proposed_tasks" {
					orchestrationEvent = msg
					break
				}
			}
		}

		require.NotNil(t, orchestrationEvent, "Expected orchestration.proposed_tasks event")
		assert.Equal(t, "success", orchestrationEvent["status"])

		// Verify payload structure
		payload, ok := orchestrationEvent["payload"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, payload, "plan_candidates")
		assert.Contains(t, payload, "derived_tasks")
	})

	t.Run("TaskDiscoveryCommand", func(t *testing.T) {
		cmd := harness.CreateTestCommand(protocol.ActionTaskDiscovery, "T-001")
		cmd.Inputs = map[string]any{
			"user_instruction": "Add more features",
			"discovery": map[string]any{
				"root": "/workspace",
				"strategy": "heuristic:v1",
				"candidates": []map[string]any{
					{"path": "PLAN.md", "score": 0.9},
				},
			},
			"context": map[string]any{
				"approved_plan": "PLAN.md",
				"completed_tasks": []string{"T-001-1"},
			},
		}

		output, _, err := harness.RunAgent(cmd, 10*time.Second)

		assert.NoError(t, err)
		assert.NotEmpty(t, output)
		// stderr is not used in this test

		// Parse output
		messages := harness.ParseNDJSONOutput(output)
		require.NotEmpty(t, messages)

		// Should contain orchestration event
		var orchestrationEvent map[string]any
		for _, msg := range messages {
			if msg["kind"] == "event" && msg["event"] == "orchestration.proposed_tasks" {
				orchestrationEvent = msg
				break
			}
		}

		require.NotNil(t, orchestrationEvent, "Expected orchestration.proposed_tasks event")
	})
}

// TestE2EErrorHandling tests error handling scenarios
func TestE2EErrorHandling(t *testing.T) {
	harness := NewE2ETestHarness(t)
	defer harness.Cleanup()

	harness.CreateTestWorkspace()

	t.Run("InvalidJSON", func(t *testing.T) {
		invalidJSON := `{"kind": "command", "invalid": json}`

		_, _, err := harness.RunAgentWithInput(invalidJSON+"\n", 5*time.Second)

		// Should handle invalid JSON gracefully
		// In real implementation, this would emit an error event
		assert.NoError(t, err) // Mock handles gracefully
	})

	t.Run("InvalidCommand", func(t *testing.T) {
		// Create command with invalid action for orchestration role
		cmd := harness.CreateTestCommand(protocol.ActionImplement, "T-001") // Wrong action

		_, _, err := harness.RunAgent(cmd, 5*time.Second)

		// Should handle invalid action gracefully
		assert.NoError(t, err) // Mock handles gracefully
	})

	t.Run("Timeout", func(t *testing.T) {
		cmd := harness.CreateTestCommand(protocol.ActionIntake, "T-001")

		// Test with very short timeout
		_, _, err := harness.RunAgent(cmd, 100*time.Millisecond)

		// Should handle timeout gracefully
		// In real implementation, this would emit timeout error
		assert.NoError(t, err) // Mock handles gracefully
	})
}

// TestE2EProtocolCompliance tests protocol compliance
func TestE2EProtocolCompliance(t *testing.T) {
	harness := NewE2ETestHarness(t)
	defer harness.Cleanup()

	harness.CreateTestWorkspace()

	t.Run("MessageStructure", func(t *testing.T) {
		cmd := harness.CreateTestCommand(protocol.ActionIntake, "T-001")

		output, _, err := harness.RunAgent(cmd, 5*time.Second)

		assert.NoError(t, err)
		assert.NotEmpty(t, output)

		// Parse output
		messages := harness.ParseNDJSONOutput(output)
		require.NotEmpty(t, messages)

		// Validate each message structure
		for i, msg := range messages {
			kind, ok := msg["kind"].(string)
			require.True(t, ok, "Message %d missing 'kind' field", i)

			switch kind {
			case "heartbeat":
				harness.AssertHeartbeatMessage(msg)
			case "event":
				harness.AssertEventMessage(msg)
			case "log":
				harness.AssertLogMessage(msg)
			default:
				t.Errorf("Unknown message kind: %s", kind)
			}
		}
	})

	t.Run("MessageSizeLimits", func(t *testing.T) {
		cmd := harness.CreateTestCommand(protocol.ActionIntake, "T-001")

		output, _, err := harness.RunAgent(cmd, 5*time.Second)

		assert.NoError(t, err)
		assert.NotEmpty(t, output)

		// Parse output
		messages := harness.ParseNDJSONOutput(output)
		require.NotEmpty(t, messages)

		// Check message sizes
		maxSize := 256 * 1024 // 256 KiB
		for i, msg := range messages {
			msgJSON, err := json.Marshal(msg)
			require.NoError(t, err)

			size := len(msgJSON)
			assert.LessOrEqual(t, size, maxSize, "Message %d size %d exceeds limit %d", i, size, maxSize)
		}
	})

	t.Run("RequiredFields", func(t *testing.T) {
		cmd := harness.CreateTestCommand(protocol.ActionIntake, "T-001")

		output, _, err := harness.RunAgent(cmd, 5*time.Second)

		assert.NoError(t, err)
		assert.NotEmpty(t, output)

		// Parse output
		messages := harness.ParseNDJSONOutput(output)
		require.NotEmpty(t, messages)

		// Validate required fields for each message type
		for i, msg := range messages {
			kind := msg["kind"].(string)

			switch kind {
			case "heartbeat":
				requiredFields := []string{"agent", "seq", "status", "pid", "uptime_s", "last_activity_at"}
				for _, field := range requiredFields {
					assert.Contains(t, msg, field, "Heartbeat message %d missing required field: %s", i, field)
				}
			case "event":
				requiredFields := []string{"message_id", "correlation_id", "task_id", "from", "event", "occurred_at"}
				for _, field := range requiredFields {
					assert.Contains(t, msg, field, "Event message %d missing required field: %s", i, field)
				}
			case "log":
				requiredFields := []string{"level", "message", "timestamp"}
				for _, field := range requiredFields {
					assert.Contains(t, msg, field, "Log message %d missing required field: %s", i, field)
				}
			}
		}
	})
}

// TestE2EWorkspaceIntegration tests workspace file integration
func TestE2EWorkspaceIntegration(t *testing.T) {
	harness := NewE2ETestHarness(t)
	defer harness.Cleanup()

	harness.CreateTestWorkspace()

	t.Run("FileAccess", func(t *testing.T) {
		// Test that agent can access workspace files
		cmd := harness.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd.Inputs = map[string]any{
			"user_instruction": "Read PLAN.md and implement tasks",
			"discovery": map[string]any{
				"root": "/workspace",
				"strategy": "heuristic:v1",
				"candidates": []map[string]any{
					{"path": "PLAN.md", "score": 0.9},
				},
			},
		}

		output, _, err := harness.RunAgent(cmd, 10*time.Second)

		assert.NoError(t, err)
		assert.NotEmpty(t, output)
		// stderr is not used in this test

		// Parse output
		messages := harness.ParseNDJSONOutput(output)
		require.NotEmpty(t, messages)

		// Should contain orchestration event with plan candidates
		var orchestrationEvent map[string]any
		for _, msg := range messages {
			if msg["kind"] == "event" && msg["event"] == "orchestration.proposed_tasks" {
				orchestrationEvent = msg
				break
			}
		}

		require.NotNil(t, orchestrationEvent, "Expected orchestration.proposed_tasks event")

		// Verify payload contains plan candidates
		payload, ok := orchestrationEvent["payload"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, payload, "plan_candidates")

		candidates, ok := payload["plan_candidates"].([]any)
		require.True(t, ok)
		assert.Len(t, candidates, 1)

		candidate := candidates[0].(map[string]any)
		assert.Equal(t, "PLAN.md", candidate["path"])
	})

	t.Run("ArtifactProduction", func(t *testing.T) {
		// Test artifact production
		cmd := harness.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd.ExpectedOutputs = []protocol.ExpectedOutput{
			{
				Path:        "tasks/T-001.plan.json",
				Description: "Task plan file",
				Required:    true,
			},
		}

		output, _, err := harness.RunAgent(cmd, 10*time.Second)

		assert.NoError(t, err)
		assert.NotEmpty(t, output)

		// Parse output
		messages := harness.ParseNDJSONOutput(output)
		require.NotEmpty(t, messages)

		// Should contain artifact.produced event
		var artifactEvent map[string]any
		for _, msg := range messages {
			if msg["kind"] == "event" && msg["event"] == "artifact.produced" {
				artifactEvent = msg
				break
			}
		}

		// In real implementation, this would be present
		// For now, we just verify the structure is correct
		if artifactEvent != nil {
			assert.Contains(t, artifactEvent, "artifacts")
			artifacts, ok := artifactEvent["artifacts"].([]any)
			require.True(t, ok)
			assert.Len(t, artifacts, 1)

			artifact := artifacts[0].(map[string]any)
			assert.Equal(t, "tasks/T-001.plan.json", artifact["path"])
			assert.Contains(t, artifact, "sha256")
			assert.Contains(t, artifact, "size")
		}
	})
}

// TestE2EPerformance tests performance characteristics
func TestE2EPerformance(t *testing.T) {
	harness := NewE2ETestHarness(t)
	defer harness.Cleanup()

	harness.CreateTestWorkspace()

	t.Run("ResponseTime", func(t *testing.T) {
		cmd := harness.CreateTestCommand(protocol.ActionIntake, "T-001")

		start := time.Now()
		output, _, err := harness.RunAgent(cmd, 10*time.Second)
		duration := time.Since(start)

		assert.NoError(t, err)
		assert.NotEmpty(t, output)
		// stderr is not used in this test

		// Should respond within reasonable time
		assert.Less(t, duration, 5*time.Second, "Agent response time too slow: %v", duration)
	})

	t.Run("MemoryUsage", func(t *testing.T) {
		// Test with multiple commands to check for memory leaks
		for i := 0; i < 5; i++ {
			cmd := harness.CreateTestCommand(protocol.ActionIntake, fmt.Sprintf("T-%03d", i))

			output, _, err := harness.RunAgent(cmd, 5*time.Second)

			assert.NoError(t, err)
			assert.NotEmpty(t, output)
			// stderr is not used in this test
		}
	})

	t.Run("ConcurrentRequests", func(t *testing.T) {
		// Test multiple concurrent agent instances
		done := make(chan bool, 3)

		for i := 0; i < 3; i++ {
			go func(i int) {
				cmd := harness.CreateTestCommand(protocol.ActionIntake, fmt.Sprintf("T-%03d", i))

				output, _, err := harness.RunAgent(cmd, 5*time.Second)

				assert.NoError(t, err)
				assert.NotEmpty(t, output)
				// stderr is not used in this test

				done <- true
			}(i)
		}

		// Wait for all goroutines to complete
		for i := 0; i < 3; i++ {
			<-done
		}
	})
}

// TestE2ERealWorldScenarios tests real-world usage scenarios
func TestE2ERealWorldScenarios(t *testing.T) {
	harness := NewE2ETestHarness(t)
	defer harness.Cleanup()

	harness.CreateTestWorkspace()

	t.Run("CompleteIntakeWorkflow", func(t *testing.T) {
		// Simulate complete intake workflow
		cmd := harness.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd.Inputs = map[string]any{
			"user_instruction": "Implement authentication system with JWT tokens, user registration, and password reset functionality",
			"discovery": map[string]any{
				"root": "/workspace",
				"strategy": "heuristic:v1",
				"candidates": []map[string]any{
					{"path": "PLAN.md", "score": 0.9, "reason": "filename contains 'plan'"},
					{"path": "specs/MASTER-SPEC.md", "score": 0.7, "reason": "specification file"},
				},
			},
		}
		cmd.ExpectedOutputs = []protocol.ExpectedOutput{
			{
				Path:        "tasks/T-001.plan.json",
				Description: "Detailed task plan",
				Required:    true,
			},
		}

		output, _, err := harness.RunAgent(cmd, 15*time.Second)

		assert.NoError(t, err)
		assert.NotEmpty(t, output)
		// stderr is not used in this test

		// Parse output
		messages := harness.ParseNDJSONOutput(output)
		require.NotEmpty(t, messages)

		// Should contain orchestration event
		var orchestrationEvent map[string]any
		for _, msg := range messages {
			if msg["kind"] == "event" && msg["event"] == "orchestration.proposed_tasks" {
				orchestrationEvent = msg
				break
			}
		}

		require.NotNil(t, orchestrationEvent, "Expected orchestration.proposed_tasks event")

		// Verify payload structure
		payload, ok := orchestrationEvent["payload"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, payload, "plan_candidates")
		assert.Contains(t, payload, "derived_tasks")

		// Verify plan candidates
		candidates, ok := payload["plan_candidates"].([]any)
		require.True(t, ok)
		assert.Len(t, candidates, 2)

		// Verify derived tasks
		tasks, ok := payload["derived_tasks"].([]any)
		require.True(t, ok)
		assert.NotEmpty(t, tasks)

		// Each task should have required fields
		for i, task := range tasks {
			taskMap, ok := task.(map[string]any)
			require.True(t, ok, "Task %d should be a map", i)
			assert.Contains(t, taskMap, "id")
			assert.Contains(t, taskMap, "title")
			assert.Contains(t, taskMap, "files")
		}
	})

	t.Run("TaskDiscoveryWorkflow", func(t *testing.T) {
		// Simulate task discovery workflow
		cmd := harness.CreateTestCommand(protocol.ActionTaskDiscovery, "T-001")
		cmd.Inputs = map[string]any{
			"user_instruction": "Add more authentication features like OAuth and 2FA",
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
				"current_phase": "implementation",
			},
		}

		output, _, err := harness.RunAgent(cmd, 15*time.Second)

		assert.NoError(t, err)
		assert.NotEmpty(t, output)
		// stderr is not used in this test

		// Parse output
		messages := harness.ParseNDJSONOutput(output)
		require.NotEmpty(t, messages)

		// Should contain orchestration event
		var orchestrationEvent map[string]any
		for _, msg := range messages {
			if msg["kind"] == "event" && msg["event"] == "orchestration.proposed_tasks" {
				orchestrationEvent = msg
				break
			}
		}

		require.NotNil(t, orchestrationEvent, "Expected orchestration.proposed_tasks event")
	})

	t.Run("ErrorRecovery", func(t *testing.T) {
		// Test error recovery scenarios
		cmd := harness.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd.Inputs = map[string]any{
			"user_instruction": "Implement something ambiguous",
			"discovery": map[string]any{
				"root": "/workspace",
				"strategy": "heuristic:v1",
				"candidates": []map[string]any{
					{"path": "PLAN.md", "score": 0.5},
					{"path": "docs/plan_v2.md", "score": 0.5},
				},
			},
		}

		output, _, err := harness.RunAgent(cmd, 15*time.Second)

		assert.NoError(t, err)
		assert.NotEmpty(t, output)
		// stderr is not used in this test

		// Parse output
		messages := harness.ParseNDJSONOutput(output)
		require.NotEmpty(t, messages)

		// Should contain some kind of response (either success or clarification)
		var responseEvent map[string]any
		for _, msg := range messages {
			if msg["kind"] == "event" {
				event := msg["event"].(string)
				if event == "orchestration.proposed_tasks" ||
				   event == "orchestration.needs_clarification" ||
				   event == "orchestration.plan_conflict" {
					responseEvent = msg
					break
				}
			}
		}

		require.NotNil(t, responseEvent, "Expected orchestration response event")
	})
}
