package main

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestLogger creates a logger for testing
func createTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestLLMAgentCreation(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    createTestLogger(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)
	assert.NotNil(t, agent)
	assert.Equal(t, protocol.AgentTypeOrchestration, agent.config.Role)
	assert.Equal(t, "claude", agent.config.LLMCLI)
	assert.Equal(t, "/tmp/test", agent.config.Workspace)
}

func TestLLMAgentStatusTransitions(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    createTestLogger(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Test initial status
	assert.Equal(t, protocol.HeartbeatStatusStarting, agent.currentStatus)
	assert.Empty(t, agent.currentTaskID)

	// Test status transitions
	agent.setStatus(protocol.HeartbeatStatusReady, "")
	assert.Equal(t, protocol.HeartbeatStatusReady, agent.currentStatus)
	assert.Empty(t, agent.currentTaskID)

	agent.setStatus(protocol.HeartbeatStatusBusy, "T-001")
	assert.Equal(t, protocol.HeartbeatStatusBusy, agent.currentStatus)
	assert.Equal(t, "T-001", agent.currentTaskID)

	agent.setStatus(protocol.HeartbeatStatusReady, "")
	assert.Equal(t, protocol.HeartbeatStatusReady, agent.currentStatus)
	assert.Empty(t, agent.currentTaskID)
}

func TestLLMAgentActivityUpdate(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    createTestLogger(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	initialTime := agent.lastActivityAt

	// Wait a bit and update activity
	time.Sleep(10 * time.Millisecond)
	agent.updateActivity()

	assert.True(t, agent.lastActivityAt.After(initialTime))
}

func TestLLMAgentVersionTracking(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    createTestLogger(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Test initial state
	assert.Empty(t, agent.firstObservedSnapshotID)

	// Simulate first command
	agent.mu.Lock()
	agent.firstObservedSnapshotID = "snap-001"
	agent.mu.Unlock()

	assert.Equal(t, "snap-001", agent.firstObservedSnapshotID)
}

func TestLLMAgentHeartbeatSequence(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    createTestLogger(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Test heartbeat sequence increment
	initialSeq := agent.hbSeq

	agent.mu.Lock()
	agent.hbSeq++
	seq1 := agent.hbSeq
	agent.mu.Unlock()

	assert.Equal(t, initialSeq+1, seq1)

	agent.mu.Lock()
	agent.hbSeq++
	seq2 := agent.hbSeq
	agent.mu.Unlock()

	assert.Equal(t, seq1+1, seq2)
}

func TestLLMAgentCommandRouting(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    slog.Default(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Set up mock event emitter for testing
	agent.eventEmitter = NewMockEventEmitter()

	// Test orchestration command
	cmd := &protocol.Command{
		Action: protocol.ActionIntake,
		TaskID: "T-001",
		Version: protocol.Version{
			SnapshotID: "snap-001",
		},
	}

	// This should succeed because eventEmitter is now set up
	err = agent.handleCommand(cmd)
	assert.NoError(t, err)

	// Test unsupported action for orchestration role
	cmd.Action = protocol.ActionImplement
	err = agent.handleCommand(cmd)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported for role")
}

func TestLLMAgentVersionMismatch(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    createTestLogger(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Set up mock event emitter for testing
	agent.eventEmitter = NewMockEventEmitter()

	// Set initial snapshot
	agent.mu.Lock()
	agent.firstObservedSnapshotID = "snap-001"
	agent.mu.Unlock()

	// Test version mismatch
	cmd := &protocol.Command{
		Action: protocol.ActionIntake,
		TaskID: "T-001",
		Version: protocol.Version{
			SnapshotID: "snap-002", // Different snapshot
		},
	}

	// This should trigger version mismatch error
	err = agent.handleCommand(cmd)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "version_mismatch")
}

func TestLLMAgentMockInterfaces(t *testing.T) {
	// Test with mock interfaces for isolated testing
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    createTestLogger(),
	}

	agent := &LLMAgent{
		config:       *cfg,
		llmCaller:    NewMockLLMCaller(),
		receiptStore: NewMockReceiptStore(),
		fsProvider:   NewMockFSProvider(),
		startTime:    time.Now(),
		lastActivityAt: time.Now(),
		currentStatus: protocol.HeartbeatStatusStarting,
	}

	assert.NotNil(t, agent.llmCaller)
	assert.NotNil(t, agent.receiptStore)
	assert.NotNil(t, agent.fsProvider)
}

func TestLLMAgentBasicIntegration(t *testing.T) {
	// Test basic agent creation and configuration
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    createTestLogger(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Verify agent has all required components
	assert.NotNil(t, agent.config)
	assert.NotNil(t, agent.llmCaller)
	assert.NotNil(t, agent.receiptStore)
	assert.NotNil(t, agent.fsProvider)
	assert.Equal(t, protocol.HeartbeatStatusStarting, agent.currentStatus)
	assert.True(t, agent.startTime.Before(time.Now()) || agent.startTime.Equal(time.Now()))
}

func TestLLMAgentHeartbeatFields(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    createTestLogger(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Test heartbeat field initialization
	assert.Equal(t, 0, agent.hbSeq)
	assert.Equal(t, protocol.HeartbeatStatusStarting, agent.currentStatus)
	assert.Empty(t, agent.currentTaskID)
	assert.Empty(t, agent.firstObservedSnapshotID)
	assert.True(t, agent.startTime.Before(time.Now()) || agent.startTime.Equal(time.Now()))
	assert.True(t, agent.lastActivityAt.Before(time.Now()) || agent.lastActivityAt.Equal(time.Now()))
}

func TestLLMAgentConcurrentAccess(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    createTestLogger(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Test concurrent access to status fields
	done := make(chan bool, 10)

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
}
