package ndjson

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/iambrandonn/lorch/internal/protocol"
)

// MaxMessageSize is the maximum NDJSON message size (256 KiB)
const MaxMessageSize = 256 * 1024

// Encoder writes NDJSON messages to an output stream
type Encoder struct {
	writer *bufio.Writer
	logger *slog.Logger
}

// NewEncoder creates a new NDJSON encoder
func NewEncoder(w io.Writer, logger *slog.Logger) *Encoder {
	return &Encoder{
		writer: bufio.NewWriter(w),
		logger: logger,
	}
}

// Encode writes a message as a single JSON line
func (e *Encoder) Encode(v any) error {
	// Marshal to JSON
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Check size limit
	if len(data) > MaxMessageSize {
		e.logger.Error("message exceeds size limit",
			"size", len(data),
			"limit", MaxMessageSize,
			"overflow", len(data)-MaxMessageSize)
		return fmt.Errorf("message size %d exceeds limit %d", len(data), MaxMessageSize)
	}

	// Write JSON + newline
	if _, err := e.writer.Write(data); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}
	if err := e.writer.WriteByte('\n'); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	// Flush immediately for real-time communication
	if err := e.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush output: %w", err)
	}

	return nil
}

// Decoder reads NDJSON messages from an input stream
type Decoder struct {
	scanner *bufio.Scanner
	logger  *slog.Logger
	lineNum int
}

// NewDecoder creates a new NDJSON decoder
func NewDecoder(r io.Reader, logger *slog.Logger) *Decoder {
	scanner := bufio.NewScanner(r)

	// Set custom buffer with max size enforcement
	buf := make([]byte, MaxMessageSize)
	scanner.Buffer(buf, MaxMessageSize)

	return &Decoder{
		scanner: scanner,
		logger:  logger,
		lineNum: 0,
	}
}

// Decode reads the next NDJSON message
func (d *Decoder) Decode(v any) error {
	if !d.scanner.Scan() {
		if err := d.scanner.Err(); err != nil {
			return fmt.Errorf("scanner error at line %d: %w", d.lineNum, err)
		}
		return io.EOF
	}

	d.lineNum++
	data := d.scanner.Bytes()

	// Check size (should be caught by scanner buffer, but double-check)
	if len(data) > MaxMessageSize {
		d.logger.Error("line exceeds size limit",
			"line", d.lineNum,
			"size", len(data),
			"limit", MaxMessageSize)
		return fmt.Errorf("line %d size %d exceeds limit %d", d.lineNum, len(data), MaxMessageSize)
	}

	// Skip empty lines
	if len(data) == 0 {
		return d.Decode(v)
	}

	// Unmarshal JSON
	if err := json.Unmarshal(data, v); err != nil {
		d.logger.Error("failed to unmarshal JSON",
			"line", d.lineNum,
			"error", err,
			"data", string(data[:min(100, len(data))]))
		return fmt.Errorf("failed to unmarshal line %d: %w", d.lineNum, err)
	}

	return nil
}

// DecodeEnvelope reads and routes a message based on its kind
func (d *Decoder) DecodeEnvelope() (any, error) {
	// First decode into a map to peek at the "kind" field
	var envelope map[string]any
	if err := d.Decode(&envelope); err != nil {
		return nil, err
	}

	kind, ok := envelope["kind"].(string)
	if !ok {
		return nil, fmt.Errorf("line %d: missing or invalid 'kind' field", d.lineNum)
	}

	// Re-marshal and unmarshal into the correct type
	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("line %d: failed to re-marshal envelope: %w", d.lineNum, err)
	}

	switch protocol.MessageKind(kind) {
	case protocol.MessageKindCommand:
		var cmd protocol.Command
		if err := json.Unmarshal(data, &cmd); err != nil {
			return nil, fmt.Errorf("line %d: failed to decode command: %w", d.lineNum, err)
		}
		return &cmd, nil

	case protocol.MessageKindEvent:
		var evt protocol.Event
		if err := json.Unmarshal(data, &evt); err != nil {
			return nil, fmt.Errorf("line %d: failed to decode event: %w", d.lineNum, err)
		}
		return &evt, nil

	case protocol.MessageKindHeartbeat:
		var hb protocol.Heartbeat
		if err := json.Unmarshal(data, &hb); err != nil {
			return nil, fmt.Errorf("line %d: failed to decode heartbeat: %w", d.lineNum, err)
		}
		return &hb, nil

	case protocol.MessageKindLog:
		var log protocol.Log
		if err := json.Unmarshal(data, &log); err != nil {
			return nil, fmt.Errorf("line %d: failed to decode log: %w", d.lineNum, err)
		}
		return &log, nil

	default:
		d.logger.Warn("unknown message kind",
			"line", d.lineNum,
			"kind", kind)
		return nil, fmt.Errorf("line %d: unknown message kind: %s", d.lineNum, kind)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
