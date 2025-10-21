package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/iambrandonn/lorch/internal/config"
	"github.com/iambrandonn/lorch/internal/eventlog"
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
		// Phase 2: Natural language intake not yet implemented
		logger.Info("no task specified; natural language intake (Phase 2) not yet implemented")
		return fmt.Errorf("--task is required in Phase 1.3; Phase 2 will add natural language intake")
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
