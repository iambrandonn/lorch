package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/iambrandonn/lorch/internal/ndjson"
	"github.com/iambrandonn/lorch/internal/protocol"
)

// LLMAgent represents the main agent implementation
type LLMAgent struct {
	// Configuration
	config AgentConfig

	// NDJSON I/O
	encoder *ndjson.Encoder
	decoder *ndjson.Decoder

	// Injected interfaces
	llmCaller    LLMCaller
	receiptStore ReceiptStore
	fsProvider   FSProvider
	eventEmitter EventEmitter

	// Heartbeat fields
	startTime              time.Time
	hbSeq                  int64
	currentStatus          protocol.HeartbeatStatus
	currentTaskID          string
	lastActivityAt         time.Time
	agentID                string
	mu                     sync.Mutex

	// Version tracking
	firstObservedSnapshotID string
}

// NewLLMAgent creates a new LLM agent with the given configuration
func NewLLMAgent(cfg *AgentConfig) (*LLMAgent, error) {
	// Create real implementations of interfaces
	llmConfig := DefaultLLMConfig(cfg.LLMCLI)
	llmCaller := NewRealLLMCaller(llmConfig)
	receiptStore := NewRealReceiptStore(cfg.Workspace)
	fsProvider := NewRealFSProvider(cfg.Workspace)

	// Generate unique agent ID
	agentID := fmt.Sprintf("%s-%d", string(cfg.Role), time.Now().UnixNano())

	agent := &LLMAgent{
		config:       *cfg,
		llmCaller:    llmCaller,
		receiptStore: receiptStore,
		fsProvider:   fsProvider,
		startTime:    time.Now(),
		lastActivityAt: time.Now(),
		currentStatus: protocol.HeartbeatStatusStarting,
		agentID:      agentID,
	}

	// Create event emitter (will be set up in Run method with encoder)
	return agent, nil
}

// Run starts the agent's NDJSON I/O loop
func (a *LLMAgent) Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) error {
	// Set up encoder/decoder
	a.encoder = ndjson.NewEncoder(stdout, a.config.Logger)
	a.decoder = ndjson.NewDecoder(stdin, a.config.Logger)

	// Create event emitter with encoder
	a.eventEmitter = NewRealEventEmitter(a.encoder, a.config.Logger, a.config.Role, a.agentID)

	// Initialize heartbeat timers before first emission
	a.startTime = time.Now()
	a.lastActivityAt = time.Now()
	a.currentStatus = protocol.HeartbeatStatusStarting

	// Start heartbeat goroutine
	go a.heartbeatLoop(ctx)

	// Send initial heartbeat (status: starting)
	if err := a.sendHeartbeat(protocol.HeartbeatStatusStarting, ""); err != nil {
		return fmt.Errorf("failed to send starting heartbeat: %w", err)
	}

	// Mark ready
	a.setStatus(protocol.HeartbeatStatusReady, "")
	if err := a.sendHeartbeat(protocol.HeartbeatStatusReady, ""); err != nil {
		return fmt.Errorf("failed to send ready heartbeat: %w", err)
	}

	// Process commands from stdin
	for {
		select {
		case <-ctx.Done():
			a.setStatus(protocol.HeartbeatStatusStopping, "")
			a.sendHeartbeat(protocol.HeartbeatStatusStopping, "")
			return nil
		default:
			var cmd protocol.Command
			if err := a.decoder.Decode(&cmd); err != nil {
				if err == io.EOF {
					return nil // Clean shutdown
				}
				return fmt.Errorf("failed to decode command: %w", err)
			}

			// Route to handler
			if err := a.handleCommand(&cmd); err != nil {
				a.config.Logger.Error("command failed", "action", cmd.Action, "error", err)
				// Send error event but continue processing
				a.eventEmitter.SendErrorEvent(&cmd, "command_failed", err.Error())
			}
		}
	}
}

// handleCommand routes commands to appropriate handlers
func (a *LLMAgent) handleCommand(cmd *protocol.Command) error {
	a.config.Logger.Info("handling command", "action", cmd.Action, "task_id", cmd.TaskID)

	// Update activity timestamp
	a.updateActivity()

	// Version mismatch detection
	a.mu.Lock()
	if a.firstObservedSnapshotID == "" {
		a.firstObservedSnapshotID = cmd.Version.SnapshotID
		a.config.Logger.Info("recorded initial snapshot", "snapshot_id", a.firstObservedSnapshotID)
	} else if cmd.Version.SnapshotID != a.firstObservedSnapshotID {
		a.mu.Unlock()
		return a.eventEmitter.SendErrorEvent(cmd, "version_mismatch",
			fmt.Sprintf("expected snapshot %s, received %s", a.firstObservedSnapshotID, cmd.Version.SnapshotID))
	}
	a.mu.Unlock()

	// Set status to busy
	a.setStatus(protocol.HeartbeatStatusBusy, cmd.TaskID)
	defer a.setStatus(protocol.HeartbeatStatusReady, "")

	// Route to appropriate handler based on action
	switch cmd.Action {
	case protocol.ActionIntake, protocol.ActionTaskDiscovery:
		if a.config.Role != protocol.AgentTypeOrchestration {
			return fmt.Errorf("action %s not supported for role %s", cmd.Action, a.config.Role)
		}
		return a.handleOrchestration(cmd)

	case protocol.ActionImplement, protocol.ActionImplementChanges:
		if a.config.Role != protocol.AgentTypeBuilder {
			return fmt.Errorf("action %s not supported for role %s", cmd.Action, a.config.Role)
		}
		return fmt.Errorf("builder not yet implemented")

	case protocol.ActionReview:
		if a.config.Role != protocol.AgentTypeReviewer {
			return fmt.Errorf("action %s not supported for role %s", cmd.Action, a.config.Role)
		}
		return fmt.Errorf("reviewer not yet implemented")

	case protocol.ActionUpdateSpec:
		if a.config.Role != protocol.AgentTypeSpecMaintainer {
			return fmt.Errorf("action %s not supported for role %s", cmd.Action, a.config.Role)
		}
		return fmt.Errorf("spec maintainer not yet implemented")

	default:
		return fmt.Errorf("unknown action: %s", cmd.Action)
	}
}

// handleOrchestration handles orchestration-specific commands
func (a *LLMAgent) handleOrchestration(cmd *protocol.Command) error {
	// TODO: Implement orchestration logic
	// This is a placeholder that will be implemented by workstream F
	a.config.Logger.Info("handling orchestration command", "action", cmd.Action)

	// Check if eventEmitter is set up (it's only set up in Run method)
	if a.eventEmitter == nil {
		return fmt.Errorf("eventEmitter not initialized - agent not running")
	}

	// For now, emit a simple success event
	evt := a.eventEmitter.NewEvent(cmd, "orchestration.proposed_tasks")
	evt.Status = "success"
	evt.Payload = map[string]any{
		"plan_candidates": []map[string]any{
			{"path": "PLAN.md", "confidence": 0.9},
		},
		"derived_tasks": []map[string]any{
			{"id": "T-001", "title": "Mock task", "files": []string{"test.go"}},
		},
		"notes": "Mock orchestration response",
	}

	return a.eventEmitter.EncodeEventCapped(evt)
}

// setStatus updates the agent status and activity timestamp
func (a *LLMAgent) setStatus(status protocol.HeartbeatStatus, taskID string) {
	a.mu.Lock()
	a.currentStatus = status
	a.currentTaskID = taskID
	a.lastActivityAt = time.Now()
	a.mu.Unlock()
}

// updateActivity updates the last activity timestamp
func (a *LLMAgent) updateActivity() {
	a.mu.Lock()
	a.lastActivityAt = time.Now()
	a.mu.Unlock()
}

// heartbeatLoop sends heartbeats at regular intervals
func (a *LLMAgent) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.mu.Lock()
			status := a.currentStatus
			taskID := a.currentTaskID
			a.mu.Unlock()

			// Send heartbeat (errors are logged but don't stop the loop)
			if err := a.sendHeartbeat(status, taskID); err != nil {
				a.config.Logger.Error("failed to send heartbeat", "error", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

// sendHeartbeat sends a heartbeat message
func (a *LLMAgent) sendHeartbeat(status protocol.HeartbeatStatus, taskID string) error {
	a.mu.Lock()
	a.hbSeq++
	seq := a.hbSeq
	lastActivityAt := a.lastActivityAt
	a.mu.Unlock()

	hb := protocol.Heartbeat{
		Kind: protocol.MessageKindHeartbeat,
		Agent: protocol.AgentRef{
			AgentType: a.config.Role,
			AgentID:   a.agentID,
		},
		Seq:            seq,
		Status:         status,
		PID:            os.Getpid(),
		PPID:           os.Getppid(),
		UptimeS:        time.Since(a.startTime).Seconds(),
		LastActivityAt: lastActivityAt,
		TaskID:         taskID,
		// Stats optional - can add CPU/memory monitoring
	}
	return a.encoder.Encode(hb)
}
