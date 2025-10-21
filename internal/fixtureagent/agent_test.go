package fixtureagent

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/agent/script"
	"github.com/iambrandonn/lorch/internal/protocol"
)

func TestAgentReplaysScriptedEvents(t *testing.T) {
	t.Parallel()

	fixturePath := filepath.Join("..", "..", "testdata", "fixtures", "orchestration-simple.json")
	s, err := script.Load(fixturePath)
	if err != nil {
		t.Fatalf("script.Load() error = %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	agent, err := New("orchestration", Options{
		Logger:            logger,
		HeartbeatInterval: 5 * time.Second,
		Script:            s,
		DisableHeartbeat:  true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Build command to send to agent
	cmd := protocol.Command{
		Kind:          protocol.MessageKindCommand,
		MessageID:     "cmd-1",
		CorrelationID: "corr-1",
		TaskID:        "T-test",
		Action:        protocol.ActionIntake,
		Inputs: map[string]any{
			"user_instruction": "Manage PLAN.md",
		},
	}

	var input bytes.Buffer
	if err := json.NewEncoder(&input).Encode(cmd); err != nil {
		t.Fatalf("encode command: %v", err)
	}

	var output bytes.Buffer
	if err := agent.Run(context.Background(), &input, &output); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 event, got %d", len(lines))
	}

	var event protocol.Event
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}

	if event.Event != protocol.EventOrchestrationProposedTasks {
		t.Fatalf("expected event type %q, got %q", protocol.EventOrchestrationProposedTasks, event.Event)
	}

	if event.Payload["plan_candidates"] == nil {
		t.Fatalf("expected plan_candidates in payload")
	}
}

func TestAgentMissingResponse(t *testing.T) {
	t.Parallel()

	s := &script.Script{Responses: map[string]script.ResponseTemplate{}}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	agent, err := New("builder", Options{
		Logger:           logger,
		Script:           s,
		DisableHeartbeat: true,
	})
	if err == nil || agent != nil {
		t.Fatalf("expected error when creating agent with empty script")
	}
}
