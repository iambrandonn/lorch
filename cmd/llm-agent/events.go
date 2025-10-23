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
	encoder  *ndjson.Encoder
	logger   *slog.Logger
	agentType protocol.AgentType
	agentID  string
	maxMessageBytes int
}

// NewRealEventEmitter creates a new real event emitter
func NewRealEventEmitter(encoder *ndjson.Encoder, logger *slog.Logger, agentType protocol.AgentType, agentID string, maxMessageBytes int) *RealEventEmitter {
	if maxMessageBytes <= 0 {
		maxMessageBytes = 256 * 1024 // 256 KiB default (Spec §12)
	}
	return &RealEventEmitter{
		encoder:   encoder,
		logger:    logger,
		agentType: agentType,
		agentID:   agentID,
		maxMessageBytes: maxMessageBytes,
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
			AgentType: e.agentType,
			AgentID:   e.agentID,
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
	b, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	if len(b) <= e.maxMessageBytes {
		return e.encoder.Encode(evt)
	}

	// Apply deterministic truncation strategy based on event type
	truncatedPayload := e.truncatePayloadDeterministically(evt.Event, evt.Payload)
	evt.Payload = truncatedPayload
	return e.encoder.Encode(evt)
}


// truncatePayloadDeterministically applies event-specific truncation strategies
func (e *RealEventEmitter) truncatePayloadDeterministically(eventName string, payload map[string]any) map[string]any {
	switch eventName {
	case "orchestration.proposed_tasks":
		return e.truncateOrchestrationProposedTasks(payload)
	case "orchestration.needs_clarification":
		return e.truncateOrchestrationNeedsClarification(payload)
	case "orchestration.plan_conflict":
		return e.truncateOrchestrationPlanConflict(payload)
	default:
		return e.truncateGenericPayload(payload)
	}
}

// truncateOrchestrationProposedTasks truncates orchestration.proposed_tasks payload deterministically
func (e *RealEventEmitter) truncateOrchestrationProposedTasks(payload map[string]any) map[string]any {
	result := make(map[string]any)

	// Preserve notes (usually small and important)
	if notes, ok := payload["notes"].(string); ok {
		result["notes"] = notes
	}

	// Truncate plan_candidates deterministically (keep top candidates by confidence)
	if candidates, ok := payload["plan_candidates"].([]map[string]any); ok {
		// Sort by confidence (descending) to keep the most important ones
		sortedCandidates := e.sortCandidatesByConfidence(candidates)
		// Keep top 3 candidates or until we hit size limit
		truncatedCandidates := e.truncateCandidatesList(sortedCandidates, e.maxMessageBytes/4)
		result["plan_candidates"] = truncatedCandidates
		result["plan_candidates_truncated"] = len(candidates) > len(truncatedCandidates)
	}

	// Truncate derived_tasks deterministically (keep by ID order)
	if tasks, ok := payload["derived_tasks"].([]map[string]any); ok {
		// Sort by ID to ensure deterministic order
		sortedTasks := e.sortTasksByID(tasks)
		// Keep top 5 tasks or until we hit size limit
		truncatedTasks := e.truncateTasksList(sortedTasks, e.maxMessageBytes/4)
		result["derived_tasks"] = truncatedTasks
		result["derived_tasks_truncated"] = len(tasks) > len(truncatedTasks)
	}

	// Add truncation summary
	result["_truncated"] = "Event payload truncated due to size limits. Check plan_candidates_truncated and derived_tasks_truncated flags."

	return result
}

// truncateOrchestrationNeedsClarification truncates orchestration.needs_clarification payload
func (e *RealEventEmitter) truncateOrchestrationNeedsClarification(payload map[string]any) map[string]any {
	result := make(map[string]any)

	// Preserve notes (usually small and important)
	if notes, ok := payload["notes"].(string); ok {
		result["notes"] = notes
	}

	// Truncate questions list (keep first few questions)
	if questions, ok := payload["questions"].([]string); ok {
		maxQuestions := 3 // Keep first 3 questions
		if len(questions) > maxQuestions {
			result["questions"] = questions[:maxQuestions]
			result["questions_truncated"] = true
			result["total_questions"] = len(questions)
		} else {
			result["questions"] = questions
		}
	}

	result["_truncated"] = "Questions list truncated due to size limits."
	return result
}

// truncateOrchestrationPlanConflict truncates orchestration.plan_conflict payload
func (e *RealEventEmitter) truncateOrchestrationPlanConflict(payload map[string]any) map[string]any {
	result := make(map[string]any)

	// Preserve reason (usually small and important)
	if reason, ok := payload["reason"].(string); ok {
		result["reason"] = reason
	}

	// Truncate candidates list (keep top candidates by confidence)
	if candidates, ok := payload["candidates"].([]map[string]any); ok {
		sortedCandidates := e.sortCandidatesByConfidence(candidates)
		truncatedCandidates := e.truncateCandidatesList(sortedCandidates, e.maxMessageBytes/3)
		result["candidates"] = truncatedCandidates
		result["candidates_truncated"] = len(candidates) > len(truncatedCandidates)
	}

	result["_truncated"] = "Candidates list truncated due to size limits."
	return result
}

// truncateGenericPayload provides fallback truncation for non-orchestration events
func (e *RealEventEmitter) truncateGenericPayload(payload map[string]any) map[string]any {
	// Fallback: stringify payload preview under "_truncated" and clear original payload
	preview := ""
	if pb, err := json.Marshal(payload); err == nil {
		// Limit preview to 25% of max message size or 2KB, whichever is smaller
		maxPreviewSize := e.maxMessageBytes / 4
		if maxPreviewSize > 2048 {
			maxPreviewSize = 2048
		}
		if len(pb) > maxPreviewSize {
			preview = string(pb[:maxPreviewSize]) + "…"
		} else {
			preview = string(pb)
		}
	}
	return map[string]any{"_truncated": preview}
}

// sortCandidatesByConfidence sorts candidates by confidence (descending)
func (e *RealEventEmitter) sortCandidatesByConfidence(candidates []map[string]any) []map[string]any {
	// Create a copy to avoid modifying the original
	sorted := make([]map[string]any, len(candidates))
	copy(sorted, candidates)

	// Simple bubble sort by confidence (descending)
	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			conf1, _ := sorted[j]["confidence"].(float64)
			conf2, _ := sorted[j+1]["confidence"].(float64)
			if conf1 < conf2 {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}
	return sorted
}

// sortTasksByID sorts tasks by ID (ascending)
func (e *RealEventEmitter) sortTasksByID(tasks []map[string]any) []map[string]any {
	// Create a copy to avoid modifying the original
	sorted := make([]map[string]any, len(tasks))
	copy(sorted, tasks)

	// Simple bubble sort by ID (ascending)
	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			id1, _ := sorted[j]["id"].(string)
			id2, _ := sorted[j+1]["id"].(string)
			if id1 > id2 {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}
	return sorted
}

// truncateCandidatesList truncates candidates list to fit within size limit
func (e *RealEventEmitter) truncateCandidatesList(candidates []map[string]any, maxSize int) []map[string]any {
	var result []map[string]any
	currentSize := 0

	for _, candidate := range candidates {
		candidateBytes, _ := json.Marshal(candidate)
		if currentSize+len(candidateBytes) > maxSize && len(result) > 0 {
			break
		}
		result = append(result, candidate)
		currentSize += len(candidateBytes)
	}

	return result
}

// truncateTasksList truncates tasks list to fit within size limit
func (e *RealEventEmitter) truncateTasksList(tasks []map[string]any, maxSize int) []map[string]any {
	var result []map[string]any
	currentSize := 0

	for _, task := range tasks {
		taskBytes, _ := json.Marshal(task)
		if currentSize+len(taskBytes) > maxSize && len(result) > 0 {
			break
		}
		result = append(result, task)
		currentSize += len(taskBytes)
	}

	return result
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

	// Return an error for critical failures to test error handling
	if code == "version_mismatch" || code == "llm_call_failed" || code == "invalid_inputs" {
		return fmt.Errorf("%s: %s", code, message)
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
