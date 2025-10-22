package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/config"
	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/iambrandonn/lorch/internal/runstate"
	"github.com/iambrandonn/lorch/internal/workspace"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestRunIntakeFlow_PlanApprovalRecordsDecision(t *testing.T) {
	logger := newTestLogger()
	workspaceRoot := t.TempDir()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Plan\n"), 0o644))

	cfg := config.GenerateDefault()
	cfg.Agents.Orchestration = &config.AgentConfig{
		Enabled: true,
		Cmd:     []string{"fake"},
		Env:     map[string]string{},
		TimeoutsS: map[string]int{
			"intake":         60,
			"task_discovery": 60,
		},
	}

	sup := newFakeOrchestrationSupervisor()
	sup.onSend = func(cmd *protocol.Command) {
		switch cmd.Action {
		case protocol.ActionIntake:
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-proposed",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeOrchestration,
					AgentID:   "orch#fake",
				},
				Event: protocol.EventOrchestrationProposedTasks,
				Payload: map[string]any{
					"plan_candidates": []any{
						map[string]any{"path": "PLAN.md", "confidence": 0.9, "reason": "Contains main plan"},
						map[string]any{"path": "docs/plan_v2.md", "confidence": 0.8, "reason": "Alternate"},
					},
					"derived_tasks": []any{
						map[string]any{"id": "TASK-1", "title": "Do the thing", "files": []any{"src/main.go"}},
						map[string]any{"id": "TASK-2", "title": "Tests", "files": []any{"src/main_test.go"}},
					},
					"notes": "Primary recommendation listed first.",
				},
			})
		default:
			t.Fatalf("unexpected action: %s", cmd.Action)
		}
	}

	overrideOrchestrationSupervisorFactory(t, sup)

	inReader := strings.NewReader("Manage PLAN.md\n1\n\n")

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(inReader)
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	outcome, err := runIntakeFlow(cmd, cfg, workspaceRoot, logger)
	require.NoError(t, err, out.String())
	require.NotNil(t, outcome)
	require.NotNil(t, outcome.FinalEvent)
	require.NotNil(t, outcome.Decision)
	require.Equal(t, "approved", outcome.Decision.Status)
	require.Equal(t, "PLAN.md", outcome.Decision.ApprovedPlan)
	require.ElementsMatch(t, []string{"TASK-1", "TASK-2"}, outcome.Decision.ApprovedTasks)

	require.Len(t, sup.commands, 1)
	require.Equal(t, protocol.ActionIntake, sup.commands[0].Action)

	systemEvt := findSystemDecisionEvent(t, outcome.LogPath)
	require.Equal(t, "approved", systemEvt.Status)
	require.Equal(t, "PLAN.md", systemEvt.Payload["approved_plan"])
	require.ElementsMatch(t, []any{"TASK-1", "TASK-2"}, systemEvt.Payload["approved_tasks"].([]any))

	state := loadRunStateFile(t, workspaceRoot)
	require.Equal(t, "intake", getStringField(state, "current_stage"))
	require.Equal(t, "approved", getNestedString(state, "intake", "last_decision", "status"))
	require.Equal(t, "PLAN.md", getNestedString(state, "intake", "last_decision", "approved_plan"))
}

func TestRunIntakeFlow_ClarificationLoopReuseIdempotencyKey(t *testing.T) {
	logger := newTestLogger()
	workspaceRoot := t.TempDir()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Plan\n"), 0o644))

	cfg := config.GenerateDefault()
	cfg.Agents.Orchestration = &config.AgentConfig{
		Enabled: true,
		Cmd:     []string{"fake"},
		Env:     map[string]string{},
		TimeoutsS: map[string]int{
			"intake":         60,
			"task_discovery": 60,
		},
	}

	sup := newFakeOrchestrationSupervisor()
	overrideOrchestrationSupervisorFactory(t, sup)

	var mu sync.Mutex
	var firstIK string
	sup.onSend = func(cmd *protocol.Command) {
		mu.Lock()
		defer mu.Unlock()

		switch len(sup.commands) {
		case 1:
			firstIK = cmd.IdempotencyKey
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-clarify",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeOrchestration,
					AgentID:   "orch#fake",
				},
				Event: protocol.EventOrchestrationNeedsClarification,
				Payload: map[string]any{
					"questions": []any{
						"What section of PLAN.md should be prioritized?",
					},
				},
			})
		case 2:
			require.Equal(t, firstIK, cmd.IdempotencyKey, "expected same idempotency key")
			clarifications, ok := cmd.Inputs["clarifications"].([]string)
			if !ok {
				t.Fatalf("expected clarifications slice, got %T", cmd.Inputs["clarifications"])
			}
			require.Equal(t, []string{"Focus on section 2"}, clarifications)

			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-proposed",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeOrchestration,
					AgentID:   "orch#fake",
				},
				Event: protocol.EventOrchestrationProposedTasks,
				Payload: map[string]any{
					"plan_candidates": []any{
						map[string]any{"path": "PLAN.md", "confidence": 0.88},
					},
					"derived_tasks": []any{
						map[string]any{"id": "TASK-CLARIFY", "title": "Handle section 2", "files": []any{"src/section2.go"}},
					},
				},
			})
		default:
			t.Fatalf("unexpected number of commands: %d", len(sup.commands))
		}
	}

	inReader := strings.NewReader("Manage PLAN.md\nFocus on section 2\n1\n\n")

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(inReader)
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	outcome, err := runIntakeFlow(cmd, cfg, workspaceRoot, logger)
	require.NoError(t, err, out.String())
	require.NotNil(t, outcome)
	require.NotNil(t, outcome.Decision)
	require.Equal(t, []string{"TASK-CLARIFY"}, outcome.Decision.ApprovedTasks)

	require.Len(t, sup.commands, 2)
	require.Equal(t, firstIK, sup.commands[1].IdempotencyKey)

	state := loadRunStateFile(t, workspaceRoot)
	clarifications := getNestedStringSlice(state, "intake", "last_clarifications")
	require.NotEmpty(t, clarifications)
	require.Equal(t, "focus on section 2", strings.ToLower(clarifications[0]))
}

func TestRunIntakeFlow_TaskDiscoveryRequest(t *testing.T) {
	logger := newTestLogger()
	workspaceRoot := t.TempDir()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Plan\n"), 0o644))

	cfg := config.GenerateDefault()
	cfg.Agents.Orchestration = &config.AgentConfig{
		Enabled: true,
		Cmd:     []string{"fake"},
		Env:     map[string]string{},
		TimeoutsS: map[string]int{
			"intake":         60,
			"task_discovery": 60,
		},
	}

	sup := newFakeOrchestrationSupervisor()
	overrideOrchestrationSupervisorFactory(t, sup)

	sup.onSend = func(cmd *protocol.Command) {
		switch {
		case cmd.Action == protocol.ActionIntake && len(sup.commands) == 1:
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-proposed-initial",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeOrchestration,
					AgentID:   "orch#fake",
				},
				Event: protocol.EventOrchestrationProposedTasks,
				Payload: map[string]any{
					"plan_candidates": []any{
						map[string]any{"path": "PLAN.md", "confidence": 0.6},
					},
					"derived_tasks": []any{
						map[string]any{"id": "TASK-OLD", "title": "Old task"},
					},
					"notes": "More options may be available.",
				},
			})
		case cmd.Action == protocol.ActionTaskDiscovery:
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-proposed-more",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeOrchestration,
					AgentID:   "orch#fake",
				},
				Event: protocol.EventOrchestrationProposedTasks,
				Payload: map[string]any{
					"plan_candidates": []any{
						map[string]any{"path": "PLAN.md", "confidence": 0.65},
						map[string]any{"path": "docs/plan_v2.md", "confidence": 0.9},
					},
					"derived_tasks": []any{
						map[string]any{"id": "TASK-NEW", "title": "New task"},
					},
				},
			})
		default:
			t.Fatalf("unexpected command: %s (count=%d)", cmd.Action, len(sup.commands))
		}
	}

	inReader := strings.NewReader("Manage PLAN.md\nm\n2\n\n")

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(inReader)
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	outcome, err := runIntakeFlow(cmd, cfg, workspaceRoot, logger)
	require.NoError(t, err, out.String())
	require.NotNil(t, outcome.Decision)
	require.Equal(t, "docs/plan_v2.md", outcome.Decision.ApprovedPlan)
	require.Equal(t, []string{"TASK-NEW"}, outcome.Decision.ApprovedTasks)

	require.Len(t, sup.commands, 2)
	require.Equal(t, protocol.ActionTaskDiscovery, sup.commands[1].Action)
}

func TestRunIntakeFlow_MultipleClarifications(t *testing.T) {
	logger := newTestLogger()
	workspaceRoot := t.TempDir()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Plan\n"), 0o644))

	cfg := config.GenerateDefault()
	cfg.Agents.Orchestration = &config.AgentConfig{
		Enabled: true,
		Cmd:     []string{"fake"},
		Env:     map[string]string{},
		TimeoutsS: map[string]int{
			"intake": 60,
		},
	}

	sup := newFakeOrchestrationSupervisor()
	var firstIK string
	sup.onSend = func(cmd *protocol.Command) {
		switch len(sup.commands) {
		case 1:
			firstIK = cmd.IdempotencyKey
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-clarify-1",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
				Event:         protocol.EventOrchestrationNeedsClarification,
				Payload: map[string]any{
					"questions": []any{"Which components should be prioritized?"},
				},
			})
		case 2:
			require.Equal(t, firstIK, cmd.IdempotencyKey)
			values := make([]string, 0)
			if list, ok := cmd.Inputs["clarifications"].([]string); ok {
				values = append(values, list...)
			} else if list, ok := cmd.Inputs["clarifications"].([]any); ok {
				for _, v := range list {
					values = append(values, v.(string))
				}
			}
			require.Equal(t, []string{"Focus on API"}, values)
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-clarify-2",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
				Event:         protocol.EventOrchestrationNeedsClarification,
				Payload: map[string]any{
					"questions": []any{"Should tests be updated as well?"},
				},
			})
		case 3:
			require.Equal(t, firstIK, cmd.IdempotencyKey)
			values := make([]string, 0)
			if list, ok := cmd.Inputs["clarifications"].([]string); ok {
				values = append(values, list...)
			} else if list, ok := cmd.Inputs["clarifications"].([]any); ok {
				for _, v := range list {
					values = append(values, v.(string))
				}
			}
			require.Equal(t, []string{"Focus on API", "Add unit tests"}, values)
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-proposed",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
				Event:         protocol.EventOrchestrationProposedTasks,
				Payload: map[string]any{
					"plan_candidates": []any{
						map[string]any{"path": "PLAN.md", "confidence": 0.8},
					},
					"derived_tasks": []any{
						map[string]any{"id": "TASK-MULTI", "title": "Handle clarifications"},
					},
				},
			})
		default:
			t.Fatalf("unexpected number of commands: %d", len(sup.commands))
		}
	}

	overrideOrchestrationSupervisorFactory(t, sup)

	inReader := strings.NewReader("Manage PLAN.md\nFocus on API\nAdd unit tests\n1\n\n")

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(inReader)
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	outcome, err := runIntakeFlow(cmd, cfg, workspaceRoot, logger)
	require.NoError(t, err, out.String())
	require.NotNil(t, outcome)
	require.Equal(t, []string{"Focus on API", "Add unit tests"}, outcome.Clarifications)

	state := loadRunStateFile(t, workspaceRoot)
	stored := getNestedStringSlice(state, "intake", "last_clarifications")
	require.Equal(t, []string{"Focus on API", "Add unit tests"}, stored)

	require.Len(t, sup.commands, 3)
}

func TestRunIntakeFlow_PlanConflictResolution(t *testing.T) {
	logger := newTestLogger()
	workspaceRoot := t.TempDir()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Plan\n"), 0o644))

	cfg := config.GenerateDefault()
	cfg.Agents.Orchestration = &config.AgentConfig{
		Enabled: true,
		Cmd:     []string{"fake"},
		Env:     map[string]string{},
		TimeoutsS: map[string]int{
			"intake":         60,
			"task_discovery": 60,
		},
	}

	sup := newFakeOrchestrationSupervisor()
	var firstIK string
	sup.onSend = func(cmd *protocol.Command) {
		switch len(sup.commands) {
		case 1:
			firstIK = cmd.IdempotencyKey
			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-conflict",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeOrchestration,
					AgentID:   "orch#fake",
				},
				Event: protocol.EventOrchestrationPlanConflict,
				Payload: map[string]any{
					"message":   "Multiple conflicting plan files detected",
					"conflicts": []any{"PLAN.md vs docs/plan_v2.md"},
				},
			})
		case 2:
			require.Equal(t, firstIK, cmd.IdempotencyKey, "expected plan conflict retry to reuse IK")
			values, ok := cmd.Inputs["conflict_resolutions"].([]string)
			if !ok {
				anySlice, ok := cmd.Inputs["conflict_resolutions"].([]any)
				require.True(t, ok, "conflict_resolutions not provided")
				values = make([]string, 0, len(anySlice))
				for _, v := range anySlice {
					values = append(values, v.(string))
				}
			}
			require.Contains(t, values, "Use docs/plan_v2.md")

			sup.emit(&protocol.Event{
				Kind:          protocol.MessageKindEvent,
				MessageID:     "evt-proposed",
				CorrelationID: cmd.CorrelationID,
				TaskID:        cmd.TaskID,
				From: protocol.AgentRef{
					AgentType: protocol.AgentTypeOrchestration,
					AgentID:   "orch#fake",
				},
				Event: protocol.EventOrchestrationProposedTasks,
				Payload: map[string]any{
					"plan_candidates": []any{
						map[string]any{"path": "PLAN.md", "confidence": 0.4},
						map[string]any{"path": "docs/plan_v2.md", "confidence": 0.9},
					},
					"derived_tasks": []any{
						map[string]any{"id": "TASK-NEW", "title": "New task"},
					},
				},
			})
		default:
			t.Fatalf("unexpected number of commands: %d", len(sup.commands))
		}
	}

	overrideOrchestrationSupervisorFactory(t, sup)

	inReader := strings.NewReader("Manage PLAN.md\nUse docs/plan_v2.md\n2\n\n")

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(inReader)
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	outcome, err := runIntakeFlow(cmd, cfg, workspaceRoot, logger)
	require.NoError(t, err, out.String())
	require.NotNil(t, outcome)
	require.NotNil(t, outcome.Decision)
	require.Equal(t, "docs/plan_v2.md", outcome.Decision.ApprovedPlan)
	require.Contains(t, outcome.ConflictResolutions, "Use docs/plan_v2.md")

	require.Len(t, sup.commands, 2)

	state := loadRunStateFile(t, workspaceRoot)
	conflicts := getNestedStringSlice(state, "intake", "conflict_resolutions")
	require.NotEmpty(t, conflicts)
	require.Equal(t, "use docs/plan_v2.md", strings.ToLower(conflicts[0]))
}

func TestRunIntakeFlow_ResumesExistingState(t *testing.T) {
	logger := newTestLogger()
	workspaceRoot := t.TempDir()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Plan\n"), 0o644))

	inputs := protocol.OrchestrationInputs{
		UserInstruction: "Manage PLAN.md",
		Discovery: &protocol.DiscoveryMetadata{
			Root:        workspaceRoot,
			Strategy:    "heuristic:v1",
			SearchPaths: []string{"."},
			GeneratedAt: time.Now().UTC(),
			Candidates:  []protocol.DiscoveryCandidate{{Path: "PLAN.md", Score: 0.9}},
		},
	}
	baseInputs, err := inputs.ToInputsMap()
	require.NoError(t, err)

	runID := "intake-resume-test"
	snapshotID := "snap-intake-resume"
	state := runstate.NewIntakeState(runID, snapshotID, "Manage PLAN.md", baseInputs)
	pendingInputs := cloneInputsMap(baseInputs)
	state.RecordIntakeCommand(string(protocol.ActionIntake), pendingInputs, "ik-resume", "corr-resume")
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, "state"), 0o755))
	require.NoError(t, runstate.SaveRunState(state, runstate.GetRunStatePath(workspaceRoot)))

	cfg := config.GenerateDefault()
	cfg.Agents.Orchestration = &config.AgentConfig{
		Enabled: true,
		Cmd:     []string{"fake"},
		Env:     map[string]string{},
		TimeoutsS: map[string]int{
			"intake": 60,
		},
	}

	sup := newFakeOrchestrationSupervisor()
	sup.onSend = func(cmd *protocol.Command) {
		require.Equal(t, protocol.ActionIntake, cmd.Action)
		require.Equal(t, "ik-resume", cmd.IdempotencyKey)
		sup.emit(&protocol.Event{
			Kind:          protocol.MessageKindEvent,
			MessageID:     "evt-proposed",
			CorrelationID: cmd.CorrelationID,
			TaskID:        cmd.TaskID,
			From:          protocol.AgentRef{AgentType: protocol.AgentTypeOrchestration, AgentID: "orch#fake"},
			Event:         protocol.EventOrchestrationProposedTasks,
			Payload: map[string]any{
				"plan_candidates": []any{
					map[string]any{"path": "PLAN.md", "confidence": 0.8},
				},
				"derived_tasks": []any{
					map[string]any{"id": "TASK-RESUME", "title": "Resume flow"},
				},
			},
		})
	}
	overrideOrchestrationSupervisorFactory(t, sup)

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(strings.NewReader("1\n\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	outcome, err := runIntakeFlow(cmd, cfg, workspaceRoot, logger)
	require.NoError(t, err, out.String())
	require.NotNil(t, outcome)
	require.Equal(t, "Manage PLAN.md", outcome.Instruction)

	reloaded := loadRunStateFile(t, workspaceRoot)
	decisionStatus := getNestedString(reloaded, "intake", "last_decision", "status")
	require.Equal(t, "approved", decisionStatus)
	require.Len(t, sup.commands, 1)
}

func TestRunIntakeFlow_UserDeclineRecorded(t *testing.T) {
	logger := newTestLogger()
	workspaceRoot := t.TempDir()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Plan\n"), 0o644))

	cfg := config.GenerateDefault()
	cfg.Agents.Orchestration = &config.AgentConfig{
		Enabled: true,
		Cmd:     []string{"fake"},
		Env:     map[string]string{},
		TimeoutsS: map[string]int{
			"intake":         60,
			"task_discovery": 60,
		},
	}

	sup := newFakeOrchestrationSupervisor()
	overrideOrchestrationSupervisorFactory(t, sup)

	sup.onSend = func(cmd *protocol.Command) {
		if cmd.Action != protocol.ActionIntake {
			t.Fatalf("expected intake action, got %s", cmd.Action)
		}
		sup.emit(&protocol.Event{
			Kind:          protocol.MessageKindEvent,
			MessageID:     "evt-proposed",
			CorrelationID: cmd.CorrelationID,
			TaskID:        cmd.TaskID,
			From: protocol.AgentRef{
				AgentType: protocol.AgentTypeOrchestration,
				AgentID:   "orch#fake",
			},
			Event: protocol.EventOrchestrationProposedTasks,
			Payload: map[string]any{
				"plan_candidates": []any{
					map[string]any{"path": "PLAN.md", "confidence": 0.8},
				},
				"derived_tasks": []any{
					map[string]any{"id": "TASK-DECLINE", "title": "Declined task"},
				},
			},
		})
	}

	inReader := strings.NewReader(strings.Join([]string{
		"Manage PLAN.md",
		"0",
	}, "\n"))

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(inReader)
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	outcome, err := runIntakeFlow(cmd, cfg, workspaceRoot, logger)
	require.Error(t, err)
	require.True(t, errors.Is(err, errUserDeclined))
	require.Nil(t, outcome)

	declineLog := findLatestIntakeLog(t, workspaceRoot)
	systemEvt := findSystemDecisionEvent(t, declineLog)
	require.Equal(t, "denied", systemEvt.Status)
	require.Equal(t, "User declined all plan candidates", systemEvt.Payload["reason"])
}

// --- Helpers ----------------------------------------------------------------

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

type fakeOrchestrationSupervisor struct {
	events     chan *protocol.Event
	heartbeats chan *protocol.Heartbeat
	logs       chan *protocol.Log

	mu       sync.Mutex
	commands []*protocol.Command

	onSend func(*protocol.Command)

	lastLog string

	once sync.Once
}

func newFakeOrchestrationSupervisor() *fakeOrchestrationSupervisor {
	return &fakeOrchestrationSupervisor{
		events:     make(chan *protocol.Event, 10),
		heartbeats: make(chan *protocol.Heartbeat, 1),
		logs:       make(chan *protocol.Log, 1),
	}
}

func (f *fakeOrchestrationSupervisor) Start(context.Context) error {
	return nil
}

func (f *fakeOrchestrationSupervisor) Stop(context.Context) error {
	f.once.Do(func() {
		close(f.events)
		close(f.heartbeats)
		close(f.logs)
	})
	return nil
}

func (f *fakeOrchestrationSupervisor) SendCommand(cmd *protocol.Command) error {
	f.mu.Lock()
	cloned := cloneCommand(cmd)
	f.commands = append(f.commands, cloned)
	f.mu.Unlock()

	if f.onSend != nil {
		f.onSend(cloned)
	}
	return nil
}

func (f *fakeOrchestrationSupervisor) Events() <-chan *protocol.Event {
	return f.events
}

func (f *fakeOrchestrationSupervisor) Heartbeats() <-chan *protocol.Heartbeat {
	return f.heartbeats
}

func (f *fakeOrchestrationSupervisor) Logs() <-chan *protocol.Log {
	return f.logs
}

func (f *fakeOrchestrationSupervisor) emit(evt *protocol.Event) {
	evtCopy := *evt
	select {
	case f.events <- &evtCopy:
	default:
		go func() {
			f.events <- &evtCopy
		}()
	}
}

func cloneCommand(cmd *protocol.Command) *protocol.Command {
	if cmd == nil {
		return nil
	}
	copyCmd := *cmd
	copyCmd.Inputs = cloneInputs(cmd.Inputs)
	copyCmd.ExpectedOutputs = append([]protocol.ExpectedOutput(nil), cmd.ExpectedOutputs...)
	return &copyCmd
}

func cloneInputs(inputs map[string]any) map[string]any {
	if inputs == nil {
		return nil
	}
	result := make(map[string]any, len(inputs))
	for k, v := range inputs {
		switch t := v.(type) {
		case []string:
			cpy := append([]string(nil), t...)
			result[k] = cpy
		case []any:
			cpy := append([]any(nil), t...)
			result[k] = cpy
		default:
			result[k] = t
		}
	}
	return result
}

func overrideOrchestrationSupervisorFactory(t *testing.T, sup *fakeOrchestrationSupervisor) {
	t.Helper()
	oldFactory := agentSupervisorFactory
	agentSupervisorFactory = func(cfg *config.AgentConfig, agentType protocol.AgentType, logger *slog.Logger) (agentSupervisor, error) {
		require.Equal(t, protocol.AgentTypeOrchestration, agentType)
		return sup, nil
	}
	t.Cleanup(func() {
		agentSupervisorFactory = oldFactory
	})
}

func findSystemDecisionEvent(t *testing.T, logPath string) *protocol.Event {
	t.Helper()
	file, err := os.Open(logPath)
	require.NoError(t, err)
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Bytes()
		var raw map[string]any
		require.NoError(t, json.Unmarshal(line, &raw))

		if kind, _ := raw["kind"].(string); kind != string(protocol.MessageKindEvent) {
			continue
		}
		if eventName, _ := raw["event"].(string); eventName != protocol.EventSystemUserDecision {
			continue
		}
		var evt protocol.Event
		require.NoError(t, json.Unmarshal(line, &evt))
		return &evt
	}
	require.NoError(t, scanner.Err())
	t.Fatalf("system.user_decision event not found in %s", logPath)
	return nil
}

func loadRunStateFile(t *testing.T, workspaceRoot string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(workspaceRoot, "state", "run.json"))
	require.NoError(t, err)
	var state map[string]any
	require.NoError(t, json.Unmarshal(data, &state))
	return state
}

func findLatestIntakeLog(t *testing.T, workspaceRoot string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(workspaceRoot, "events"))
	require.NoError(t, err)
	require.NotEmpty(t, entries, "expected at least one intake log")
	var newest os.DirEntry
	var newestTime time.Time
	for _, entry := range entries {
		info, err := entry.Info()
		require.NoError(t, err)
		if info.ModTime().After(newestTime) {
			newest = entry
			newestTime = info.ModTime()
		}
	}
	require.NotNil(t, newest)
	return filepath.Join(workspaceRoot, "events", newest.Name())
}

func getStringField(state map[string]any, key string) string {
	if value, ok := state[key].(string); ok {
		return value
	}
	return ""
}

func getNestedString(state map[string]any, keys ...string) string {
	var current any = state
	for _, key := range keys {
		obj, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = obj[key]
	}
	if str, ok := current.(string); ok {
		return str
	}
	return ""
}

func getNestedStringSlice(state map[string]any, keys ...string) []string {
	var current any = state
	for idx, key := range keys {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		if idx == len(keys)-1 {
			raw, ok := obj[key].([]any)
			if !ok {
				return nil
			}
			out := make([]string, 0, len(raw))
			for _, val := range raw {
				if s, ok := val.(string); ok {
					out = append(out, s)
				}
			}
			return out
		}
		current = obj[key]
	}
	return nil
}
