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
	"strings"
	"time"

	"github.com/google/uuid"
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
		outcome, err := runIntakeFlow(cmd, cfg, workspaceRoot, logger)
		if err != nil {
			return err
		}
		fmt.Fprintf(outWriter, "Natural language intake completed. Transcript: %s\n", outcome.LogPath)
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

	// Create agent supervisors
	builder, err := createAgentSupervisor(cfg.Agents.Builder, protocol.AgentTypeBuilder, logger)
	if err != nil {
		return fmt.Errorf("failed to create builder supervisor: %w", err)
	}

	reviewer, err := createAgentSupervisor(cfg.Agents.Reviewer, protocol.AgentTypeReviewer, logger)
	if err != nil {
		return fmt.Errorf("failed to create reviewer supervisor: %w", err)
	}

	specMaintainer, err := createAgentSupervisor(cfg.Agents.SpecMaintainer, protocol.AgentTypeSpecMaintainer, logger)
	if err != nil {
		return fmt.Errorf("failed to create spec maintainer supervisor: %w", err)
	}

	// Start agents
	if err := builder.Start(ctx); err != nil {
		return fmt.Errorf("failed to start builder: %w", err)
	}
	defer builder.Stop(context.Background())

	if err := reviewer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start reviewer: %w", err)
	}
	defer reviewer.Stop(context.Background())

	if err := specMaintainer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start spec maintainer: %w", err)
	}
	defer specMaintainer.Stop(context.Background())

	// Create scheduler
	sched := scheduler.NewScheduler(builder, reviewer, specMaintainer, logger)
	sched.SetSnapshotID(snap.SnapshotID)
	sched.SetWorkspaceRoot(workspaceRoot)
	sched.SetEventLogger(evtLog)
	sched.SetTranscriptFormatter(transcript.NewFormatter())

	// Execute task
	logger.Info("starting task execution...")
	if err := sched.ExecuteTask(ctx, taskID, task.Goal); err != nil {
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

var errInstructionRequired = errors.New("instruction is required")

type IntakeOutcome struct {
	RunID       string                      `json:"run_id"`
	Instruction string                      `json:"instruction"`
	Discovery   *protocol.DiscoveryMetadata `json:"discovery"`
	FinalEvent  *protocol.Event             `json:"final_event"`
	LogPath     string                      `json:"log_path"`
	StartedAt   time.Time                   `json:"started_at"`
	CompletedAt time.Time                   `json:"completed_at"`
}

func runIntakeFlow(cmd *cobra.Command, cfg *config.Config, workspaceRoot string, logger *slog.Logger) (*IntakeOutcome, error) {
	if cfg.Agents.Orchestration == nil || !cfg.Agents.Orchestration.Enabled {
		return nil, fmt.Errorf("orchestration agent is not configured or enabled in lorch.json")
	}

	inputReader := cmd.InOrStdin()
	outputWriter := cmd.OutOrStdout()

	isTTY := false
	if file, ok := inputReader.(*os.File); ok {
		isTTY = isTerminalFile(file)
	}

	instruction, err := promptForInstruction(inputReader, outputWriter, isTTY)
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

	inputsMap, err := orchestrationInputs.ToInputsMap()
	if err != nil {
		return nil, fmt.Errorf("failed to build orchestration inputs: %w", err)
	}

	runID := fmt.Sprintf("intake-%s-%s", time.Now().UTC().Format("20060102-150405"), uuid.New().String()[:8])
	now := time.Now().UTC()

	command, err := buildIntakeCommand(runID, inputsMap, now, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build orchestration command: %w", err)
	}

	logPath := filepath.Join(workspaceRoot, "events", runID+"-intake.ndjson")
	intakeLog, err := eventlog.NewEventLog(logPath, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create intake log: %w", err)
	}
	defer intakeLog.Close()

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	timeoutSeconds := resolveTimeout(cfg, "intake", 180)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	orchSupervisor, err := createAgentSupervisor(cfg.Agents.Orchestration, protocol.AgentTypeOrchestration, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create orchestration supervisor: %w", err)
	}
	if err := orchSupervisor.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start orchestration agent: %w", err)
	}
	defer orchSupervisor.Stop(context.Background())

	if err := intakeLog.WriteCommand(command); err != nil {
		return nil, fmt.Errorf("failed to write intake command: %w", err)
	}

	formatter := transcript.NewFormatter()
	fmt.Fprintln(outputWriter, formatter.FormatCommand(command))

	if err := orchSupervisor.SendCommand(command); err != nil {
		return nil, fmt.Errorf("failed to send intake command: %w", err)
	}

	events := orchSupervisor.Events()
	heartbeats := orchSupervisor.Heartbeats()
	logs := orchSupervisor.Logs()

	var finalEvent *protocol.Event
	var finalErr error

	startedAt := now

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case evt, ok := <-events:
				if !ok {
					if finalEvent == nil && finalErr == nil {
						finalErr = fmt.Errorf("orchestration agent exited before responding")
					}
					return
				}
				_ = intakeLog.WriteEvent(evt)
				fmt.Fprintln(outputWriter, formatter.FormatEvent(evt))

				if evt.Event == protocol.EventOrchestrationProposedTasks ||
					evt.Event == protocol.EventOrchestrationNeedsClarification ||
					evt.Event == protocol.EventError {
					finalEvent = evt
					return
				}

			case hb, ok := <-heartbeats:
				if !ok {
					continue
				}
				_ = intakeLog.WriteHeartbeat(hb)
				fmt.Fprintln(outputWriter, formatter.FormatHeartbeat(hb))

			case logMsg, ok := <-logs:
				if !ok {
					continue
				}
				_ = intakeLog.WriteLog(logMsg)
				fmt.Fprintln(outputWriter, formatter.FormatLog(logMsg))

			case <-ctx.Done():
				finalErr = ctx.Err()
				return
			}
		}
	}()

	<-done

	if finalErr != nil {
		return nil, finalErr
	}

	if finalEvent == nil {
		return nil, fmt.Errorf("orchestration agent did not return a response")
	}

	printIntakeSummary(outputWriter, finalEvent)

	fmt.Fprintf(outputWriter, "Intake transcript written to %s\n", logPath)

	outcome := &IntakeOutcome{
		RunID:       runID,
		Instruction: instruction,
		Discovery:   meta,
		FinalEvent:  finalEvent,
		LogPath:     logPath,
		StartedAt:   startedAt,
		CompletedAt: time.Now().UTC(),
	}

	if err := saveIntakeOutcome(workspaceRoot, outcome); err != nil {
		return nil, err
	}

	return outcome, nil
}

func printIntakeSummary(w io.Writer, evt *protocol.Event) {
	switch evt.Event {
	case protocol.EventOrchestrationProposedTasks:
		if planCandidates, ok := evt.Payload["plan_candidates"].([]any); ok && len(planCandidates) > 0 {
			fmt.Fprintln(w)
			fmt.Fprintf(w, "Top plan candidates (%d):\n", len(planCandidates))
			for idx, raw := range planCandidates {
				if candidate, ok := raw.(map[string]any); ok {
					path, _ := candidate["path"].(string)
					score, _ := candidate["score"].(float64)
					reason, _ := candidate["reason"].(string)
					fmt.Fprintf(w, "  %d. %s (score %.2f)\n", idx+1, path, score)
					if reason != "" {
						fmt.Fprintf(w, "     %s\n", reason)
					}
				}
			}
		}
	case protocol.EventOrchestrationNeedsClarification:
		if questions, ok := evt.Payload["questions"].([]any); ok && len(questions) > 0 {
			fmt.Fprintln(w)
			fmt.Fprintf(w, "Orchestration agent requested clarification (%d question(s)):\n", len(questions))
			for idx, raw := range questions {
				if q, ok := raw.(string); ok {
					fmt.Fprintf(w, "  %d. %s\n", idx+1, q)
				}
			}
		}
	}
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

func promptForInstruction(r io.Reader, w io.Writer, tty bool) (string, error) {
	reader := bufio.NewReader(r)
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

func buildIntakeCommand(runID string, inputs map[string]any, now time.Time, cfg *config.Config) (*protocol.Command, error) {
	timeoutSeconds := resolveTimeout(cfg, "intake", 180)

	command := &protocol.Command{
		Kind:           protocol.MessageKindCommand,
		MessageID:      fmt.Sprintf("cmd-%s", uuid.New().String()[:8]),
		CorrelationID:  fmt.Sprintf("corr-intake-%s", uuid.New().String()[:8]),
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

// createAgentSupervisor creates an agent supervisor from config
func createAgentSupervisor(agentCfg *config.AgentConfig, agentType protocol.AgentType, logger *slog.Logger) (*supervisor.AgentSupervisor, error) {
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
