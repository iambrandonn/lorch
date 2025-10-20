package ndjson

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
)

func TestEncoderDecoder(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	encoder := NewEncoder(&buf, logger)
	decoder := NewDecoder(&buf, logger)

	// Test command encoding/decoding
	cmd := protocol.Command{
		Kind:           protocol.MessageKindCommand,
		MessageID:      "m-01",
		CorrelationID:  "corr-1",
		TaskID:         "T-001",
		IdempotencyKey: "pending-ik:test",
		To: protocol.AgentRef{
			AgentType: protocol.AgentTypeBuilder,
		},
		Action: protocol.ActionImplement,
		Inputs: map[string]any{
			"goal": "implement feature X",
		},
		ExpectedOutputs: []protocol.ExpectedOutput{},
		Version: protocol.Version{
			SnapshotID: "snap-test-0001",
		},
		Deadline: time.Now().UTC(),
		Retry: protocol.Retry{
			Attempt:     0,
			MaxAttempts: 3,
		},
		Priority: 5,
	}

	if err := encoder.Encode(cmd); err != nil {
		t.Fatalf("failed to encode command: %v", err)
	}

	var decoded protocol.Command
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("failed to decode command: %v", err)
	}

	if decoded.MessageID != cmd.MessageID {
		t.Errorf("message_id mismatch: got %s, want %s", decoded.MessageID, cmd.MessageID)
	}
	if decoded.Action != cmd.Action {
		t.Errorf("action mismatch: got %s, want %s", decoded.Action, cmd.Action)
	}
}

func TestEncoderDecoderEvent(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	encoder := NewEncoder(&buf, logger)
	decoder := NewDecoder(&buf, logger)

	evt := protocol.Event{
		Kind:          protocol.MessageKindEvent,
		MessageID:     "e-01",
		CorrelationID: "corr-1",
		TaskID:        "T-001",
		From: protocol.AgentRef{
			AgentType: protocol.AgentTypeBuilder,
		},
		Event:  protocol.EventBuilderCompleted,
		Status: "success",
		Payload: map[string]any{
			"tests": map[string]any{
				"status": "pass",
			},
		},
		OccurredAt: time.Now().UTC(),
	}

	if err := encoder.Encode(evt); err != nil {
		t.Fatalf("failed to encode event: %v", err)
	}

	var decoded protocol.Event
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("failed to decode event: %v", err)
	}

	if decoded.Event != evt.Event {
		t.Errorf("event type mismatch: got %s, want %s", decoded.Event, evt.Event)
	}
}

func TestEncoderDecoderHeartbeat(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	encoder := NewEncoder(&buf, logger)
	decoder := NewDecoder(&buf, logger)

	hb := protocol.Heartbeat{
		Kind: protocol.MessageKindHeartbeat,
		Agent: protocol.AgentRef{
			AgentType: protocol.AgentTypeBuilder,
			AgentID:   "builder#1",
		},
		Seq:            1,
		Status:         protocol.HeartbeatStatusReady,
		PID:            12345,
		UptimeS:        10.5,
		LastActivityAt: time.Now().UTC(),
	}

	if err := encoder.Encode(hb); err != nil {
		t.Fatalf("failed to encode heartbeat: %v", err)
	}

	var decoded protocol.Heartbeat
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("failed to decode heartbeat: %v", err)
	}

	if decoded.Seq != hb.Seq {
		t.Errorf("seq mismatch: got %d, want %d", decoded.Seq, hb.Seq)
	}
	if decoded.Status != hb.Status {
		t.Errorf("status mismatch: got %s, want %s", decoded.Status, hb.Status)
	}
}

func TestDecodeEnvelope(t *testing.T) {
	tests := []struct {
		name     string
		message  any
		wantType string
	}{
		{
			name: "command",
			message: protocol.Command{
				Kind:           protocol.MessageKindCommand,
				MessageID:      "m-01",
				CorrelationID:  "corr-1",
				TaskID:         "T-001",
				IdempotencyKey: "pending-ik:test",
				To:             protocol.AgentRef{AgentType: protocol.AgentTypeBuilder},
				Action:         protocol.ActionImplement,
				Inputs:         map[string]any{},
				ExpectedOutputs: []protocol.ExpectedOutput{},
				Version:        protocol.Version{SnapshotID: "snap-test-0001"},
				Deadline:       time.Now().UTC(),
				Retry:          protocol.Retry{Attempt: 0, MaxAttempts: 3},
				Priority:       5,
			},
			wantType: "*protocol.Command",
		},
		{
			name: "event",
			message: protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "e-01",
				CorrelationID: "corr-1",
				TaskID:        "T-001",
				From:          protocol.AgentRef{AgentType: protocol.AgentTypeBuilder},
				Event:         protocol.EventBuilderCompleted,
				Payload:       map[string]any{},
				OccurredAt:    time.Now().UTC(),
			},
			wantType: "*protocol.Event",
		},
		{
			name: "heartbeat",
			message: protocol.Heartbeat{
				Kind:           protocol.MessageKindHeartbeat,
				Agent:          protocol.AgentRef{AgentType: protocol.AgentTypeBuilder},
				Seq:            1,
				Status:         protocol.HeartbeatStatusReady,
				PID:            12345,
				UptimeS:        10.0,
				LastActivityAt: time.Now().UTC(),
			},
			wantType: "*protocol.Heartbeat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))

			encoder := NewEncoder(&buf, logger)
			decoder := NewDecoder(&buf, logger)

			if err := encoder.Encode(tt.message); err != nil {
				t.Fatalf("failed to encode: %v", err)
			}

			msg, err := decoder.DecodeEnvelope()
			if err != nil {
				t.Fatalf("failed to decode envelope: %v", err)
			}

			gotType := fmt.Sprintf("%T", msg)
			if gotType != tt.wantType {
				t.Errorf("wrong type: got %s, want %s", gotType, tt.wantType)
			}
		})
	}
}

func TestEncoderSizeLimit(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	encoder := NewEncoder(&buf, logger)

	// Create a message with oversized payload
	largePayload := make(map[string]any)
	largePayload["data"] = strings.Repeat("x", MaxMessageSize)

	evt := protocol.Event{
		Kind:          protocol.MessageKindEvent,
		MessageID:     "e-01",
		CorrelationID: "corr-1",
		TaskID:        "T-001",
		From:          protocol.AgentRef{AgentType: protocol.AgentTypeBuilder},
		Event:         "test.event",
		Payload:       largePayload,
		OccurredAt:    time.Now().UTC(),
	}

	err := encoder.Encode(evt)
	if err == nil {
		t.Error("expected error for oversized message, got nil")
	}

	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Errorf("expected 'exceeds limit' error, got: %v", err)
	}
}

func TestDecoderSizeLimit(t *testing.T) {
	// Create a line that exceeds the size limit
	largeLine := strings.Repeat("x", MaxMessageSize+1000)
	input := strings.NewReader(largeLine + "\n")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	decoder := NewDecoder(input, logger)

	var msg map[string]any
	err := decoder.Decode(&msg)
	if err == nil {
		t.Error("expected error for oversized line, got nil")
	}
}

func TestDecoderEmptyLines(t *testing.T) {
	input := strings.NewReader("\n\n{\"kind\":\"event\",\"message_id\":\"e-01\",\"correlation_id\":\"c-1\",\"task_id\":\"T-1\",\"from\":{\"agent_type\":\"builder\"},\"event\":\"test\",\"payload\":{},\"occurred_at\":\"2025-10-19T12:00:00Z\"}\n")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	decoder := NewDecoder(input, logger)

	var evt protocol.Event
	if err := decoder.Decode(&evt); err != nil {
		t.Fatalf("failed to decode after empty lines: %v", err)
	}

	if evt.MessageID != "e-01" {
		t.Errorf("got message_id %s, want e-01", evt.MessageID)
	}
}

func TestDecoderEOF(t *testing.T) {
	input := strings.NewReader("")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	decoder := NewDecoder(input, logger)

	var msg map[string]any
	err := decoder.Decode(&msg)
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestMultipleMessages(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	encoder := NewEncoder(&buf, logger)

	// Encode multiple messages
	messages := []protocol.Event{
		{
			Kind:          protocol.MessageKindEvent,
			MessageID:     "e-01",
			CorrelationID: "corr-1",
			TaskID:        "T-001",
			From:          protocol.AgentRef{AgentType: protocol.AgentTypeBuilder},
			Event:         "event1",
			Payload:       map[string]any{},
			OccurredAt:    time.Now().UTC(),
		},
		{
			Kind:          protocol.MessageKindEvent,
			MessageID:     "e-02",
			CorrelationID: "corr-1",
			TaskID:        "T-001",
			From:          protocol.AgentRef{AgentType: protocol.AgentTypeBuilder},
			Event:         "event2",
			Payload:       map[string]any{},
			OccurredAt:    time.Now().UTC(),
		},
		{
			Kind:          protocol.MessageKindEvent,
			MessageID:     "e-03",
			CorrelationID: "corr-1",
			TaskID:        "T-001",
			From:          protocol.AgentRef{AgentType: protocol.AgentTypeBuilder},
			Event:         "event3",
			Payload:       map[string]any{},
			OccurredAt:    time.Now().UTC(),
		},
	}

	for _, msg := range messages {
		if err := encoder.Encode(msg); err != nil {
			t.Fatalf("failed to encode message: %v", err)
		}
	}

	// Decode and verify
	decoder := NewDecoder(&buf, logger)
	for i, expected := range messages {
		var decoded protocol.Event
		if err := decoder.Decode(&decoded); err != nil {
			t.Fatalf("failed to decode message %d: %v", i, err)
		}

		if decoded.MessageID != expected.MessageID {
			t.Errorf("message %d: got message_id %s, want %s", i, decoded.MessageID, expected.MessageID)
		}
		if decoded.Event != expected.Event {
			t.Errorf("message %d: got event %s, want %s", i, decoded.Event, expected.Event)
		}
	}

	// Should get EOF after all messages
	var extra protocol.Event
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Errorf("expected EOF after all messages, got %v", err)
	}
}
