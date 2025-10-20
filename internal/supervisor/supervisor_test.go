package supervisor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/iambrandonn/lorch/internal/protocol"
)

func TestSupervisorStartStop(t *testing.T) {
	// Build mock agent if not already built
	mockAgentPath, err := buildMockAgent(t)
	if err != nil {
		t.Fatalf("failed to build mock agent: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	supervisor := NewAgentSupervisor(
		protocol.AgentTypeBuilder,
		[]string{mockAgentPath, "-type", "builder", "-no-heartbeat"},
		map[string]string{},
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start agent
	if err := supervisor.Start(ctx); err != nil {
		t.Fatalf("failed to start agent: %v", err)
	}

	// Verify it's running
	if !supervisor.IsRunning() {
		t.Error("agent should be running")
	}

	// Give it a moment to initialize
	time.Sleep(100 * time.Millisecond)

	// Stop agent
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := supervisor.Stop(stopCtx); err != nil {
		t.Errorf("failed to stop agent: %v", err)
	}

	// Verify it's stopped
	if supervisor.IsRunning() {
		t.Error("agent should not be running")
	}
}

func TestSupervisorSendCommand(t *testing.T) {
	mockAgentPath, err := buildMockAgent(t)
	if err != nil {
		t.Fatalf("failed to build mock agent: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	supervisor := NewAgentSupervisor(
		protocol.AgentTypeBuilder,
		[]string{mockAgentPath, "-type", "builder", "-no-heartbeat"},
		map[string]string{},
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := supervisor.Start(ctx); err != nil {
		t.Fatalf("failed to start agent: %v", err)
	}
	defer supervisor.Stop(context.Background())

	// Send implement command
	cmd := &protocol.Command{
		Kind:           protocol.MessageKindCommand,
		MessageID:      uuid.New().String(),
		CorrelationID:  "test-corr-1",
		TaskID:         "T-TEST-001",
		IdempotencyKey: "pending-ik:test",
		To: protocol.AgentRef{
			AgentType: protocol.AgentTypeBuilder,
		},
		Action:          protocol.ActionImplement,
		Inputs:          map[string]any{"goal": "test"},
		ExpectedOutputs: []protocol.ExpectedOutput{},
		Version: protocol.Version{
			SnapshotID: "snap-test-0001",
		},
		Deadline: time.Now().Add(1 * time.Minute).UTC(),
		Retry: protocol.Retry{
			Attempt:     0,
			MaxAttempts: 3,
		},
		Priority: 5,
	}

	if err := supervisor.SendCommand(cmd); err != nil {
		t.Fatalf("failed to send command: %v", err)
	}

	// Wait for response
	timeout := time.After(5 * time.Second)
	var gotBuilderCompleted bool

	for !gotBuilderCompleted {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for builder.completed")
		case evt, ok := <-supervisor.Events():
			if !ok {
				t.Fatal("events channel closed unexpectedly")
			}

			if evt.Event == protocol.EventBuilderCompleted {
				gotBuilderCompleted = true

				if evt.TaskID != cmd.TaskID {
					t.Errorf("task_id mismatch: got %s, want %s", evt.TaskID, cmd.TaskID)
				}

				if evt.CorrelationID != cmd.CorrelationID {
					t.Errorf("correlation_id mismatch: got %s, want %s", evt.CorrelationID, cmd.CorrelationID)
				}
			}
		}
	}
}

func TestSupervisorHeartbeats(t *testing.T) {
	mockAgentPath, err := buildMockAgent(t)
	if err != nil {
		t.Fatalf("failed to build mock agent: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	supervisor := NewAgentSupervisor(
		protocol.AgentTypeBuilder,
		[]string{mockAgentPath, "-type", "builder", "-heartbeat-interval", "100ms"},
		map[string]string{},
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := supervisor.Start(ctx); err != nil {
		t.Fatalf("failed to start agent: %v", err)
	}
	defer supervisor.Stop(context.Background())

	// Wait for a few heartbeats
	heartbeatCount := 0
	timeout := time.After(1 * time.Second)

	for heartbeatCount < 3 {
		select {
		case <-timeout:
			if heartbeatCount == 0 {
				t.Fatal("did not receive any heartbeats")
			}
			// Got some heartbeats, that's good enough
			return
		case hb, ok := <-supervisor.Heartbeats():
			if !ok {
				t.Fatal("heartbeat channel closed unexpectedly")
			}

			if hb.Agent.AgentType != protocol.AgentTypeBuilder {
				t.Errorf("unexpected agent type: %s", hb.Agent.AgentType)
			}

			heartbeatCount++
		case <-supervisor.Events():
			// Drain events channel
		}
	}

	// Verify LastHeartbeat is recent
	lastHB := supervisor.LastHeartbeat()
	if time.Since(lastHB) > 2*time.Second {
		t.Errorf("last heartbeat too old: %v", time.Since(lastHB))
	}
}

func TestSupervisorMultipleCommands(t *testing.T) {
	mockAgentPath, err := buildMockAgent(t)
	if err != nil {
		t.Fatalf("failed to build mock agent: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	supervisor := NewAgentSupervisor(
		protocol.AgentTypeBuilder,
		[]string{mockAgentPath, "-type", "builder", "-no-heartbeat"},
		map[string]string{},
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := supervisor.Start(ctx); err != nil {
		t.Fatalf("failed to start agent: %v", err)
	}
	defer supervisor.Stop(context.Background())

	// Send multiple commands
	numCommands := 3
	receivedEvents := 0

	for i := 0; i < numCommands; i++ {
		cmd := &protocol.Command{
			Kind:           protocol.MessageKindCommand,
			MessageID:      uuid.New().String(),
			CorrelationID:  fmt.Sprintf("test-corr-%d", i),
			TaskID:         fmt.Sprintf("T-TEST-%03d", i),
			IdempotencyKey: fmt.Sprintf("pending-ik:test-%d", i),
			To: protocol.AgentRef{
				AgentType: protocol.AgentTypeBuilder,
			},
			Action:          protocol.ActionImplement,
			Inputs:          map[string]any{},
			ExpectedOutputs: []protocol.ExpectedOutput{},
			Version: protocol.Version{
				SnapshotID: "snap-test-0001",
			},
			Deadline: time.Now().Add(1 * time.Minute).UTC(),
			Retry: protocol.Retry{
				Attempt:     0,
				MaxAttempts: 3,
			},
			Priority: 5,
		}

		if err := supervisor.SendCommand(cmd); err != nil {
			t.Fatalf("failed to send command %d: %v", i, err)
		}
	}

	// Wait for all responses
	timeout := time.After(5 * time.Second)

	for receivedEvents < numCommands {
		select {
		case <-timeout:
			t.Fatalf("timeout waiting for events (got %d/%d)", receivedEvents, numCommands)
		case evt, ok := <-supervisor.Events():
			if !ok {
				t.Fatal("events channel closed unexpectedly")
			}

			if evt.Event == protocol.EventBuilderCompleted {
				receivedEvents++
			}
		}
	}
}

// buildMockAgent compiles the mock agent for testing
func buildMockAgent(t *testing.T) (string, error) {
	t.Helper()

	// Check if mockagent is already built
	mockAgentPath := filepath.Join(t.TempDir(), "mockagent")

	cmd := exec.Command("go", "build", "-o", mockAgentPath, "../../cmd/mockagent")
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to build mockagent: %w", err)
	}

	return mockAgentPath, nil
}
