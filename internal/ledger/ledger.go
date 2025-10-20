package ledger

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/iambrandonn/lorch/internal/ndjson"
	"github.com/iambrandonn/lorch/internal/protocol"
)

// Ledger represents a parsed event log with all messages categorized
type Ledger struct {
	Commands   []*protocol.Command
	Events     []*protocol.Event
	Heartbeats []*protocol.Heartbeat
	Logs       []*protocol.Log
}

// ReadLedger reads and parses an NDJSON ledger file
func ReadLedger(path string) (*Ledger, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open ledger: %w", err)
	}
	defer file.Close()

	ledger := &Ledger{
		Commands:   make([]*protocol.Command, 0),
		Events:     make([]*protocol.Event, 0),
		Heartbeats: make([]*protocol.Heartbeat, 0),
		Logs:       make([]*protocol.Log, 0),
	}

	scanner := bufio.NewScanner(file)
	// Set buffer size to match NDJSON protocol limit (256 KiB)
	// Default scanner buffer is 64 KiB which would truncate larger messages
	buf := make([]byte, ndjson.MaxMessageSize)
	scanner.Buffer(buf, ndjson.MaxMessageSize)

	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()

		if len(line) == 0 {
			continue
		}

		// Parse the message based on its kind
		var envelope struct {
			Kind protocol.MessageKind `json:"kind"`
		}

		if err := json.Unmarshal(line, &envelope); err != nil {
			return nil, fmt.Errorf("line %d: failed to parse envelope: %w", lineNum, err)
		}

		switch envelope.Kind {
		case protocol.MessageKindCommand:
			var cmd protocol.Command
			if err := json.Unmarshal(line, &cmd); err != nil {
				return nil, fmt.Errorf("line %d: failed to parse command: %w", lineNum, err)
			}
			ledger.Commands = append(ledger.Commands, &cmd)

		case protocol.MessageKindEvent:
			var evt protocol.Event
			if err := json.Unmarshal(line, &evt); err != nil {
				return nil, fmt.Errorf("line %d: failed to parse event: %w", lineNum, err)
			}
			ledger.Events = append(ledger.Events, &evt)

		case protocol.MessageKindHeartbeat:
			var hb protocol.Heartbeat
			if err := json.Unmarshal(line, &hb); err != nil {
				return nil, fmt.Errorf("line %d: failed to parse heartbeat: %w", lineNum, err)
			}
			ledger.Heartbeats = append(ledger.Heartbeats, &hb)

		case protocol.MessageKindLog:
			var log protocol.Log
			if err := json.Unmarshal(line, &log); err != nil {
				return nil, fmt.Errorf("line %d: failed to parse log: %w", lineNum, err)
			}
			ledger.Logs = append(ledger.Logs, &log)

		default:
			return nil, fmt.Errorf("line %d: unknown message kind: %s", lineNum, envelope.Kind)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading ledger: %w", err)
	}

	return ledger, nil
}

// GetTerminalEvents returns a map of command_message_id → terminal event
// A terminal event is one that signals command completion (success or failure)
func (l *Ledger) GetTerminalEvents() map[string]*protocol.Event {
	terminals := make(map[string]*protocol.Event)

	// Build correlation_id → command_id mapping
	corrToCmd := make(map[string]string)
	for _, cmd := range l.Commands {
		corrToCmd[cmd.CorrelationID] = cmd.MessageID
	}

	// Find terminal events for each correlation
	for _, evt := range l.Events {
		if isTerminalEvent(evt.Event) {
			if cmdID, ok := corrToCmd[evt.CorrelationID]; ok {
				// Keep the last terminal event for this command
				terminals[cmdID] = evt
			}
		}
	}

	return terminals
}

// HasTerminalEvent checks if a command has a terminal event
func (l *Ledger) HasTerminalEvent(commandID string) bool {
	terminals := l.GetTerminalEvents()
	_, exists := terminals[commandID]
	return exists
}

// GetPendingCommands returns commands that have no terminal event
func (l *Ledger) GetPendingCommands() []*protocol.Command {
	terminals := l.GetTerminalEvents()
	pending := make([]*protocol.Command, 0)

	for _, cmd := range l.Commands {
		if _, completed := terminals[cmd.MessageID]; !completed {
			pending = append(pending, cmd)
		}
	}

	return pending
}

// isTerminalEvent returns true if an event type signals command completion
func isTerminalEvent(eventType string) bool {
	switch eventType {
	case protocol.EventBuilderCompleted,
		protocol.EventReviewCompleted,
		protocol.EventSpecUpdated,
		protocol.EventSpecNoChangesNeeded,
		protocol.EventSpecChangesRequested,
		protocol.EventError:
		return true
	default:
		return false
	}
}
