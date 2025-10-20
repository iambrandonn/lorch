package eventlog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/iambrandonn/lorch/internal/ndjson"
	"github.com/iambrandonn/lorch/internal/protocol"
	"log/slog"
)

// EventLog writes protocol messages to an NDJSON file
type EventLog struct {
	file    *os.File
	encoder *ndjson.Encoder
	logger  *slog.Logger
	mu      sync.Mutex
}

// NewEventLog creates a new event log
func NewEventLog(logPath string, logger *slog.Logger) (*EventLog, error) {
	// Ensure directory exists
	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open file for appending (create if not exists)
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	encoder := ndjson.NewEncoder(file, logger)

	return &EventLog{
		file:    file,
		encoder: encoder,
		logger:  logger,
	}, nil
}

// WriteCommand writes a command to the log
func (l *EventLog) WriteCommand(cmd *protocol.Command) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.encoder.Encode(cmd)
}

// WriteEvent writes an event to the log
func (l *EventLog) WriteEvent(evt *protocol.Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.encoder.Encode(evt)
}

// WriteHeartbeat writes a heartbeat to the log
func (l *EventLog) WriteHeartbeat(hb *protocol.Heartbeat) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.encoder.Encode(hb)
}

// WriteLog writes a log message to the log
func (l *EventLog) WriteLog(log *protocol.Log) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.encoder.Encode(log)
}

// Close closes the event log file
func (l *EventLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		return l.file.Close()
	}
	return nil
}
