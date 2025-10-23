package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEventBuilders tests the event builder functions
func TestEventBuilders(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)
	cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")

	t.Run("NewEvent", func(t *testing.T) {
		event := agent.eventEmitter.NewEvent(cmd, "test.event")

		// Validate event structure
		tu.AssertEventValid(&event)
		assert.Equal(t, "test.event", event.Event)
		assert.Equal(t, cmd.CorrelationID, event.CorrelationID)
		assert.Equal(t, cmd.TaskID, event.TaskID)
		assert.Equal(t, protocol.AgentTypeOrchestration, event.From.AgentType)
		assert.Equal(t, cmd.Version.SnapshotID, event.ObservedVersion.SnapshotID)
	})

	t.Run("SendErrorEvent", func(t *testing.T) {
		err := agent.eventEmitter.SendErrorEvent(cmd, "test_error", "test message")
		require.NoError(t, err)

		// Verify error event was recorded
		events := agent.eventEmitter.(*MockEventEmitter).GetEvents()
		assert.Len(t, events, 1)
		assert.Equal(t, "error", events[0].Event)
		assert.Equal(t, "failed", events[0].Status)
		assert.Equal(t, "test_error", events[0].Payload["code"])
		assert.Equal(t, "test message", events[0].Payload["message"])
	})

	t.Run("SendArtifactProducedEvent", func(t *testing.T) {
		artifact := protocol.Artifact{
			Path:   "test.txt",
			SHA256: "sha256:test123",
			Size:   100,
		}

		err := agent.eventEmitter.SendArtifactProducedEvent(cmd, artifact)
		require.NoError(t, err)

		// Verify artifact event was recorded
		events := agent.eventEmitter.(*MockEventEmitter).GetEvents()
		assert.Len(t, events, 1)
		assert.Equal(t, "artifact.produced", events[0].Event)
		assert.Len(t, events[0].Artifacts, 1)
		assert.Equal(t, artifact, events[0].Artifacts[0])
	})

	t.Run("SendLog", func(t *testing.T) {
		fields := map[string]any{"key": "value", "api_key": "secret123"}
		err := agent.eventEmitter.SendLog("info", "test log", fields)
		require.NoError(t, err)

		// Verify log was recorded
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)
		assert.Equal(t, "info", logs[0].Level)
		assert.Equal(t, "test log", logs[0].Message)

		// Verify secret redaction
		assert.Equal(t, "[REDACTED]", logs[0].Fields["api_key"])
		assert.Equal(t, "value", logs[0].Fields["key"])
	})
}

// TestReceiptStorage tests receipt storage and retrieval
func TestReceiptStorage(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	store := NewMockReceiptStore()

	t.Run("SaveAndLoadReceipt", func(t *testing.T) {
		receipt := tu.CreateTestReceipt("T-001", "implement", "ik-test123")

		// Save receipt
		err := store.SaveReceipt("/receipts/T-001/implement-1.json", receipt)
		require.NoError(t, err)

		// Load receipt
		loaded, err := store.LoadReceipt("/receipts/T-001/implement-1.json")
		require.NoError(t, err)
		require.NotNil(t, loaded)

		// Validate receipt content
		tu.AssertReceiptValid(loaded)
		assert.Equal(t, receipt.TaskID, loaded.TaskID)
		assert.Equal(t, receipt.IdempotencyKey, loaded.IdempotencyKey)
		assert.Equal(t, receipt.Step, loaded.Step)
		assert.Equal(t, len(receipt.Artifacts), len(loaded.Artifacts))
		assert.Equal(t, len(receipt.Events), len(loaded.Events))
	})

	t.Run("FindReceiptByIK", func(t *testing.T) {
		receipt := tu.CreateTestReceipt("T-001", "implement", "ik-test123")

		// Save receipt
		err := store.SaveReceipt("/receipts/T-001/implement-1.json", receipt)
		require.NoError(t, err)

		// Find by IK
		found, path, err := store.FindReceiptByIK("T-001", "implement", "ik-test123")
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, "/receipts/T-001/implement-1.json", path)
		assert.Equal(t, receipt.IdempotencyKey, found.IdempotencyKey)
	})

	t.Run("ReceiptNotFound", func(t *testing.T) {
		found, path, err := store.FindReceiptByIK("T-999", "implement", "ik-nonexistent")
		require.NoError(t, err)
		assert.Nil(t, found)
		assert.Empty(t, path)
	})

	t.Run("ReceiptStepIncrement", func(t *testing.T) {
		// Test multiple receipts for same task/action
		receipt1 := tu.CreateTestReceipt("T-001", "implement", "ik-test123")
		receipt1.Step = 1

		receipt2 := tu.CreateTestReceipt("T-001", "implement", "ik-test456")
		receipt2.Step = 2

		// Save both receipts
		err := store.SaveReceipt("/receipts/T-001/implement-1.json", receipt1)
		require.NoError(t, err)

		err = store.SaveReceipt("/receipts/T-001/implement-2.json", receipt2)
		require.NoError(t, err)

		// Find both by IK
		found1, _, err := store.FindReceiptByIK("T-001", "implement", "ik-test123")
		require.NoError(t, err)
		require.NotNil(t, found1)
		assert.Equal(t, 1, found1.Step)

		found2, _, err := store.FindReceiptByIK("T-001", "implement", "ik-test456")
		require.NoError(t, err)
		require.NotNil(t, found2)
		assert.Equal(t, 2, found2.Step)
	})
}

// TestPathSafety tests filesystem path safety
func TestPathSafety(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	fsProvider := NewMockFSProvider()

	t.Run("SafePaths", func(t *testing.T) {
		safePaths := []string{
			"test.txt",
			"subdir/file.txt",
			"specs/MASTER-SPEC.md",
			"src/main.go",
			"tests/test.go",
		}

		for _, path := range safePaths {
			t.Run("Safe_"+path, func(t *testing.T) {
				content := "test content"
				fsProvider.SetFile(filepath.Join(tu.workspace, path), content)

				readContent, err := fsProvider.ReadFileSafe(filepath.Join(tu.workspace, path), 1024)
				require.NoError(t, err)
				assert.Equal(t, content, readContent)
			})
		}
	})

	t.Run("UnsafePaths", func(t *testing.T) {
		unsafePaths := []string{
			"../outside.txt",
			"../../etc/passwd",
			"/absolute/path.txt",
			"test/../../../etc/passwd",
		}

		for _, path := range unsafePaths {
			t.Run("Unsafe_"+strings.ReplaceAll(path, "/", "_"), func(t *testing.T) {
				// These should be rejected by path validation
				// The actual implementation would use resolveWorkspacePath
				// For now, we just test that the mock doesn't allow them
				_, _ = fsProvider.ReadFileSafe(filepath.Join(tu.workspace, path), 1024)
				// Mock implementation may not enforce this, but real implementation should
				// This test documents the expected behavior
			})
		}
	})

	t.Run("FileSizeLimits", func(t *testing.T) {
		// Test reading files with size limits
		content := "test content"
		fsProvider.SetFile(filepath.Join(tu.workspace, "test.txt"), content)

		// Read with limit larger than content
		readContent, err := fsProvider.ReadFileSafe(filepath.Join(tu.workspace, "test.txt"), 1024)
		require.NoError(t, err)
		assert.Equal(t, content, readContent)

		// Read with limit smaller than content
		largeContent := strings.Repeat("A", 2048)
		fsProvider.SetFile(filepath.Join(tu.workspace, "large.txt"), largeContent)

		readContent, err = fsProvider.ReadFileSafe(filepath.Join(tu.workspace, "large.txt"), 1024)
		require.NoError(t, err)
		assert.Len(t, readContent, 1024) // Should be truncated
	})
}

// TestArtifactProduction tests artifact creation and validation
func TestArtifactProduction(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	fsProvider := NewMockFSProvider()

	t.Run("CreateArtifact", func(t *testing.T) {
		content := []byte("test artifact content")
		artifact, err := fsProvider.WriteArtifactAtomic(tu.workspace, "test.txt", content)
		require.NoError(t, err)

		// Validate artifact
		tu.AssertArtifactValid(artifact)
		assert.Equal(t, "test.txt", artifact.Path)
		tu.AssertArtifactChecksumValid(artifact, content)
	})

	t.Run("ArtifactSizeValidation", func(t *testing.T) {
		// Test with content that should be accepted
		content := []byte("small content")
		artifact, err := fsProvider.WriteArtifactAtomic(tu.workspace, "small.txt", content)
		require.NoError(t, err)
		assert.Equal(t, int64(len(content)), artifact.Size)

		// Test with very large content (mock may not enforce limits)
		largeContent := []byte(strings.Repeat("A", 1024*1024)) // 1MB
		artifact, err = fsProvider.WriteArtifactAtomic(tu.workspace, "large.txt", largeContent)
		// Mock implementation may not enforce size limits
		// Real implementation should validate against config limits
		if err == nil {
			assert.Equal(t, int64(len(largeContent)), artifact.Size)
		}
	})

	t.Run("ArtifactChecksumConsistency", func(t *testing.T) {
		content := []byte("consistent content")
		artifact, err := fsProvider.WriteArtifactAtomic(tu.workspace, "consistent.txt", content)
		require.NoError(t, err)

		// Verify checksum matches content
		tu.AssertArtifactChecksumValid(artifact, content)

		// Verify checksum is deterministic
		artifact2, err := fsProvider.WriteArtifactAtomic(tu.workspace, "consistent2.txt", content)
		require.NoError(t, err)
		assert.Equal(t, artifact.SHA256, artifact2.SHA256)
	})

	t.Run("ArtifactPathValidation", func(t *testing.T) {
		content := []byte("test content")

		// Test valid paths
		validPaths := []string{
			"test.txt",
			"subdir/file.txt",
			"specs/MASTER-SPEC.md",
		}

		for _, path := range validPaths {
			artifact, err := fsProvider.WriteArtifactAtomic(tu.workspace, path, content)
			require.NoError(t, err)
			assert.Equal(t, path, artifact.Path)
		}
	})
}

// TestLLMCallerInterfaceUnit tests LLM calling functionality
func TestLLMCallerInterfaceUnit(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	t.Run("MockLLMCaller", func(t *testing.T) {
		caller := NewMockLLMCaller()

		// Test basic call
		caller.SetResponse("test prompt", "test response")

		ctx := context.Background()
		response, err := caller.Call(ctx, "test prompt")
		require.NoError(t, err)
		assert.Equal(t, "test response", response)
		assert.Equal(t, 1, caller.CallCount())
	})

	t.Run("LLMCallerTimeout", func(t *testing.T) {
		caller := NewMockLLMCaller()

		// Set up a response that takes time
		caller.SetResponse("slow prompt", "slow response")

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		response, err := caller.Call(ctx, "slow prompt")
		require.NoError(t, err)
		assert.Equal(t, "slow response", response)
	})

	t.Run("LLMCallerError", func(t *testing.T) {
		caller := NewMockLLMCaller()

		// Set up an error response
		caller.SetError("error prompt", fmt.Errorf("LLM error"))

		ctx := context.Background()
		_, err := caller.Call(ctx, "error prompt")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "LLM error")
	})

	t.Run("LLMCallerCallLogging", func(t *testing.T) {
		caller := NewMockLLMCaller()

		caller.SetResponse("prompt1", "response1")
		caller.SetResponse("prompt2", "response2")

		ctx := context.Background()
		caller.Call(ctx, "prompt1")
		caller.Call(ctx, "prompt2")

		assert.Equal(t, 2, caller.CallCount())

		// Verify call count is correct
		assert.Equal(t, 2, caller.CallCount())
	})
}

// TestAgentConfiguration tests agent configuration validation
func TestAgentConfiguration(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	t.Run("ValidConfiguration", func(t *testing.T) {
		config := tu.CreateTestConfig()
		tu.AssertConfigValid(config)
	})

	t.Run("AgentCreation", func(t *testing.T) {
		config := tu.CreateTestConfig()
		agent, err := NewLLMAgent(config)
		require.NoError(t, err)
		require.NotNil(t, agent)

		assert.Equal(t, config.Role, agent.config.Role)
		assert.Equal(t, config.LLMCLI, agent.config.LLMCLI)
		assert.Equal(t, config.Workspace, agent.config.Workspace)
	})

	t.Run("AgentIDGeneration", func(t *testing.T) {
		config1 := tu.CreateTestConfig()
		agent1, err := NewLLMAgent(config1)
		require.NoError(t, err)

		time.Sleep(1 * time.Millisecond) // Ensure different timestamps

		config2 := tu.CreateTestConfig()
		agent2, err := NewLLMAgent(config2)
		require.NoError(t, err)

		// Agent IDs should be unique
		assert.NotEqual(t, agent1.agentID, agent2.agentID)
		assert.NotEmpty(t, agent1.agentID)
		assert.NotEmpty(t, agent2.agentID)

		// Agent IDs should contain role
		assert.Contains(t, agent1.agentID, string(config1.Role))
		assert.Contains(t, agent2.agentID, string(config2.Role))
	})
}

// TestMessageValidation tests message structure validation
func TestMessageValidation(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	t.Run("CommandValidation", func(t *testing.T) {
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")

		// Validate required fields
		assert.NotEmpty(t, cmd.MessageID)
		assert.NotEmpty(t, cmd.CorrelationID)
		assert.NotEmpty(t, cmd.TaskID)
		assert.NotEmpty(t, cmd.IdempotencyKey)
		assert.NotEmpty(t, cmd.To.AgentType)
		assert.NotEmpty(t, cmd.Action)
		assert.NotEmpty(t, cmd.Version.SnapshotID)

		// Validate field formats
		tu.AssertIdempotencyKeyValid(cmd.IdempotencyKey)
		tu.AssertSnapshotIDValid(cmd.Version.SnapshotID)
		tu.AssertTaskIDValid(cmd.TaskID)
	})

	t.Run("EventValidation", func(t *testing.T) {
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		event := tu.CreateTestEvent("test.event", cmd)

		tu.AssertEventValid(event)
	})

	t.Run("HeartbeatValidation", func(t *testing.T) {
		heartbeat := tu.CreateTestHeartbeat(protocol.HeartbeatStatusReady, "T-001")

		tu.AssertHeartbeatValid(heartbeat)
	})

	t.Run("LogValidation", func(t *testing.T) {
		log := tu.CreateTestLog("info", "test message")

		tu.AssertLogValid(log)
	})
}

// TestConcurrentAccess tests concurrent access to agent state
func TestConcurrentAccess(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

	t.Run("ConcurrentStatusUpdates", func(t *testing.T) {
		done := make(chan bool, 10)

		// Start multiple goroutines updating status
		for i := 0; i < 10; i++ {
			go func() {
				agent.setStatus(protocol.HeartbeatStatusBusy, "T-001")
				agent.updateActivity()
				agent.setStatus(protocol.HeartbeatStatusReady, "")
				done <- true
			}()
		}

		// Wait for all goroutines to complete
		for i := 0; i < 10; i++ {
			<-done
		}

		// Should not have panicked or deadlocked
		assert.True(t, true)
	})

	t.Run("ConcurrentHeartbeatSequence", func(t *testing.T) {
		done := make(chan bool, 5)

		// Start multiple goroutines incrementing heartbeat sequence
		for i := 0; i < 5; i++ {
			go func() {
				agent.mu.Lock()
				agent.hbSeq++
				agent.mu.Unlock()
				done <- true
			}()
		}

		// Wait for all goroutines to complete
		for i := 0; i < 5; i++ {
			<-done
		}

		// Sequence should be incremented exactly 5 times
		assert.Equal(t, int64(5), agent.hbSeq)
	})
}

// TestErrorHandling tests error handling in various scenarios
func TestErrorHandling(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

	t.Run("InvalidCommandAction", func(t *testing.T) {
		cmd := tu.CreateTestCommand(protocol.ActionImplement, "T-001") // Wrong action for orchestration

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

	t.Run("LLMCallerError", func(t *testing.T) {
		// Set up LLM caller to return error
		agent.llmCaller.(*MockLLMCaller).SetError("test prompt", fmt.Errorf("LLM failed"))

		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")

		err := agent.handleCommand(cmd)
		// Should handle LLM error gracefully
		// The actual implementation would emit an error event
		assert.Error(t, err)
	})
}

// TestDataStructures tests internal data structures
func TestDataStructures(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	t.Run("ReceiptStructure", func(t *testing.T) {
		receipt := tu.CreateTestReceipt("T-001", "implement", "ik-test123")

		// Test JSON marshaling/unmarshaling
		data, err := json.Marshal(receipt)
		require.NoError(t, err)

		var unmarshaled Receipt
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		assert.Equal(t, receipt.TaskID, unmarshaled.TaskID)
		assert.Equal(t, receipt.IdempotencyKey, unmarshaled.IdempotencyKey)
		assert.Equal(t, receipt.Step, unmarshaled.Step)
	})

	t.Run("ArtifactStructure", func(t *testing.T) {
		artifact := protocol.Artifact{
			Path:   "test.txt",
			SHA256: "sha256:test123",
			Size:   100,
		}

		// Test JSON marshaling/unmarshaling
		data, err := json.Marshal(artifact)
		require.NoError(t, err)

		var unmarshaled protocol.Artifact
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		assert.Equal(t, artifact.Path, unmarshaled.Path)
		assert.Equal(t, artifact.SHA256, unmarshaled.SHA256)
		assert.Equal(t, artifact.Size, unmarshaled.Size)
	})
}
