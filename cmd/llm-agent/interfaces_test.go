package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMCallerInterface(t *testing.T) {
	t.Run("RealLLMCaller", func(t *testing.T) {
		// Create a mock LLM CLI script for testing
		mockScript := createMockLLMScript(t)
		defer os.Remove(mockScript)

		config := DefaultLLMConfig(mockScript)
		caller := NewRealLLMCaller(config)

		ctx := context.Background()
		prompt := "Test prompt"

		response, err := caller.Call(ctx, prompt)
		require.NoError(t, err)
		// Trim newlines from the response
		response = strings.TrimSpace(response)
		assert.Contains(t, response, "LLM response to:")
	})

	t.Run("MockLLMCaller", func(t *testing.T) {
		caller := NewMockLLMCaller()

		// Set up mock response
		caller.SetResponse("test prompt", "mock response")

		ctx := context.Background()
		response, err := caller.Call(ctx, "test prompt")
		require.NoError(t, err)
		assert.Equal(t, "mock response", response)
		assert.Equal(t, 1, caller.CallCount())
	})
}

func TestReceiptStoreInterface(t *testing.T) {
	t.Run("MockReceiptStore", func(t *testing.T) {
		store := NewMockReceiptStore()

		// Test SaveReceipt
		receipt := &Receipt{
			TaskID:         "T-001",
			Step:           1,
			IdempotencyKey: "ik-123",
			Artifacts:     []protocol.Artifact{},
			Events:        []string{"event-1"},
			CreatedAt:     time.Now(),
		}

		err := store.SaveReceipt("/receipts/T-001/step-1.json", receipt)
		require.NoError(t, err)

		// Test LoadReceipt
		loaded, err := store.LoadReceipt("/receipts/T-001/step-1.json")
		require.NoError(t, err)
		assert.Equal(t, receipt.TaskID, loaded.TaskID)
		assert.Equal(t, receipt.IdempotencyKey, loaded.IdempotencyKey)

		// Test FindReceiptByIK
		found, path, err := store.FindReceiptByIK("T-001", "implement", "ik-123")
		require.NoError(t, err)
		assert.NotNil(t, found)
		assert.Equal(t, "/receipts/T-001/step-1.json", path)

		// Test call log
		log := store.GetCallLog()
		assert.Contains(t, log, "SaveReceipt(/receipts/T-001/step-1.json)")
		assert.Contains(t, log, "LoadReceipt(/receipts/T-001/step-1.json)")
		assert.Contains(t, log, "FindReceiptByIK(T-001, implement, ik-123)")
	})
}

func TestFSProviderInterface(t *testing.T) {
	t.Run("MockFSProvider", func(t *testing.T) {
		provider := NewMockFSProvider()

		// Test SetFile
		provider.SetFile("/workspace/test.txt", "test content")

		// Test ReadFileSafe
		content, err := provider.ReadFileSafe("/workspace/test.txt", 1024)
		require.NoError(t, err)
		assert.Equal(t, "test content", content)

	// Test WriteArtifactAtomic
	contentBytes := []byte("new content")
	artifact, err := provider.WriteArtifactAtomic("/workspace", "test.txt", contentBytes)
	require.NoError(t, err)
	assert.Equal(t, "test.txt", artifact.Path)
	assert.NotEmpty(t, artifact.SHA256)
	assert.Equal(t, int64(len(contentBytes)), artifact.Size)

	// Test logs
	writeLog := provider.GetWriteLog()
	assert.Contains(t, writeLog, fmt.Sprintf("WriteArtifactAtomic(/workspace, test.txt, %d bytes)", len(contentBytes)))

		readLog := provider.GetReadLog()
		assert.Contains(t, readLog, "ReadFileSafe(/workspace/test.txt, 1024)")
	})
}

func TestEventEmitterInterface(t *testing.T) {
	t.Run("MockEventEmitter", func(t *testing.T) {
		emitter := NewMockEventEmitter()

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

		logs := emitter.GetLogs()
		assert.Len(t, logs, 1)

		// Test call logs
		callLog := emitter.GetCallLog()
		assert.Contains(t, callLog, "NewEvent(test.event)")
		assert.Contains(t, callLog, "SendLog(info, test log)")

		errorLog := emitter.GetErrorLog()
		assert.Contains(t, errorLog, "SendErrorEvent(test_error, test message)")

		artifactLog := emitter.GetArtifactLog()
		assert.Contains(t, artifactLog, "SendArtifactProducedEvent(test.txt)")
	})
}

func TestAgentConfig(t *testing.T) {
	config := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/workspace",
		Logger:    nil, // Will be set in real usage
	}

	assert.Equal(t, protocol.AgentTypeOrchestration, config.Role)
	assert.Equal(t, "claude", config.LLMCLI)
	assert.Equal(t, "/workspace", config.Workspace)
}

func TestNewLLMAgent(t *testing.T) {
	config := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/workspace",
		Logger:    nil,
	}

	agent, err := NewLLMAgent(config)
	require.NoError(t, err)
	assert.NotNil(t, agent)
	assert.Equal(t, config.Role, agent.config.Role)
	assert.Equal(t, config.LLMCLI, agent.config.LLMCLI)
	assert.Equal(t, config.Workspace, agent.config.Workspace)
}

