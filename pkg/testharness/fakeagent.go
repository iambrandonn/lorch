package testharness

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/iambrandonn/lorch/internal/ndjson"
	"github.com/iambrandonn/lorch/internal/protocol"
)

// FakeAgent is an in-process mock agent for testing
type FakeAgent struct {
	AgentType         protocol.AgentType
	AgentID           string
	HeartbeatInterval time.Duration
	DisableHeartbeat  bool

	// Behavior controls
	ImplementDelay    time.Duration
	ReviewResult      string // "approved" or "changes_requested"
	SpecResult        string // "updated", "no_changes_needed", or "changes_requested"

	// IO
	stdin  io.Reader
	stdout io.Writer
	logger *slog.Logger

	// Internal state
	mu             sync.Mutex
	heartbeatSeq   int64
	lastActivityAt time.Time
	currentTaskID  string
	status         protocol.HeartbeatStatus
	startTime      time.Time
	pid            int
	ppid           int
}

// NewFakeAgent creates an in-process fake agent
func NewFakeAgent(agentType protocol.AgentType, stdin io.Reader, stdout io.Writer, logger *slog.Logger) *FakeAgent {
	agentID := fmt.Sprintf("%s#fake-%s", agentType, uuid.New().String()[:8])

	return &FakeAgent{
		AgentType:         agentType,
		AgentID:           agentID,
		HeartbeatInterval: 1 * time.Second, // Fast heartbeat for tests
		stdin:             stdin,
		stdout:            stdout,
		logger:            logger,
		startTime:         time.Now(),
		pid:               12345, // Fake PID for testing
		ppid:              12344,
		status:            protocol.HeartbeatStatusReady,
		ReviewResult:      protocol.ReviewStatusApproved,
		SpecResult:        "updated",
	}
}

// Run starts the fake agent
func (a *FakeAgent) Run(ctx context.Context) error {
	a.updateActivity()
	a.setStatus(protocol.HeartbeatStatusStarting)

	// Create internal context that can be cancelled on stdin EOF
	internalCtx, internalCancel := context.WithCancel(ctx)
	defer internalCancel()

	encoder := ndjson.NewEncoder(a.stdout, a.logger)
	decoder := ndjson.NewDecoder(a.stdin, a.logger)

	// Start heartbeat goroutine
	var wg sync.WaitGroup
	if !a.DisableHeartbeat {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.heartbeatLoop(internalCtx, encoder)
		}()
	}

	// Send initial heartbeat
	if err := a.sendHeartbeat(encoder); err != nil {
		return fmt.Errorf("failed to send initial heartbeat: %w", err)
	}

	a.setStatus(protocol.HeartbeatStatusReady)

	// Process commands
	wg.Add(1)
	go func() {
		defer wg.Done()
		a.processCommands(internalCtx, internalCancel, encoder, decoder)
	}()

	<-internalCtx.Done()

	a.setStatus(protocol.HeartbeatStatusStopping)
	a.sendHeartbeat(encoder)

	wg.Wait()
	return nil
}

func (a *FakeAgent) heartbeatLoop(ctx context.Context, encoder *ndjson.Encoder) {
	ticker := time.NewTicker(a.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.sendHeartbeat(encoder); err != nil {
				a.logger.Error("failed to send heartbeat", "error", err)
			}
		}
	}
}

func (a *FakeAgent) sendHeartbeat(encoder *ndjson.Encoder) error {
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
			AgentType: a.AgentType,
			AgentID:   a.AgentID,
		},
		Seq:            seq,
		Status:         status,
		PID:            a.pid,
		PPID:           a.ppid,
		UptimeS:        time.Since(a.startTime).Seconds(),
		LastActivityAt: lastActivity,
		TaskID:         taskID,
	}

	return encoder.Encode(hb)
}

func (a *FakeAgent) processCommands(ctx context.Context, cancel context.CancelFunc, encoder *ndjson.Encoder, decoder *ndjson.Decoder) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := decoder.DecodeEnvelope()
		if err == io.EOF {
			cancel() // Cancel context on stdin EOF
			return
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

		a.handleCommand(cmd, encoder)

		a.setStatus(protocol.HeartbeatStatusReady)
	}
}

func (a *FakeAgent) handleCommand(cmd *protocol.Command, encoder *ndjson.Encoder) {
	switch cmd.Action {
	case protocol.ActionImplement, protocol.ActionImplementChanges:
		a.handleImplement(cmd, encoder)
	case protocol.ActionReview:
		a.handleReview(cmd, encoder)
	case protocol.ActionUpdateSpec:
		a.handleUpdateSpec(cmd, encoder)
	default:
		a.logger.Warn("unknown action", "action", cmd.Action)
	}
}

func (a *FakeAgent) handleImplement(cmd *protocol.Command, encoder *ndjson.Encoder) {
	// Simulate work
	if a.ImplementDelay > 0 {
		time.Sleep(a.ImplementDelay)
	}

	evt := protocol.Event{
		Kind:          protocol.MessageKindEvent,
		MessageID:     uuid.New().String(),
		CorrelationID: cmd.CorrelationID,
		TaskID:        cmd.TaskID,
		From: protocol.AgentRef{
			AgentType: a.AgentType,
			AgentID:   a.AgentID,
		},
		Event:  protocol.EventBuilderCompleted,
		Status: "success",
		Payload: map[string]any{
			"tests": map[string]any{
				"status":  "pass",
				"summary": "Tests passed",
			},
		},
		OccurredAt: time.Now().UTC(),
	}

	encoder.Encode(evt)
}

func (a *FakeAgent) handleReview(cmd *protocol.Command, encoder *ndjson.Encoder) {
	evt := protocol.Event{
		Kind:          protocol.MessageKindEvent,
		MessageID:     uuid.New().String(),
		CorrelationID: cmd.CorrelationID,
		TaskID:        cmd.TaskID,
		From: protocol.AgentRef{
			AgentType: a.AgentType,
			AgentID:   a.AgentID,
		},
		Event:  protocol.EventReviewCompleted,
		Status: a.ReviewResult,
		Payload: map[string]any{
			"summary": fmt.Sprintf("Review %s", a.ReviewResult),
		},
		OccurredAt: time.Now().UTC(),
	}

	encoder.Encode(evt)
}

func (a *FakeAgent) handleUpdateSpec(cmd *protocol.Command, encoder *ndjson.Encoder) {
	var event string
	switch a.SpecResult {
	case "updated":
		event = protocol.EventSpecUpdated
	case "no_changes_needed":
		event = protocol.EventSpecNoChangesNeeded
	case "changes_requested":
		event = protocol.EventSpecChangesRequested
	default:
		event = protocol.EventSpecUpdated
	}

	evt := protocol.Event{
		Kind:          protocol.MessageKindEvent,
		MessageID:     uuid.New().String(),
		CorrelationID: cmd.CorrelationID,
		TaskID:        cmd.TaskID,
		From: protocol.AgentRef{
			AgentType: a.AgentType,
			AgentID:   a.AgentID,
		},
		Event:      event,
		Status:     "success",
		Payload:    map[string]any{},
		OccurredAt: time.Now().UTC(),
	}

	encoder.Encode(evt)
}

func (a *FakeAgent) updateActivity() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastActivityAt = time.Now().UTC()
}

func (a *FakeAgent) setTaskID(taskID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.currentTaskID = taskID
}

func (a *FakeAgent) setStatus(status protocol.HeartbeatStatus) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status = status
}
