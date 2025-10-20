package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/iambrandonn/lorch/internal/ndjson"
	"github.com/iambrandonn/lorch/internal/protocol"
)

func main() {
	// Parse flags
	agentType := flag.String("type", "builder", "Agent type (builder, reviewer, spec_maintainer)")
	agentID := flag.String("id", "", "Agent ID (auto-generated if not provided)")
	heartbeatInterval := flag.Duration("heartbeat-interval", 10*time.Second, "Heartbeat interval")
	disableHeartbeat := flag.Bool("no-heartbeat", false, "Disable automatic heartbeats")
	scriptFile := flag.String("script", "", "Path to response script file (JSON)")
	reviewChangesCount := flag.Int("review-changes-count", 0, "Number of times to request changes before approving")
	specChangesCount := flag.Int("spec-changes-count", 0, "Number of times to request spec changes before updating")
	flag.Parse()

	// Setup logger (stderr for diagnostics, stdout for protocol)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Generate agent ID if not provided
	if *agentID == "" {
		*agentID = fmt.Sprintf("%s#%s", *agentType, uuid.New().String()[:8])
	}

	logger.Info("mock agent starting",
		"type", *agentType,
		"id", *agentID,
		"pid", os.Getpid(),
		"ppid", os.Getppid())

	// Parse agent type
	var agentTypeEnum protocol.AgentType
	switch *agentType {
	case "builder":
		agentTypeEnum = protocol.AgentTypeBuilder
	case "reviewer":
		agentTypeEnum = protocol.AgentTypeReviewer
	case "spec_maintainer":
		agentTypeEnum = protocol.AgentTypeSpecMaintainer
	case "orchestration":
		agentTypeEnum = protocol.AgentTypeOrchestration
	default:
		logger.Error("invalid agent type", "type", *agentType)
		os.Exit(1)
	}

	// Create agent
	agent := &MockAgent{
		agentType:         agentTypeEnum,
		agentID:           *agentID,
		logger:            logger,
		encoder:           ndjson.NewEncoder(os.Stdout, logger),
		decoder:           ndjson.NewDecoder(os.Stdin, logger),
		heartbeatInterval: *heartbeatInterval,
		disableHeartbeat:  *disableHeartbeat,
		startTime:         time.Now(),
		pid:               os.Getpid(),
		ppid:              os.Getppid(),
		reviewChangesCount: *reviewChangesCount,
		specChangesCount:   *specChangesCount,
	}

	// Load script if provided
	if *scriptFile != "" {
		if err := agent.loadScript(*scriptFile); err != nil {
			logger.Error("failed to load script", "error", err)
			os.Exit(1)
		}
	}

	// Run agent
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		logger.Info("received signal", "signal", sig)
		cancel()
	}()

	if err := agent.Run(ctx); err != nil {
		logger.Error("agent failed", "error", err)
		os.Exit(1)
	}

	logger.Info("mock agent stopped")
}

// MockAgent simulates an agent for testing
type MockAgent struct {
	agentType         protocol.AgentType
	agentID           string
	logger            *slog.Logger
	encoder           *ndjson.Encoder
	decoder           *ndjson.Decoder
	heartbeatInterval time.Duration
	disableHeartbeat  bool
	startTime         time.Time
	pid               int
	ppid              int

	// Configuration for iteration testing
	reviewChangesCount int // How many times to request changes before approving
	specChangesCount   int // How many times to request spec changes before updating

	mu              sync.Mutex
	heartbeatSeq    int64
	lastActivityAt  time.Time
	currentTaskID   string
	status          protocol.HeartbeatStatus
	script          *Script
	reviewCallCount int // Counter for review calls
	specCallCount   int // Counter for spec calls
}

// Script contains pre-programmed responses
type Script struct {
	// Maps action name to response template
	Responses map[string]ResponseTemplate `json:"responses"`
}

// ResponseTemplate defines how to respond to a command
type ResponseTemplate struct {
	// Events to send in response (will be sent in order)
	Events []EventTemplate `json:"events"`
	// Optional delay before sending events
	DelayMs int `json:"delay_ms,omitempty"`
	// Error to return instead of sending events
	Error string `json:"error,omitempty"`
}

// EventTemplate defines an event to send
type EventTemplate struct {
	// Event type (e.g., "builder.completed")
	Type string `json:"type"`
	// Event status (optional, e.g., "approved")
	Status string `json:"status,omitempty"`
	// Event payload
	Payload map[string]any `json:"payload,omitempty"`
	// Artifacts to include
	Artifacts []ArtifactTemplate `json:"artifacts,omitempty"`
}

// ArtifactTemplate defines an artifact
type ArtifactTemplate struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func (a *MockAgent) loadScript(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read script file: %w", err)
	}

	var script Script
	if err := json.Unmarshal(data, &script); err != nil {
		return fmt.Errorf("failed to parse script JSON: %w", err)
	}

	a.script = &script
	a.logger.Info("loaded script", "path", path, "actions", len(script.Responses))
	return nil
}

func (a *MockAgent) Run(ctx context.Context) error {
	a.updateActivity()
	a.setStatus(protocol.HeartbeatStatusStarting)

	// Create internal context that we can cancel on stdin EOF
	internalCtx, internalCancel := context.WithCancel(ctx)
	defer internalCancel()

	// Start heartbeat goroutine
	var wg sync.WaitGroup
	if !a.disableHeartbeat {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.heartbeatLoop(internalCtx)
		}()
	}

	// Send initial heartbeat
	if err := a.sendHeartbeat(); err != nil {
		return fmt.Errorf("failed to send initial heartbeat: %w", err)
	}

	a.setStatus(protocol.HeartbeatStatusReady)

	// Process commands from stdin
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := a.processCommands(internalCtx, internalCancel); err != nil && err != io.EOF {
			a.logger.Error("command processing failed", "error", err)
		}
	}()

	// Wait for context cancellation (either from parent or stdin EOF)
	<-internalCtx.Done()

	a.setStatus(protocol.HeartbeatStatusStopping)
	a.sendHeartbeat()

	wg.Wait()
	return nil
}

func (a *MockAgent) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(a.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.sendHeartbeat(); err != nil {
				a.logger.Error("failed to send heartbeat", "error", err)
			}
		}
	}
}

func (a *MockAgent) sendHeartbeat() error {
	a.mu.Lock()
	a.heartbeatSeq++
	seq := a.heartbeatSeq
	status := a.status
	taskID := a.currentTaskID
	lastActivity := a.lastActivityAt
	a.mu.Unlock()

	hb := protocol.Heartbeat{
		Kind: protocol.MessageKindHeartbeat,
		Agent: protocol.AgentRef{
			AgentType: a.agentType,
			AgentID:   a.agentID,
		},
		Seq:            seq,
		Status:         status,
		PID:            a.pid,
		PPID:           a.ppid,
		UptimeS:        time.Since(a.startTime).Seconds(),
		LastActivityAt: lastActivity,
		TaskID:         taskID,
	}

	return a.encoder.Encode(hb)
}

func (a *MockAgent) processCommands(ctx context.Context, cancel context.CancelFunc) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		msg, err := a.decoder.DecodeEnvelope()
		if err == io.EOF {
			a.logger.Info("stdin closed, exiting")
			cancel() // Cancel context to trigger shutdown
			return io.EOF
		}
		if err != nil {
			a.logger.Error("failed to decode message", "error", err)
			continue
		}

		cmd, ok := msg.(*protocol.Command)
		if !ok {
			a.logger.Warn("received non-command message", "type", fmt.Sprintf("%T", msg))
			continue
		}

		a.updateActivity()
		a.setTaskID(cmd.TaskID)
		a.setStatus(protocol.HeartbeatStatusBusy)

		if err := a.handleCommand(cmd); err != nil {
			a.logger.Error("failed to handle command", "error", err, "command_id", cmd.MessageID)
		}

		a.setStatus(protocol.HeartbeatStatusReady)
	}
}

func (a *MockAgent) handleCommand(cmd *protocol.Command) error {
	a.logger.Info("handling command",
		"action", cmd.Action,
		"task_id", cmd.TaskID,
		"message_id", cmd.MessageID,
		"idempotency_key", cmd.IdempotencyKey)

	// Check if we have a scripted response for this action
	if a.script != nil {
		if template, ok := a.script.Responses[string(cmd.Action)]; ok {
			a.logger.Info("using scripted response", "action", cmd.Action)
			return a.executeScriptedResponse(cmd, template)
		}
	}

	// Fall back to default behavior
	switch cmd.Action {
	case protocol.ActionImplement, protocol.ActionImplementChanges:
		return a.handleImplement(cmd)
	case protocol.ActionReview:
		return a.handleReview(cmd)
	case protocol.ActionUpdateSpec:
		return a.handleUpdateSpec(cmd)
	case protocol.ActionIntake, protocol.ActionTaskDiscovery:
		return a.handleIntake(cmd)
	default:
		return fmt.Errorf("unknown action: %s", cmd.Action)
	}
}

func (a *MockAgent) executeScriptedResponse(cmd *protocol.Command, template ResponseTemplate) error {
	// Check if this is an error response
	if template.Error != "" {
		a.logger.Info("returning scripted error", "error", template.Error)
		return fmt.Errorf("%s", template.Error)
	}

	// Apply delay if specified
	if template.DelayMs > 0 {
		delay := time.Duration(template.DelayMs) * time.Millisecond
		a.logger.Info("applying scripted delay", "delay_ms", template.DelayMs)
		time.Sleep(delay)
	}

	// Send all events in order
	for i, evtTemplate := range template.Events {
		a.logger.Info("sending scripted event", "index", i, "type", evtTemplate.Type)

		// Convert artifact templates to protocol artifacts
		var artifacts []protocol.Artifact
		for _, artTemplate := range evtTemplate.Artifacts {
			artifacts = append(artifacts, protocol.Artifact{
				Path:   artTemplate.Path,
				SHA256: artTemplate.SHA256,
				Size:   artTemplate.Size,
			})
		}

		evt := protocol.Event{
			Kind:          protocol.MessageKindEvent,
			MessageID:     fmt.Sprintf("evt-%s", uuid.New().String()[:8]),
			CorrelationID: cmd.CorrelationID,
			TaskID:        cmd.TaskID,
			From: protocol.AgentRef{
				AgentType: a.agentType,
				AgentID:   a.agentID,
			},
			Event:      evtTemplate.Type,
			Status:     evtTemplate.Status,
			Payload:    evtTemplate.Payload,
			Artifacts:  artifacts,
			OccurredAt: time.Now().UTC(),
		}

		if err := a.encoder.Encode(evt); err != nil {
			return fmt.Errorf("failed to send event %d: %w", i, err)
		}
	}

	return nil
}

func (a *MockAgent) handleImplement(cmd *protocol.Command) error {
	// Simulate work
	time.Sleep(100 * time.Millisecond)

	// Send completion event
	evt := protocol.Event{
		Kind:          protocol.MessageKindEvent,
		MessageID:     uuid.New().String(),
		CorrelationID: cmd.CorrelationID,
		TaskID:        cmd.TaskID,
		From: protocol.AgentRef{
			AgentType: a.agentType,
			AgentID:   a.agentID,
		},
		Event:  protocol.EventBuilderCompleted,
		Status: "success",
		Payload: map[string]any{
			"tests": map[string]any{
				"status":  "pass",
				"summary": "Mock tests passed",
			},
		},
		OccurredAt: time.Now().UTC(),
	}

	return a.encoder.Encode(evt)
}

func (a *MockAgent) handleReview(cmd *protocol.Command) error {
	// Simulate work
	time.Sleep(100 * time.Millisecond)

	a.mu.Lock()
	a.reviewCallCount++
	callCount := a.reviewCallCount
	a.mu.Unlock()

	// Determine status based on configuration
	status := protocol.ReviewStatusApproved
	summary := "Mock review approved"

	if callCount <= a.reviewChangesCount {
		status = protocol.ReviewStatusChangesRequested
		summary = fmt.Sprintf("Mock review requesting changes (iteration %d/%d)", callCount, a.reviewChangesCount)
	}

	// Send completion event
	evt := protocol.Event{
		Kind:          protocol.MessageKindEvent,
		MessageID:     uuid.New().String(),
		CorrelationID: cmd.CorrelationID,
		TaskID:        cmd.TaskID,
		From: protocol.AgentRef{
			AgentType: a.agentType,
			AgentID:   a.agentID,
		},
		Event:  protocol.EventReviewCompleted,
		Status: status,
		Payload: map[string]any{
			"summary": summary,
		},
		OccurredAt: time.Now().UTC(),
	}

	return a.encoder.Encode(evt)
}

func (a *MockAgent) handleUpdateSpec(cmd *protocol.Command) error {
	// Simulate work
	time.Sleep(100 * time.Millisecond)

	a.mu.Lock()
	a.specCallCount++
	callCount := a.specCallCount
	a.mu.Unlock()

	// Determine event type based on configuration
	event := protocol.EventSpecUpdated
	summary := "Mock spec updated"

	if callCount <= a.specChangesCount {
		event = protocol.EventSpecChangesRequested
		summary = fmt.Sprintf("Mock spec requesting changes (iteration %d/%d)", callCount, a.specChangesCount)
	}

	// Send spec event
	evt := protocol.Event{
		Kind:          protocol.MessageKindEvent,
		MessageID:     uuid.New().String(),
		CorrelationID: cmd.CorrelationID,
		TaskID:        cmd.TaskID,
		From: protocol.AgentRef{
			AgentType: a.agentType,
			AgentID:   a.agentID,
		},
		Event:  event,
		Status: "success",
		Payload: map[string]any{
			"summary": summary,
		},
		OccurredAt: time.Now().UTC(),
	}

	return a.encoder.Encode(evt)
}

func (a *MockAgent) handleIntake(cmd *protocol.Command) error {
	// Simulate work
	time.Sleep(100 * time.Millisecond)

	// Send proposed tasks event
	evt := protocol.Event{
		Kind:          protocol.MessageKindEvent,
		MessageID:     uuid.New().String(),
		CorrelationID: cmd.CorrelationID,
		TaskID:        cmd.TaskID,
		From: protocol.AgentRef{
			AgentType: a.agentType,
			AgentID:   a.agentID,
		},
		Event: protocol.EventOrchestrationProposedTasks,
		Payload: map[string]any{
			"plan_candidates": []any{
				map[string]any{"path": "PLAN.md", "confidence": 0.9},
			},
			"derived_tasks": []any{
				map[string]any{
					"id":    "T-001",
					"title": "Mock task",
				},
			},
		},
		OccurredAt: time.Now().UTC(),
	}

	return a.encoder.Encode(evt)
}

func (a *MockAgent) updateActivity() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastActivityAt = time.Now().UTC()
}

func (a *MockAgent) setTaskID(taskID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.currentTaskID = taskID
}

func (a *MockAgent) setStatus(status protocol.HeartbeatStatus) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status = status
}
