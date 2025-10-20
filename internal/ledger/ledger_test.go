package ledger

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
)

func TestReadLedger(t *testing.T) {
	tmpDir := t.TempDir()
	ledgerPath := filepath.Join(tmpDir, "run-001.ndjson")

	// Create test ledger with mixed message types
	messages := []interface{}{
		&protocol.Command{
			Kind:          protocol.MessageKindCommand,
			MessageID:     "cmd-1",
			CorrelationID: "corr-1",
			TaskID:        "T-0042",
			Action:        protocol.ActionImplement,
			IdempotencyKey: "ik:abc123",
		},
		&protocol.Event{
			Kind:          protocol.MessageKindEvent,
			MessageID:     "evt-1",
			CorrelationID: "corr-1",
			TaskID:        "T-0042",
			From:          protocol.AgentRef{AgentType: protocol.AgentTypeBuilder},
			Event:         protocol.EventBuilderCompleted,
			OccurredAt:    time.Now().UTC(),
		},
		&protocol.Heartbeat{
			Kind:   protocol.MessageKindHeartbeat,
			Agent:  protocol.AgentRef{AgentType: protocol.AgentTypeBuilder},
			Seq:    1,
			Status: protocol.HeartbeatStatusReady,
			PID:    12345,
		},
	}

	// Write test ledger
	if err := writeTestLedger(ledgerPath, messages); err != nil {
		t.Fatalf("failed to write test ledger: %v", err)
	}

	// Read back
	ledger, err := ReadLedger(ledgerPath)
	if err != nil {
		t.Fatalf("ReadLedger() error = %v", err)
	}

	// Verify counts
	if len(ledger.Commands) != 1 {
		t.Errorf("Commands count = %d, want 1", len(ledger.Commands))
	}

	if len(ledger.Events) != 1 {
		t.Errorf("Events count = %d, want 1", len(ledger.Events))
	}

	if len(ledger.Heartbeats) != 1 {
		t.Errorf("Heartbeats count = %d, want 1", len(ledger.Heartbeats))
	}
}

func TestGetTerminalEvents(t *testing.T) {
	ledger := &Ledger{
		Commands: []*protocol.Command{
			{
				MessageID:     "cmd-1",
				CorrelationID: "corr-1",
				Action:        protocol.ActionImplement,
			},
			{
				MessageID:     "cmd-2",
				CorrelationID: "corr-2",
				Action:        protocol.ActionReview,
			},
		},
		Events: []*protocol.Event{
			{
				MessageID:     "evt-1",
				CorrelationID: "corr-1",
				Event:         protocol.EventBuilderProgress,
			},
			{
				MessageID:     "evt-2",
				CorrelationID: "corr-1",
				Event:         protocol.EventBuilderCompleted,
			},
			{
				MessageID:     "evt-3",
				CorrelationID: "corr-2",
				Event:         protocol.EventReviewCompleted,
				Status:        protocol.ReviewStatusApproved,
			},
		},
	}

	terminals := ledger.GetTerminalEvents()

	// Should have 2 terminal events (one per command)
	if len(terminals) != 2 {
		t.Errorf("terminal events count = %d, want 2", len(terminals))
	}

	// Verify cmd-1 terminal
	if evt, ok := terminals["cmd-1"]; !ok {
		t.Error("missing terminal event for cmd-1")
	} else if evt.Event != protocol.EventBuilderCompleted {
		t.Errorf("cmd-1 terminal event = %s, want %s", evt.Event, protocol.EventBuilderCompleted)
	}

	// Verify cmd-2 terminal
	if evt, ok := terminals["cmd-2"]; !ok {
		t.Error("missing terminal event for cmd-2")
	} else if evt.Event != protocol.EventReviewCompleted {
		t.Errorf("cmd-2 terminal event = %s, want %s", evt.Event, protocol.EventReviewCompleted)
	}
}

func TestHasTerminalEvent(t *testing.T) {
	ledger := &Ledger{
		Commands: []*protocol.Command{
			{MessageID: "cmd-1", CorrelationID: "corr-1"},
			{MessageID: "cmd-2", CorrelationID: "corr-2"},
			{MessageID: "cmd-3", CorrelationID: "corr-3"},
		},
		Events: []*protocol.Event{
			{CorrelationID: "corr-1", Event: protocol.EventBuilderCompleted},
			{CorrelationID: "corr-2", Event: protocol.EventBuilderProgress},
		},
	}

	tests := []struct {
		name          string
		commandID     string
		wantCompleted bool
	}{
		{
			name:          "completed command",
			commandID:     "cmd-1",
			wantCompleted: true,
		},
		{
			name:          "incomplete command",
			commandID:     "cmd-2",
			wantCompleted: false,
		},
		{
			name:          "no events",
			commandID:     "cmd-3",
			wantCompleted: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ledger.HasTerminalEvent(tt.commandID)
			if result != tt.wantCompleted {
				t.Errorf("HasTerminalEvent(%s) = %v, want %v", tt.commandID, result, tt.wantCompleted)
			}
		})
	}
}

func TestGetPendingCommands(t *testing.T) {
	ledger := &Ledger{
		Commands: []*protocol.Command{
			{MessageID: "cmd-1", CorrelationID: "corr-1", Action: protocol.ActionImplement},
			{MessageID: "cmd-2", CorrelationID: "corr-2", Action: protocol.ActionReview},
			{MessageID: "cmd-3", CorrelationID: "corr-3", Action: protocol.ActionUpdateSpec},
		},
		Events: []*protocol.Event{
			{CorrelationID: "corr-1", Event: protocol.EventBuilderCompleted},
			// cmd-2 has no terminal event
			{CorrelationID: "corr-3", Event: protocol.EventSpecUpdated},
		},
	}

	pending := ledger.GetPendingCommands()

	if len(pending) != 1 {
		t.Errorf("pending commands count = %d, want 1", len(pending))
	}

	if pending[0].MessageID != "cmd-2" {
		t.Errorf("pending command ID = %s, want cmd-2", pending[0].MessageID)
	}
}

func TestIsTerminalEvent(t *testing.T) {
	tests := []struct {
		name       string
		eventType  string
		isTerminal bool
	}{
		{"builder completed", protocol.EventBuilderCompleted, true},
		{"review completed", protocol.EventReviewCompleted, true},
		{"spec updated", protocol.EventSpecUpdated, true},
		{"spec no changes", protocol.EventSpecNoChangesNeeded, true},
		{"spec changes requested", protocol.EventSpecChangesRequested, true},
		{"error", protocol.EventError, true},
		{"builder progress", protocol.EventBuilderProgress, false},
		{"artifact produced", protocol.EventArtifactProduced, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTerminalEvent(tt.eventType)
			if result != tt.isTerminal {
				t.Errorf("isTerminalEvent(%s) = %v, want %v", tt.eventType, result, tt.isTerminal)
			}
		})
	}
}

func TestEmptyLedger(t *testing.T) {
	tmpDir := t.TempDir()
	ledgerPath := filepath.Join(tmpDir, "empty.ndjson")

	// Create empty file
	if err := os.WriteFile(ledgerPath, []byte{}, 0600); err != nil {
		t.Fatalf("failed to create empty file: %v", err)
	}

	ledger, err := ReadLedger(ledgerPath)
	if err != nil {
		t.Fatalf("ReadLedger() error = %v", err)
	}

	if len(ledger.Commands) != 0 {
		t.Errorf("empty ledger has %d commands, want 0", len(ledger.Commands))
	}
}

func TestLargeMessageHandling(t *testing.T) {
	tmpDir := t.TempDir()
	ledgerPath := filepath.Join(tmpDir, "large.ndjson")

	// Create a command with large inputs (> 64 KiB default buffer, < 256 KiB limit)
	largeInput := make([]byte, 128*1024) // 128 KiB
	for i := range largeInput {
		largeInput[i] = 'x'
	}

	// Write a command with large payload
	file, err := os.Create(ledgerPath)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Manually construct large JSON message
	_, err = fmt.Fprintf(file, `{"kind":"command","message_id":"cmd-1","correlation_id":"corr-1","task_id":"T-0042","idempotency_key":"ik:test","to":{"agent_type":"builder"},"action":"implement","inputs":{"large_data":"%s"},"expected_outputs":[],"version":{"snapshot_id":""},"deadline":"2025-01-01T00:00:00Z","retry":{"attempt":0,"max_attempts":3},"priority":0}`+"\n", string(largeInput))
	if err != nil {
		t.Fatalf("failed to write large message: %v", err)
	}
	file.Close()

	// Should successfully read despite large message
	ledger, err := ReadLedger(ledgerPath)
	if err != nil {
		t.Fatalf("ReadLedger() error = %v (scanner should handle up to 256 KiB)", err)
	}

	if len(ledger.Commands) != 1 {
		t.Errorf("Commands count = %d, want 1", len(ledger.Commands))
	}

	// Verify the large data was preserved
	if len(ledger.Commands[0].Inputs) == 0 {
		t.Error("large inputs were not preserved")
	}
}

// Helper function to write test ledger
func writeTestLedger(path string, messages []interface{}) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Use ndjson package for proper encoding
	// For simplicity in tests, we'll manually write JSON lines
	encoder := file
	for _, msg := range messages {
		var line string
		switch m := msg.(type) {
		case *protocol.Command:
			line = `{"kind":"command","message_id":"` + m.MessageID + `","correlation_id":"` + m.CorrelationID + `","task_id":"` + m.TaskID + `","idempotency_key":"` + m.IdempotencyKey + `","to":{"agent_type":"builder"},"action":"` + string(m.Action) + `","inputs":{},"expected_outputs":[],"version":{"snapshot_id":""},"deadline":"2025-01-01T00:00:00Z","retry":{"attempt":0,"max_attempts":3},"priority":0}` + "\n"
		case *protocol.Event:
			line = `{"kind":"event","message_id":"` + m.MessageID + `","correlation_id":"` + m.CorrelationID + `","task_id":"` + m.TaskID + `","from":{"agent_type":"` + string(m.From.AgentType) + `"},"event":"` + m.Event + `","status":"` + m.Status + `","payload":{},"occurred_at":"2025-01-01T00:00:00Z"}` + "\n"
		case *protocol.Heartbeat:
			line = `{"kind":"heartbeat","agent":{"agent_type":"` + string(m.Agent.AgentType) + `"},"seq":` + string(rune(m.Seq+'0')) + `,"status":"` + string(m.Status) + `","pid":` + string(rune(m.PID%10+'0')) + `,"uptime_s":0,"last_activity_at":"2025-01-01T00:00:00Z"}` + "\n"
		}
		if _, err := encoder.Write([]byte(line)); err != nil {
			return err
		}
	}

	return nil
}
