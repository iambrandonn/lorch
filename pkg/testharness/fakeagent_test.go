package testharness

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/iambrandonn/lorch/internal/ndjson"
	"github.com/iambrandonn/lorch/internal/protocol"
)

func TestFakeAgentBasicFlow(t *testing.T) {
	// Create pipes for bidirectional communication
	agentStdinR, orchestratorStdoutW := io.Pipe()
	orchestratorStdinR, agentStdoutW := io.Pipe()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create fake builder agent
	agent := NewFakeAgent(protocol.AgentTypeBuilder, agentStdinR, agentStdoutW, logger)
	agent.DisableHeartbeat = true // Disable for simpler testing

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start agent in background
	agentDone := make(chan error, 1)
	go func() {
		agentDone <- agent.Run(ctx)
	}()

	// Setup encoders/decoders from orchestrator perspective
	encoder := ndjson.NewEncoder(orchestratorStdoutW, logger)
	decoder := ndjson.NewDecoder(orchestratorStdinR, logger)

	// Start reading messages in background
	messageChan := make(chan any, 10)
	go func() {
		for {
			msg, err := decoder.DecodeEnvelope()
			if err != nil {
				close(messageChan)
				return
			}
			messageChan <- msg
		}
	}()

	// Send implement command
	cmd := protocol.Command{
		Kind:           protocol.MessageKindCommand,
		MessageID:      uuid.New().String(),
		CorrelationID:  "test-corr-1",
		TaskID:         "T-TEST-001",
		IdempotencyKey: "pending-ik:test",
		To: protocol.AgentRef{
			AgentType: protocol.AgentTypeBuilder,
		},
		Action:          protocol.ActionImplement,
		Inputs:          map[string]any{"goal": "test implementation"},
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

	if err := encoder.Encode(cmd); err != nil {
		t.Fatalf("failed to send command: %v", err)
	}

	// Read response (skip initial heartbeat if any, read until builder.completed)
	timeout := time.After(2 * time.Second)
	var gotBuilderCompleted bool

	for !gotBuilderCompleted {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for builder.completed")
		case msg, ok := <-messageChan:
			if !ok {
				t.Fatal("message channel closed unexpectedly")
			}

			switch v := msg.(type) {
			case *protocol.Event:
				if v.Event == protocol.EventBuilderCompleted {
					gotBuilderCompleted = true

					if v.Status != "success" {
						t.Errorf("expected status=success, got %s", v.Status)
					}

					if v.TaskID != cmd.TaskID {
						t.Errorf("task_id mismatch: got %s, want %s", v.TaskID, cmd.TaskID)
					}

					if v.CorrelationID != cmd.CorrelationID {
						t.Errorf("correlation_id mismatch: got %s, want %s", v.CorrelationID, cmd.CorrelationID)
					}

					// Verify test results in payload
					if tests, ok := v.Payload["tests"].(map[string]any); ok {
						if status, ok := tests["status"].(string); ok && status != "pass" {
							t.Errorf("expected tests.status=pass, got %s", status)
						}
					} else {
						t.Error("missing tests payload")
					}
				}
			case *protocol.Heartbeat:
				// Skip heartbeats
			default:
				t.Errorf("unexpected message type: %T", msg)
			}
		}
	}

	// Stop agent
	cancel()
	orchestratorStdoutW.Close()

	select {
	case err := <-agentDone:
		if err != nil {
			t.Errorf("agent returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Error("agent did not stop in time")
	}
}

func TestFakeAgentReviewFlow(t *testing.T) {
	// Create pipes
	agentStdinR, orchestratorStdoutW := io.Pipe()
	orchestratorStdinR, agentStdoutW := io.Pipe()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create fake reviewer agent configured to request changes
	agent := NewFakeAgent(protocol.AgentTypeReviewer, agentStdinR, agentStdoutW, logger)
	agent.DisableHeartbeat = true
	agent.ReviewResult = protocol.ReviewStatusChangesRequested

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start agent
	go agent.Run(ctx)

	encoder := ndjson.NewEncoder(orchestratorStdoutW, logger)
	decoder := ndjson.NewDecoder(orchestratorStdinR, logger)

	// Start reading messages in background
	messageChan := make(chan any, 10)
	go func() {
		for {
			msg, err := decoder.DecodeEnvelope()
			if err != nil {
				close(messageChan)
				return
			}
			messageChan <- msg
		}
	}()

	// Send review command
	cmd := protocol.Command{
		Kind:           protocol.MessageKindCommand,
		MessageID:      uuid.New().String(),
		CorrelationID:  "test-corr-2",
		TaskID:         "T-TEST-002",
		IdempotencyKey: "pending-ik:review:test",
		To: protocol.AgentRef{
			AgentType: protocol.AgentTypeReviewer,
		},
		Action:          protocol.ActionReview,
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

	if err := encoder.Encode(cmd); err != nil {
		t.Fatalf("failed to send command: %v", err)
	}

	// Read response
	timeout := time.After(2 * time.Second)
	var gotReviewCompleted bool

	for !gotReviewCompleted {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for review.completed")
		case msg, ok := <-messageChan:
			if !ok {
				t.Fatal("message channel closed unexpectedly")
			}

			if evt, ok := msg.(*protocol.Event); ok && evt.Event == protocol.EventReviewCompleted {
				gotReviewCompleted = true

				if evt.Status != protocol.ReviewStatusChangesRequested {
					t.Errorf("expected status=%s, got %s", protocol.ReviewStatusChangesRequested, evt.Status)
				}
			}
		}
	}

	cancel()
	orchestratorStdoutW.Close()
}

func TestFakeAgentSpecMaintainerFlow(t *testing.T) {
	// Create pipes
	agentStdinR, orchestratorStdoutW := io.Pipe()
	orchestratorStdinR, agentStdoutW := io.Pipe()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create fake spec maintainer configured to signal no changes needed
	agent := NewFakeAgent(protocol.AgentTypeSpecMaintainer, agentStdinR, agentStdoutW, logger)
	agent.DisableHeartbeat = true
	agent.SpecResult = "no_changes_needed"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start agent
	go agent.Run(ctx)

	encoder := ndjson.NewEncoder(orchestratorStdoutW, logger)
	decoder := ndjson.NewDecoder(orchestratorStdinR, logger)

	// Start reading messages in background
	messageChan := make(chan any, 10)
	go func() {
		for {
			msg, err := decoder.DecodeEnvelope()
			if err != nil {
				close(messageChan)
				return
			}
			messageChan <- msg
		}
	}()

	// Send update_spec command
	cmd := protocol.Command{
		Kind:           protocol.MessageKindCommand,
		MessageID:      uuid.New().String(),
		CorrelationID:  "test-corr-3",
		TaskID:         "T-TEST-003",
		IdempotencyKey: "pending-ik:spec:test",
		To: protocol.AgentRef{
			AgentType: protocol.AgentTypeSpecMaintainer,
		},
		Action:          protocol.ActionUpdateSpec,
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

	if err := encoder.Encode(cmd); err != nil {
		t.Fatalf("failed to send command: %v", err)
	}

	// Read response
	timeout := time.After(2 * time.Second)
	var gotSpecEvent bool

	for !gotSpecEvent {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for spec event")
		case msg, ok := <-messageChan:
			if !ok {
				t.Fatal("message channel closed unexpectedly")
			}

			if evt, ok := msg.(*protocol.Event); ok {
				if evt.Event == protocol.EventSpecNoChangesNeeded {
					gotSpecEvent = true
				}
			}
		}
	}

	cancel()
	orchestratorStdoutW.Close()
}
