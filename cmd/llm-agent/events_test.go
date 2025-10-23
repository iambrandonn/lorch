package main

import (
	"testing"

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
