package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/iambrandonn/lorch/internal/protocol"
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

	// Event handlers
	onEvent func(*protocol.Event)
	onHeartbeat func(*protocol.Heartbeat)

	// Optional logging/formatting (for CLI integration)
	eventLog   EventLogger
	transcript TranscriptFormatter
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

// ExecuteTask runs the full Implement → Review → Spec Maintenance flow
func (s *Scheduler) ExecuteTask(ctx context.Context, taskID string, goal string) error {
	s.logger.Info("starting task execution", "task_id", taskID, "goal", goal)

	// Generate correlation ID for this execution
	correlationID := fmt.Sprintf("corr-%s-%s", taskID, uuid.New().String()[:8])

	// Stage 1: Implement
	s.logger.Info("stage: implement", "task_id", taskID)
	if err := s.executeImplement(ctx, taskID, correlationID, goal); err != nil {
		return fmt.Errorf("implement failed: %w", err)
	}

	// Stage 2: Review (with loop for changes_requested)
	s.logger.Info("stage: review", "task_id", taskID)
	for {
		reviewResult, err := s.executeReview(ctx, taskID, correlationID)
		if err != nil {
			return fmt.Errorf("review failed: %w", err)
		}

		if reviewResult == protocol.ReviewStatusApproved {
			break // Exit review loop
		}

		// Changes requested, implement changes
		s.logger.Info("changes requested, implementing fixes", "task_id", taskID)
		if err := s.executeImplementChanges(ctx, taskID, correlationID); err != nil {
			return fmt.Errorf("implement_changes failed: %w", err)
		}
	}

	// Stage 3: Spec Maintenance (with loop for changes_requested)
	s.logger.Info("stage: spec maintenance", "task_id", taskID)
	for {
		specResult, err := s.executeSpecMaintenance(ctx, taskID, correlationID)
		if err != nil {
			return fmt.Errorf("spec maintenance failed: %w", err)
		}

		// Terminal events: spec.updated or spec.no_changes_needed
		if specResult == protocol.EventSpecUpdated || specResult == protocol.EventSpecNoChangesNeeded {
			break // Task complete
		}

		// Changes requested, re-implement, re-review, then try spec maintenance again
		s.logger.Info("spec changes requested, re-implementing", "task_id", taskID)
		if err := s.executeImplementChanges(ctx, taskID, correlationID); err != nil {
			return fmt.Errorf("implement_changes (spec loop) failed: %w", err)
		}

		// Re-review after changes
		for {
			reviewResult, err := s.executeReview(ctx, taskID, correlationID)
			if err != nil {
				return fmt.Errorf("review (spec loop) failed: %w", err)
			}

			if reviewResult == protocol.ReviewStatusApproved {
				break
			}

			s.logger.Info("changes requested in spec loop, fixing", "task_id", taskID)
			if err := s.executeImplementChanges(ctx, taskID, correlationID); err != nil {
				return fmt.Errorf("implement_changes (spec loop review) failed: %w", err)
			}
		}
	}

	s.logger.Info("task execution complete", "task_id", taskID)
	return nil
}

func (s *Scheduler) sendCommand(sup *supervisor.AgentSupervisor, cmd *protocol.Command) error {
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

func (s *Scheduler) executeImplement(ctx context.Context, taskID string, correlationID string, goal string) error {
	cmd := s.makeCommand(
		taskID,
		correlationID,
		protocol.AgentTypeBuilder,
		protocol.ActionImplement,
		map[string]any{"goal": goal},
	)

	if err := s.sendCommand(s.builder, cmd); err != nil {
		return err
	}

	return s.waitForEvent(ctx, s.builder, protocol.EventBuilderCompleted, taskID)
}

func (s *Scheduler) executeImplementChanges(ctx context.Context, taskID string, correlationID string) error {
	cmd := s.makeCommand(
		taskID,
		correlationID,
		protocol.AgentTypeBuilder,
		protocol.ActionImplementChanges,
		map[string]any{},
	)

	if err := s.sendCommand(s.builder, cmd); err != nil {
		return err
	}

	return s.waitForEvent(ctx, s.builder, protocol.EventBuilderCompleted, taskID)
}

func (s *Scheduler) executeReview(ctx context.Context, taskID string, correlationID string) (string, error) {
	cmd := s.makeCommand(
		taskID,
		correlationID,
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

	return evt.Status, nil
}

func (s *Scheduler) executeSpecMaintenance(ctx context.Context, taskID string, correlationID string) (string, error) {
	cmd := s.makeCommand(
		taskID,
		correlationID,
		protocol.AgentTypeSpecMaintainer,
		protocol.ActionUpdateSpec,
		map[string]any{},
	)

	if err := s.sendCommand(s.specMaintainer, cmd); err != nil {
		return "", err
	}

	// Wait for one of the terminal spec events
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
				return evt.Event, nil
			}
		case hb := <-s.specMaintainer.Heartbeats():
			s.notifyHeartbeat(hb)
		}
	}
}

func (s *Scheduler) makeCommand(
	taskID string,
	correlationID string,
	agentType protocol.AgentType,
	action protocol.Action,
	inputs map[string]any,
) *protocol.Command {
	return &protocol.Command{
		Kind:           protocol.MessageKindCommand,
		MessageID:      uuid.New().String(),
		CorrelationID:  correlationID,
		TaskID:         taskID,
		IdempotencyKey: fmt.Sprintf("pending-ik:%s:%s", action, taskID),
		To: protocol.AgentRef{
			AgentType: agentType,
		},
		Action:          action,
		Inputs:          inputs,
		ExpectedOutputs: []protocol.ExpectedOutput{},
		Version: protocol.Version{
			SnapshotID: "snap-test-0001", // Placeholder for P1.2
		},
		Deadline: time.Now().Add(10 * time.Minute).UTC(),
		Retry: protocol.Retry{
			Attempt:     0,
			MaxAttempts: 3,
		},
		Priority: 5,
	}
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
