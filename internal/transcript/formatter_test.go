package transcript

import (
	"testing"

	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/stretchr/testify/require"
)

func TestFormatEvent_BuilderCompleted(t *testing.T) {
	tests := []struct {
		name     string
		event    *protocol.Event
		expected string
	}{
		{
			name: "with passing tests",
			event: &protocol.Event{
				Event: protocol.EventBuilderCompleted,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeBuilder,
				},
				Payload: map[string]any{
					"tests": map[string]any{
						"status": "pass",
					},
				},
			},
			expected: "[builder] builder.completed: tests: pass",
		},
		{
			name: "with failing tests",
			event: &protocol.Event{
				Event: protocol.EventBuilderCompleted,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeBuilder,
				},
				Payload: map[string]any{
					"tests": map[string]any{
						"status": "fail",
					},
				},
			},
			expected: "[builder] builder.completed: tests: fail",
		},
		{
			name: "without test results",
			event: &protocol.Event{
				Event: protocol.EventBuilderCompleted,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeBuilder,
				},
				Payload: map[string]any{},
			},
			expected: "[builder] builder.completed",
		},
	}

	formatter := NewFormatter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.FormatEvent(tt.event)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatEvent_ReviewCompleted(t *testing.T) {
	tests := []struct {
		name     string
		event    *protocol.Event
		expected string
	}{
		{
			name: "approved without review file",
			event: &protocol.Event{
				Event:  protocol.EventReviewCompleted,
				Status: "approved",
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeReviewer,
				},
				Payload: map[string]any{},
			},
			expected: "[reviewer] review.completed: status: approved",
		},
		{
			name: "changes requested with review file",
			event: &protocol.Event{
				Event:  protocol.EventReviewCompleted,
				Status: "changes_requested",
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeReviewer,
				},
				Payload: map[string]any{},
				Artifacts: []protocol.Artifact{
					{Path: "reviews/T-0042.json", SHA256: "abc123", Size: 1024},
				},
			},
			expected: "[reviewer] review.completed: status: changes_requested, review: reviews/T-0042.json",
		},
	}

	formatter := NewFormatter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.FormatEvent(tt.event)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatEvent_SpecMaintainer(t *testing.T) {
	tests := []struct {
		name     string
		event    *protocol.Event
		expected string
	}{
		{
			name: "spec updated",
			event: &protocol.Event{
				Event: protocol.EventSpecUpdated,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeSpecMaintainer,
				},
			},
			expected: "[spec_maintainer] spec.updated: spec updated",
		},
		{
			name: "no changes needed",
			event: &protocol.Event{
				Event: protocol.EventSpecNoChangesNeeded,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeSpecMaintainer,
				},
			},
			expected: "[spec_maintainer] spec.no_changes_needed: no changes needed",
		},
		{
			name: "changes requested without notes",
			event: &protocol.Event{
				Event: protocol.EventSpecChangesRequested,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeSpecMaintainer,
				},
			},
			expected: "[spec_maintainer] spec.changes_requested: changes requested",
		},
		{
			name: "changes requested with notes",
			event: &protocol.Event{
				Event: protocol.EventSpecChangesRequested,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeSpecMaintainer,
				},
				Artifacts: []protocol.Artifact{
					{Path: "spec_notes/T-0042.json", SHA256: "def456", Size: 512},
				},
			},
			expected: "[spec_maintainer] spec.changes_requested: changes requested, notes: spec_notes/T-0042.json",
		},
	}

	formatter := NewFormatter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.FormatEvent(tt.event)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatEvent_Orchestration(t *testing.T) {
	tests := []struct {
		name     string
		event    *protocol.Event
		expected string
	}{
		{
			name: "proposed tasks with candidates",
			event: &protocol.Event{
				Event: protocol.EventOrchestrationProposedTasks,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeOrchestration,
				},
				Payload: map[string]any{
					"plan_candidates": []any{
						map[string]any{"path": "PLAN.md", "confidence": 0.9},
						map[string]any{"path": "docs/plan_v2.md", "confidence": 0.8},
					},
				},
			},
			expected: "[orchestration] orchestration.proposed_tasks: plan candidates: 2",
		},
		{
			name: "proposed tasks without payload",
			event: &protocol.Event{
				Event: protocol.EventOrchestrationProposedTasks,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeOrchestration,
				},
				Payload: map[string]any{},
			},
			expected: "[orchestration] orchestration.proposed_tasks: plan candidates",
		},
		{
			name: "needs clarification with questions",
			event: &protocol.Event{
				Event: protocol.EventOrchestrationNeedsClarification,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeOrchestration,
				},
				Payload: map[string]any{
					"questions": []any{
						"Which section to implement first?",
						"Should tests be included?",
					},
				},
			},
			expected: "[orchestration] orchestration.needs_clarification: clarification requested (2 question(s))",
		},
		{
			name: "needs clarification without questions",
			event: &protocol.Event{
				Event: protocol.EventOrchestrationNeedsClarification,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeOrchestration,
				},
				Payload: map[string]any{},
			},
			expected: "[orchestration] orchestration.needs_clarification: clarification requested",
		},
		{
			name: "plan conflict",
			event: &protocol.Event{
				Event: protocol.EventOrchestrationPlanConflict,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeOrchestration,
				},
				Payload: map[string]any{
					"message": "Multiple plans found",
				},
			},
			expected: "[orchestration] orchestration.plan_conflict: plan conflict reported",
		},
	}

	formatter := NewFormatter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.FormatEvent(tt.event)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatEvent_ArtifactProduced(t *testing.T) {
	tests := []struct {
		name     string
		event    *protocol.Event
		expected string
	}{
		{
			name: "with artifact",
			event: &protocol.Event{
				Event: protocol.EventArtifactProduced,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeBuilder,
				},
				Artifacts: []protocol.Artifact{
					{Path: "src/foo/bar.js", SHA256: "hash123", Size: 1432},
				},
			},
			expected: "[builder] artifact.produced: src/foo/bar.js (1.4 KiB)",
		},
		{
			name: "without artifact",
			event: &protocol.Event{
				Event: protocol.EventArtifactProduced,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeBuilder,
				},
				Artifacts: []protocol.Artifact{},
			},
			expected: "[builder] artifact.produced",
		},
	}

	formatter := NewFormatter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.FormatEvent(tt.event)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatEvent_GenericWithStatus(t *testing.T) {
	event := &protocol.Event{
		Event:  "custom.event",
		Status: "success",
		From: protocol.AgentRef{
			AgentType: protocol.AgentTypeBuilder,
		},
		Payload: map[string]any{},
	}

	formatter := NewFormatter()
	result := formatter.FormatEvent(event)
	require.Equal(t, "[builder] custom.event: status: success", result)
}

func TestFormatEvent_GenericWithoutStatus(t *testing.T) {
	event := &protocol.Event{
		Event: "custom.event",
		From: protocol.AgentRef{
			AgentType: protocol.AgentTypeBuilder,
		},
		Payload: map[string]any{},
	}

	formatter := NewFormatter()
	result := formatter.FormatEvent(event)
	require.Equal(t, "[builder] custom.event", result)
}

func TestFormatHeartbeat(t *testing.T) {
	tests := []struct {
		name      string
		heartbeat *protocol.Heartbeat
		expected  string
	}{
		{
			name: "ready state",
			heartbeat: &protocol.Heartbeat{
				Agent: protocol.AgentRef{
					AgentType: protocol.AgentTypeBuilder,
					AgentID:   "builder#1",
				},
				Seq:      5,
				Status:   protocol.HeartbeatStatusReady,
				UptimeS:  45.2,
			},
			expected: "[builder] heartbeat seq=5 status=ready uptime=45.2s",
		},
		{
			name: "busy state",
			heartbeat: &protocol.Heartbeat{
				Agent: protocol.AgentRef{
					AgentType: protocol.AgentTypeReviewer,
					AgentID:   "reviewer#1",
				},
				Seq:      12,
				Status:   protocol.HeartbeatStatusBusy,
				UptimeS:  120.5,
			},
			expected: "[reviewer] heartbeat seq=12 status=busy uptime=120.5s",
		},
		{
			name: "starting state",
			heartbeat: &protocol.Heartbeat{
				Agent: protocol.AgentRef{
					AgentType: protocol.AgentTypeOrchestration,
					AgentID:   "orch#1",
				},
				Seq:      0,
				Status:   protocol.HeartbeatStatusStarting,
				UptimeS:  0.1,
			},
			expected: "[orchestration] heartbeat seq=0 status=starting uptime=0.1s",
		},
	}

	formatter := NewFormatter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.FormatHeartbeat(tt.heartbeat)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatCommand(t *testing.T) {
	tests := []struct {
		name     string
		command  *protocol.Command
		expected string
	}{
		{
			name: "implement action",
			command: &protocol.Command{
				Action: protocol.ActionImplement,
				TaskID: "T-0042",
				To: protocol.AgentRef{
					AgentType: protocol.AgentTypeBuilder,
				},
			},
			expected: "[lorch→builder] implement (task: T-0042)",
		},
		{
			name: "review action",
			command: &protocol.Command{
				Action: protocol.ActionReview,
				TaskID: "T-0043",
				To: protocol.AgentRef{
					AgentType: protocol.AgentTypeReviewer,
				},
			},
			expected: "[lorch→reviewer] review (task: T-0043)",
		},
		{
			name: "intake action",
			command: &protocol.Command{
				Action: protocol.ActionIntake,
				TaskID: "intake-20250101-000000-abc123",
				To: protocol.AgentRef{
					AgentType: protocol.AgentTypeOrchestration,
				},
			},
			expected: "[lorch→orchestration] intake (task: intake-20250101-000000-abc123)",
		},
	}

	formatter := NewFormatter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.FormatCommand(tt.command)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatLog(t *testing.T) {
	tests := []struct {
		name     string
		log      *protocol.Log
		expected string
	}{
		{
			name: "info level",
			log: &protocol.Log{
				Level:   protocol.LogLevelInfo,
				Message: "Starting agent",
			},
			expected: "[LOG:INFO] Starting agent",
		},
		{
			name: "error level",
			log: &protocol.Log{
				Level:   protocol.LogLevelError,
				Message: "Connection failed",
			},
			expected: "[LOG:ERROR] Connection failed",
		},
		{
			name: "warn level",
			log: &protocol.Log{
				Level:   protocol.LogLevelWarn,
				Message: "Retry attempt",
			},
			expected: "[LOG:WARN] Retry attempt",
		},
	}

	formatter := NewFormatter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.FormatLog(tt.log)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{
			name:     "bytes",
			bytes:    512,
			expected: "512 B",
		},
		{
			name:     "kilobytes",
			bytes:    1432,
			expected: "1.4 KiB",
		},
		{
			name:     "kilobytes rounded",
			bytes:    2048,
			expected: "2.0 KiB",
		},
		{
			name:     "megabytes",
			bytes:    1536 * 1024,
			expected: "1.5 MiB",
		},
		{
			name:     "gigabytes",
			bytes:    2 * 1024 * 1024 * 1024,
			expected: "2.0 GiB",
		},
		{
			name:     "zero bytes",
			bytes:    0,
			expected: "0 B",
		},
		{
			name:     "1 byte",
			bytes:    1,
			expected: "1 B",
		},
		{
			name:     "exactly 1 KiB",
			bytes:    1024,
			expected: "1.0 KiB",
		},
		{
			name:     "exactly 1 MiB",
			bytes:    1024 * 1024,
			expected: "1.0 MiB",
		},
		{
			name:     "exactly 1 GiB",
			bytes:    1024 * 1024 * 1024,
			expected: "1.0 GiB",
		},
	}

	formatter := NewFormatter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.formatSize(tt.bytes)
			require.Equal(t, tt.expected, result)
		})
	}
}
