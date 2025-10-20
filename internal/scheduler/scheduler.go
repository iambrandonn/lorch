package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/iambrandonn/lorch/internal/idempotency"
	"github.com/iambrandonn/lorch/internal/ledger"
	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/iambrandonn/lorch/internal/receipt"
	"github.com/iambrandonn/lorch/internal/supervisor"
)

// EventLogger writes protocol messages to persistent storage
type EventLogger interface {
	WriteCommand(*protocol.Command) error
	WriteEvent(*protocol.Event) error
	WriteHeartbeat(*protocol.Heartbeat) error
}

// TranscriptFormatter formats messages for console display
type TranscriptFormatter interface {
	FormatEvent(*protocol.Event) string
	FormatHeartbeat(*protocol.Heartbeat) string
	FormatCommand(*protocol.Command) string
}

// Stage represents the current stage of task execution
type Stage string

const (
	StageImplement     Stage = "implement"
	StageReview        Stage = "review"
	StageSpecMaintain  Stage = "spec_maintain"
	StageComplete      Stage = "complete"
)

// Scheduler orchestrates single-agent-at-a-time execution
type Scheduler struct {
	builder        *supervisor.AgentSupervisor
	reviewer       *supervisor.AgentSupervisor
	specMaintainer *supervisor.AgentSupervisor
	logger         *slog.Logger

	// Snapshot ID for version pinning
	snapshotID string

	// Workspace root for receipt writing
	workspaceRoot string

	// Step counter for receipts
	stepCounter int

	// Event handlers
	onEvent     func(*protocol.Event)
	onHeartbeat func(*protocol.Heartbeat)

	// Optional logging/formatting (for CLI integration)
	eventLog   EventLogger
	transcript TranscriptFormatter

	// Tracking for current command execution
	currentCommand *protocol.Command
	currentEvents  []*protocol.Event
}

// NewScheduler creates a new scheduler
func NewScheduler(
	builder *supervisor.AgentSupervisor,
	reviewer *supervisor.AgentSupervisor,
	specMaintainer *supervisor.AgentSupervisor,
	logger *slog.Logger,
) *Scheduler {
	return &Scheduler{
		builder:        builder,
		reviewer:       reviewer,
		specMaintainer: specMaintainer,
		logger:         logger,
	}
}

// SetEventHandler sets the callback for events
func (s *Scheduler) SetEventHandler(handler func(*protocol.Event)) {
	s.onEvent = handler
}

// SetHeartbeatHandler sets the callback for heartbeats
func (s *Scheduler) SetHeartbeatHandler(handler func(*protocol.Heartbeat)) {
	s.onHeartbeat = handler
}

// SetEventLogger sets the event logger for persistence
func (s *Scheduler) SetEventLogger(logger EventLogger) {
	s.eventLog = logger
}

// SetTranscriptFormatter sets the transcript formatter for console output
func (s *Scheduler) SetTranscriptFormatter(formatter TranscriptFormatter) {
	s.transcript = formatter
}

// SetSnapshotID sets the snapshot ID for version pinning
func (s *Scheduler) SetSnapshotID(snapshotID string) {
	s.snapshotID = snapshotID
}

// SetWorkspaceRoot sets the workspace root for receipt writing
func (s *Scheduler) SetWorkspaceRoot(workspaceRoot string) {
	s.workspaceRoot = workspaceRoot
}

// ExecuteTask runs the full Implement → Review → Spec Maintenance flow
func (s *Scheduler) ExecuteTask(ctx context.Context, taskID string, goal string) error {
	s.logger.Info("starting task execution", "task_id", taskID, "goal", goal)

	// Stage 1: Implement
	s.logger.Info("stage: implement", "task_id", taskID)
	if err := s.executeImplement(ctx, taskID, goal); err != nil {
		return fmt.Errorf("implement failed: %w", err)
	}

	// Stage 2: Review (with loop for changes_requested)
	s.logger.Info("stage: review", "task_id", taskID)
	for {
		reviewResult, err := s.executeReview(ctx, taskID)
		if err != nil {
			return fmt.Errorf("review failed: %w", err)
		}

		if reviewResult == protocol.ReviewStatusApproved {
			break // Exit review loop
		}

		// Changes requested, implement changes
		s.logger.Info("changes requested, implementing fixes", "task_id", taskID)
		if err := s.executeImplementChanges(ctx, taskID); err != nil {
			return fmt.Errorf("implement_changes failed: %w", err)
		}
	}

	// Stage 3: Spec Maintenance (with loop for changes_requested)
	s.logger.Info("stage: spec maintenance", "task_id", taskID)
	for {
		specResult, err := s.executeSpecMaintenance(ctx, taskID)
		if err != nil {
			return fmt.Errorf("spec maintenance failed: %w", err)
		}

		// Terminal events: spec.updated or spec.no_changes_needed
		if specResult == protocol.EventSpecUpdated || specResult == protocol.EventSpecNoChangesNeeded {
			break // Task complete
		}

		// Changes requested, re-implement, re-review, then try spec maintenance again
		s.logger.Info("spec changes requested, re-implementing", "task_id", taskID)
		if err := s.executeImplementChanges(ctx, taskID); err != nil {
			return fmt.Errorf("implement_changes (spec loop) failed: %w", err)
		}

		// Re-review after changes
		for {
			reviewResult, err := s.executeReview(ctx, taskID)
			if err != nil {
				return fmt.Errorf("review (spec loop) failed: %w", err)
			}

			if reviewResult == protocol.ReviewStatusApproved {
				break
			}

			s.logger.Info("changes requested in spec loop, fixing", "task_id", taskID)
			if err := s.executeImplementChanges(ctx, taskID); err != nil {
				return fmt.Errorf("implement_changes (spec loop review) failed: %w", err)
			}
		}
	}

	s.logger.Info("task execution complete", "task_id", taskID)
	return nil
}

// ResumeTask continues a task from where it left off by checking the ledger
// Only commands that don't have terminal events will be executed
func (s *Scheduler) ResumeTask(ctx context.Context, taskID string, goal string, lg *ledger.Ledger) error {
	s.logger.Info("resuming task execution", "task_id", taskID, "goal", goal)

	// Get terminal events from ledger
	terminals := lg.GetTerminalEvents()

	// Check if implement step is complete
	implementComplete := false
	for _, cmd := range lg.Commands {
		if cmd.TaskID == taskID && (cmd.Action == protocol.ActionImplement || cmd.Action == protocol.ActionImplementChanges) {
			if _, hasTerminal := terminals[cmd.MessageID]; hasTerminal {
				implementComplete = true
				s.logger.Info("implement step already complete, skipping", "task_id", taskID)
				break
			}
		}
	}

	// Stage 1: Implement (if not complete)
	if !implementComplete {
		s.logger.Info("stage: implement (resuming)", "task_id", taskID)
		if err := s.executeImplement(ctx, taskID, goal); err != nil {
			return fmt.Errorf("implement failed: %w", err)
		}
	}

	// Check if review step is complete
	reviewComplete := false
	reviewApproved := false
	for _, cmd := range lg.Commands {
		if cmd.TaskID == taskID && cmd.Action == protocol.ActionReview {
			if _, hasTerminal := terminals[cmd.MessageID]; hasTerminal {
				reviewComplete = true
				// Find the terminal event to check status
				for _, evt := range lg.Events {
					if evt.CorrelationID == cmd.CorrelationID && evt.Event == protocol.EventReviewCompleted {
						if evt.Status == protocol.ReviewStatusApproved {
							reviewApproved = true
						}
						break
					}
				}
				s.logger.Info("review step already complete", "approved", reviewApproved, "task_id", taskID)
				break
			}
		}
	}

	// Stage 2: Review (with loop for changes_requested, if not complete or not approved)
	if !reviewComplete || !reviewApproved {
		s.logger.Info("stage: review (resuming)", "task_id", taskID)
		for {
			reviewResult, err := s.executeReview(ctx, taskID)
			if err != nil {
				return fmt.Errorf("review failed: %w", err)
			}

			if reviewResult == protocol.ReviewStatusApproved {
				break // Exit review loop
			}

			// Changes requested, implement changes
			s.logger.Info("changes requested, implementing fixes", "task_id", taskID)
			if err := s.executeImplementChanges(ctx, taskID); err != nil {
				return fmt.Errorf("implement_changes failed: %w", err)
			}
		}
	}

	// Check if spec maintenance step is complete
	specComplete := false
	for _, cmd := range lg.Commands {
		if cmd.TaskID == taskID && cmd.Action == protocol.ActionUpdateSpec {
			if _, hasTerminal := terminals[cmd.MessageID]; hasTerminal {
				specComplete = true
				s.logger.Info("spec maintenance step already complete, skipping", "task_id", taskID)
				break
			}
		}
	}

	// Stage 3: Spec Maintenance (if not complete)
	if !specComplete {
		s.logger.Info("stage: spec maintenance (resuming)", "task_id", taskID)
		for {
			specResult, err := s.executeSpecMaintenance(ctx, taskID)
			if err != nil {
				return fmt.Errorf("spec maintenance failed: %w", err)
			}

			// Terminal events: spec.updated or spec.no_changes_needed
			if specResult == protocol.EventSpecUpdated || specResult == protocol.EventSpecNoChangesNeeded {
				break // Task complete
			}

			// Changes requested, re-implement, re-review, then try spec maintenance again
			s.logger.Info("spec changes requested, re-implementing", "task_id", taskID)
			if err := s.executeImplementChanges(ctx, taskID); err != nil {
				return fmt.Errorf("implement_changes (spec loop) failed: %w", err)
			}

			// Re-review after changes
			for {
				reviewResult, err := s.executeReview(ctx, taskID)
				if err != nil {
					return fmt.Errorf("review (spec loop) failed: %w", err)
				}

				if reviewResult == protocol.ReviewStatusApproved {
					break
				}

				s.logger.Info("changes requested in spec loop, fixing", "task_id", taskID)
				if err := s.executeImplementChanges(ctx, taskID); err != nil {
					return fmt.Errorf("implement_changes (spec loop review) failed: %w", err)
				}
			}
		}
	}

	s.logger.Info("task resume complete", "task_id", taskID)
	return nil
}

func (s *Scheduler) sendCommand(sup *supervisor.AgentSupervisor, cmd *protocol.Command) error {
	// Track this command for receipt generation
	s.currentCommand = cmd
	s.currentEvents = make([]*protocol.Event, 0)
	s.stepCounter++

	// Log command to event log
	if s.eventLog != nil {
		if err := s.eventLog.WriteCommand(cmd); err != nil {
			s.logger.Warn("failed to log command", "error", err)
		}
	}

	// Format command for console
	if s.transcript != nil {
		fmt.Println(s.transcript.FormatCommand(cmd))
	}

	return sup.SendCommand(cmd)
}

func (s *Scheduler) writeReceipt() error {
	// Only write receipts if we have a workspace root and a command
	if s.workspaceRoot == "" || s.currentCommand == nil {
		return nil
	}

	// Create receipt from command and collected events
	rec := receipt.NewReceipt(s.currentCommand, s.stepCounter, s.currentEvents)

	// Determine receipt path
	receiptPath := filepath.Join(s.workspaceRoot, "receipts", s.currentCommand.TaskID, fmt.Sprintf("step-%d.json", s.stepCounter))

	// Write receipt
	if err := receipt.WriteReceipt(rec, receiptPath); err != nil {
		return fmt.Errorf("failed to write receipt: %w", err)
	}

	s.logger.Info("wrote receipt",
		"task_id", s.currentCommand.TaskID,
		"step", s.stepCounter,
		"path", receiptPath)

	return nil
}

func (s *Scheduler) executeImplement(ctx context.Context, taskID string, goal string) error {
	cmd := s.makeCommand(
		taskID,
		protocol.AgentTypeBuilder,
		protocol.ActionImplement,
		map[string]any{"goal": goal},
	)

	if err := s.sendCommand(s.builder, cmd); err != nil {
		return err
	}

	if err := s.waitForEvent(ctx, s.builder, protocol.EventBuilderCompleted, taskID); err != nil {
		return err
	}

	// Write receipt after successful completion
	if err := s.writeReceipt(); err != nil {
		s.logger.Warn("failed to write receipt", "error", err)
	}

	return nil
}

func (s *Scheduler) executeImplementChanges(ctx context.Context, taskID string) error {
	cmd := s.makeCommand(
		taskID,
		protocol.AgentTypeBuilder,
		protocol.ActionImplementChanges,
		map[string]any{},
	)

	if err := s.sendCommand(s.builder, cmd); err != nil {
		return err
	}

	if err := s.waitForEvent(ctx, s.builder, protocol.EventBuilderCompleted, taskID); err != nil {
		return err
	}

	// Write receipt after successful completion
	if err := s.writeReceipt(); err != nil {
		s.logger.Warn("failed to write receipt", "error", err)
	}

	return nil
}

func (s *Scheduler) executeReview(ctx context.Context, taskID string) (string, error) {
	cmd := s.makeCommand(
		taskID,
		protocol.AgentTypeReviewer,
		protocol.ActionReview,
		map[string]any{},
	)

	if err := s.sendCommand(s.reviewer, cmd); err != nil {
		return "", err
	}

	evt, err := s.waitForEventReturn(ctx, s.reviewer, protocol.EventReviewCompleted, taskID)
	if err != nil {
		return "", err
	}

	// Write receipt after successful completion
	if err := s.writeReceipt(); err != nil {
		s.logger.Warn("failed to write receipt", "error", err)
	}

	return evt.Status, nil
}

func (s *Scheduler) executeSpecMaintenance(ctx context.Context, taskID string) (string, error) {
	cmd := s.makeCommand(
		taskID,
		protocol.AgentTypeSpecMaintainer,
		protocol.ActionUpdateSpec,
		map[string]any{},
	)

	if err := s.sendCommand(s.specMaintainer, cmd); err != nil {
		return "", err
	}

	// Wait for one of the terminal spec events
	var eventType string
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case evt, ok := <-s.specMaintainer.Events():
			if !ok {
				return "", fmt.Errorf("spec maintainer events channel closed")
			}

			s.notifyEvent(evt)

			if evt.TaskID != taskID {
				continue
			}

			// Check for terminal events
			switch evt.Event {
			case protocol.EventSpecUpdated,
				protocol.EventSpecNoChangesNeeded,
				protocol.EventSpecChangesRequested:
				eventType = evt.Event
				goto done
			}
		case hb := <-s.specMaintainer.Heartbeats():
			s.notifyHeartbeat(hb)
		}
	}

done:
	// Write receipt after successful completion
	if err := s.writeReceipt(); err != nil {
		s.logger.Warn("failed to write receipt", "error", err)
	}

	return eventType, nil
}

func (s *Scheduler) makeCommand(
	taskID string,
	agentType protocol.AgentType,
	action protocol.Action,
	inputs map[string]any,
) *protocol.Command {
	// Use configured snapshot ID, or placeholder if not set
	snapshotID := s.snapshotID
	if snapshotID == "" {
		snapshotID = "snap-test-0001" // Placeholder for tests
	}

	// Generate unique correlation ID for this command
	correlationID := fmt.Sprintf("corr-%s-%s-%s",
		taskID,
		string(action),
		uuid.New().String()[:8])

	// Create command with all required fields
	cmd := &protocol.Command{
		Kind:           protocol.MessageKindCommand,
		MessageID:      uuid.New().String(),
		CorrelationID:  correlationID,
		TaskID:         taskID,
		IdempotencyKey: "", // Will be set below
		To: protocol.AgentRef{
			AgentType: agentType,
		},
		Action:          action,
		Inputs:          inputs,
		ExpectedOutputs: []protocol.ExpectedOutput{},
		Version: protocol.Version{
			SnapshotID: snapshotID,
		},
		Deadline: time.Now().Add(10 * time.Minute).UTC(),
		Retry: protocol.Retry{
			Attempt:     0,
			MaxAttempts: 3,
		},
		Priority: 5,
	}

	// Generate idempotency key
	ik, err := idempotency.GenerateIK(cmd)
	if err != nil {
		// Fall back to simple key if generation fails (shouldn't happen in practice)
		s.logger.Warn("failed to generate idempotency key, using fallback", "error", err)
		ik = fmt.Sprintf("fallback-ik:%s:%s:%s", action, taskID, snapshotID)
	}
	cmd.IdempotencyKey = ik

	return cmd
}

func (s *Scheduler) waitForEvent(ctx context.Context, sup *supervisor.AgentSupervisor, eventType string, taskID string) error {
	_, err := s.waitForEventReturn(ctx, sup, eventType, taskID)
	return err
}

func (s *Scheduler) waitForEventReturn(ctx context.Context, sup *supervisor.AgentSupervisor, eventType string, taskID string) (*protocol.Event, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case evt, ok := <-sup.Events():
			if !ok {
				return nil, fmt.Errorf("agent events channel closed")
			}

			s.notifyEvent(evt)

			if evt.TaskID == taskID && evt.Event == eventType {
				return evt, nil
			}
		case hb := <-sup.Heartbeats():
			s.notifyHeartbeat(hb)
		}
	}
}

func (s *Scheduler) notifyEvent(evt *protocol.Event) {
	// Track event for current command (if correlation IDs match)
	if s.currentCommand != nil && evt.CorrelationID == s.currentCommand.CorrelationID {
		s.currentEvents = append(s.currentEvents, evt)
	}

	// Log event to event log
	if s.eventLog != nil {
		if err := s.eventLog.WriteEvent(evt); err != nil {
			s.logger.Warn("failed to log event", "error", err)
		}
	}

	// Format event for console
	if s.transcript != nil {
		fmt.Println(s.transcript.FormatEvent(evt))
	}

	// Call custom handler if set
	if s.onEvent != nil {
		s.onEvent(evt)
	}
}

func (s *Scheduler) notifyHeartbeat(hb *protocol.Heartbeat) {
	// Log heartbeat to event log
	if s.eventLog != nil {
		if err := s.eventLog.WriteHeartbeat(hb); err != nil {
			s.logger.Warn("failed to log heartbeat", "error", err)
		}
	}

	// Optionally format heartbeat for console (usually too verbose, skip by default)
	// if s.transcript != nil {
	// 	fmt.Println(s.transcript.FormatHeartbeat(hb))
	// }

	// Call custom handler if set
	if s.onHeartbeat != nil {
		s.onHeartbeat(hb)
	}
}
