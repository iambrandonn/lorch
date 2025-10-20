package eventlog

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/ndjson"
	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/google/uuid"
)

func TestEventLogWriteRead(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "events", "test-run.ndjson")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create event log
	eventLog, err := NewEventLog(logPath, logger)
	if err != nil {
		t.Fatalf("failed to create event log: %v", err)
	}
	defer eventLog.Close()

	// Write some messages
	cmd := &protocol.Command{
		Kind:           protocol.MessageKindCommand,
		MessageID:      uuid.New().String(),
		CorrelationID:  "test-corr",
		TaskID:         "T-001",
		IdempotencyKey: "pending-ik:test",
		To:             protocol.AgentRef{AgentType: protocol.AgentTypeBuilder},
		Action:         protocol.ActionImplement,
		Inputs:         map[string]any{},
		ExpectedOutputs: []protocol.ExpectedOutput{},
		Version:        protocol.Version{SnapshotID: "snap-test-0001"},
		Deadline:       time.Now().Add(1 * time.Hour).UTC(),
		Retry:          protocol.Retry{Attempt: 0, MaxAttempts: 3},
		Priority:       5,
	}

	if err := eventLog.WriteCommand(cmd); err != nil {
		t.Fatalf("failed to write command: %v", err)
	}

	evt := &protocol.Event{
		Kind:          protocol.MessageKindEvent,
		MessageID:     uuid.New().String(),
		CorrelationID: "test-corr",
		TaskID:        "T-001",
		From:          protocol.AgentRef{AgentType: protocol.AgentTypeBuilder},
		Event:         protocol.EventBuilderCompleted,
		Status:        "success",
		Payload:       map[string]any{},
		OccurredAt:    time.Now().UTC(),
	}

	if err := eventLog.WriteEvent(evt); err != nil {
		t.Fatalf("failed to write event: %v", err)
	}

	hb := &protocol.Heartbeat{
		Kind:           protocol.MessageKindHeartbeat,
		Agent:          protocol.AgentRef{AgentType: protocol.AgentTypeBuilder},
		Seq:            1,
		Status:         protocol.HeartbeatStatusReady,
		PID:            12345,
		UptimeS:        10.0,
		LastActivityAt: time.Now().UTC(),
	}

	if err := eventLog.WriteHeartbeat(hb); err != nil {
		t.Fatalf("failed to write heartbeat: %v", err)
	}

	// Close to flush
	if err := eventLog.Close(); err != nil {
		t.Fatalf("failed to close event log: %v", err)
	}

	// Read back and verify
	file, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("failed to open log file for reading: %v", err)
	}
	defer file.Close()

	decoder := ndjson.NewDecoder(file, logger)

	// Read command
	msg1, err := decoder.DecodeEnvelope()
	if err != nil {
		t.Fatalf("failed to decode first message: %v", err)
	}

	if _, ok := msg1.(*protocol.Command); !ok {
		t.Errorf("expected command, got %T", msg1)
	}

	// Read event
	msg2, err := decoder.DecodeEnvelope()
	if err != nil {
		t.Fatalf("failed to decode second message: %v", err)
	}

	if _, ok := msg2.(*protocol.Event); !ok {
		t.Errorf("expected event, got %T", msg2)
	}

	// Read heartbeat
	msg3, err := decoder.DecodeEnvelope()
	if err != nil {
		t.Fatalf("failed to decode third message: %v", err)
	}

	if _, ok := msg3.(*protocol.Heartbeat); !ok {
		t.Errorf("expected heartbeat, got %T", msg3)
	}

	// Should be EOF now
	_, err = decoder.DecodeEnvelope()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestEventLogDirectoryCreation(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "nested", "dirs", "events", "test.ndjson")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	eventLog, err := NewEventLog(logPath, logger)
	if err != nil {
		t.Fatalf("failed to create event log: %v", err)
	}
	defer eventLog.Close()

	// Verify directory was created
	if _, err := os.Stat(filepath.Dir(logPath)); os.IsNotExist(err) {
		t.Error("log directory was not created")
	}
}
