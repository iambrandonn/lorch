package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/ndjson"
	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMAgentNDJSONIO(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    slog.Default(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Create mock stdin with a command
	cmd := protocol.Command{
		Kind:          protocol.MessageKindCommand,
		MessageID:     "msg-001",
		CorrelationID: "corr-001",
		TaskID:        "T-001",
		IdempotencyKey: "ik-001",
		To: protocol.AgentRef{
			AgentType: protocol.AgentTypeOrchestration,
		},
		Action: protocol.ActionIntake,
		Inputs: map[string]any{
			"user_instruction": "Test instruction",
		},
		Version: protocol.Version{
			SnapshotID: "snap-001",
		},
		Deadline: time.Now().Add(180 * time.Second),
		Retry: protocol.Retry{
			Attempt:     0,
			MaxAttempts: 3,
		},
		Priority: 5,
	}

	// Marshal command to JSON
	cmdJSON, err := json.Marshal(cmd)
	require.NoError(t, err)

	// Create stdin with the command
	stdin := strings.NewReader(string(cmdJSON) + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run agent (this should succeed with mock orchestration response)
	err = agent.Run(ctx, stdin, &stdout, &stderr)

	// The agent should complete successfully with mock orchestration response
	assert.NoError(t, err)

	// Check that some output was produced (heartbeats, etc.)
	output := stdout.String()
	assert.NotEmpty(t, output)

	// Verify output contains valid JSON lines
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if line != "" {
			var msg map[string]any
			err := json.Unmarshal([]byte(line), &msg)
			assert.NoError(t, err, "Invalid JSON in output: %s", line)
		}
	}
}

func TestLLMAgentHeartbeatOutput(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    nil,
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Test heartbeat generation
	var stdout bytes.Buffer
	agent.encoder = ndjson.NewEncoder(&stdout, nil)

	// Send a heartbeat
	err = agent.sendHeartbeat(protocol.HeartbeatStatusReady, "")
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
	assert.Equal(t, int64(1), hb.Seq)
	assert.Greater(t, hb.PID, 0)
	assert.GreaterOrEqual(t, hb.UptimeS, 0.0)
	assert.NotEmpty(t, hb.LastActivityAt)
}

func TestLLMAgentCommandParsing(t *testing.T) {
	// Test command parsing with various inputs
	cmdJSON := `{
		"kind": "command",
		"message_id": "msg-001",
		"correlation_id": "corr-001",
		"task_id": "T-001",
		"idempotency_key": "ik-001",
		"to": {
			"agent_type": "orchestration"
		},
		"action": "intake",
		"inputs": {
			"user_instruction": "Test instruction"
		},
		"version": {
			"snapshot_id": "snap-001"
		},
		"deadline": "2025-12-31T23:59:59Z",
		"retry": {
			"attempt": 0,
			"max_attempts": 3
		},
		"priority": 5
	}`

	var cmd protocol.Command
	err := json.Unmarshal([]byte(cmdJSON), &cmd)
	require.NoError(t, err)

	assert.Equal(t, protocol.MessageKindCommand, cmd.Kind)
	assert.Equal(t, "msg-001", cmd.MessageID)
	assert.Equal(t, "corr-001", cmd.CorrelationID)
	assert.Equal(t, "T-001", cmd.TaskID)
	assert.Equal(t, "ik-001", cmd.IdempotencyKey)
	assert.Equal(t, protocol.AgentTypeOrchestration, cmd.To.AgentType)
	assert.Equal(t, protocol.ActionIntake, cmd.Action)
	assert.Equal(t, "snap-001", cmd.Version.SnapshotID)
	assert.Equal(t, 5, cmd.Priority)
}

func TestLLMAgentErrorHandling(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    slog.Default(),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Test with invalid JSON input
	invalidJSON := `{"invalid": json}`
	stdin := strings.NewReader(invalidJSON + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// This should fail due to invalid JSON
	err = agent.Run(ctx, stdin, &stdout, &stderr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode command")
}

func TestLLMAgentEmptyInput(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    nil,
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Test with empty input (EOF)
	stdin := strings.NewReader("")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// This should return nil (clean shutdown) on EOF
	err = agent.Run(ctx, stdin, &stdout, &stderr)
	assert.NoError(t, err) // EOF should be handled gracefully
}

func TestLLMAgentContextCancellation(t *testing.T) {
	cfg := &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "claude",
		Workspace: "/tmp/test",
		Logger:    nil,
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(t, err)

	// Test with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	stdin := strings.NewReader("")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// This should return nil due to context cancellation
	err = agent.Run(ctx, stdin, &stdout, &stderr)
	assert.NoError(t, err) // Context cancellation should be handled gracefully
}