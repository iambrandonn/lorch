package main

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/iambrandonn/lorch/internal/ndjson"
	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockEventEmitter(t *testing.T) {
	emitter := NewMockEventEmitter()

	// Test initial state
	assert.Empty(t, emitter.GetEvents())
	assert.Empty(t, emitter.GetLogs())
	assert.Empty(t, emitter.GetCallLog())
	assert.Empty(t, emitter.GetErrorLog())
	assert.Empty(t, emitter.GetArtifactLog())

	// Create a mock command
	cmd := &protocol.Command{
		CorrelationID: "corr-1",
		TaskID:        "T-001",
		Version: protocol.Version{
			SnapshotID: "snap-1",
		},
	}

	// Test NewEvent
	evt := emitter.NewEvent(cmd, "test.event")
	assert.Equal(t, "test.event", evt.Event)
	assert.Equal(t, "corr-1", evt.CorrelationID)
	assert.Equal(t, "T-001", evt.TaskID)
	assert.Equal(t, "snap-1", evt.ObservedVersion.SnapshotID)
	assert.Equal(t, protocol.MessageKindEvent, evt.Kind)

	// Test SendErrorEvent
	err := emitter.SendErrorEvent(cmd, "test_error", "test message")
	require.NoError(t, err)

	// Test SendArtifactProducedEvent
	artifact := protocol.Artifact{
		Path:   "test.txt",
		SHA256: "sha256:abc123",
		Size:   100,
	}
	err = emitter.SendArtifactProducedEvent(cmd, artifact)
	require.NoError(t, err)

	// Test SendLog
	err = emitter.SendLog("info", "test log", map[string]any{"key": "value"})
	require.NoError(t, err)

	// Verify events were recorded
	events := emitter.GetEvents()
	assert.Len(t, events, 2) // error + artifact events

	// Check error event
	errorEvent := events[0]
	assert.Equal(t, "error", errorEvent.Event)
	assert.Equal(t, "failed", errorEvent.Status)
	assert.Equal(t, "test_error", errorEvent.Payload["code"])
	assert.Equal(t, "test message", errorEvent.Payload["message"])

	// Check artifact event
	artifactEvent := events[1]
	assert.Equal(t, "artifact.produced", artifactEvent.Event)
	assert.Equal(t, "success", artifactEvent.Status)
	assert.Len(t, artifactEvent.Artifacts, 1)
	assert.Equal(t, "test.txt", artifactEvent.Artifacts[0].Path)

	// Verify logs
	logs := emitter.GetLogs()
	assert.Len(t, logs, 1)
	assert.Equal(t, "info", string(logs[0].Level))
	assert.Equal(t, "test log", logs[0].Message)
	assert.Equal(t, "value", logs[0].Fields["key"])

	// Test call logs
	callLog := emitter.GetCallLog()
	assert.Contains(t, callLog, "NewEvent(test.event)")
	assert.Contains(t, callLog, "SendLog(info, test log)")

	errorLog := emitter.GetErrorLog()
	assert.Contains(t, errorLog, "SendErrorEvent(test_error, test message)")

	artifactLog := emitter.GetArtifactLog()
	assert.Contains(t, artifactLog, "SendArtifactProducedEvent(test.txt)")
}

func TestMockEventEmitterClearLogs(t *testing.T) {
	emitter := NewMockEventEmitter()

	// Make some operations
	cmd := &protocol.Command{
		CorrelationID: "corr-1",
		TaskID:        "T-001",
		Version: protocol.Version{
			SnapshotID: "snap-1",
		},
	}

	emitter.NewEvent(cmd, "test.event")
	emitter.SendErrorEvent(cmd, "error", "message")
	emitter.SendLog("info", "log", nil)

	// Verify logs have entries
	assert.Len(t, emitter.GetEvents(), 1)
	assert.Len(t, emitter.GetLogs(), 1)
	assert.Len(t, emitter.GetCallLog(), 3) // NewEvent + SendErrorEvent + SendLog
	assert.Len(t, emitter.GetErrorLog(), 1)

	// Clear logs
	emitter.ClearLogs()

	// Verify logs are empty
	assert.Empty(t, emitter.GetEvents())
	assert.Empty(t, emitter.GetLogs())
	assert.Empty(t, emitter.GetCallLog())
	assert.Empty(t, emitter.GetErrorLog())
	assert.Empty(t, emitter.GetArtifactLog())
}

func TestMockEventEmitterMultipleEvents(t *testing.T) {
	emitter := NewMockEventEmitter()

	cmd := &protocol.Command{
		CorrelationID: "corr-1",
		TaskID:        "T-001",
		Version: protocol.Version{
			SnapshotID: "snap-1",
		},
	}

	// Send multiple events
	emitter.SendErrorEvent(cmd, "error1", "message1")
	emitter.SendErrorEvent(cmd, "error2", "message2")

	artifact1 := protocol.Artifact{Path: "file1.txt", SHA256: "sha256:abc", Size: 100}
	artifact2 := protocol.Artifact{Path: "file2.txt", SHA256: "sha256:def", Size: 200}

	emitter.SendArtifactProducedEvent(cmd, artifact1)
	emitter.SendArtifactProducedEvent(cmd, artifact2)

	// Verify all events were recorded
	events := emitter.GetEvents()
	assert.Len(t, events, 4) // 2 errors + 2 artifacts

	// Check error events
	errorEvents := make([]protocol.Event, 0)
	for _, evt := range events {
		if evt.Event == "error" {
			errorEvents = append(errorEvents, evt)
		}
	}
	assert.Len(t, errorEvents, 2)

	// Check artifact events
	artifactEvents := make([]protocol.Event, 0)
	for _, evt := range events {
		if evt.Event == "artifact.produced" {
			artifactEvents = append(artifactEvents, evt)
		}
	}
	assert.Len(t, artifactEvents, 2)

	// Verify logs
	errorLog := emitter.GetErrorLog()
	assert.Len(t, errorLog, 2)
	assert.Contains(t, errorLog, "SendErrorEvent(error1, message1)")
	assert.Contains(t, errorLog, "SendErrorEvent(error2, message2)")

	artifactLog := emitter.GetArtifactLog()
	assert.Len(t, artifactLog, 2)
	assert.Contains(t, artifactLog, "SendArtifactProducedEvent(file1.txt)")
	assert.Contains(t, artifactLog, "SendArtifactProducedEvent(file2.txt)")
}

func TestMockEventEmitterLogLevels(t *testing.T) {
	emitter := NewMockEventEmitter()

	// Test different log levels
	emitter.SendLog("info", "info message", nil)
	emitter.SendLog("warn", "warn message", nil)
	emitter.SendLog("error", "error message", nil)

	logs := emitter.GetLogs()
	assert.Len(t, logs, 3)

	// Check log levels
	levels := make([]string, 0)
	for _, log := range logs {
		levels = append(levels, string(log.Level))
	}
	assert.Contains(t, levels, "info")
	assert.Contains(t, levels, "warn")
	assert.Contains(t, levels, "error")

	// Check messages
	messages := make([]string, 0)
	for _, log := range logs {
		messages = append(messages, log.Message)
	}
	assert.Contains(t, messages, "info message")
	assert.Contains(t, messages, "warn message")
	assert.Contains(t, messages, "error message")
}

func TestMockEventEmitterLogFields(t *testing.T) {
	emitter := NewMockEventEmitter()

	// Test log with fields
	fields := map[string]any{
		"key1": "value1",
		"key2": 42,
		"key3": true,
	}

	emitter.SendLog("info", "test log", fields)

	logs := emitter.GetLogs()
	assert.Len(t, logs, 1)

	log := logs[0]
	assert.Equal(t, "test log", log.Message)
	assert.Equal(t, "value1", log.Fields["key1"])
	assert.Equal(t, 42, log.Fields["key2"])
	assert.Equal(t, true, log.Fields["key3"])
}

func TestMockEventEmitterOrchestrationEvents(t *testing.T) {
	emitter := NewMockEventEmitter()

	cmd := &protocol.Command{
		CorrelationID: "corr-1",
		TaskID:        "T-001",
		Version: protocol.Version{
			SnapshotID: "snap-1",
		},
	}

	// Test SendOrchestrationProposedTasksEvent
	planCandidates := []map[string]any{
		{"path": "PLAN.md", "confidence": 0.9},
		{"path": "docs/plan_v2.md", "confidence": 0.7},
	}
	derivedTasks := []map[string]any{
		{"id": "T-001-1", "title": "Task 1", "files": []string{"src/a.js"}},
		{"id": "T-001-2", "title": "Task 2", "files": []string{"src/b.js"}},
	}
	notes := "Found 2 plan candidates and derived 2 tasks"

	err := emitter.SendOrchestrationProposedTasksEvent(cmd, planCandidates, derivedTasks, notes)
	require.NoError(t, err)

	// Test SendOrchestrationNeedsClarificationEvent
	questions := []string{
		"Which plan file should be used?",
		"Should we implement phases A and B together?",
	}
	clarificationNotes := "Ambiguous instruction; multiple plausible interpretations"

	err = emitter.SendOrchestrationNeedsClarificationEvent(cmd, questions, clarificationNotes)
	require.NoError(t, err)

	// Test SendOrchestrationPlanConflictEvent
	candidates := []map[string]any{
		{"path": "PLAN.md", "confidence": 0.81},
		{"path": "docs/plan_v2.md", "confidence": 0.80},
	}
	reason := "Two high-confidence plans diverge in scope; human selection required."

	err = emitter.SendOrchestrationPlanConflictEvent(cmd, candidates, reason)
	require.NoError(t, err)

	// Verify events were recorded
	events := emitter.GetEvents()
	assert.Len(t, events, 3)

	// Check proposed tasks event
	proposedEvent := events[0]
	assert.Equal(t, "orchestration.proposed_tasks", proposedEvent.Event)
	assert.Equal(t, "success", proposedEvent.Status)
	assert.Equal(t, "corr-1", proposedEvent.CorrelationID)
	assert.Equal(t, "T-001", proposedEvent.TaskID)
	assert.Equal(t, "snap-1", proposedEvent.ObservedVersion.SnapshotID)
	assert.Len(t, proposedEvent.Payload["plan_candidates"], 2)
	assert.Len(t, proposedEvent.Payload["derived_tasks"], 2)
	assert.Equal(t, notes, proposedEvent.Payload["notes"])

	// Check needs clarification event
	clarificationEvent := events[1]
	assert.Equal(t, "orchestration.needs_clarification", clarificationEvent.Event)
	assert.Equal(t, "needs_input", clarificationEvent.Status)
	assert.Len(t, clarificationEvent.Payload["questions"], 2)
	assert.Equal(t, clarificationNotes, clarificationEvent.Payload["notes"])

	// Check plan conflict event
	conflictEvent := events[2]
	assert.Equal(t, "orchestration.plan_conflict", conflictEvent.Event)
	assert.Equal(t, "needs_input", conflictEvent.Status)
	assert.Len(t, conflictEvent.Payload["candidates"], 2)
	assert.Equal(t, reason, conflictEvent.Payload["reason"])

	// Verify call logs
	callLog := emitter.GetCallLog()
	assert.Contains(t, callLog, "SendOrchestrationProposedTasksEvent(2 candidates, 2 tasks)")
	assert.Contains(t, callLog, "SendOrchestrationNeedsClarificationEvent(2 questions)")
	assert.Contains(t, callLog, "SendOrchestrationPlanConflictEvent(2 candidates)")
}

func TestMockEventEmitterOrchestrationEventValidation(t *testing.T) {
	emitter := NewMockEventEmitter()

	cmd := &protocol.Command{
		CorrelationID: "corr-1",
		TaskID:        "T-001",
		Version: protocol.Version{
			SnapshotID: "snap-1",
		},
	}

	// Test with empty data
	err := emitter.SendOrchestrationProposedTasksEvent(cmd, []map[string]any{}, []map[string]any{}, "")
	require.NoError(t, err)

	err = emitter.SendOrchestrationNeedsClarificationEvent(cmd, []string{}, "")
	require.NoError(t, err)

	err = emitter.SendOrchestrationPlanConflictEvent(cmd, []map[string]any{}, "")
	require.NoError(t, err)

	// Verify events were still recorded
	events := emitter.GetEvents()
	assert.Len(t, events, 3)

	// Check that empty data is handled gracefully
	for _, event := range events {
		assert.NotNil(t, event.Payload)
		assert.Equal(t, "corr-1", event.CorrelationID)
		assert.Equal(t, "T-001", event.TaskID)
		assert.Equal(t, "snap-1", event.ObservedVersion.SnapshotID)
	}
}

func TestMockEventEmitterMessageSizeCapping(t *testing.T) {
	emitter := NewMockEventEmitter()

	cmd := &protocol.Command{
		CorrelationID: "corr-1",
		TaskID:        "T-001",
		Version: protocol.Version{
			SnapshotID: "snap-1",
		},
	}

	// Test with large payload that would exceed size limits
	largePlanCandidates := make([]map[string]any, 1000)
	for i := 0; i < 1000; i++ {
		largePlanCandidates[i] = map[string]any{
			"path":       fmt.Sprintf("plan_%d.md", i),
			"confidence": 0.5 + float64(i%50)/100.0,
			"content":    strings.Repeat("x", 1000), // 1KB per candidate
		}
	}

	largeDerivedTasks := make([]map[string]any, 1000)
	for i := 0; i < 1000; i++ {
		largeDerivedTasks[i] = map[string]any{
			"id":    fmt.Sprintf("T-001-%d", i),
			"title": fmt.Sprintf("Task %d", i),
			"files": []string{fmt.Sprintf("src/file_%d.js", i)},
			"notes": strings.Repeat("y", 500), // 500 bytes per task
		}
	}

	// This should trigger size capping in the real implementation
	err := emitter.SendOrchestrationProposedTasksEvent(cmd, largePlanCandidates, largeDerivedTasks, "Large payload test")
	require.NoError(t, err)

	// Verify event was recorded (mock doesn't actually cap, but real implementation would)
	events := emitter.GetEvents()
	assert.Len(t, events, 1)

	event := events[0]
	assert.Equal(t, "orchestration.proposed_tasks", event.Event)
	assert.Equal(t, "success", event.Status)
	assert.Len(t, event.Payload["plan_candidates"], 1000)
	assert.Len(t, event.Payload["derived_tasks"], 1000)
}

func TestMockEventEmitterErrorCodes(t *testing.T) {
	emitter := NewMockEventEmitter()

	cmd := &protocol.Command{
		CorrelationID: "corr-1",
		TaskID:        "T-001",
		Version: protocol.Version{
			SnapshotID: "snap-1",
		},
	}

	// Test various error codes as specified in the plan
	errorCodes := []string{
		"invalid_inputs",
		"llm_call_failed",
		"invalid_llm_response",
		"version_mismatch",
		"file_read_failed",
		"artifact_write_failed",
		"receipt_lookup_failed",
	}

	for i, code := range errorCodes {
		message := fmt.Sprintf("Test error message %d", i)
		err := emitter.SendErrorEvent(cmd, code, message)
		if code == "version_mismatch" {
			// version_mismatch returns an error in the mock
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "version_mismatch")
		} else {
			require.NoError(t, err)
		}
	}

	// Verify all error events were recorded
	events := emitter.GetEvents()
	assert.Len(t, events, len(errorCodes))

	// Check each error event
	for i, event := range events {
		assert.Equal(t, "error", event.Event)
		assert.Equal(t, "failed", event.Status)
		assert.Equal(t, errorCodes[i], event.Payload["code"])
		assert.Equal(t, fmt.Sprintf("Test error message %d", i), event.Payload["message"])
		assert.Equal(t, "corr-1", event.CorrelationID)
		assert.Equal(t, "T-001", event.TaskID)
		assert.Equal(t, "snap-1", event.ObservedVersion.SnapshotID)
	}

	// Verify error log
	errorLog := emitter.GetErrorLog()
	assert.Len(t, errorLog, len(errorCodes))
	for i, code := range errorCodes {
		expectedLog := fmt.Sprintf("SendErrorEvent(%s, Test error message %d)", code, i)
		assert.Contains(t, errorLog, expectedLog)
	}
}

func TestRealEventEmitterSizeCapping(t *testing.T) {
	// Create a buffer to capture output
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))
	encoder := ndjson.NewEncoder(&buf, logger)

	emitter := NewRealEventEmitter(encoder, logger, protocol.AgentTypeOrchestration, "test-agent")

	cmd := &protocol.Command{
		CorrelationID: "corr-1",
		TaskID:        "T-001",
		Version: protocol.Version{
			SnapshotID: "snap-1",
		},
	}

	// Test with a payload that exceeds the 256 KiB limit
	largePayload := make(map[string]any)
	largePayload["data"] = strings.Repeat("x", 300*1024) // 300 KiB of data

	evt := emitter.NewEvent(cmd, "test.large_event")
	evt.Payload = largePayload

	// This should trigger size capping
	err := emitter.EncodeEventCapped(evt)
	require.NoError(t, err)

	// Verify the output was capped
	output := buf.String()
	assert.Contains(t, output, "_truncated")
	assert.NotContains(t, output, strings.Repeat("x", 300*1024))
}

func TestRealEventEmitterProtocolCompliance(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))
	encoder := ndjson.NewEncoder(&buf, logger)

	emitter := NewRealEventEmitter(encoder, logger, protocol.AgentTypeOrchestration, "test-agent")

	cmd := &protocol.Command{
		CorrelationID: "corr-1",
		TaskID:        "T-001",
		Version: protocol.Version{
			SnapshotID: "snap-1",
		},
	}

	// Test that all required fields are present
	evt := emitter.NewEvent(cmd, "test.event")

	// Verify required fields
	assert.Equal(t, protocol.MessageKindEvent, evt.Kind)
	assert.NotEmpty(t, evt.MessageID)
	assert.Equal(t, "corr-1", evt.CorrelationID)
	assert.Equal(t, "T-001", evt.TaskID)
	assert.Equal(t, protocol.AgentTypeOrchestration, evt.From.AgentType)
	assert.Equal(t, "test.event", evt.Event)
	assert.NotZero(t, evt.OccurredAt)
	assert.NotNil(t, evt.ObservedVersion)
	assert.Equal(t, "snap-1", evt.ObservedVersion.SnapshotID)
}

func TestRealEventEmitterErrorEventStructure(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))
	encoder := ndjson.NewEncoder(&buf, logger)

	emitter := NewRealEventEmitter(encoder, logger, protocol.AgentTypeOrchestration, "test-agent")

	cmd := &protocol.Command{
		CorrelationID: "corr-1",
		TaskID:        "T-001",
		Version: protocol.Version{
			SnapshotID: "snap-1",
		},
	}

	// Test error event structure
	err := emitter.SendErrorEvent(cmd, "test_error", "test message")
	require.NoError(t, err)

	// Verify the output contains proper error structure
	output := buf.String()
	assert.Contains(t, output, `"event":"error"`)
	assert.Contains(t, output, `"status":"failed"`)
	assert.Contains(t, output, `"code":"test_error"`)
	assert.Contains(t, output, `"message":"test message"`)
}

func TestRedactSecrets(t *testing.T) {
	// Test basic redaction
	fields := map[string]any{
		"api_key":        "secret123",
		"access_token":   "token456",
		"database_secret": "secret789",
		"user_name":      "john_doe",
		"normal_field":   "normal_value",
	}

	// Use the real emitter's redactSecrets method
	realEmitter := &RealEventEmitter{}
	redacted := realEmitter.redactSecrets(fields)

	// Verify secrets are redacted
	assert.Equal(t, "[REDACTED]", redacted["api_key"])
	assert.Equal(t, "[REDACTED]", redacted["access_token"])
	assert.Equal(t, "[REDACTED]", redacted["database_secret"])

	// Verify non-secret fields are preserved
	assert.Equal(t, "john_doe", redacted["user_name"])
	assert.Equal(t, "normal_value", redacted["normal_field"])
}

func TestRedactSecretsCaseInsensitive(t *testing.T) {
	realEmitter := &RealEventEmitter{}

	// Test case insensitive redaction
	fields := map[string]any{
		"API_KEY":        "secret123",
		"Access_Token":   "token456",
		"database_SECRET": "secret789",
		"user_name":      "john_doe",
	}

	redacted := realEmitter.redactSecrets(fields)

	// Verify all variations are redacted
	assert.Equal(t, "[REDACTED]", redacted["API_KEY"])
	assert.Equal(t, "[REDACTED]", redacted["Access_Token"])
	assert.Equal(t, "[REDACTED]", redacted["database_SECRET"])
	assert.Equal(t, "john_doe", redacted["user_name"])
}

func TestRedactSecretsNestedMaps(t *testing.T) {
	realEmitter := &RealEventEmitter{}

	// Test nested map redaction
	fields := map[string]any{
		"config": map[string]any{
			"api_key":    "secret123",
			"user_name":  "john_doe",
			"nested": map[string]any{
				"access_token": "token456",
				"normal_field": "normal_value",
			},
		},
		"top_level_secret": "secret789",
		"normal_field":     "normal_value",
	}

	redacted := realEmitter.redactSecrets(fields)

	// Verify top-level redaction
	assert.Equal(t, "[REDACTED]", redacted["top_level_secret"])
	assert.Equal(t, "normal_value", redacted["normal_field"])

	// Verify nested redaction
	config, ok := redacted["config"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "[REDACTED]", config["api_key"])
	assert.Equal(t, "john_doe", config["user_name"])

	// Verify deeply nested redaction
	nested, ok := config["nested"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "[REDACTED]", nested["access_token"])
	assert.Equal(t, "normal_value", nested["normal_field"])
}

func TestRedactSecretsNilAndEmpty(t *testing.T) {
	realEmitter := &RealEventEmitter{}

	// Test nil input
	redacted := realEmitter.redactSecrets(nil)
	assert.Nil(t, redacted)

	// Test empty map
	redacted = realEmitter.redactSecrets(map[string]any{})
	assert.Empty(t, redacted)
}

func TestRedactSecretsMixedTypes(t *testing.T) {
	realEmitter := &RealEventEmitter{}

	// Test with mixed value types
	fields := map[string]any{
		"api_key":        "secret123",
		"access_token":   12345,
		"database_secret": true,
		"user_name":      "john_doe",
		"count":          42,
	}

	redacted := realEmitter.redactSecrets(fields)

	// Verify secrets are redacted regardless of type
	assert.Equal(t, "[REDACTED]", redacted["api_key"])
	assert.Equal(t, "[REDACTED]", redacted["access_token"])
	assert.Equal(t, "[REDACTED]", redacted["database_secret"])

	// Verify non-secret fields are preserved
	assert.Equal(t, "john_doe", redacted["user_name"])
	assert.Equal(t, 42, redacted["count"])
}

func TestSendLogWithRedaction(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	encoder := ndjson.NewEncoder(&buf, logger)

	emitter := NewRealEventEmitter(encoder, logger, protocol.AgentTypeOrchestration, "test-agent")

	// Test log with secrets
	fields := map[string]any{
		"api_key":        "secret123",
		"access_token":   "token456",
		"user_name":      "john_doe",
		"normal_field":   "normal_value",
	}

	err := emitter.SendLog("info", "test log with secrets", fields)
	require.NoError(t, err)

	// Verify the output contains redacted secrets
	output := buf.String()
	assert.Contains(t, output, `"message":"test log with secrets"`)
	assert.Contains(t, output, `"level":"info"`)
	assert.Contains(t, output, `"api_key":"[REDACTED]"`)
	assert.Contains(t, output, `"access_token":"[REDACTED]"`)
	assert.Contains(t, output, `"user_name":"john_doe"`)
	assert.Contains(t, output, `"normal_field":"normal_value"`)
}

func TestSendLogWithoutFields(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	encoder := ndjson.NewEncoder(&buf, logger)

	emitter := NewRealEventEmitter(encoder, logger, protocol.AgentTypeOrchestration, "test-agent")

	// Test log without fields
	err := emitter.SendLog("info", "simple log message", nil)
	require.NoError(t, err)

	// Verify the output
	output := buf.String()
	assert.Contains(t, output, `"message":"simple log message"`)
	assert.Contains(t, output, `"level":"info"`)
}

func TestSendLogWithEmptyFields(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	encoder := ndjson.NewEncoder(&buf, logger)

	emitter := NewRealEventEmitter(encoder, logger, protocol.AgentTypeOrchestration, "test-agent")

	// Test log with empty fields
	err := emitter.SendLog("warn", "warning message", map[string]any{})
	require.NoError(t, err)

	// Verify the output
	output := buf.String()
	assert.Contains(t, output, `"message":"warning message"`)
	assert.Contains(t, output, `"level":"warn"`)
}

func TestSendLogDifferentLevels(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	encoder := ndjson.NewEncoder(&buf, logger)

	emitter := NewRealEventEmitter(encoder, logger, protocol.AgentTypeOrchestration, "test-agent")

	// Test different log levels
	levels := []string{"debug", "info", "warn", "error"}
	for _, level := range levels {
		err := emitter.SendLog(level, fmt.Sprintf("%s message", level), map[string]any{"level": level})
		require.NoError(t, err)
	}

	// Verify all levels were logged
	output := buf.String()
	for _, level := range levels {
		assert.Contains(t, output, fmt.Sprintf(`"level":"%s"`, level))
		assert.Contains(t, output, fmt.Sprintf(`"message":"%s message"`, level))
	}
}

func TestSendLogMessageSizeLimit(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	encoder := ndjson.NewEncoder(&buf, logger)

	emitter := NewRealEventEmitter(encoder, logger, protocol.AgentTypeOrchestration, "test-agent")

	// Test with a very large message that might approach size limits
	largeMessage := strings.Repeat("x", 10000) // 10KB message
	largeFields := map[string]any{
		"large_data": strings.Repeat("y", 5000), // 5KB field
		"api_key":    "secret123", // Should be redacted
	}

	err := emitter.SendLog("info", largeMessage, largeFields)
	require.NoError(t, err)

	// Verify the output contains the message and redacted secret
	output := buf.String()
	assert.Contains(t, output, largeMessage)
	assert.Contains(t, output, `"api_key":"[REDACTED]"`)
	assert.Contains(t, output, `"large_data":"`+strings.Repeat("y", 5000)+`"`)
}

func TestMockEventEmitterRedaction(t *testing.T) {
	emitter := NewMockEventEmitter()

	// Test that mock emitter also handles redaction
	fields := map[string]any{
		"api_key":      "secret123",
		"user_name":    "john_doe",
		"access_token": "token456",
	}

	err := emitter.SendLog("info", "test log", fields)
	require.NoError(t, err)

	// Verify log was recorded
	logs := emitter.GetLogs()
	assert.Len(t, logs, 1)

	log := logs[0]
	assert.Equal(t, "test log", log.Message)
	assert.Equal(t, "info", string(log.Level))

	// Note: MockEventEmitter doesn't actually redact, but the interface is tested
	// The real redaction happens in RealEventEmitter
	assert.Equal(t, "secret123", log.Fields["api_key"])
	assert.Equal(t, "john_doe", log.Fields["user_name"])
	assert.Equal(t, "token456", log.Fields["access_token"])
}

func TestMockEventEmitterArtifactProducedEvent(t *testing.T) {
	emitter := NewMockEventEmitter()

	cmd := &protocol.Command{
		CorrelationID: "corr-1",
		TaskID:        "T-001",
		Version: protocol.Version{
			SnapshotID: "snap-1",
		},
	}

	// Test artifact.produced event
	artifact := protocol.Artifact{
		Path:   "test/artifact.txt",
		SHA256: "sha256:abc123def456",
		Size:   1024,
	}

	err := emitter.SendArtifactProducedEvent(cmd, artifact)
	require.NoError(t, err)

	// Verify event was recorded
	events := emitter.GetEvents()
	assert.Len(t, events, 1)

	event := events[0]
	assert.Equal(t, "artifact.produced", event.Event)
	assert.Equal(t, "success", event.Status)
	assert.Equal(t, "corr-1", event.CorrelationID)
	assert.Equal(t, "T-001", event.TaskID)
	assert.Equal(t, "snap-1", event.ObservedVersion.SnapshotID)

	// Verify artifact details
	assert.Len(t, event.Artifacts, 1)
	assert.Equal(t, "test/artifact.txt", event.Artifacts[0].Path)
	assert.Equal(t, "sha256:abc123def456", event.Artifacts[0].SHA256)
	assert.Equal(t, int64(1024), event.Artifacts[0].Size)

	// Verify payload
	assert.Equal(t, "Generated artifact", event.Payload["description"])

	// Verify artifact log
	artifactLog := emitter.GetArtifactLog()
	assert.Len(t, artifactLog, 1)
	assert.Contains(t, artifactLog, "SendArtifactProducedEvent(test/artifact.txt)")
}

func TestMockEventEmitterMultipleArtifactProducedEvents(t *testing.T) {
	emitter := NewMockEventEmitter()

	cmd := &protocol.Command{
		CorrelationID: "corr-1",
		TaskID:        "T-001",
		Version: protocol.Version{
			SnapshotID: "snap-1",
		},
	}

	// Test multiple artifact.produced events
	artifacts := []protocol.Artifact{
		{Path: "file1.txt", SHA256: "sha256:abc", Size: 100},
		{Path: "file2.txt", SHA256: "sha256:def", Size: 200},
		{Path: "file3.txt", SHA256: "sha256:ghi", Size: 300},
	}

	for _, artifact := range artifacts {
		err := emitter.SendArtifactProducedEvent(cmd, artifact)
		require.NoError(t, err)
	}

	// Verify all events were recorded
	events := emitter.GetEvents()
	assert.Len(t, events, 3)

	// Check each event
	for i, event := range events {
		assert.Equal(t, "artifact.produced", event.Event)
		assert.Equal(t, "success", event.Status)
		assert.Equal(t, "corr-1", event.CorrelationID)
		assert.Equal(t, "T-001", event.TaskID)
		assert.Equal(t, "snap-1", event.ObservedVersion.SnapshotID)
		assert.Len(t, event.Artifacts, 1)
		assert.Equal(t, artifacts[i].Path, event.Artifacts[0].Path)
		assert.Equal(t, artifacts[i].SHA256, event.Artifacts[0].SHA256)
		assert.Equal(t, artifacts[i].Size, event.Artifacts[0].Size)
	}

	// Verify artifact log
	artifactLog := emitter.GetArtifactLog()
	assert.Len(t, artifactLog, 3)
	assert.Contains(t, artifactLog, "SendArtifactProducedEvent(file1.txt)")
	assert.Contains(t, artifactLog, "SendArtifactProducedEvent(file2.txt)")
	assert.Contains(t, artifactLog, "SendArtifactProducedEvent(file3.txt)")
}

func TestMockEventEmitterArtifactProducedEventWithEmptyArtifact(t *testing.T) {
	emitter := NewMockEventEmitter()

	cmd := &protocol.Command{
		CorrelationID: "corr-1",
		TaskID:        "T-001",
		Version: protocol.Version{
			SnapshotID: "snap-1",
		},
	}

	// Test artifact.produced event with empty artifact
	artifact := protocol.Artifact{
		Path:   "",
		SHA256: "",
		Size:   0,
	}

	err := emitter.SendArtifactProducedEvent(cmd, artifact)
	require.NoError(t, err)

	// Verify event was recorded
	events := emitter.GetEvents()
	assert.Len(t, events, 1)

	event := events[0]
	assert.Equal(t, "artifact.produced", event.Event)
	assert.Equal(t, "success", event.Status)
	assert.Len(t, event.Artifacts, 1)
	assert.Equal(t, "", event.Artifacts[0].Path)
	assert.Equal(t, "", event.Artifacts[0].SHA256)
	assert.Equal(t, int64(0), event.Artifacts[0].Size)
}

func TestRealEventEmitterArtifactProducedEvent(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))
	encoder := ndjson.NewEncoder(&buf, logger)

	emitter := NewRealEventEmitter(encoder, logger, protocol.AgentTypeOrchestration, "test-agent")

	cmd := &protocol.Command{
		CorrelationID: "corr-1",
		TaskID:        "T-001",
		Version: protocol.Version{
			SnapshotID: "snap-1",
		},
	}

	// Test artifact.produced event
	artifact := protocol.Artifact{
		Path:   "test/artifact.txt",
		SHA256: "sha256:abc123def456",
		Size:   1024,
	}

	err := emitter.SendArtifactProducedEvent(cmd, artifact)
	require.NoError(t, err)

	// Verify the output contains proper artifact structure
	output := buf.String()
	assert.Contains(t, output, `"event":"artifact.produced"`)
	assert.Contains(t, output, `"status":"success"`)
	assert.Contains(t, output, `"path":"test/artifact.txt"`)
	assert.Contains(t, output, `"sha256":"sha256:abc123def456"`)
	assert.Contains(t, output, `"size":1024`)
	assert.Contains(t, output, `"description":"Generated artifact"`)
}

func TestRealEventEmitterArtifactProducedEventProtocolCompliance(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))
	encoder := ndjson.NewEncoder(&buf, logger)

	emitter := NewRealEventEmitter(encoder, logger, protocol.AgentTypeOrchestration, "test-agent")

	cmd := &protocol.Command{
		CorrelationID: "corr-1",
		TaskID:        "T-001",
		Version: protocol.Version{
			SnapshotID: "snap-1",
		},
	}

	// Test that all required fields are present
	artifact := protocol.Artifact{
		Path:   "test/artifact.txt",
		SHA256: "sha256:abc123def456",
		Size:   1024,
	}

	err := emitter.SendArtifactProducedEvent(cmd, artifact)
	require.NoError(t, err)

	// Verify the output contains all required protocol fields
	output := buf.String()
	assert.Contains(t, output, `"kind":"event"`)
	assert.Contains(t, output, `"event":"artifact.produced"`)
	assert.Contains(t, output, `"message_id"`)
	assert.Contains(t, output, `"correlation_id":"corr-1"`)
	assert.Contains(t, output, `"task_id":"T-001"`)
	assert.Contains(t, output, `"from":{"agent_type":"orchestration","agent_id":"test-agent"}`)
	assert.Contains(t, output, `"status":"success"`)
	assert.Contains(t, output, `"occurred_at"`)
	assert.Contains(t, output, `"observed_version":{"snapshot_id":"snap-1"}`)
	assert.Contains(t, output, `"artifacts":[{"path":"test/artifact.txt","sha256":"sha256:abc123def456","size":1024}]`)
}
