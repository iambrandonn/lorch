package fixtureagent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/iambrandonn/lorch/internal/agent/script"
	"github.com/iambrandonn/lorch/internal/ndjson"
	"github.com/iambrandonn/lorch/internal/protocol"
)

// Options configures the behaviour of the fixture agent.
type Options struct {
	Logger            *slog.Logger
	HeartbeatInterval time.Duration
	DisableHeartbeat  bool
	Script            *script.Script
}

// Agent replays scripted responses for deterministic tests.
type Agent struct {
	role    protocol.AgentType
	options Options
	logger  *slog.Logger

	agentID string

	heartbeatSeq   int64
	lastActivityAt time.Time
	status         protocol.HeartbeatStatus
	startedAt      time.Time
}

// New constructs a fixture agent for the given role and options.
func New(role string, opts Options) (*Agent, error) {
	agentType := normalizeRole(role)
	if agentType == "" {
		return nil, fmt.Errorf("unsupported role %q", role)
	}
	if opts.Script == nil || len(opts.Script.Responses) == 0 {
		return nil, errors.New("script is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	if opts.HeartbeatInterval == 0 {
		opts.HeartbeatInterval = 10 * time.Second
	}

	return &Agent{
		role:           agentType,
		options:        opts,
		logger:         logger,
		agentID:        fmt.Sprintf("%s#fixture-%s", agentType, uuid.New().String()[:8]),
		status:         protocol.HeartbeatStatusReady,
		startedAt:      time.Now().UTC(),
		lastActivityAt: time.Now().UTC(),
	}, nil
}

// Run processes commands from stdin and writes scripted responses to stdout.
func (a *Agent) Run(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	encoder := ndjson.NewEncoder(stdout, a.logger)
	decoder := ndjson.NewDecoder(stdin, a.logger)

	if !a.options.DisableHeartbeat {
		if err := a.sendHeartbeat(encoder, protocol.HeartbeatStatusStarting, ""); err != nil {
			return fmt.Errorf("send initial heartbeat: %w", err)
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if !a.options.DisableHeartbeat {
		go a.heartbeatLoop(ctx, encoder)
	}

	for {
		msg, err := decoder.DecodeEnvelope()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("decode envelope: %w", err)
		}

		cmd, ok := msg.(*protocol.Command)
		if !ok {
			a.logger.Warn("ignoring non-command message", "type", fmt.Sprintf("%T", msg))
			continue
		}

		if err := a.handleCommand(ctx, cmd, encoder); err != nil {
			return err
		}
	}
}

func (a *Agent) heartbeatLoop(ctx context.Context, encoder *ndjson.Encoder) {
	ticker := time.NewTicker(a.options.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.sendHeartbeat(encoder, a.status, a.lastTaskID()); err != nil {
				a.logger.Error("heartbeat error", "error", err)
			}
		}
	}
}

func (a *Agent) handleCommand(ctx context.Context, cmd *protocol.Command, encoder *ndjson.Encoder) error {
	a.lastActivityAt = time.Now().UTC()

	if !a.options.DisableHeartbeat {
		a.status = protocol.HeartbeatStatusBusy
		if err := a.sendHeartbeat(encoder, a.status, cmd.TaskID); err != nil {
			return err
		}
	}

	template, ok := a.options.Script.Responses[string(cmd.Action)]
	if !ok {
		return fmt.Errorf("no scripted response for action %q", cmd.Action)
	}

	if template.Error != "" {
		return fmt.Errorf("scripted error: %s", template.Error)
	}

	if template.DelayMs > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(template.DelayMs) * time.Millisecond):
		}
	}

	for idx, evtTemplate := range template.Events {
		if err := a.sendEvent(cmd, evtTemplate, encoder); err != nil {
			return fmt.Errorf("send scripted event %d: %w", idx, err)
		}
	}

	if !a.options.DisableHeartbeat {
		a.status = protocol.HeartbeatStatusReady
		if err := a.sendHeartbeat(encoder, a.status, ""); err != nil {
			return err
		}
	}

	return nil
}

func (a *Agent) sendEvent(cmd *protocol.Command, tmpl script.EventTemplate, encoder *ndjson.Encoder) error {
	var artifacts []protocol.Artifact
	for _, art := range tmpl.Artifacts {
		artifacts = append(artifacts, protocol.Artifact{
			Path:   art.Path,
			SHA256: art.SHA256,
			Size:   art.Size,
		})
	}

	evt := protocol.Event{
		Kind:          protocol.MessageKindEvent,
		MessageID:     uuid.New().String(),
		CorrelationID: cmd.CorrelationID,
		TaskID:        cmd.TaskID,
		From: protocol.AgentRef{
			AgentType: a.role,
			AgentID:   a.agentID,
		},
		Event:      tmpl.Type,
		Status:     tmpl.Status,
		Payload:    tmpl.Payload,
		Artifacts:  artifacts,
		OccurredAt: time.Now().UTC(),
	}

	return encoder.Encode(evt)
}

func (a *Agent) sendHeartbeat(encoder *ndjson.Encoder, status protocol.HeartbeatStatus, taskID string) error {
	a.heartbeatSeq++
	hb := protocol.Heartbeat{
		Kind: protocol.MessageKindHeartbeat,
		Agent: protocol.AgentRef{
			AgentType: a.role,
			AgentID:   a.agentID,
		},
		Seq:            a.heartbeatSeq,
		Status:         status,
		PID:            0,
		PPID:           0,
		UptimeS:        time.Since(a.startedAt).Seconds(),
		LastActivityAt: a.lastActivityAt,
		TaskID:         taskID,
	}
	return encoder.Encode(hb)
}

func (a *Agent) lastTaskID() string {
	// Heartbeats only need to know the last task ID if status is busy; we default to empty string.
	return ""
}

func normalizeRole(role string) protocol.AgentType {
	s := strings.ToLower(strings.TrimSpace(role))
	s = strings.ReplaceAll(s, "-", "_")
	switch protocol.AgentType(s) {
	case protocol.AgentTypeBuilder,
		protocol.AgentTypeReviewer,
		protocol.AgentTypeSpecMaintainer,
		protocol.AgentTypeOrchestration:
		return protocol.AgentType(s)
	default:
		return ""
	}
}
