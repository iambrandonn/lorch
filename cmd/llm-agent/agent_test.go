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
	assert.Equal(t, int64(0), agent.hbSeq)
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

func TestLLMAgentHeartbeatLifecycle(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    createTestLogger(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Test complete heartbeat lifecycle: starting → ready → busy → ready → stopping
	assert.Equal(t, protocol.HeartbeatStatusStarting, agent.currentStatus)
	assert.Empty(t, agent.currentTaskID)

	// Transition to ready
	agent.setStatus(protocol.HeartbeatStatusReady, "")
	assert.Equal(t, protocol.HeartbeatStatusReady, agent.currentStatus)
	assert.Empty(t, agent.currentTaskID)

	// Transition to busy with task
	agent.setStatus(protocol.HeartbeatStatusBusy, "T-001")
	assert.Equal(t, protocol.HeartbeatStatusBusy, agent.currentStatus)
	assert.Equal(t, "T-001", agent.currentTaskID)

	// Transition back to ready
	agent.setStatus(protocol.HeartbeatStatusReady, "")
	assert.Equal(t, protocol.HeartbeatStatusReady, agent.currentStatus)
	assert.Empty(t, agent.currentTaskID)

	// Transition to stopping
	agent.setStatus(protocol.HeartbeatStatusStopping, "")
	assert.Equal(t, protocol.HeartbeatStatusStopping, agent.currentStatus)
	assert.Empty(t, agent.currentTaskID)
}

func TestLLMAgentHeartbeatSequenceIncrement(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    createTestLogger(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Test that heartbeat sequence increments properly
	initialSeq := agent.hbSeq
	assert.Equal(t, int64(0), initialSeq)

	// Simulate multiple heartbeat sends
	for i := 0; i < 5; i++ {
		agent.mu.Lock()
		agent.hbSeq++
		seq := agent.hbSeq
		agent.mu.Unlock()
		assert.Equal(t, int64(i+1), seq)
	}
}

func TestLLMAgentHeartbeatFieldsInitialization(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    createTestLogger(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Test that all required heartbeat fields are properly initialized
	assert.NotEmpty(t, agent.agentID)
	assert.True(t, agent.startTime.Before(time.Now()) || agent.startTime.Equal(time.Now()))
	assert.True(t, agent.lastActivityAt.Before(time.Now()) || agent.lastActivityAt.Equal(time.Now()))
	assert.Equal(t, int64(0), agent.hbSeq)
	assert.Equal(t, protocol.HeartbeatStatusStarting, agent.currentStatus)
	assert.Empty(t, agent.currentTaskID)
}

func TestLLMAgentActivityTracking(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    createTestLogger(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Test activity tracking
	initialActivity := agent.lastActivityAt

	// Wait a bit and update activity
	time.Sleep(10 * time.Millisecond)
	agent.updateActivity()

	assert.True(t, agent.lastActivityAt.After(initialActivity))

	// Test that setStatus also updates activity
	time.Sleep(10 * time.Millisecond)
	agent.setStatus(protocol.HeartbeatStatusBusy, "T-001")

	assert.True(t, agent.lastActivityAt.After(initialActivity))
}

func TestLLMAgentHeartbeatContinuity(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    createTestLogger(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Test that heartbeats continue during long operations
	// This simulates the scenario where an agent is busy for a long time
	agent.setStatus(protocol.HeartbeatStatusBusy, "T-001")

	// Simulate activity updates during long operation
	for i := 0; i < 5; i++ {
		time.Sleep(5 * time.Millisecond)
		agent.updateActivity()
	}

	// Status should still be busy
	assert.Equal(t, protocol.HeartbeatStatusBusy, agent.currentStatus)
	assert.Equal(t, "T-001", agent.currentTaskID)

	// Activity should be recent
	assert.True(t, time.Since(agent.lastActivityAt) < 100*time.Millisecond)
}

func TestLLMAgentAgentIDGeneration(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    createTestLogger(),
	}

	agent1, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Wait a bit to ensure different timestamps
	time.Sleep(1 * time.Millisecond)

	agent2, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Agent IDs should be unique
	assert.NotEqual(t, agent1.agentID, agent2.agentID)
	assert.NotEmpty(t, agent1.agentID)
	assert.NotEmpty(t, agent2.agentID)

	// Agent IDs should contain the role
	assert.Contains(t, agent1.agentID, string(cfg.Role))
	assert.Contains(t, agent2.agentID, string(cfg.Role))
}

func TestLLMAgentHeartbeatStatusTransitions(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    createTestLogger(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Test all valid status transitions
	statuses := []protocol.HeartbeatStatus{
		protocol.HeartbeatStatusStarting,
		protocol.HeartbeatStatusReady,
		protocol.HeartbeatStatusBusy,
		protocol.HeartbeatStatusReady,
		protocol.HeartbeatStatusStopping,
	}

	for i, status := range statuses {
		agent.setStatus(status, "")
		assert.Equal(t, status, agent.currentStatus, "Status transition %d failed", i)
	}

	// Test busy status with task ID
	agent.setStatus(protocol.HeartbeatStatusBusy, "T-001")
	assert.Equal(t, protocol.HeartbeatStatusBusy, agent.currentStatus)
	assert.Equal(t, "T-001", agent.currentTaskID)

	// Test backoff status
	agent.setStatus(protocol.HeartbeatStatusBackoff, "")
	assert.Equal(t, protocol.HeartbeatStatusBackoff, agent.currentStatus)
}
