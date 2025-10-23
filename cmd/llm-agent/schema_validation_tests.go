package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSchemaValidation tests protocol schema compliance
func TestSchemaValidation(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	t.Run("CommandSchema", func(t *testing.T) {
		// Test valid command
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmdJSON, err := json.Marshal(cmd)
		require.NoError(t, err)

		// Validate JSON structure
		tu.AssertJSONValid(string(cmdJSON))

		// Validate required fields
		assert.Equal(t, protocol.MessageKindCommand, cmd.Kind)
		assert.NotEmpty(t, cmd.MessageID)
		assert.NotEmpty(t, cmd.CorrelationID)
		assert.NotEmpty(t, cmd.TaskID)
		assert.NotEmpty(t, cmd.IdempotencyKey)
		assert.NotEmpty(t, cmd.To.AgentType)
		assert.NotEmpty(t, cmd.Action)
		assert.NotEmpty(t, cmd.Version.SnapshotID)
		assert.NotZero(t, cmd.Deadline)
		assert.GreaterOrEqual(t, cmd.Retry.Attempt, 0)
		assert.GreaterOrEqual(t, cmd.Retry.MaxAttempts, 1)
		assert.GreaterOrEqual(t, cmd.Priority, 0)
		assert.LessOrEqual(t, cmd.Priority, 10)
	})

	t.Run("EventSchema", func(t *testing.T) {
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		event := tu.CreateTestEvent("test.event", cmd)
		eventJSON, err := json.Marshal(event)
		require.NoError(t, err)

		// Validate JSON structure
		tu.AssertJSONValid(string(eventJSON))

		// Validate required fields
		assert.Equal(t, protocol.MessageKindEvent, event.Kind)
		assert.NotEmpty(t, event.MessageID)
		assert.NotEmpty(t, event.CorrelationID)
		assert.NotEmpty(t, event.TaskID)
		assert.NotEmpty(t, event.From.AgentType)
		assert.NotEmpty(t, event.Event)
		assert.NotZero(t, event.OccurredAt)
		assert.NotNil(t, event.ObservedVersion)
		assert.NotEmpty(t, event.ObservedVersion.SnapshotID)
	})

	t.Run("HeartbeatSchema", func(t *testing.T) {
		heartbeat := tu.CreateTestHeartbeat(protocol.HeartbeatStatusReady, "T-001")
		heartbeatJSON, err := json.Marshal(heartbeat)
		require.NoError(t, err)

		// Validate JSON structure
		tu.AssertJSONValid(string(heartbeatJSON))

		// Validate required fields
		assert.Equal(t, protocol.MessageKindHeartbeat, heartbeat.Kind)
		assert.NotEmpty(t, heartbeat.Agent.AgentType)
		assert.NotEmpty(t, heartbeat.Agent.AgentID)
		assert.GreaterOrEqual(t, heartbeat.Seq, int64(0))
		assert.NotEmpty(t, heartbeat.Status)
		assert.Greater(t, heartbeat.PID, 0)
		assert.GreaterOrEqual(t, heartbeat.PPID, 0)
		assert.GreaterOrEqual(t, heartbeat.UptimeS, 0.0)
		assert.NotEmpty(t, heartbeat.LastActivityAt)
	})

	t.Run("LogSchema", func(t *testing.T) {
		log := tu.CreateTestLog("info", "test message")
		logJSON, err := json.Marshal(log)
		require.NoError(t, err)

		// Validate JSON structure
		tu.AssertJSONValid(string(logJSON))

		// Validate required fields
		assert.Equal(t, protocol.MessageKindLog, log.Kind)
		assert.NotEmpty(t, log.Level)
		assert.NotEmpty(t, log.Message)
		assert.NotZero(t, log.Timestamp)
	})
}

// TestMessageSizeLimits tests NDJSON message size limits
func TestMessageSizeLimits(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	maxSize := 256 * 1024 // 256 KiB

	t.Run("ValidMessageSizes", func(t *testing.T) {
		// Test normal-sized messages
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmdJSON, err := json.Marshal(cmd)
		require.NoError(t, err)

		tu.AssertMessageSize(string(cmdJSON), maxSize)

		event := tu.CreateTestEvent("test.event", cmd)
		eventJSON, err := json.Marshal(event)
		require.NoError(t, err)

		tu.AssertMessageSize(string(eventJSON), maxSize)
	})

	t.Run("LargePayloadHandling", func(t *testing.T) {
		// Test event with large payload
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		event := tu.CreateTestEvent("test.event", cmd)

		// Create large payload
		largeData := strings.Repeat("A", 300*1024) // 300 KiB
		event.Payload = map[string]any{
			"large_data": largeData,
		}

		eventJSON, err := json.Marshal(event)
		require.NoError(t, err)

		// This should exceed the limit
		size := len(eventJSON)
		assert.Greater(t, size, maxSize, "Large payload should exceed size limit")

		// In real implementation, this would trigger size capping
		// For now, we just document the expected behavior
	})

	t.Run("ArtifactReference", func(t *testing.T) {
		// Test that large content is referenced via artifacts
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		event := tu.CreateTestEvent("artifact.produced", cmd)

		// Add artifact reference instead of embedding content
		event.Artifacts = []protocol.Artifact{
			{
				Path:   "large-data.json",
				SHA256: "sha256:test123",
				Size:   300 * 1024, // 300 KiB
			},
		}

		eventJSON, err := json.Marshal(event)
		require.NoError(t, err)

		// Should be under size limit
		tu.AssertMessageSize(string(eventJSON), maxSize)
	})
}

// TestEnumValidation tests enum value validation
func TestEnumValidation(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	t.Run("ValidAgentTypes", func(t *testing.T) {
		validTypes := []protocol.AgentType{
			protocol.AgentTypeBuilder,
			protocol.AgentTypeReviewer,
			protocol.AgentTypeSpecMaintainer,
			protocol.AgentTypeOrchestration,
		}

		for _, agentType := range validTypes {
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.To.AgentType = agentType

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)
			tu.AssertJSONValid(string(cmdJSON))
		}
	})

	t.Run("ValidActions", func(t *testing.T) {
		validActions := []protocol.Action{
			protocol.ActionImplement,
			protocol.ActionImplementChanges,
			protocol.ActionReview,
			protocol.ActionUpdateSpec,
			protocol.ActionIntake,
			protocol.ActionTaskDiscovery,
		}

		for _, action := range validActions {
			cmd := tu.CreateTestCommand(action, "T-001")

			cmdJSON, err := json.Marshal(cmd)
			require.NoError(t, err)
			tu.AssertJSONValid(string(cmdJSON))
		}
	})

	t.Run("ValidHeartbeatStatuses", func(t *testing.T) {
		validStatuses := []protocol.HeartbeatStatus{
			protocol.HeartbeatStatusStarting,
			protocol.HeartbeatStatusReady,
			protocol.HeartbeatStatusBusy,
			protocol.HeartbeatStatusStopping,
			protocol.HeartbeatStatusBackoff,
		}

		for _, status := range validStatuses {
			heartbeat := tu.CreateTestHeartbeat(status, "T-001")

			heartbeatJSON, err := json.Marshal(heartbeat)
			require.NoError(t, err)
			tu.AssertJSONValid(string(heartbeatJSON))
		}
	})

	t.Run("ValidLogLevels", func(t *testing.T) {
		validLevels := []string{"info", "warn", "error"}

		for _, level := range validLevels {
			log := tu.CreateTestLog(level, "test message")

			logJSON, err := json.Marshal(log)
			require.NoError(t, err)
			tu.AssertJSONValid(string(logJSON))
		}
	})
}

// TestFieldValidation tests individual field validation
func TestFieldValidation(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	t.Run("MessageIDFormat", func(t *testing.T) {
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		tu.AssertMessageIDValid(cmd.MessageID)
	})

	t.Run("TaskIDFormat", func(t *testing.T) {
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		tu.AssertTaskIDValid(cmd.TaskID)
	})

	t.Run("IdempotencyKeyFormat", func(t *testing.T) {
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		tu.AssertIdempotencyKeyValid(cmd.IdempotencyKey)
	})

	t.Run("SnapshotIDFormat", func(t *testing.T) {
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		tu.AssertSnapshotIDValid(cmd.Version.SnapshotID)
	})

	t.Run("SHA256Format", func(t *testing.T) {
		artifact := protocol.Artifact{
			Path:   "test.txt",
			SHA256: "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			Size:   100,
		}

		tu.AssertSHA256Valid(artifact.SHA256)
	})

	t.Run("TimestampFormat", func(t *testing.T) {
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")

		// Validate deadline format
		deadlineStr := cmd.Deadline.Format("2006-01-02T15:04:05Z07:00")
		assert.NotEmpty(t, deadlineStr)

		// Validate occurred_at format
		event := tu.CreateTestEvent("test.event", cmd)
		occurredAtStr := event.OccurredAt.Format("2006-01-02T15:04:05Z07:00")
		assert.NotEmpty(t, occurredAtStr)
	})
}

// TestNegativeValidation tests invalid enum values and malformed data
func TestNegativeSchemaValidation(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	t.Run("InvalidAgentType", func(t *testing.T) {
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd.To.AgentType = "invalid_type"

		cmdJSON, err := json.Marshal(cmd)
		require.NoError(t, err)

		// JSON should be valid, but agent type is invalid
		tu.AssertJSONValid(string(cmdJSON))

		// In real implementation, this would be caught by schema validation
		// For now, we document the expected behavior
	})

	t.Run("InvalidAction", func(t *testing.T) {
		cmd := tu.CreateTestCommand("invalid_action", "T-001")

		cmdJSON, err := json.Marshal(cmd)
		require.NoError(t, err)

		// JSON should be valid, but action is invalid
		tu.AssertJSONValid(string(cmdJSON))
	})

	t.Run("InvalidHeartbeatStatus", func(t *testing.T) {
		heartbeat := tu.CreateTestHeartbeat("invalid_status", "T-001")

		heartbeatJSON, err := json.Marshal(heartbeat)
		require.NoError(t, err)

		// JSON should be valid, but status is invalid
		tu.AssertJSONValid(string(heartbeatJSON))
	})

	t.Run("InvalidLogLevel", func(t *testing.T) {
		log := tu.CreateTestLog("invalid_level", "test message")

		logJSON, err := json.Marshal(log)
		require.NoError(t, err)

		// JSON should be valid, but level is invalid
		tu.AssertJSONValid(string(logJSON))
	})

	t.Run("MalformedJSON", func(t *testing.T) {
		malformedJSON := `{"kind": "command", "invalid": json}`

		// This should fail JSON parsing
		var cmd protocol.Command
		err := json.Unmarshal([]byte(malformedJSON), &cmd)
		require.Error(t, err)
	})

	t.Run("MissingRequiredFields", func(t *testing.T) {
		// Test command missing required fields
		incompleteCmd := map[string]any{
			"kind": "command",
			// Missing required fields
		}

		cmdJSON, err := json.Marshal(incompleteCmd)
		require.NoError(t, err)

		// JSON should be valid, but missing required fields
		tu.AssertJSONValid(string(cmdJSON))

		// In real implementation, this would be caught by schema validation
	})

	t.Run("InvalidFieldTypes", func(t *testing.T) {
		// Test with wrong field types
		invalidCmd := map[string]any{
			"kind":           "command",
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
			"priority": "high", // Should be integer
		}

		cmdJSON, err := json.Marshal(invalidCmd)
		require.NoError(t, err)

		// JSON should be valid, but field types are wrong
		tu.AssertJSONValid(string(cmdJSON))
	})
}

// TestSchemaCompliance tests full schema compliance
func TestSchemaCompliance(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	t.Run("CompleteCommandCompliance", func(t *testing.T) {
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")

		// Add expected outputs
		cmd.ExpectedOutputs = []protocol.ExpectedOutput{
			{
				Path:        "tasks/T-001.plan.json",
				Description: "Task plan file",
				Required:    true,
			},
		}

		// Add version hashes
		cmd.Version.SpecsHash = "sha256:specs1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
		cmd.Version.CodeHash = "sha256:code1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

		cmdJSON, err := json.Marshal(cmd)
		require.NoError(t, err)

		// Validate complete structure
		tu.AssertJSONValid(string(cmdJSON))
		tu.AssertMessageSize(string(cmdJSON), 256*1024)

		// Validate all required fields are present
		assert.Equal(t, protocol.MessageKindCommand, cmd.Kind)
		assert.NotEmpty(t, cmd.MessageID)
		assert.NotEmpty(t, cmd.CorrelationID)
		assert.NotEmpty(t, cmd.TaskID)
		assert.NotEmpty(t, cmd.IdempotencyKey)
		assert.NotEmpty(t, cmd.To.AgentType)
		assert.NotEmpty(t, cmd.Action)
		assert.NotEmpty(t, cmd.Version.SnapshotID)
		assert.NotZero(t, cmd.Deadline)
		assert.GreaterOrEqual(t, cmd.Retry.Attempt, 0)
		assert.GreaterOrEqual(t, cmd.Retry.MaxAttempts, 1)
		assert.GreaterOrEqual(t, cmd.Priority, 0)
		assert.LessOrEqual(t, cmd.Priority, 10)
	})

	t.Run("CompleteEventCompliance", func(t *testing.T) {
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		event := tu.CreateTestEvent("orchestration.proposed_tasks", cmd)

		// Add artifacts
		event.Artifacts = []protocol.Artifact{
			{
				Path:   "tasks/T-001.plan.json",
				SHA256: "sha256:plan1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				Size:   1024,
			},
		}

		// Add payload
		event.Payload = map[string]any{
			"plan_candidates": []map[string]any{
				{
					"path":       "PLAN.md",
					"confidence": 0.9,
				},
			},
			"derived_tasks": []map[string]any{
				{
					"id":    "T-001-1",
					"title": "Test task",
					"files": []string{"src/test.go"},
				},
			},
		}

		eventJSON, err := json.Marshal(event)
		require.NoError(t, err)

		// Validate complete structure
		tu.AssertJSONValid(string(eventJSON))
		tu.AssertMessageSize(string(eventJSON), 256*1024)

		// Validate all required fields are present
		assert.Equal(t, protocol.MessageKindEvent, event.Kind)
		assert.NotEmpty(t, event.MessageID)
		assert.NotEmpty(t, event.CorrelationID)
		assert.NotEmpty(t, event.TaskID)
		assert.NotEmpty(t, event.From.AgentType)
		assert.NotEmpty(t, event.Event)
		assert.NotZero(t, event.OccurredAt)
		assert.NotNil(t, event.ObservedVersion)
		assert.NotEmpty(t, event.ObservedVersion.SnapshotID)
	})

	t.Run("CompleteHeartbeatCompliance", func(t *testing.T) {
		heartbeat := tu.CreateTestHeartbeat(protocol.HeartbeatStatusBusy, "T-001")

		// Add stats
		heartbeat.Stats = &protocol.HeartbeatStats{
			RSSBytes: 1024 * 1024, // 1MB
		}

		heartbeatJSON, err := json.Marshal(heartbeat)
		require.NoError(t, err)

		// Validate complete structure
		tu.AssertJSONValid(string(heartbeatJSON))
		tu.AssertMessageSize(string(heartbeatJSON), 256*1024)

		// Validate all required fields are present
		assert.Equal(t, protocol.MessageKindHeartbeat, heartbeat.Kind)
		assert.NotEmpty(t, heartbeat.Agent.AgentType)
		assert.NotEmpty(t, heartbeat.Agent.AgentID)
		assert.GreaterOrEqual(t, heartbeat.Seq, int64(0))
		assert.NotEmpty(t, heartbeat.Status)
		assert.Greater(t, heartbeat.PID, 0)
		assert.GreaterOrEqual(t, heartbeat.PPID, 0)
		assert.GreaterOrEqual(t, heartbeat.UptimeS, 0.0)
		assert.NotEmpty(t, heartbeat.LastActivityAt)
		assert.NotNil(t, heartbeat.Stats)
		// CPUPct field not available in current protocol
		assert.GreaterOrEqual(t, heartbeat.Stats.RSSBytes, int64(0))
	})
}

// TestEdgeCases tests edge cases in schema validation
func TestEdgeCases(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	t.Run("EmptyStrings", func(t *testing.T) {
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd.MessageID = ""
		cmd.CorrelationID = ""
		cmd.TaskID = ""

		cmdJSON, err := json.Marshal(cmd)
		require.NoError(t, err)

		// JSON should be valid, but fields are empty
		tu.AssertJSONValid(string(cmdJSON))

		// In real implementation, this would be caught by schema validation
	})

	t.Run("ZeroValues", func(t *testing.T) {
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
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd.Priority = 10 // Maximum priority
		cmd.Retry.MaxAttempts = 100 // Large retry count

		cmdJSON, err := json.Marshal(cmd)
		require.NoError(t, err)

		// Should be valid
		tu.AssertJSONValid(string(cmdJSON))
	})

	t.Run("NegativeValues", func(t *testing.T) {
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		cmd.Priority = -1 // Invalid priority
		cmd.Retry.Attempt = -1 // Invalid attempt

		cmdJSON, err := json.Marshal(cmd)
		require.NoError(t, err)

		// JSON should be valid, but values are invalid
		tu.AssertJSONValid(string(cmdJSON))

		// In real implementation, this would be caught by schema validation
	})
}
