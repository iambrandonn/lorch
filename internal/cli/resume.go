package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/iambrandonn/lorch/internal/config"
	"github.com/iambrandonn/lorch/internal/eventlog"
	"github.com/iambrandonn/lorch/internal/ledger"
	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/iambrandonn/lorch/internal/runstate"
	"github.com/iambrandonn/lorch/internal/scheduler"
	"github.com/iambrandonn/lorch/internal/transcript"
	"github.com/spf13/cobra"
)

var resumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume a previous run",
	Long:  `Resume a previous run from its saved state and ledger.`,
	RunE:  runResume,
}

func init() {
	resumeCmd.Flags().StringP("run", "r", "", "Run ID to resume (required)")
	resumeCmd.MarkFlagRequired("run")
}

func runResume(cmd *cobra.Command, args []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	runID, err := cmd.Flags().GetString("run")
	if err != nil {
		return err
	}

	logger.Info("resuming run", "run_id", runID)

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

	// Determine workspace root
	workspaceRoot := determineWorkspaceRoot(cfg, cfgPath)
	logger.Info("workspace root", "path", workspaceRoot)

	// Load run state
	statePath := runstate.GetRunStatePath(workspaceRoot)
	state, err := runstate.LoadRunState(statePath)
	if err != nil {
		return fmt.Errorf("failed to load run state: %w", err)
	}

	// Verify this is the correct run
	if state.RunID != runID {
		return fmt.Errorf("run state mismatch: found %s, expected %s", state.RunID, runID)
	}

	// Check if run is already complete
	if state.Status == runstate.StatusCompleted {
		logger.Info("run already completed", "run_id", runID)
		return nil
	}

	if state.Status == runstate.StatusAborted {
		return fmt.Errorf("run was aborted, cannot resume")
	}

	logger.Info("resuming run",
		"task_id", state.TaskID,
		"snapshot_id", state.SnapshotID,
		"current_stage", state.CurrentStage,
		"status", state.Status)

	// Load event ledger
	ledgerPath := filepath.Join(workspaceRoot, "events", runID+".ndjson")
	lg, err := ledger.ReadLedger(ledgerPath)
	if err != nil {
		return fmt.Errorf("failed to read ledger: %w", err)
	}

	logger.Info("ledger loaded",
		"commands", len(lg.Commands),
		"events", len(lg.Events),
		"heartbeats", len(lg.Heartbeats))

	// Analyze ledger to find pending commands
	pending := lg.GetPendingCommands()
	if len(pending) == 0 {
		logger.Info("no pending commands, marking run complete")
		state.MarkCompleted()
		return runstate.SaveRunState(state, statePath)
	}

	logger.Info("found pending commands", "count", len(pending))

	// Find task in config
	var task *config.Task
	for i := range cfg.Tasks {
		if cfg.Tasks[i].ID == state.TaskID {
			task = &cfg.Tasks[i]
			break
		}
	}

	if task == nil {
		return fmt.Errorf("task %s not found in config", state.TaskID)
	}

	// Reopen event log for appending
	evtLog, err := eventlog.NewEventLog(ledgerPath, logger)
	if err != nil {
		return fmt.Errorf("failed to reopen event log: %w", err)
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
	sched.SetSnapshotID(state.SnapshotID)
	sched.SetWorkspaceRoot(workspaceRoot)
	sched.SetEventLogger(evtLog)
	sched.SetTranscriptFormatter(transcript.NewFormatter())

	// Resume execution using ledger-aware resume
	// This will skip commands that already have terminal events
	logger.Info("resuming task execution...")
	if err := sched.ResumeTask(ctx, state.TaskID, task.Goal, lg); err != nil {
		state.MarkFailed()
		runstate.SaveRunState(state, statePath)
		return fmt.Errorf("task execution failed: %w", err)
	}

	// Mark run complete
	state.MarkCompleted()
	if err := runstate.SaveRunState(state, statePath); err != nil {
		logger.Warn("failed to save final run state", "error", err)
	}

	logger.Info("resume complete", "run_id", runID)
	return nil
}
