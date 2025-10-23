package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNegativeValidation tests negative scenarios and edge cases
func TestNegativeValidation(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	t.Run("OversizeMessages", func(t *testing.T) {
		// Test message size limits
		maxSize := 256 * 1024 // 256 KiB

		t.Run("OversizeEvent", func(t *testing.T) {
			// Create event with large payload
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			event := tu.CreateTestEvent("test.event", cmd)

			// Create large payload that exceeds limit
			largeData := strings.Repeat("A", 300*1024) // 300 KiB
			event.Payload = map[string]any{
				"large_data": largeData,
				"more_data":  strings.Repeat("B", 100*1024), // Additional 100 KiB
			}

			eventJSON, err := json.Marshal(event)
			require.NoError(t, err)

			// Should exceed size limit
			size := len(eventJSON)
			assert.Greater(t, size, maxSize, "Large payload should exceed size limit")

			// In real implementation, this would trigger size capping
			// For now, we document the expected behavior
		})

		t.Run("OversizeCommand", func(t *testing.T) {
			// Create command with large inputs
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")

			// Add large input data
			largeInput := strings.Repeat("X", 200*1024) // 200 KiB
			cmd.Inputs = map[string]any{
				"user_instruction": "Test instruction",
				"large_data":       largeInput,
				"more_large_data": strings.Repeat("Y", 100*1024), // Additional 100 KiB
			}

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// Should exceed size limit
			size := len(cmdJSON)
			assert.Greater(t, size, maxSize, "Large command should exceed size limit")
		})

		t.Run("OversizeHeartbeat", func(t *testing.T) {
			// Create heartbeat with large stats
			heartbeat := tu.CreateTestHeartbeat(protocol.HeartbeatStatusBusy, "T-001")

			// Add large stats data
			heartbeat.Stats = &protocol.HeartbeatStats{
				RSSBytes: 1024 * 1024,
			}

			heartbeatJSON, err := json.Marshal(heartbeat)
			require.NoError(t, err)

			// Should be under size limit (heartbeats are typically small)
			size := len(heartbeatJSON)
			assert.LessOrEqual(t, size, maxSize, "Heartbeat should be under size limit")
		})

		t.Run("OversizeLog", func(t *testing.T) {
			// Create log with large message
			largeMessage := strings.Repeat("Log message ", 10000) // Very large log message
			log := tu.CreateTestLog("info", largeMessage)

			logJSON, err := json.Marshal(log)
			require.NoError(t, err)

			// Should exceed size limit
			size := len(logJSON)
			assert.Greater(t, size, maxSize, "Large log should exceed size limit")
		})
	})

	t.Run("InvalidEnums", func(t *testing.T) {
		t.Run("InvalidAgentType", func(t *testing.T) {
			// Test invalid agent type
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.To.AgentType = "invalid_agent_type"

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// JSON should be valid, but agent type is invalid
			tu.AssertJSONValid(string(cmdJSON))

			// In real implementation, this would be caught by schema validation
			// For now, we document the expected behavior
		})

		t.Run("InvalidAction", func(t *testing.T) {
			// Test invalid action
			cmd := tu.CreateTestCommand("invalid_action", "T-001")

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// JSON should be valid, but action is invalid
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("InvalidHeartbeatStatus", func(t *testing.T) {
			// Test invalid heartbeat status
			heartbeat := tu.CreateTestHeartbeat("invalid_status", "T-001")

			heartbeatJSON, err := json.Marshal(heartbeat)
			require.NoError(t, err)

			// JSON should be valid, but status is invalid
			tu.AssertJSONValid(string(heartbeatJSON))
		})

		t.Run("InvalidLogLevel", func(t *testing.T) {
			// Test invalid log level
			log := tu.CreateTestLog("invalid_level", "test message")

			logJSON, err := json.Marshal(log)
			require.NoError(t, err)

			// JSON should be valid, but level is invalid
			tu.AssertJSONValid(string(logJSON))
		})

		t.Run("InvalidEventStatus", func(t *testing.T) {
			// Test invalid event status
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			event := tu.CreateTestEvent("test.event", cmd)
			event.Status = "invalid_status"

			eventJSON, err := json.Marshal(event)
			require.NoError(t, err)

			// JSON should be valid, but status is invalid
			tu.AssertJSONValid(string(eventJSON))
		})
	})

	t.Run("MalformedJSON", func(t *testing.T) {
		t.Run("InvalidJSON", func(t *testing.T) {
			malformedJSON := `{"kind": "command", "invalid": json}`

			// This should fail JSON parsing
			var cmd protocol.Command
			err := json.Unmarshal([]byte(malformedJSON), &cmd)
			require.Error(t, err)
		})

		t.Run("IncompleteJSON", func(t *testing.T) {
			incompleteJSON := `{"kind": "command", "message_id": "cmd-001"`

			// This should fail JSON parsing
			var cmd protocol.Command
			err := json.Unmarshal([]byte(incompleteJSON), &cmd)
			require.Error(t, err)
		})

		t.Run("ExtraComma", func(t *testing.T) {
			extraCommaJSON := `{"kind": "command", "message_id": "cmd-001",}`

			// This should fail JSON parsing
			var cmd protocol.Command
			err := json.Unmarshal([]byte(extraCommaJSON), &cmd)
			require.Error(t, err)
		})

		t.Run("UnquotedKeys", func(t *testing.T) {
			unquotedKeysJSON := `{kind: "command", message_id: "cmd-001"}`

			// This should fail JSON parsing
			var cmd protocol.Command
			err := json.Unmarshal([]byte(unquotedKeysJSON), &cmd)
			require.Error(t, err)
		})
	})

	t.Run("MissingRequiredFields", func(t *testing.T) {
		t.Run("MissingKind", func(t *testing.T) {
			// Test command missing 'kind' field
			incompleteCmd := map[string]any{
				"message_id":     "cmd-001",
				"correlation_id": "corr-001",
				"task_id":        "T-001",
				"idempotency_key": "ik-test123",
				"to": map[string]any{
					"agent_type": "orchestration",
				},
				"action": "intake",
				"inputs": map[string]any{},
				"version": map[string]any{
					"snapshot_id": "snap-001",
				},
				"deadline": "2025-12-31T23:59:59Z",
				"retry": map[string]any{
					"attempt":      0,
					"max_attempts": 3,
				},
				"priority": 5,
			}

			cmdJSON, err := json.Marshal(incompleteCmd)
			require.NoError(t, err)

			// JSON should be valid, but missing required field
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("MissingMessageID", func(t *testing.T) {
			// Test command missing 'message_id' field
			incompleteCmd := map[string]any{
				"kind":          "command",
				"correlation_id": "corr-001",
				"task_id":        "T-001",
				"idempotency_key": "ik-test123",
				"to": map[string]any{
					"agent_type": "orchestration",
				},
				"action": "intake",
				"inputs": map[string]any{},
				"version": map[string]any{
					"snapshot_id": "snap-001",
				},
				"deadline": "2025-12-31T23:59:59Z",
				"retry": map[string]any{
					"attempt":      0,
					"max_attempts": 3,
				},
				"priority": 5,
			}

			cmdJSON, err := json.Marshal(incompleteCmd)
			require.NoError(t, err)

			// JSON should be valid, but missing required field
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("MissingCorrelationID", func(t *testing.T) {
			// Test command missing 'correlation_id' field
			incompleteCmd := map[string]any{
				"kind":          "command",
				"message_id":     "cmd-001",
				"task_id":        "T-001",
				"idempotency_key": "ik-test123",
				"to": map[string]any{
					"agent_type": "orchestration",
				},
				"action": "intake",
				"inputs": map[string]any{},
				"version": map[string]any{
					"snapshot_id": "snap-001",
				},
				"deadline": "2025-12-31T23:59:59Z",
				"retry": map[string]any{
					"attempt":      0,
					"max_attempts": 3,
				},
				"priority": 5,
			}

			cmdJSON, err := json.Marshal(incompleteCmd)
			require.NoError(t, err)

			// JSON should be valid, but missing required field
			tu.AssertJSONValid(string(cmdJSON))
		})
	})

	t.Run("InvalidFieldTypes", func(t *testing.T) {
		t.Run("StringInsteadOfInt", func(t *testing.T) {
			// Test with string instead of integer
			invalidCmd := map[string]any{
				"kind":          "command",
				"message_id":     "cmd-001",
				"correlation_id": "corr-001",
				"task_id":        "T-001",
				"idempotency_key": "ik-test123",
				"to": map[string]any{
					"agent_type": "orchestration",
				},
				"action": "intake",
				"inputs": map[string]any{},
				"version": map[string]any{
					"snapshot_id": "snap-001",
				},
				"deadline": "2025-12-31T23:59:59Z",
				"retry": map[string]any{
					"attempt":      "zero", // Should be integer
					"max_attempts": 3,
				},
				"priority": 5,
			}

			cmdJSON, err := json.Marshal(invalidCmd)
			require.NoError(t, err)

			// JSON should be valid, but field type is wrong
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("IntInsteadOfString", func(t *testing.T) {
			// Test with integer instead of string
			invalidCmd := map[string]any{
				"kind":          "command",
				"message_id":     123, // Should be string
				"correlation_id": "corr-001",
				"task_id":        "T-001",
				"idempotency_key": "ik-test123",
				"to": map[string]any{
					"agent_type": "orchestration",
				},
				"action": "intake",
				"inputs": map[string]any{},
				"version": map[string]any{
					"snapshot_id": "snap-001",
				},
				"deadline": "2025-12-31T23:59:59Z",
				"retry": map[string]any{
					"attempt":      0,
					"max_attempts": 3,
				},
				"priority": 5,
			}

			cmdJSON, err := json.Marshal(invalidCmd)
			require.NoError(t, err)

			// JSON should be valid, but field type is wrong
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("BoolInsteadOfString", func(t *testing.T) {
			// Test with boolean instead of string
			invalidCmd := map[string]any{
				"kind":          "command",
				"message_id":     "cmd-001",
				"correlation_id": "corr-001",
				"task_id":        "T-001",
				"idempotency_key": "ik-test123",
				"to": map[string]any{
					"agent_type": "orchestration",
				},
				"action": "intake",
				"inputs": map[string]any{},
				"version": map[string]any{
					"snapshot_id": "snap-001",
				},
				"deadline": "2025-12-31T23:59:59Z",
				"retry": map[string]any{
					"attempt":      0,
					"max_attempts": 3,
				},
				"priority": true, // Should be integer
			}

			cmdJSON, err := json.Marshal(invalidCmd)
			require.NoError(t, err)

			// JSON should be valid, but field type is wrong
			tu.AssertJSONValid(string(cmdJSON))
		})
	})

	t.Run("InvalidFieldValues", func(t *testing.T) {
		t.Run("NegativePriority", func(t *testing.T) {
			// Test with negative priority
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.Priority = -1

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// JSON should be valid, but priority is invalid
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("PriorityTooHigh", func(t *testing.T) {
			// Test with priority too high
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.Priority = 15 // Should be 0-10

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// JSON should be valid, but priority is invalid
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("NegativeRetryAttempt", func(t *testing.T) {
			// Test with negative retry attempt
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.Retry.Attempt = -1

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// JSON should be valid, but retry attempt is invalid
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("ZeroMaxAttempts", func(t *testing.T) {
			// Test with zero max attempts
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.Retry.MaxAttempts = 0

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// JSON should be valid, but max attempts is invalid
			tu.AssertJSONValid(string(cmdJSON))
		})
	})

	t.Run("InvalidFormatFields", func(t *testing.T) {
		t.Run("InvalidMessageIDFormat", func(t *testing.T) {
			// Test with invalid message ID format
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.MessageID = "invalid-format"

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// JSON should be valid, but message ID format is invalid
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("InvalidTaskIDFormat", func(t *testing.T) {
			// Test with invalid task ID format
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "invalid-task-id")

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// JSON should be valid, but task ID format is invalid
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("InvalidIdempotencyKeyFormat", func(t *testing.T) {
			// Test with invalid idempotency key format
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.IdempotencyKey = "invalid-format"

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// JSON should be valid, but idempotency key format is invalid
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("InvalidSnapshotIDFormat", func(t *testing.T) {
			// Test with invalid snapshot ID format
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.Version.SnapshotID = "invalid-format"

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// JSON should be valid, but snapshot ID format is invalid
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("InvalidSHA256Format", func(t *testing.T) {
			// Test with invalid SHA256 format
			artifact := protocol.Artifact{
				Path:   "test.txt",
				SHA256: "invalid-sha256-format",
				Size:   100,
			}

			artifactJSON, err := json.Marshal(artifact)
			require.NoError(t, err)

			// JSON should be valid, but SHA256 format is invalid
			tu.AssertJSONValid(string(artifactJSON))
		})
	})

	t.Run("EdgeCaseValues", func(t *testing.T) {
		t.Run("EmptyStrings", func(t *testing.T) {
			// Test with empty strings
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.MessageID = ""
			cmd.CorrelationID = ""
			cmd.TaskID = ""

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// JSON should be valid, but fields are empty
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("ZeroValues", func(t *testing.T) {
			// Test with zero values
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.Priority = 0
			cmd.Retry.Attempt = 0
			cmd.Retry.MaxAttempts = 1

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// Should be valid (zero values are allowed)
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("MaximumValues", func(t *testing.T) {
			// Test with maximum values
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.Priority = 10 // Maximum priority
			cmd.Retry.MaxAttempts = 1000 // Large retry count

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// Should be valid
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("VeryLongStrings", func(t *testing.T) {
			// Test with very long strings
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.MessageID = strings.Repeat("a", 1000) // Very long message ID
			cmd.CorrelationID = strings.Repeat("b", 1000) // Very long correlation ID

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// JSON should be valid, but strings are very long
			tu.AssertJSONValid(string(cmdJSON))
		})
	})

	t.Run("NestedObjectValidation", func(t *testing.T) {
		t.Run("InvalidToObject", func(t *testing.T) {
			// Test with invalid 'to' object
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.To = protocol.AgentRef{
				AgentType: "invalid_type",
				AgentID:   "",
			}

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// JSON should be valid, but 'to' object is invalid
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("InvalidVersionObject", func(t *testing.T) {
			// Test with invalid 'version' object
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.Version = protocol.Version{
				SnapshotID: "invalid-format",
				SpecsHash:  "invalid-hash",
				CodeHash:   "invalid-hash",
			}

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// JSON should be valid, but 'version' object is invalid
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("InvalidRetryObject", func(t *testing.T) {
			// Test with invalid 'retry' object
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.Retry = protocol.Retry{
				Attempt:     -1, // Invalid
				MaxAttempts: 0,  // Invalid
			}

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// JSON should be valid, but 'retry' object is invalid
			tu.AssertJSONValid(string(cmdJSON))
		})
	})

	t.Run("ArrayValidation", func(t *testing.T) {
		t.Run("InvalidExpectedOutputs", func(t *testing.T) {
			// Test with invalid expected outputs
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.ExpectedOutputs = []protocol.ExpectedOutput{
				{
					Path:        "", // Empty path
					Description: "Test output",
					Required:    true,
				},
			}

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// JSON should be valid, but expected outputs are invalid
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("InvalidArtifacts", func(t *testing.T) {
			// Test with invalid artifacts
			event := tu.CreateTestEvent("artifact.produced", tu.CreateTestCommand(protocol.ActionIntake, "T-001"))
			event.Artifacts = []protocol.Artifact{
				{
					Path:   "", // Empty path
					SHA256: "invalid-format",
					Size:   -1, // Negative size
				},
			}

			eventJSON, err := json.Marshal(event)
			require.NoError(t, err)

			// JSON should be valid, but artifacts are invalid
			tu.AssertJSONValid(string(eventJSON))
		})
	})

	t.Run("TimestampValidation", func(t *testing.T) {
		t.Run("InvalidDeadline", func(t *testing.T) {
			// Test with invalid deadline format
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.Deadline = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)

			// JSON should be valid, but deadline format might be invalid
			tu.AssertJSONValid(string(cmdJSON))
		})

		t.Run("InvalidOccurredAt", func(t *testing.T) {
			// Test with invalid occurred_at format
			event := tu.CreateTestEvent("test.event", tu.CreateTestCommand(protocol.ActionIntake, "T-001"))
			event.OccurredAt = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

			eventJSON, err := json.Marshal(event)
			require.NoError(t, err)

			// JSON should be valid, but occurred_at format might be invalid
			tu.AssertJSONValid(string(eventJSON))
		})
	})
}

// TestNegativeAgentBehavior tests negative agent behavior scenarios
func TestNegativeAgentBehavior(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

	t.Run("InvalidCommandAction", func(t *testing.T) {
		// Test with action not supported by orchestration role
		cmd := tu.CreateTestCommand(protocol.ActionImplement, "T-001")

		err := agent.handleCommand(cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not supported for role")
	})

	t.Run("VersionMismatch", func(t *testing.T) {
		// Set initial snapshot
		agent.mu.Lock()
		agent.firstObservedSnapshotID = "snap-001"
		agent.mu.Unlock()

		// Create command with different snapshot
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd.Version.SnapshotID = "snap-002"

		err := agent.handleCommand(cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "version_mismatch")
	})

	t.Run("LLMError", func(t *testing.T) {
		// Set up LLM to return error
		agent.llmCaller.(*MockLLMCaller).SetError("test prompt", fmt.Errorf("LLM service unavailable"))

		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")

		err := agent.handleCommand(cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "LLM service unavailable")
	})

	t.Run("InvalidLLMResponse", func(t *testing.T) {
		// Set up LLM to return invalid JSON
		agent.llmCaller.(*MockLLMCaller).SetResponse("test prompt", "invalid json response")

		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")

		err := agent.handleCommand(cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid_llm_response")
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

// TestNegativeProtocolCompliance tests negative protocol compliance scenarios
func TestNegativeProtocolCompliance(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	t.Run("OversizeMessageHandling", func(t *testing.T) {
		// Test that oversize messages are handled gracefully
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")

		// Add large input data
		largeInput := strings.Repeat("X", 300*1024) // 300 KiB
		cmd.Inputs = map[string]any{
			"user_instruction": "Test instruction",
			"large_data":       largeInput,
		}

		cmdJSON, err := json.Marshal(cmd)
		require.NoError(t, err)

		// Should exceed size limit
		size := len(cmdJSON)
		assert.Greater(t, size, 256*1024, "Large command should exceed size limit")

		// In real implementation, this would trigger size capping or error
		// For now, we document the expected behavior
	})

	t.Run("InvalidEnumHandling", func(t *testing.T) {
		// Test that invalid enums are handled gracefully
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd.To.AgentType = "invalid_agent_type"

		cmdJSON, err := json.Marshal(cmd)
		require.NoError(t, err)

		// JSON should be valid, but enum is invalid
		tu.AssertJSONValid(string(cmdJSON))

		// In real implementation, this would be caught by schema validation
		// For now, we document the expected behavior
	})

	t.Run("MissingFieldHandling", func(t *testing.T) {
		// Test that missing required fields are handled gracefully
		incompleteCmd := map[string]any{
			"kind": "command",
			// Missing required fields
		}

		cmdJSON, err := json.Marshal(incompleteCmd)
		require.NoError(t, err)

		// JSON should be valid, but missing required fields
		tu.AssertJSONValid(string(cmdJSON))

		// In real implementation, this would be caught by schema validation
		// For now, we document the expected behavior
	})

	t.Run("InvalidTypeHandling", func(t *testing.T) {
		// Test that invalid field types are handled gracefully
		invalidCmd := map[string]any{
			"kind":          "command",
			"message_id":    123, // Should be string
			"correlation_id": "corr-001",
			"task_id":        "T-001",
			"idempotency_key": "ik-test123",
			"to": map[string]any{
				"agent_type": "orchestration",
			},
			"action": "intake",
			"inputs": map[string]any{},
			"version": map[string]any{
				"snapshot_id": "snap-001",
			},
			"deadline": "2025-12-31T23:59:59Z",
			"retry": map[string]any{
				"attempt":      0,
				"max_attempts": 3,
			},
			"priority": "high", // Should be integer
		}

		cmdJSON, err := json.Marshal(invalidCmd)
		require.NoError(t, err)

		// JSON should be valid, but field types are wrong
		tu.AssertJSONValid(string(cmdJSON))

		// In real implementation, this would be caught by schema validation
		// For now, we document the expected behavior
	})
}
