package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestCommandSerialization(t *testing.T) {
	deadline := time.Date(2025, 10, 19, 19, 0, 0, 0, time.UTC)

	cmd := Command{
		Kind:           MessageKindCommand,
		MessageID:      "m-01",
		CorrelationID:  "corr-intake-1",
		TaskID:         "T-0050",
		IdempotencyKey: "pending-ik:intake:T-0050",
		To: AgentRef{
			AgentType: AgentTypeOrchestration,
		},
		Action: ActionIntake,
		Inputs: map[string]any{
			"user_instruction": "I've got a PLAN.md. Manage the implementation of it",
		},
		ExpectedOutputs: []ExpectedOutput{
			{
				Path:     "tasks/T-0050.plan.json",
				Required: true,
			},
		},
		Version: Version{
			SnapshotID: "snap-test-0001",
		},
		Deadline: deadline,
		Retry: Retry{
			Attempt:     0,
			MaxAttempts: 3,
		},
		Priority: 5,
	}

	// Serialize
	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("failed to marshal command: %v", err)
	}

	// Deserialize
	var decoded Command
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal command: %v", err)
	}

	// Compare (using go-cmp for deep equality)
	if diff := cmp.Diff(cmd, decoded); diff != "" {
		t.Errorf("command mismatch (-want +got):\n%s", diff)
	}

	// Verify required fields
	if decoded.Kind != MessageKindCommand {
		t.Errorf("expected kind=command, got %s", decoded.Kind)
	}
	if decoded.Action != ActionIntake {
		t.Errorf("expected action=intake, got %s", decoded.Action)
	}
}

func TestEventSerialization(t *testing.T) {
	occurredAt := time.Date(2025, 10, 19, 18, 3, 0, 0, time.UTC)

	evt := Event{
		Kind:          MessageKindEvent,
		MessageID:     "e-01",
		CorrelationID: "corr-intake-1",
		TaskID:        "T-0050",
		From: AgentRef{
			AgentType: AgentTypeOrchestration,
		},
		Event:  EventOrchestrationProposedTasks,
		Status: "proposed",
		Payload: map[string]any{
			"plan_candidates": []any{
				map[string]any{"path": "PLAN.md", "confidence": 0.82},
			},
			"derived_tasks": []any{
				map[string]any{
					"id":    "T-0050-1",
					"title": "Implement sections 1–2",
					"files": []any{"src/a.js", "tests/a.spec.js"},
				},
			},
		},
		OccurredAt: occurredAt,
	}

	// Serialize
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}

	// Deserialize
	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	// Verify core fields
	if decoded.Kind != MessageKindEvent {
		t.Errorf("expected kind=event, got %s", decoded.Kind)
	}
	if decoded.Event != EventOrchestrationProposedTasks {
		t.Errorf("expected event=%s, got %s", EventOrchestrationProposedTasks, decoded.Event)
	}
	if decoded.From.AgentType != AgentTypeOrchestration {
		t.Errorf("expected from.agent_type=orchestration, got %s", decoded.From.AgentType)
	}
}

func TestHeartbeatSerialization(t *testing.T) {
	lastActivity := time.Date(2025, 10, 19, 18, 20, 20, 0, time.UTC)

	hb := Heartbeat{
		Kind: MessageKindHeartbeat,
		Agent: AgentRef{
			AgentType: AgentTypeReviewer,
			AgentID:   "reviewer#1",
		},
		Seq:            42,
		Status:         HeartbeatStatusBusy,
		PID:            32100,
		PPID:           32000,
		UptimeS:        122.4,
		LastActivityAt: lastActivity,
		Stats: &HeartbeatStats{
			CPUPercent: 23.1,
			RSSBytes:   73400320,
		},
		TaskID: "T-0042",
	}

	// Serialize
	data, err := json.Marshal(hb)
	if err != nil {
		t.Fatalf("failed to marshal heartbeat: %v", err)
	}

	// Deserialize
	var decoded Heartbeat
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal heartbeat: %v", err)
	}

	// Compare
	if diff := cmp.Diff(hb, decoded); diff != "" {
		t.Errorf("heartbeat mismatch (-want +got):\n%s", diff)
	}

	// Verify required fields
	if decoded.Kind != MessageKindHeartbeat {
		t.Errorf("expected kind=heartbeat, got %s", decoded.Kind)
	}
	if decoded.Status != HeartbeatStatusBusy {
		t.Errorf("expected status=busy, got %s", decoded.Status)
	}
}

func TestArtifactProducedEvent(t *testing.T) {
	occurredAt := time.Date(2025, 10, 19, 18, 9, 58, 0, time.UTC)

	evt := Event{
		Kind:          MessageKindEvent,
		MessageID:     "m1",
		CorrelationID: "corr-T-0042-1",
		TaskID:        "T-0042",
		From: AgentRef{
			AgentType: AgentTypeBuilder,
		},
		Event: EventArtifactProduced,
		Payload: map[string]any{
			"description": "main module",
		},
		Artifacts: []Artifact{
			{
				Path:   "src/foo/bar.js",
				SHA256: "sha256:abcd1234...",
				Size:   1432,
			},
		},
		OccurredAt: occurredAt,
	}

	// Serialize
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}

	// Deserialize
	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	// Verify artifacts
	if len(decoded.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(decoded.Artifacts))
	}

	artifact := decoded.Artifacts[0]
	if artifact.Path != "src/foo/bar.js" {
		t.Errorf("expected path=src/foo/bar.js, got %s", artifact.Path)
	}
	if artifact.Size != 1432 {
		t.Errorf("expected size=1432, got %d", artifact.Size)
	}
}

func TestBuilderCompletedEvent(t *testing.T) {
	occurredAt := time.Now().UTC()

	evt := Event{
		Kind:          MessageKindEvent,
		MessageID:     "e-build-1",
		CorrelationID: "corr-T-0042-1",
		TaskID:        "T-0042",
		From: AgentRef{
			AgentType: AgentTypeBuilder,
		},
		Event:  EventBuilderCompleted,
		Status: "success",
		Payload: map[string]any{
			"tests": map[string]any{
				"status":  "pass",
				"summary": "All 42 tests passed",
			},
		},
		OccurredAt: occurredAt,
	}

	// Serialize and deserialize
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	// Verify test results in payload
	tests, ok := decoded.Payload["tests"].(map[string]any)
	if !ok {
		t.Fatalf("expected tests payload, got %T", decoded.Payload["tests"])
	}

	status, ok := tests["status"].(string)
	if !ok || status != "pass" {
		t.Errorf("expected tests.status=pass, got %v", tests["status"])
	}
}

func TestReviewCompletedEvent(t *testing.T) {
	occurredAt := time.Now().UTC()

	evt := Event{
		Kind:          MessageKindEvent,
		MessageID:     "e-review-1",
		CorrelationID: "corr-T-0042-2",
		TaskID:        "T-0042",
		From: AgentRef{
			AgentType: AgentTypeReviewer,
		},
		Event:  EventReviewCompleted,
		Status: ReviewStatusChangesRequested,
		Artifacts: []Artifact{
			{
				Path:   "reviews/T-0042.json",
				SHA256: "sha256:xyz789...",
				Size:   512,
			},
		},
		OccurredAt: occurredAt,
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	if decoded.Status != ReviewStatusChangesRequested {
		t.Errorf("expected status=%s, got %s", ReviewStatusChangesRequested, decoded.Status)
	}

	if len(decoded.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(decoded.Artifacts))
	}
}
