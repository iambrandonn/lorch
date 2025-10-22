package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/iambrandonn/lorch/internal/activation"
	"github.com/iambrandonn/lorch/internal/config"
	"github.com/iambrandonn/lorch/internal/discovery"
	"github.com/iambrandonn/lorch/internal/eventlog"
	"github.com/iambrandonn/lorch/internal/idempotency"
	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/iambrandonn/lorch/internal/runstate"
	"github.com/iambrandonn/lorch/internal/scheduler"
	"github.com/iambrandonn/lorch/internal/snapshot"
	"github.com/iambrandonn/lorch/internal/supervisor"
	"github.com/iambrandonn/lorch/internal/transcript"
	"github.com/iambrandonn/lorch/internal/workspace"
	"github.com/spf13/cobra"
)

type agentSupervisor interface {
	Start(context.Context) error
	Stop(context.Context) error
	SendCommand(*protocol.Command) error
	Events() <-chan *protocol.Event
	Heartbeats() <-chan *protocol.Heartbeat
	Logs() <-chan *protocol.Log
}

var agentSupervisorFactory = realAgentSupervisorFactory

const (
	optionMore     = "m"
	optionMoreWord = "more"
	optionNone     = "0"
	optionNoneWord = "none"
	optionAbort    = "abort"
	optionCancel   = "cancel"
	optionAll      = "all"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start a new orchestration run",
	Long: `Start a new orchestration run. If --task is not specified,
lorch will prompt for natural language instructions (Phase 2).`,
	RunE: runRun,
}

func runRun(cmd *cobra.Command, args []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	outWriter := cmd.OutOrStdout()

	// Find or create config
	configPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return err
	}

	cfg, cfgPath, err := loadOrCreateConfig(configPath, logger)
	if err != nil {
		return err
	}

	logger.Info("loaded configuration", "path", cfgPath)

	// Validate config
	if err := cfg.Validate(); err != nil {
		return err
	}

	// Determine workspace root (relative to config file location)
	workspaceRoot := determineWorkspaceRoot(cfg, cfgPath)
	logger.Info("workspace root", "path", workspaceRoot)

	// Initialize workspace directories
	if err := workspace.Initialize(workspaceRoot); err != nil {
		return fmt.Errorf("failed to initialize workspace: %w", err)
	}

	logger.Info("workspace initialized")

	// Get task ID if specified
	taskID, err := cmd.Flags().GetString("task")
	if err != nil {
		return err
	}

	if taskID == "" {
		// P2.4: NL intake → activation → execution flow
		outcome, err := runIntakeFlow(cmd, cfg, workspaceRoot, logger)
		if err != nil {
			return err
		}

		// If no tasks were approved, stop here (intake only)
		if outcome.Decision == nil || len(outcome.Decision.ApprovedTasks) == 0 {
			fmt.Fprintf(outWriter, "Natural language intake completed. Transcript: %s\n", outcome.LogPath)
			return nil
		}

		// P2.4 Task B: Execute the approved tasks
		// Create context and snapshot for execution
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		snap, err := snapshot.CaptureSnapshot(workspaceRoot)
		if err != nil {
			return fmt.Errorf("failed to capture snapshot for execution: %w", err)
		}

		snapshotPath := filepath.Join(workspaceRoot, "snapshots", snap.SnapshotID+".manifest.json")
		if err := snapshot.SaveSnapshot(snap, snapshotPath); err != nil {
			return fmt.Errorf("failed to save execution snapshot: %w", err)
		}

		// Execute approved tasks through scheduler pipeline
		if err := executeApprovedTasks(ctx, cmd, outcome, cfg, workspaceRoot, snap.SnapshotID, logger); err != nil {
			return fmt.Errorf("task execution failed: %w", err)
		}

		return nil
	}

	// Find task in config
	var task *config.Task
	for i := range cfg.Tasks {
		if cfg.Tasks[i].ID == taskID {
			task = &cfg.Tasks[i]
			break
		}
	}

	if task == nil {
		return fmt.Errorf("task %s not found in config", taskID)
	}

	logger.Info("executing task", "task_id", taskID, "goal", task.Goal)

	// P1.3: Capture snapshot
	logger.Info("capturing workspace snapshot...")
	snap, err := snapshot.CaptureSnapshot(workspaceRoot)
	if err != nil {
		return fmt.Errorf("failed to capture snapshot: %w", err)
	}

	logger.Info("snapshot captured", "snapshot_id", snap.SnapshotID, "files", len(snap.Files))

	// Save snapshot
	snapshotPath := filepath.Join(workspaceRoot, "snapshots", snap.SnapshotID+".manifest.json")
	if err := snapshot.SaveSnapshot(snap, snapshotPath); err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}

	// Generate run ID
	runID := fmt.Sprintf("run-%s-%s", time.Now().UTC().Format("20060102-150405"), uuid.New().String()[:8])

	// Initialize run state
	state := runstate.NewRunState(runID, taskID, snap.SnapshotID)
	statePath := runstate.GetRunStatePath(workspaceRoot)
	if err := runstate.SaveRunState(state, statePath); err != nil {
		return fmt.Errorf("failed to save run state: %w", err)
	}

	logger.Info("run initialized", "run_id", runID)

	// Create event log
	eventLogPath := filepath.Join(workspaceRoot, "events", runID+".ndjson")
	evtLog, err := eventlog.NewEventLog(eventLogPath, logger)
	if err != nil {
		return fmt.Errorf("failed to create event log: %w", err)
	}
	defer evtLog.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Set up execution environment (agents + scheduler)
	env, err := setupExecutionEnvironment(ctx, cfg, workspaceRoot, snap.SnapshotID, evtLog, logger)
	if err != nil {
		return err
	}
	defer env.cleanup()

	// Execute task
	logger.Info("starting task execution...")
	inputs := map[string]any{"goal": task.Goal}
	if err := env.scheduler.ExecuteTask(ctx, taskID, inputs); err != nil {
		state.MarkFailed()
		runstate.SaveRunState(state, statePath)
		return fmt.Errorf("task execution failed: %w", err)
	}

	// Mark run complete
	state.MarkCompleted()
	if err := runstate.SaveRunState(state, statePath); err != nil {
		logger.Warn("failed to save final run state", "error", err)
	}

	logger.Info("task execution complete", "run_id", runID)
	return nil
}

var (
	errInstructionRequired = errors.New("instruction is required")
	errUserDeclined        = errors.New("user declined all plan candidates")
	errRequestMoreOptions  = errors.New("user requested more plan options")
)

type IntakeOutcome struct {
	RunID               string                      `json:"run_id"`
	Instruction         string                      `json:"instruction"`
	Discovery           *protocol.DiscoveryMetadata `json:"discovery"`
	Clarifications      []string                    `json:"clarifications,omitempty"`
	ConflictResolutions []string                    `json:"conflict_resolutions,omitempty"`
	Decision            *UserDecision               `json:"decision,omitempty"`
	FinalEvent          *protocol.Event             `json:"final_event"`
	LogPath             string                      `json:"log_path"`
	StartedAt           time.Time                   `json:"started_at"`
	CompletedAt         time.Time                   `json:"completed_at"`
}

// UserDecision captures the user's intake approval outcome.
type UserDecision struct {
	Status        string    `json:"status"`
	ApprovedPlan  string    `json:"approved_plan,omitempty"`
	ApprovedTasks []string  `json:"approved_tasks,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	Prompt        string    `json:"prompt,omitempty"`
	OccurredAt    time.Time `json:"occurred_at"`
	CorrelationID string    `json:"correlation_id,omitempty"`
}

func runIntakeFlow(cmd *cobra.Command, cfg *config.Config, workspaceRoot string, logger *slog.Logger) (*IntakeOutcome, error) {
	if cfg.Agents.Orchestration == nil || !cfg.Agents.Orchestration.Enabled {
		return nil, fmt.Errorf("orchestration agent is not configured or enabled in lorch.json")
	}

	inputReader := cmd.InOrStdin()
	outputWriter := cmd.OutOrStdout()
	reader := bufio.NewReader(inputReader)

	isTTY := false
	if file, ok := inputReader.(*os.File); ok {
		isTTY = isTerminalFile(file)
	}

	statePath := runstate.GetRunStatePath(workspaceRoot)

	var (
		state               *runstate.RunState
		instruction         string
		baseInputs          map[string]any
		clarifications      []string
		conflictResolutions []string
		runID               string
		pendingAction       = protocol.ActionIntake
		pendingIK           string
		pendingCorrelation  string
		isResume            bool
	)

	if existingState, err := runstate.LoadRunState(statePath); err == nil &&
		existingState.Status == runstate.StatusRunning &&
		existingState.CurrentStage == runstate.StageIntake &&
		existingState.Intake != nil {
		state = existingState
		instruction = existingState.Intake.Instruction
		baseInputs = cloneInputsMap(existingState.Intake.BaseInputs)
		clarifications = append([]string(nil), existingState.Intake.LastClarifications...)
		conflictResolutions = append([]string(nil), existingState.Intake.ConflictResolutions...)
		runID = existingState.RunID
		if existingState.Intake.PendingAction != "" {
			pendingAction = protocol.Action(existingState.Intake.PendingAction)
		}
		if pendingAction == "" {
			pendingAction = protocol.ActionIntake
		}
		pendingIK = existingState.Intake.PendingIdempotencyKey
		pendingCorrelation = existingState.Intake.PendingCorrelationID
		isResume = true
		fmt.Fprintf(outputWriter, "Resuming intake run %s (instruction: %s)\n", runID, instruction)
	} else {
		var err error
		instruction, err = promptForInstruction(reader, outputWriter, isTTY)
		if err != nil {
			if errors.Is(err, errInstructionRequired) {
				return nil, fmt.Errorf("instruction required: provide text via standard input or use --task for predefined tasks")
			}
			return nil, err
		}

		fmt.Fprintln(outputWriter)
		fmt.Fprintln(outputWriter, "Running workspace discovery...")

		meta, err := discovery.Discover(discovery.DefaultConfig(workspaceRoot))
		if err != nil {
			return nil, fmt.Errorf("discovery failed: %w", err)
		}

		orchestrationInputs := protocol.OrchestrationInputs{
			UserInstruction: instruction,
			Discovery:       meta,
		}

		baseInputs, err = orchestrationInputs.ToInputsMap()
		if err != nil {
			return nil, fmt.Errorf("failed to build orchestration inputs: %w", err)
		}

		runID = fmt.Sprintf("intake-%s-%s", time.Now().UTC().Format("20060102-150405"), uuid.New().String()[:8])
		isResume = false
	}

	if baseInputs == nil {
		baseInputs = map[string]any{}
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	timeoutSeconds := resolveTimeout(cfg, "intake", 180)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	now := time.Now().UTC()

	commandInputs := composeIntakeInputs(baseInputs, clarifications, conflictResolutions)
	if pendingAction == protocol.ActionTaskDiscovery {
		commandInputs["request_more_candidates"] = true
	}

	var command *protocol.Command
	var err error

	if isResume {
		if state.Intake.PendingInputs != nil {
			commandInputs = cloneInputsMap(state.Intake.PendingInputs)
		}

		switch pendingAction {
		case protocol.ActionTaskDiscovery:
			command, err = buildTaskDiscoveryCommand(runID, commandInputs, now, cfg, state.SnapshotID, pendingCorrelation, pendingIK)
		default:
			command, err = buildIntakeCommand(runID, commandInputs, now, cfg, pendingCorrelation, pendingIK)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to rebuild orchestration command: %w", err)
		}
	} else {
		command, err = buildIntakeCommand(runID, commandInputs, now, cfg, "", "")
		if err != nil {
			return nil, fmt.Errorf("failed to build orchestration command: %w", err)
		}
		state = runstate.NewIntakeState(runID, command.Version.SnapshotID, instruction, baseInputs)
	}

	state.RecordIntakeBaseInputs(baseInputs)
	state.SetIntakeClarifications(clarifications)
	state.SetIntakeConflictResolutions(conflictResolutions)
	state.RecordIntakeCommand(string(command.Action), commandInputs, command.IdempotencyKey, command.CorrelationID)

	if err := runstate.SaveRunState(state, statePath); err != nil {
		return nil, fmt.Errorf("failed to save intake state: %w", err)
	}

	logPath := filepath.Join(workspaceRoot, "events", runID+"-intake.ndjson")
	intakeLog, err := eventlog.NewEventLog(logPath, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create intake log: %w", err)
	}
	defer intakeLog.Close()

	orchSupervisor, err := agentSupervisorFactory(cfg.Agents.Orchestration, protocol.AgentTypeOrchestration, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create orchestration supervisor: %w", err)
	}
	if err := orchSupervisor.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start orchestration agent: %w", err)
	}
	defer orchSupervisor.Stop(context.Background())

	formatter := transcript.NewFormatter()

	if err := intakeLog.WriteCommand(command); err != nil {
		return nil, fmt.Errorf("failed to write intake command: %w", err)
	}
	fmt.Fprintln(outputWriter, formatter.FormatCommand(command))
	state.RecordCommand(command.MessageID, command.CorrelationID)
	if err := runstate.SaveRunState(state, statePath); err != nil {
		return nil, fmt.Errorf("failed to save intake state: %w", err)
	}

	if err := orchSupervisor.SendCommand(command); err != nil {
		return nil, fmt.Errorf("failed to send intake command: %w", err)
	}

	lastCommandIK := command.IdempotencyKey
	lastCommandCorrelation := command.CorrelationID
	snapshotID := command.Version.SnapshotID

	events := orchSupervisor.Events()
	heartbeats := orchSupervisor.Heartbeats()
	logs := orchSupervisor.Logs()

	var heartbeatTimer *time.Timer
	var heartbeatTimeout time.Duration
	if cfg.Agents.Orchestration != nil && cfg.Agents.Orchestration.HeartbeatIntervalS > 0 {
		heartbeatTimeout = time.Duration(cfg.Agents.Orchestration.HeartbeatIntervalS*3) * time.Second
		heartbeatTimer = time.NewTimer(heartbeatTimeout)
		heartbeatTimer.Stop()
		defer heartbeatTimer.Stop()
	}
	var heartbeatCh <-chan time.Time
	if heartbeatTimer != nil {
		heartbeatCh = heartbeatTimer.C
	}
	heartbeatActive := false

	if heartbeatTimer != nil {
		heartbeatTimer.Reset(heartbeatTimeout)
		heartbeatActive = true
	}

	var finalEvent *protocol.Event
	var decision *UserDecision

	for decision == nil {
		select {
		case evt, ok := <-events:
			if !ok {
				if decision == nil {
					return nil, fmt.Errorf("orchestration agent exited before providing plan candidates")
				}
				continue
			}

			if heartbeatTimer != nil && !heartbeatActive {
				heartbeatTimer.Reset(heartbeatTimeout)
				heartbeatActive = true
			}

			if err := intakeLog.WriteEvent(evt); err != nil {
				return nil, fmt.Errorf("failed to write intake event: %w", err)
			}
			fmt.Fprintln(outputWriter, formatter.FormatEvent(evt))
			state.RecordEvent(evt.MessageID)
			if err := runstate.SaveRunState(state, statePath); err != nil {
				return nil, fmt.Errorf("failed to save intake state: %w", err)
			}

			switch evt.Event {
			case protocol.EventOrchestrationNeedsClarification:
				questions := extractClarificationQuestions(evt.Payload)
				if len(questions) == 0 {
					fmt.Fprintln(outputWriter, "Orchestration requested clarification but no questions were provided.")
					continue
				}

				answers, err := promptClarifications(reader, outputWriter, questions, isTTY)
				if err != nil {
					return nil, err
				}
				clarifications = append(clarifications, answers...)
				state.SetIntakeClarifications(clarifications)
				if err := runstate.SaveRunState(state, statePath); err != nil {
					return nil, fmt.Errorf("failed to save intake state: %w", err)
				}

				updatedInputs := composeIntakeInputs(baseInputs, clarifications, conflictResolutions)
				command, err = buildIntakeCommand(runID, updatedInputs, time.Now().UTC(), cfg, lastCommandCorrelation, lastCommandIK)
				if err != nil {
					return nil, fmt.Errorf("failed to rebuild intake command: %w", err)
				}

				if err := intakeLog.WriteCommand(command); err != nil {
					return nil, fmt.Errorf("failed to write intake command: %w", err)
				}
				fmt.Fprintln(outputWriter, formatter.FormatCommand(command))
				state.RecordCommand(command.MessageID, command.CorrelationID)
				state.RecordIntakeCommand(string(command.Action), updatedInputs, command.IdempotencyKey, command.CorrelationID)
				if err := runstate.SaveRunState(state, statePath); err != nil {
					return nil, fmt.Errorf("failed to save intake state: %w", err)
				}

				if err := orchSupervisor.SendCommand(command); err != nil {
					return nil, fmt.Errorf("failed to send intake command: %w", err)
				}
				if heartbeatTimer != nil {
					if !heartbeatTimer.Stop() {
						select {
						case <-heartbeatTimer.C:
						default:
						}
					}
					heartbeatTimer.Reset(heartbeatTimeout)
					heartbeatActive = true
				}

				lastCommandIK = command.IdempotencyKey
				lastCommandCorrelation = command.CorrelationID

			case protocol.EventOrchestrationProposedTasks:
				candidates, tasks, notes, err := parsePlanResponse(evt.Payload)
				if err != nil {
					return nil, err
				}

				decision, err = promptPlanApproval(reader, outputWriter, isTTY, instruction, candidates, tasks, notes)
				if err != nil {
					if errors.Is(err, errRequestMoreOptions) {
						discoveryInputs := composeIntakeInputs(baseInputs, clarifications, conflictResolutions)
						discoveryInputs["request_more_candidates"] = true
						discoveryCmd, err := buildTaskDiscoveryCommand(runID, discoveryInputs, time.Now().UTC(), cfg, snapshotID, "", "")
						if err != nil {
							return nil, err
						}
						if err := intakeLog.WriteCommand(discoveryCmd); err != nil {
							return nil, fmt.Errorf("failed to write task discovery command: %w", err)
						}
						fmt.Fprintln(outputWriter, formatter.FormatCommand(discoveryCmd))
						state.RecordCommand(discoveryCmd.MessageID, discoveryCmd.CorrelationID)
						state.RecordIntakeCommand(string(discoveryCmd.Action), discoveryInputs, discoveryCmd.IdempotencyKey, discoveryCmd.CorrelationID)
						if err := runstate.SaveRunState(state, statePath); err != nil {
							return nil, fmt.Errorf("failed to save intake state: %w", err)
						}
						if err := orchSupervisor.SendCommand(discoveryCmd); err != nil {
							return nil, fmt.Errorf("failed to send task discovery command: %w", err)
						}
						if heartbeatTimer != nil {
							if !heartbeatTimer.Stop() {
								select {
								case <-heartbeatTimer.C:
								default:
								}
							}
							heartbeatTimer.Reset(heartbeatTimeout)
							heartbeatActive = true
						}
						lastCommandIK = discoveryCmd.IdempotencyKey
						lastCommandCorrelation = discoveryCmd.CorrelationID
						decision = nil
						continue
					}
					if errors.Is(err, errUserDeclined) {
						declineDecision := &UserDecision{
							Status:        "denied",
							Reason:        "User declined all plan candidates",
							Prompt:        instruction,
							OccurredAt:    time.Now().UTC(),
							CorrelationID: evt.CorrelationID,
						}
						if recErr := recordIntakeDecision(outputWriter, formatter, intakeLog, state, statePath, runID, clarifications, declineDecision); recErr != nil {
							logger.Warn("failed to record decline decision", "error", recErr)
						}
						return nil, errUserDeclined
					}
					return nil, err
				}

				decision.CorrelationID = evt.CorrelationID
				decision.OccurredAt = time.Now().UTC()
				finalEvent = evt

			case protocol.EventOrchestrationPlanConflict:
				resolution, requestMore, err := promptPlanConflictResolution(reader, outputWriter, evt.Payload, isTTY)
				if err != nil {
					return nil, err
				}
				if requestMore {
					discoveryInputs := composeIntakeInputs(baseInputs, clarifications, conflictResolutions)
					discoveryInputs["request_more_candidates"] = true
					discoveryCmd, buildErr := buildTaskDiscoveryCommand(runID, discoveryInputs, time.Now().UTC(), cfg, snapshotID, "", "")
					if buildErr != nil {
						return nil, buildErr
					}
					if err := intakeLog.WriteCommand(discoveryCmd); err != nil {
						return nil, fmt.Errorf("failed to write task discovery command: %w", err)
					}
					fmt.Fprintln(outputWriter, formatter.FormatCommand(discoveryCmd))
					state.RecordCommand(discoveryCmd.MessageID, discoveryCmd.CorrelationID)
					state.RecordIntakeCommand(string(discoveryCmd.Action), discoveryInputs, discoveryCmd.IdempotencyKey, discoveryCmd.CorrelationID)
					if err := runstate.SaveRunState(state, statePath); err != nil {
						return nil, fmt.Errorf("failed to save intake state: %w", err)
					}
					if err := orchSupervisor.SendCommand(discoveryCmd); err != nil {
						return nil, fmt.Errorf("failed to send task discovery command: %w", err)
					}
					if heartbeatTimer != nil {
						if !heartbeatTimer.Stop() {
							select {
							case <-heartbeatTimer.C:
							default:
							}
						}
						heartbeatTimer.Reset(heartbeatTimeout)
						heartbeatActive = true
					}
					lastCommandIK = discoveryCmd.IdempotencyKey
					lastCommandCorrelation = discoveryCmd.CorrelationID
					continue
				}

				if resolution == "" {
					declineDecision := &UserDecision{
						Status:        "denied",
						Reason:        "Plan conflict unresolved by user",
						Prompt:        instruction,
						OccurredAt:    time.Now().UTC(),
						CorrelationID: evt.CorrelationID,
					}
					if recErr := recordIntakeDecision(outputWriter, formatter, intakeLog, state, statePath, runID, clarifications, declineDecision); recErr != nil {
						logger.Warn("failed to record decline decision", "error", recErr)
					}
					return nil, errUserDeclined
				}

				conflictResolutions = append(conflictResolutions, resolution)
				state.SetIntakeConflictResolutions(conflictResolutions)
				if err := runstate.SaveRunState(state, statePath); err != nil {
					return nil, fmt.Errorf("failed to save intake state: %w", err)
				}

				updatedInputs := composeIntakeInputs(baseInputs, clarifications, conflictResolutions)
				command, err = buildIntakeCommand(runID, updatedInputs, time.Now().UTC(), cfg, lastCommandCorrelation, lastCommandIK)
				if err != nil {
					return nil, fmt.Errorf("failed to rebuild intake command after conflict: %w", err)
				}

				if err := intakeLog.WriteCommand(command); err != nil {
					return nil, fmt.Errorf("failed to write intake command: %w", err)
				}
				fmt.Fprintln(outputWriter, formatter.FormatCommand(command))
				state.RecordCommand(command.MessageID, command.CorrelationID)
				state.RecordIntakeCommand(string(command.Action), updatedInputs, command.IdempotencyKey, command.CorrelationID)
				if err := runstate.SaveRunState(state, statePath); err != nil {
					return nil, fmt.Errorf("failed to save intake state: %w", err)
				}

				if err := orchSupervisor.SendCommand(command); err != nil {
					return nil, fmt.Errorf("failed to send intake command: %w", err)
				}
				if heartbeatTimer != nil {
					if !heartbeatTimer.Stop() {
						select {
						case <-heartbeatTimer.C:
						default:
						}
					}
					heartbeatTimer.Reset(heartbeatTimeout)
					heartbeatActive = true
				}
				lastCommandIK = command.IdempotencyKey
				lastCommandCorrelation = command.CorrelationID

			case protocol.EventError:
				return nil, fmt.Errorf("orchestration agent error: %v", evt.Payload)

			default:
				// ignore
			}

		case hb, ok := <-heartbeats:
			if !ok {
				heartbeats = nil
				continue
			}
			if err := intakeLog.WriteHeartbeat(hb); err != nil {
				return nil, fmt.Errorf("failed to write heartbeat: %w", err)
			}
			fmt.Fprintln(outputWriter, formatter.FormatHeartbeat(hb))

			if heartbeatTimer != nil {
				if !heartbeatTimer.Stop() {
					select {
					case <-heartbeatTimer.C:
					default:
					}
				}
				heartbeatTimer.Reset(heartbeatTimeout)
				heartbeatActive = true
			}

		case logMsg, ok := <-logs:
			if !ok {
				logs = nil
				continue
			}
			if err := intakeLog.WriteLog(logMsg); err != nil {
				return nil, fmt.Errorf("failed to write log message: %w", err)
			}
			fmt.Fprintln(outputWriter, formatter.FormatLog(logMsg))

		case <-heartbeatCh:
			if heartbeatTimer == nil {
				continue
			}
			return nil, fmt.Errorf("orchestration heartbeat timed out after %s", heartbeatTimeout)

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if decision == nil {
		return nil, fmt.Errorf("intake flow completed without a decision")
	}

	if err := recordIntakeDecision(outputWriter, formatter, intakeLog, state, statePath, runID, clarifications, decision); err != nil {
		return nil, err
	}

	fmt.Fprintln(outputWriter)
	fmt.Fprintf(outputWriter, "Approved plan: %s\n", decision.ApprovedPlan)
	if len(decision.ApprovedTasks) > 0 {
		fmt.Fprintf(outputWriter, "Approved tasks: %s\n", strings.Join(decision.ApprovedTasks, ", "))
	}
	fmt.Fprintf(outputWriter, "Intake transcript written to %s\n", logPath)

	var outcomeDiscovery *protocol.DiscoveryMetadata
	if parsed, err := protocol.ParseOrchestrationInputs(baseInputs); err == nil {
		outcomeDiscovery = parsed.Discovery
	}

	outcome := &IntakeOutcome{
		RunID:               runID,
		Instruction:         instruction,
		Discovery:           outcomeDiscovery,
		Clarifications:      append([]string(nil), clarifications...),
		ConflictResolutions: append([]string(nil), conflictResolutions...),
		Decision:            decision,
		FinalEvent:          finalEvent,
		LogPath:             logPath,
		StartedAt:           now,
		CompletedAt:         time.Now().UTC(),
	}

	if err := saveIntakeOutcome(workspaceRoot, outcome); err != nil {
		return nil, err
	}

	return outcome, nil
}

// executeApprovedTasks activates and executes intake-derived tasks through the scheduler pipeline.
// This implements P2.4 Task B: connecting intake approval → task execution.
func executeApprovedTasks(
	ctx context.Context,
	cmd *cobra.Command,
	outcome *IntakeOutcome,
	cfg *config.Config,
	workspaceRoot string,
	snapshotID string,
	logger *slog.Logger,
) error {
	// No-op if no tasks were approved
	if outcome.Decision == nil || len(outcome.Decision.ApprovedTasks) == 0 {
		logger.Info("no tasks approved, skipping execution")
		return nil
	}

	outputWriter := cmd.OutOrStdout()
	fmt.Fprintln(outputWriter)
	fmt.Fprintf(outputWriter, "Activating %d approved tasks...\n", len(outcome.Decision.ApprovedTasks))

	// Prepare activation input from intake outcome
	// Extract derived tasks from finalEvent payload
	var derivedTasks []derivedTask
	if outcome.FinalEvent != nil && outcome.FinalEvent.Payload != nil {
		if rawTasks, ok := outcome.FinalEvent.Payload["derived_tasks"].([]any); ok {
			for _, raw := range rawTasks {
				entry, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				id, _ := entry["id"].(string)
				title, _ := entry["title"].(string)
				files := extractStringSlice(entry["files"])
				derivedTasks = append(derivedTasks, derivedTask{
					ID:    id,
					Title: title,
					Files: files,
				})
			}
		}
	}

	// Load run state to check for already-activated tasks (idempotent resume)
	statePath := runstate.GetRunStatePath(workspaceRoot)
	state, err := runstate.LoadRunState(statePath)
	if err != nil {
		// Differentiate missing state from corruption (P2.4 Task B review finding #3)
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to load run state (possible corruption): %w", err)
		}
		// No state exists yet, create new intake state
		state = runstate.NewIntakeState(outcome.RunID, snapshotID, outcome.Instruction, nil)
	}

	// Build activation input
	activationInput := buildActivationInput(outcome, derivedTasks, workspaceRoot, snapshotID, state)

	// Prepare tasks via activation package
	tasks, err := prepareActivationTasks(activationInput)
	if err != nil {
		return fmt.Errorf("failed to prepare activation tasks: %w", err)
	}

	if len(tasks) == 0 {
		logger.Info("no new tasks to activate (all already completed)")
		return nil
	}

	fmt.Fprintf(outputWriter, "Prepared %d tasks for execution\n", len(tasks))

	// Create execution event log (separate from intake log)
	runID := generateExecutionRunID(outcome.RunID)
	eventLogPath := filepath.Join(workspaceRoot, "events", runID+".ndjson")
	evtLog, err := eventlog.NewEventLog(eventLogPath, logger)
	if err != nil {
		return fmt.Errorf("failed to create execution event log: %w", err)
	}
	defer evtLog.Close()

	// Set up execution environment
	env, err := setupExecutionEnvironment(ctx, cfg, workspaceRoot, snapshotID, evtLog, logger)
	if err != nil {
		return fmt.Errorf("failed to setup execution environment: %w", err)
	}
	defer env.cleanup()

	// Update run state to execution stage
	// P2.4 Task B review finding #1: assign execution snapshot ID for correct resume
	state.RunID = runID
	state.SnapshotID = snapshotID
	state.SetStage(runstate.StageImplement)
	if err := runstate.SaveRunState(state, statePath); err != nil {
		logger.Warn("failed to save run state", "error", err)
	}

	// Execute each task through scheduler pipeline
	for i, task := range tasks {
		fmt.Fprintln(outputWriter)
		fmt.Fprintf(outputWriter, "Executing task %d/%d: %s (%s)\n", i+1, len(tasks), task.Title, task.ID)

		// Prepare full command inputs
		inputs := task.ToCommandInputs()

		// P2.4 Task B review finding #2 & resume idempotency:
		// Persist current task ID and full inputs for idempotent resume
		state.TaskID = task.ID
		state.SetCurrentTaskInputs(inputs)
		if err := runstate.SaveRunState(state, statePath); err != nil {
			logger.Warn("failed to save run state before task execution", "error", err)
		}

		// Execute via scheduler (implement → review → spec-maintainer)
		if err := env.scheduler.ExecuteTask(ctx, task.ID, inputs); err != nil {
			state.MarkFailed()
			runstate.SaveRunState(state, statePath)
			return fmt.Errorf("task %s execution failed: %w", task.ID, err)
		}

		// Mark task as activated in run state
		state.MarkTaskActivated(task.ID)
		if err := runstate.SaveRunState(state, statePath); err != nil {
			logger.Warn("failed to save run state", "error", err)
		}

		fmt.Fprintf(outputWriter, "Task %s completed successfully\n", task.ID)
	}

	// Mark run complete
	state.MarkCompleted()
	if err := runstate.SaveRunState(state, statePath); err != nil {
		logger.Warn("failed to save final run state", "error", err)
	}

	fmt.Fprintln(outputWriter)
	fmt.Fprintf(outputWriter, "All %d tasks completed successfully\n", len(tasks))
	fmt.Fprintf(outputWriter, "Execution transcript: %s\n", eventLogPath)

	return nil
}

// buildActivationInput constructs activation.Input from intake outcome for P2.4 Task B.
func buildActivationInput(
	outcome *IntakeOutcome,
	derivedTasks []derivedTask,
	workspaceRoot string,
	snapshotID string,
	state *runstate.RunState,
) activation.Input {
	// Convert derivedTask to activation.DerivedTask
	actDerived := make([]activation.DerivedTask, len(derivedTasks))
	for i, dt := range derivedTasks {
		actDerived[i] = activation.DerivedTask{
			ID:    dt.ID,
			Title: dt.Title,
			Files: dt.Files,
		}
	}

	// Build map of already-activated tasks for idempotent resume
	alreadyActivated := make(map[string]struct{})
	for _, taskID := range state.ActivatedTaskIDs {
		alreadyActivated[taskID] = struct{}{}
	}

	return activation.Input{
		RunID:               outcome.RunID,
		SnapshotID:          snapshotID,
		WorkspaceRoot:       workspaceRoot,
		Instruction:         outcome.Instruction,
		ApprovedPlan:        outcome.Decision.ApprovedPlan,
		ApprovedTaskIDs:     outcome.Decision.ApprovedTasks,
		Clarifications:      outcome.Clarifications,
		ConflictResolutions: outcome.ConflictResolutions,
		DerivedTasks:        actDerived,
		DecisionStatus:      outcome.Decision.Status,
		IntakeCorrelationID: outcome.Decision.CorrelationID,
		AlreadyActivated:    alreadyActivated,
	}
}

// prepareActivationTasks wraps activation.PrepareTasks for P2.4 Task B.
func prepareActivationTasks(input activation.Input) ([]activation.Task, error) {
	return activation.PrepareTasks(input)
}

// generateExecutionRunID creates a run ID for the execution phase, distinct from intake run ID.
func generateExecutionRunID(intakeRunID string) string {
	// Extract timestamp portion from intake run ID if possible, otherwise use current time
	return fmt.Sprintf("run-%s-%s", time.Now().UTC().Format("20060102-150405"), uuid.New().String()[:8])
}

func cloneInputsMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = cloneValue(v)
	}
	return dst
}

func cloneValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return cloneInputsMap(typed)
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			result[i] = cloneValue(item)
		}
		return result
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}

func recordIntakeDecision(
	w io.Writer,
	formatter *transcript.Formatter,
	log *eventlog.EventLog,
	state *runstate.RunState,
	statePath string,
	runID string,
	clarifications []string,
	decision *UserDecision,
) error {
	payload := map[string]any{
		"prompt": decision.Prompt,
	}
	if decision.ApprovedPlan != "" {
		payload["approved_plan"] = decision.ApprovedPlan
	}
	if len(decision.ApprovedTasks) > 0 {
		approved := make([]any, len(decision.ApprovedTasks))
		for i, taskID := range decision.ApprovedTasks {
			approved[i] = taskID
		}
		payload["approved_tasks"] = approved
	}
	if decision.Reason != "" {
		payload["reason"] = decision.Reason
	}

	evt := &protocol.Event{
		Kind:          protocol.MessageKindEvent,
		MessageID:     uuid.New().String(),
		CorrelationID: decision.CorrelationID,
		TaskID:        runID,
		From: protocol.AgentRef{
			AgentType: protocol.AgentTypeSystem,
		},
		Event:      protocol.EventSystemUserDecision,
		Status:     decision.Status,
		Payload:    payload,
		OccurredAt: decision.OccurredAt,
	}

	if err := log.WriteEvent(evt); err != nil {
		return fmt.Errorf("failed to write system decision event: %w", err)
	}

	fmt.Fprintln(w, formatter.FormatEvent(evt))
	state.RecordEvent(evt.MessageID)
	state.RecordIntakeDecision(&runstate.IntakeDecision{
		Status:        decision.Status,
		ApprovedPlan:  decision.ApprovedPlan,
		ApprovedTasks: append([]string(nil), decision.ApprovedTasks...),
		Reason:        decision.Reason,
		Prompt:        decision.Prompt,
		OccurredAt:    decision.OccurredAt,
		CorrelationID: decision.CorrelationID,
	})
	if len(clarifications) > 0 {
		state.SetIntakeClarifications(clarifications)
	}
	if err := runstate.SaveRunState(state, statePath); err != nil {
		return fmt.Errorf("failed to save intake state: %w", err)
	}
	return nil
}

func composeIntakeInputs(base map[string]any, clarifications, conflictResolutions []string) map[string]any {
	inputs := cloneInputsMap(base)
	if inputs == nil {
		inputs = map[string]any{}
	}
	if len(clarifications) > 0 {
		inputs["clarifications"] = append([]string(nil), clarifications...)
	} else {
		delete(inputs, "clarifications")
	}
	if len(conflictResolutions) > 0 {
		inputs["conflict_resolutions"] = append([]string(nil), conflictResolutions...)
	} else {
		delete(inputs, "conflict_resolutions")
	}
	return inputs
}

func extractClarificationQuestions(payload map[string]any) []string {
	raw, ok := payload["questions"].([]any)
	if !ok {
		return nil
	}
	questions := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			questions = append(questions, s)
		}
	}
	return questions
}

type planCandidate struct {
	Path       string
	Confidence float64
	Reason     string
}

type derivedTask struct {
	ID    string
	Title string
	Files []string
}

func parsePlanResponse(payload map[string]any) ([]planCandidate, []derivedTask, string, error) {
	rawCandidates, ok := payload["plan_candidates"].([]any)
	if !ok || len(rawCandidates) == 0 {
		return nil, nil, "", fmt.Errorf("orchestration response missing plan candidates")
	}

	candidates := make([]planCandidate, 0, len(rawCandidates))
	for _, raw := range rawCandidates {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		path, _ := entry["path"].(string)
		confidence := toFloat(entry["confidence"])
		reason, _ := entry["reason"].(string)
		candidates = append(candidates, planCandidate{
			Path:       path,
			Confidence: confidence,
			Reason:     reason,
		})
	}

	var tasks []derivedTask
	if rawTasks, ok := payload["derived_tasks"].([]any); ok {
		tasks = make([]derivedTask, 0, len(rawTasks))
		for _, raw := range rawTasks {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := entry["id"].(string)
			title, _ := entry["title"].(string)
			files := extractStringSlice(entry["files"])
			tasks = append(tasks, derivedTask{
				ID:    id,
				Title: title,
				Files: files,
			})
		}
	}

	notes, _ := payload["notes"].(string)
	return candidates, tasks, notes, nil
}

func toFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case json.Number:
		f, _ := val.Float64()
		return f
	default:
		return 0
	}
}

func extractStringSlice(v any) []string {
	switch values := v.(type) {
	case []any:
		out := make([]string, 0, len(values))
		for _, item := range values {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), values...)
	default:
		return nil
	}
}

func promptPlanApproval(reader *bufio.Reader, w io.Writer, tty bool, instruction string, candidates []planCandidate, tasks []derivedTask, notes string) (*UserDecision, error) {
	index, err := promptPlanSelection(reader, w, tty, candidates, notes)
	if err != nil {
		return nil, err
	}

	approvedTasks, err := promptTaskSelection(reader, w, tty, tasks)
	if err != nil {
		return nil, err
	}

	return &UserDecision{
		Status:        "approved",
		ApprovedPlan:  candidates[index].Path,
		ApprovedTasks: approvedTasks,
		Prompt:        instruction,
	}, nil
}

func promptClarifications(reader *bufio.Reader, w io.Writer, questions []string, tty bool) ([]string, error) {
	answers := make([]string, len(questions))
	for i, question := range questions {
		fmt.Fprintf(w, "\nClarification %d: %s\n", i+1, question)
		if tty {
			fmt.Fprint(w, "> ")
		}
		answer, err := readLine(reader)
		if err != nil {
			return nil, err
		}
		if answer == "" {
			fmt.Fprintln(w, "Please provide an answer.")
			i--
			continue
		}
		answers[i] = answer
	}
	return answers, nil
}

func promptPlanSelection(reader *bufio.Reader, w io.Writer, tty bool, candidates []planCandidate, notes string) (int, error) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Plan candidates:")
	for idx, candidate := range candidates {
		if candidate.Path == "" {
			continue
		}
		if candidate.Confidence > 0 {
			fmt.Fprintf(w, "  %d. %s (score %.2f)\n", idx+1, candidate.Path, candidate.Confidence)
		} else {
			fmt.Fprintf(w, "  %d. %s\n", idx+1, candidate.Path)
		}
		if candidate.Reason != "" {
			fmt.Fprintf(w, "     %s\n", candidate.Reason)
		}
	}
	if notes != "" {
		fmt.Fprintf(w, "\nNotes: %s\n", notes)
	}

	for {
		fmt.Fprintf(w, "Select plan candidate [1-%d], 'm' for more options, or 0 to cancel: ", len(candidates))
		if !tty {
			fmt.Fprintln(w)
		}
		line, err := readLine(reader)
		if err != nil {
			return -1, err
		}
		if line == "" {
			fmt.Fprintln(w, "Please enter a selection.")
			continue
		}
		lower := strings.ToLower(line)
		switch lower {
		case optionMore, optionMoreWord:
			return -1, errRequestMoreOptions
		case optionNone, optionNoneWord:
			return -1, errUserDeclined
		}

		idx, convErr := strconv.Atoi(line)
		if convErr != nil || idx < 1 || idx > len(candidates) {
			fmt.Fprintln(w, "Invalid selection. Please try again.")
			continue
		}
		return idx - 1, nil
	}
}

func promptTaskSelection(reader *bufio.Reader, w io.Writer, tty bool, tasks []derivedTask) ([]string, error) {
	if len(tasks) == 0 {
		return nil, nil
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Derived tasks:")
	for idx, task := range tasks {
		fmt.Fprintf(w, "  %d. %s (%s)\n", idx+1, task.Title, task.ID)
		if len(task.Files) > 0 {
			fmt.Fprintf(w, "       files: %s\n", strings.Join(task.Files, ", "))
		}
	}

	for {
		fmt.Fprint(w, "Select tasks to approve (comma separated numbers, blank for all, 0 to cancel): ")
		if !tty {
			fmt.Fprintln(w)
		}
		line, err := readLine(reader)
		if err != nil {
			return nil, err
		}
		lower := strings.ToLower(line)
		switch lower {
		case "":
			return collectAllTaskIDs(tasks), nil
		case optionAll:
			return collectAllTaskIDs(tasks), nil
		case optionNone, optionNoneWord:
			return nil, errUserDeclined
		}

		indices, parseErr := parseNumberList(line)
		if parseErr != nil {
			fmt.Fprintln(w, "Invalid selection. Please try again.")
			continue
		}

		approved := make([]string, 0, len(indices))
		seen := make(map[int]struct{})
		valid := true
		for _, idx := range indices {
			if idx < 1 || idx > len(tasks) {
				valid = false
				break
			}
			if _, exists := seen[idx]; exists {
				continue
			}
			seen[idx] = struct{}{}
			if taskID := tasks[idx-1].ID; taskID != "" {
				approved = append(approved, taskID)
			}
		}
		if !valid || len(approved) == 0 {
			fmt.Fprintln(w, "Invalid selection. Please try again.")
			continue
		}
		return approved, nil
	}
}

func parseNumberList(input string) ([]int, error) {
	parts := strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	if len(parts) == 0 {
		return nil, fmt.Errorf("no selections")
	}
	result := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func collectAllTaskIDs(tasks []derivedTask) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if task.ID != "" {
			ids = append(ids, task.ID)
		}
	}
	return ids
}

func promptPlanConflictResolution(reader *bufio.Reader, w io.Writer, payload map[string]any, tty bool) (resolution string, requestMore bool, err error) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Plan conflict reported by orchestration:")
	fmt.Fprintln(w, formatConflictPayload(payload))

	for {
		fmt.Fprint(w, "Provide guidance (text), 'm' for more options, or type 'abort' to cancel: ")
		if !tty {
			fmt.Fprintln(w)
		}
		line, err := readLine(reader)
		if err != nil {
			return "", false, err
		}
		lower := strings.ToLower(strings.TrimSpace(line))
		switch lower {
		case "":
			fmt.Fprintln(w, "Please enter a clarification or choose an option.")
			continue
		case optionAbort, optionNone, optionCancel:
			return "", false, nil
		case optionMore, optionMoreWord:
			return "", true, nil
		default:
			return line, false, nil
		}
	}
}

func formatConflictPayload(payload map[string]any) string {
	if len(payload) == 0 {
		return "  (no additional details provided)"
	}
	data, err := json.MarshalIndent(payload, "  ", "  ")
	if err != nil {
		return fmt.Sprintf("  (failed to format conflict payload: %v)", err)
	}
	return "  " + string(data)
}

func buildTaskDiscoveryCommand(runID string, inputs map[string]any, now time.Time, cfg *config.Config, snapshotID string, reuseCorrelation, reuseIK string) (*protocol.Command, error) {
	timeoutSeconds := resolveTimeout(cfg, "task_discovery", 180)

	command := &protocol.Command{
		Kind:           protocol.MessageKindCommand,
		MessageID:      fmt.Sprintf("cmd-%s", uuid.New().String()[:8]),
		CorrelationID:  reuseCorrelation,
		TaskID:         runID,
		IdempotencyKey: "",
		To: protocol.AgentRef{
			AgentType: protocol.AgentTypeOrchestration,
		},
		Action:          protocol.ActionTaskDiscovery,
		Inputs:          inputs,
		ExpectedOutputs: []protocol.ExpectedOutput{},
		Version: protocol.Version{
			SnapshotID: snapshotID,
		},
		Deadline: now.Add(time.Duration(timeoutSeconds) * time.Second),
		Retry: protocol.Retry{
			Attempt:     0,
			MaxAttempts: cfg.Policy.Retry.MaxAttempts,
		},
		Priority: 5,
	}

	if command.CorrelationID == "" {
		command.CorrelationID = fmt.Sprintf("corr-discovery-%s", uuid.New().String()[:8])
	}

	if reuseIK != "" {
		command.IdempotencyKey = reuseIK
		return command, nil
	}

	ik, err := idempotency.GenerateIK(command)
	if err != nil {
		return nil, err
	}
	command.IdempotencyKey = ik
	return command, nil
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if errors.Is(err, io.EOF) {
		if len(line) == 0 {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func saveIntakeOutcome(workspaceRoot string, outcome *IntakeOutcome) error {
	stateDir := filepath.Join(workspaceRoot, "state", "intake")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("failed to create intake state directory: %w", err)
	}

	data, err := json.MarshalIndent(outcome, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal intake outcome: %w", err)
	}

	outPath := filepath.Join(stateDir, outcome.RunID+".json")
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write intake outcome: %w", err)
	}

	latestPath := filepath.Join(stateDir, "latest.json")
	if err := os.WriteFile(latestPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write latest intake outcome: %w", err)
	}

	return nil
}

func promptForInstruction(reader *bufio.Reader, w io.Writer, tty bool) (string, error) {
	if tty {
		fmt.Fprint(w, "lorch> What should I do? ")
	}

	line, err := reader.ReadString('\n')
	if errors.Is(err, io.EOF) {
		line = strings.TrimSpace(line)
		if line == "" {
			return "", errInstructionRequired
		}
		if tty {
			fmt.Fprintln(w)
		}
		return line, nil
	}
	if err != nil {
		return "", err
	}

	line = strings.TrimSpace(line)
	if line == "" {
		return "", errInstructionRequired
	}
	if tty {
		fmt.Fprintln(w)
	}
	return line, nil
}

func buildIntakeCommand(runID string, inputs map[string]any, now time.Time, cfg *config.Config, reuseCorrelation, reuseIK string) (*protocol.Command, error) {
	timeoutSeconds := resolveTimeout(cfg, "intake", 180)

	command := &protocol.Command{
		Kind:           protocol.MessageKindCommand,
		MessageID:      fmt.Sprintf("cmd-%s", uuid.New().String()[:8]),
		CorrelationID:  reuseCorrelation,
		TaskID:         runID,
		IdempotencyKey: "",
		To: protocol.AgentRef{
			AgentType: protocol.AgentTypeOrchestration,
		},
		Action:          protocol.ActionIntake,
		Inputs:          inputs,
		ExpectedOutputs: []protocol.ExpectedOutput{},
		Version: protocol.Version{
			SnapshotID: fmt.Sprintf("snap-%s", runID),
		},
		Deadline: now.Add(time.Duration(timeoutSeconds) * time.Second),
		Retry: protocol.Retry{
			Attempt:     0,
			MaxAttempts: cfg.Policy.Retry.MaxAttempts,
		},
		Priority: 5,
	}

	if command.CorrelationID == "" {
		command.CorrelationID = fmt.Sprintf("corr-intake-%s", uuid.New().String()[:8])
	}

	if reuseIK != "" {
		command.IdempotencyKey = reuseIK
		return command, nil
	}

	ik, err := idempotency.GenerateIK(command)
	if err != nil {
		return nil, err
	}
	command.IdempotencyKey = ik
	return command, nil
}

func resolveTimeout(cfg *config.Config, action string, fallback int) int {
	if cfg.Agents.Orchestration != nil {
		if value, ok := cfg.Agents.Orchestration.TimeoutsS[action]; ok && value > 0 {
			return value
		}
	}
	return fallback
}

func isTerminalFile(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

// executionEnvironment holds all components needed for task execution
type executionEnvironment struct {
	scheduler *scheduler.Scheduler
	cleanup   func()
}

// setupExecutionEnvironment creates agents, scheduler, and returns a cleanup function.
// This is used by both --task mode and P2.4 intake-derived task execution.
func setupExecutionEnvironment(
	ctx context.Context,
	cfg *config.Config,
	workspaceRoot string,
	snapshotID string,
	eventLog *eventlog.EventLog,
	logger *slog.Logger,
) (*executionEnvironment, error) {
	// Create agent supervisors
	builderSup, err := agentSupervisorFactory(cfg.Agents.Builder, protocol.AgentTypeBuilder, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create builder supervisor: %w", err)
	}

	reviewerSup, err := agentSupervisorFactory(cfg.Agents.Reviewer, protocol.AgentTypeReviewer, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create reviewer supervisor: %w", err)
	}

	specMaintainerSup, err := agentSupervisorFactory(cfg.Agents.SpecMaintainer, protocol.AgentTypeSpecMaintainer, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create spec maintainer supervisor: %w", err)
	}

	builder, ok := builderSup.(*supervisor.AgentSupervisor)
	if !ok {
		return nil, fmt.Errorf("unexpected supervisor type for builder")
	}
	reviewer, ok := reviewerSup.(*supervisor.AgentSupervisor)
	if !ok {
		return nil, fmt.Errorf("unexpected supervisor type for reviewer")
	}
	specMaintainer, ok := specMaintainerSup.(*supervisor.AgentSupervisor)
	if !ok {
		return nil, fmt.Errorf("unexpected supervisor type for spec maintainer")
	}

	// Start agents
	if err := builder.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start builder: %w", err)
	}

	if err := reviewer.Start(ctx); err != nil {
		builder.Stop(context.Background())
		return nil, fmt.Errorf("failed to start reviewer: %w", err)
	}

	if err := specMaintainer.Start(ctx); err != nil {
		builder.Stop(context.Background())
		reviewer.Stop(context.Background())
		return nil, fmt.Errorf("failed to start spec maintainer: %w", err)
	}

	// Create scheduler
	sched := scheduler.NewScheduler(builder, reviewer, specMaintainer, logger)
	sched.SetSnapshotID(snapshotID)
	sched.SetWorkspaceRoot(workspaceRoot)
	sched.SetEventLogger(eventLog)
	sched.SetTranscriptFormatter(transcript.NewFormatter())

	// Create cleanup function
	cleanup := func() {
		builder.Stop(context.Background())
		reviewer.Stop(context.Background())
		specMaintainer.Stop(context.Background())
	}

	return &executionEnvironment{
		scheduler: sched,
		cleanup:   cleanup,
	}, nil
}

// realAgentSupervisorFactory creates an agent supervisor from config
func realAgentSupervisorFactory(agentCfg *config.AgentConfig, agentType protocol.AgentType, logger *slog.Logger) (agentSupervisor, error) {
	if agentCfg == nil {
		return nil, fmt.Errorf("agent config for %s is nil", agentType)
	}

	return supervisor.NewAgentSupervisor(agentType, agentCfg.Cmd, agentCfg.Env, logger), nil
}

// loadOrCreateConfig finds an existing config or creates a new one
// Following the decision: walk up directory tree, create in CWD if not found
func loadOrCreateConfig(configPath string, logger *slog.Logger) (*config.Config, string, error) {
	// If explicit path provided, use it
	if configPath != "" {
		cfg, err := config.LoadFromFile(configPath)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load config from %s: %w", configPath, err)
		}
		return cfg, configPath, nil
	}

	// Search up directory tree for lorch.json
	foundPath, err := findConfigInTree()
	if err != nil {
		return nil, "", err
	}

	if foundPath != "" {
		logger.Info("found existing config", "path", foundPath)
		cfg, err := config.LoadFromFile(foundPath)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load config: %w", err)
		}
		return cfg, foundPath, nil
	}

	// No config found, create default in current directory
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get current directory: %w", err)
	}

	defaultPath := filepath.Join(cwd, "lorch.json")
	logger.Info("no config found, creating default", "path", defaultPath)

	cfg := config.GenerateDefault()
	if err := cfg.SaveToFile(defaultPath); err != nil {
		return nil, "", fmt.Errorf("failed to save default config: %w", err)
	}

	logger.Info("created default config", "path", defaultPath)
	return cfg, defaultPath, nil
}

// findConfigInTree searches up the directory tree for lorch.json
func findConfigInTree() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	for {
		configPath := filepath.Join(dir, "lorch.json")
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root
			break
		}
		dir = parent
	}

	return "", nil
}

// determineWorkspaceRoot resolves the workspace root relative to the config file
// Following the decision: resolve relative to directory containing lorch.json
func determineWorkspaceRoot(cfg *config.Config, configPath string) string {
	configDir := filepath.Dir(configPath)
	if cfg.WorkspaceRoot == "." {
		return configDir
	}
	return filepath.Join(configDir, cfg.WorkspaceRoot)
}
