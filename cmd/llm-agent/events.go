package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/iambrandonn/lorch/internal/ndjson"
)

// RealEventEmitter implements EventEmitter using real NDJSON encoding
type RealEventEmitter struct {
	encoder *ndjson.Encoder
	logger  *slog.Logger
}

// NewRealEventEmitter creates a new real event emitter
func NewRealEventEmitter(encoder *ndjson.Encoder, logger *slog.Logger) *RealEventEmitter {
	return &RealEventEmitter{
		encoder: encoder,
		logger:  logger,
	}
}

// NewEvent creates a base event with all required fields
func (e *RealEventEmitter) NewEvent(cmd *protocol.Command, eventName string) protocol.Event {
	return protocol.Event{
		Kind:          protocol.MessageKindEvent,
		MessageID:     uuid.New().String(),
		CorrelationID: cmd.CorrelationID,
		TaskID:        cmd.TaskID,
		From: protocol.AgentRef{
			AgentType: protocol.AgentTypeOrchestration, // TODO: Get from agent config
			AgentID:   "agent-1",                      // TODO: Get from agent config
		},
		Event: eventName,
		ObservedVersion: &protocol.Version{
			SnapshotID: cmd.Version.SnapshotID,
		},
		OccurredAt: time.Now().UTC(),
	}
}

// EncodeEventCapped marshals and emits an event, enforcing the NDJSON message size cap
func (e *RealEventEmitter) EncodeEventCapped(evt protocol.Event) error {
	maxSize := 256 * 1024 // 256 KiB default (Spec §12)

	b, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	if len(b) <= maxSize {
		return e.encoder.Encode(evt)
	}

	// Fallback: stringify payload preview under "_truncated" and clear original payload
	preview := ""
	if pb, err := json.Marshal(evt.Payload); err == nil {
		if len(pb) > 2048 {
			preview = string(pb[:2048]) + "…"
		} else {
			preview = string(pb)
		}
	}
	evt.Payload = map[string]any{"_truncated": preview}
	return e.encoder.Encode(evt)
}

// SendErrorEvent sends a structured error event with machine-readable error codes
func (e *RealEventEmitter) SendErrorEvent(cmd *protocol.Command, code, message string) error {
	evt := e.NewEvent(cmd, "error")
	evt.Status = "failed"
	evt.Payload = map[string]any{
		"code":    code,
		"message": message,
	}
	return e.EncodeEventCapped(evt)
}

// SendOrchestrationProposedTasksEvent sends an orchestration.proposed_tasks event
func (e *RealEventEmitter) SendOrchestrationProposedTasksEvent(cmd *protocol.Command, planCandidates []map[string]any, derivedTasks []map[string]any, notes string) error {
	evt := e.NewEvent(cmd, "orchestration.proposed_tasks")
	evt.Status = "success"
	evt.Payload = map[string]any{
		"plan_candidates": planCandidates,
		"derived_tasks":   derivedTasks,
		"notes":           notes,
	}
	return e.EncodeEventCapped(evt)
}

// SendOrchestrationNeedsClarificationEvent sends an orchestration.needs_clarification event
func (e *RealEventEmitter) SendOrchestrationNeedsClarificationEvent(cmd *protocol.Command, questions []string, notes string) error {
	evt := e.NewEvent(cmd, "orchestration.needs_clarification")
	evt.Status = "needs_input"
	evt.Payload = map[string]any{
		"questions": questions,
		"notes":     notes,
	}
	return e.EncodeEventCapped(evt)
}

// SendOrchestrationPlanConflictEvent sends an orchestration.plan_conflict event
func (e *RealEventEmitter) SendOrchestrationPlanConflictEvent(cmd *protocol.Command, candidates []map[string]any, reason string) error {
	evt := e.NewEvent(cmd, "orchestration.plan_conflict")
	evt.Status = "needs_input"
	evt.Payload = map[string]any{
		"candidates": candidates,
		"reason":     reason,
	}
	return e.EncodeEventCapped(evt)
}

// SendArtifactProducedEvent sends an artifact.produced event
func (e *RealEventEmitter) SendArtifactProducedEvent(cmd *protocol.Command, artifact protocol.Artifact) error {
	evt := e.NewEvent(cmd, "artifact.produced")
	evt.Status = "success"
	evt.Artifacts = []protocol.Artifact{artifact}
	evt.Payload = map[string]any{
		"description": "Generated artifact",
	}
	return e.EncodeEventCapped(evt)
}

// SendLog sends a log message
func (e *RealEventEmitter) SendLog(level, message string, fields map[string]any) error {
	// Redact secrets before logging
	if fields != nil {
		fields = e.redactSecrets(fields)
	}

	log := protocol.Log{
		Kind:      protocol.MessageKindLog,
		Level:     protocol.LogLevel(level),
		Message:   message,
		Fields:    fields,
		Timestamp: time.Now().UTC(),
	}
	return e.encoder.Encode(log)
}

// redactSecrets redacts fields ending with _TOKEN, _KEY, _SECRET
func (e *RealEventEmitter) redactSecrets(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}

	out := make(map[string]any, len(m))
	for k, v := range m {
		kUp := strings.ToUpper(k)
		if strings.HasSuffix(kUp, "_TOKEN") ||
			strings.HasSuffix(kUp, "_KEY") ||
			strings.HasSuffix(kUp, "_SECRET") {
			out[k] = "[REDACTED]"
		} else {
			// Recursively redact nested maps
			if nested, ok := v.(map[string]any); ok {
				out[k] = e.redactSecrets(nested)
			} else {
				out[k] = v
			}
		}
	}
	return out
}

// MockEventEmitter implements EventEmitter for testing
type MockEventEmitter struct {
	events    []protocol.Event
	logs      []protocol.Log
	callLog   []string
	errorLog  []string
	artifactLog []string
}

// NewMockEventEmitter creates a new mock event emitter
func NewMockEventEmitter() *MockEventEmitter {
	return &MockEventEmitter{
		events:     make([]protocol.Event, 0),
		logs:       make([]protocol.Log, 0),
		callLog:    make([]string, 0),
		errorLog:   make([]string, 0),
		artifactLog: make([]string, 0),
	}
}

// NewEvent creates a mock event
func (m *MockEventEmitter) NewEvent(cmd *protocol.Command, eventName string) protocol.Event {
	m.callLog = append(m.callLog, fmt.Sprintf("NewEvent(%s)", eventName))

	return protocol.Event{
		Kind:          protocol.MessageKindEvent,
		MessageID:     "mock-msg-id",
		CorrelationID: cmd.CorrelationID,
		TaskID:        cmd.TaskID,
		From: protocol.AgentRef{
			AgentType: protocol.AgentTypeOrchestration,
			AgentID:   "mock-agent",
		},
		Event: eventName,
		ObservedVersion: &protocol.Version{
			SnapshotID: cmd.Version.SnapshotID,
		},
		OccurredAt: time.Now().UTC(),
	}
}

// EncodeEventCapped mocks event encoding
func (m *MockEventEmitter) EncodeEventCapped(evt protocol.Event) error {
	m.callLog = append(m.callLog, fmt.Sprintf("EncodeEventCapped(%s)", evt.Event))
	m.events = append(m.events, evt)
	return nil
}

// SendErrorEvent mocks error event sending
func (m *MockEventEmitter) SendErrorEvent(cmd *protocol.Command, code, message string) error {
	m.errorLog = append(m.errorLog, fmt.Sprintf("SendErrorEvent(%s, %s)", code, message))

	evt := m.NewEvent(cmd, "error")
	evt.Status = "failed"
	evt.Payload = map[string]any{
		"code":    code,
		"message": message,
	}
	m.events = append(m.events, evt)

	// Return an error for version_mismatch to test error handling
	if code == "version_mismatch" {
		return fmt.Errorf("version_mismatch: %s", message)
	}
	return nil
}

// SendArtifactProducedEvent mocks artifact event sending
func (m *MockEventEmitter) SendArtifactProducedEvent(cmd *protocol.Command, artifact protocol.Artifact) error {
	m.artifactLog = append(m.artifactLog, fmt.Sprintf("SendArtifactProducedEvent(%s)", artifact.Path))

	evt := m.NewEvent(cmd, "artifact.produced")
	evt.Status = "success"
	evt.Artifacts = []protocol.Artifact{artifact}
	evt.Payload = map[string]any{
		"description": "Generated artifact",
	}
	m.events = append(m.events, evt)
	return nil
}

// SendLog mocks log sending
func (m *MockEventEmitter) SendLog(level, message string, fields map[string]any) error {
	m.callLog = append(m.callLog, fmt.Sprintf("SendLog(%s, %s)", level, message))

	log := protocol.Log{
		Kind:      protocol.MessageKindLog,
		Level:     protocol.LogLevel(level),
		Message:   message,
		Fields:    fields,
		Timestamp: time.Now().UTC(),
	}
	m.logs = append(m.logs, log)
	return nil
}

// SendOrchestrationProposedTasksEvent mocks orchestration.proposed_tasks event
func (m *MockEventEmitter) SendOrchestrationProposedTasksEvent(cmd *protocol.Command, planCandidates []map[string]any, derivedTasks []map[string]any, notes string) error {
	m.callLog = append(m.callLog, fmt.Sprintf("SendOrchestrationProposedTasksEvent(%d candidates, %d tasks)", len(planCandidates), len(derivedTasks)))

	evt := m.NewEvent(cmd, "orchestration.proposed_tasks")
	evt.Status = "success"
	evt.Payload = map[string]any{
		"plan_candidates": planCandidates,
		"derived_tasks":   derivedTasks,
		"notes":           notes,
	}
	m.events = append(m.events, evt)
	return nil
}

// SendOrchestrationNeedsClarificationEvent mocks orchestration.needs_clarification event
func (m *MockEventEmitter) SendOrchestrationNeedsClarificationEvent(cmd *protocol.Command, questions []string, notes string) error {
	m.callLog = append(m.callLog, fmt.Sprintf("SendOrchestrationNeedsClarificationEvent(%d questions)", len(questions)))

	evt := m.NewEvent(cmd, "orchestration.needs_clarification")
	evt.Status = "needs_input"
	evt.Payload = map[string]any{
		"questions": questions,
		"notes":     notes,
	}
	m.events = append(m.events, evt)
	return nil
}

// SendOrchestrationPlanConflictEvent mocks orchestration.plan_conflict event
func (m *MockEventEmitter) SendOrchestrationPlanConflictEvent(cmd *protocol.Command, candidates []map[string]any, reason string) error {
	m.callLog = append(m.callLog, fmt.Sprintf("SendOrchestrationPlanConflictEvent(%d candidates)", len(candidates)))

	evt := m.NewEvent(cmd, "orchestration.plan_conflict")
	evt.Status = "needs_input"
	evt.Payload = map[string]any{
		"candidates": candidates,
		"reason":     reason,
	}
	m.events = append(m.events, evt)
	return nil
}

// GetEvents returns all emitted events
func (m *MockEventEmitter) GetEvents() []protocol.Event {
	return m.events
}

// GetLogs returns all emitted logs
func (m *MockEventEmitter) GetLogs() []protocol.Log {
	return m.logs
}

// GetCallLog returns the call log
func (m *MockEventEmitter) GetCallLog() []string {
	return m.callLog
}

// GetErrorLog returns the error log
func (m *MockEventEmitter) GetErrorLog() []string {
	return m.errorLog
}

// GetArtifactLog returns the artifact log
func (m *MockEventEmitter) GetArtifactLog() []string {
	return m.artifactLog
}

// ClearLogs clears all logs
func (m *MockEventEmitter) ClearLogs() {
	m.events = m.events[:0]
	m.logs = m.logs[:0]
	m.callLog = m.callLog[:0]
	m.errorLog = m.errorLog[:0]
	m.artifactLog = m.artifactLog[:0]
}
