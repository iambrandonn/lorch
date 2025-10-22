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
	"github.com/iambrandonn/lorch/internal/supervisor"
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

	if state.CurrentStage == runstate.StageIntake {
		logger.Info("resuming intake negotiation")
		outcome, err := runIntakeFlow(cmd, cfg, workspaceRoot, logger)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Natural language intake completed. Transcript: %s\n", outcome.LogPath)
		return nil
	}

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

	// Determine task inputs: use stored inputs for idempotent resume (P2.4 Task B review)
	var inputs map[string]any
	if state.CurrentTaskInputs != nil && len(state.CurrentTaskInputs) > 0 {
		// P2.4 Task B: use stored inputs to ensure idempotency keys match
		logger.Info("resuming with stored task inputs", "task_id", state.TaskID)
		inputs = state.CurrentTaskInputs
	} else if state.Intake != nil && state.Intake.LastDecision != nil {
		// P2.4 Task B: older intake-derived run without stored inputs (backward compat)
		logger.Warn("resuming intake run without stored inputs, idempotency may not match", "task_id", state.TaskID)
		inputs = map[string]any{
			"instruction":   state.Intake.Instruction,
			"approved_plan": state.Intake.LastDecision.ApprovedPlan,
			"goal":          state.TaskID,
		}
		if len(state.Intake.LastClarifications) > 0 {
			inputs["clarifications"] = state.Intake.LastClarifications
		}
		if len(state.Intake.ConflictResolutions) > 0 {
			inputs["conflict_resolutions"] = state.Intake.ConflictResolutions
		}
	} else {
		// Phase 1: config-based task
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
		inputs = map[string]any{"goal": task.Goal}
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
	builderSup, err := agentSupervisorFactory(cfg.Agents.Builder, protocol.AgentTypeBuilder, logger)
	if err != nil {
		return fmt.Errorf("failed to create builder supervisor: %w", err)
	}

	reviewerSup, err := agentSupervisorFactory(cfg.Agents.Reviewer, protocol.AgentTypeReviewer, logger)
	if err != nil {
		return fmt.Errorf("failed to create reviewer supervisor: %w", err)
	}

	specMaintainerSup, err := agentSupervisorFactory(cfg.Agents.SpecMaintainer, protocol.AgentTypeSpecMaintainer, logger)
	if err != nil {
		return fmt.Errorf("failed to create spec maintainer supervisor: %w", err)
	}

	builder, ok := builderSup.(*supervisor.AgentSupervisor)
	if !ok {
		return fmt.Errorf("unexpected supervisor type for builder")
	}
	reviewer, ok := reviewerSup.(*supervisor.AgentSupervisor)
	if !ok {
		return fmt.Errorf("unexpected supervisor type for reviewer")
	}
	specMaintainer, ok := specMaintainerSup.(*supervisor.AgentSupervisor)
	if !ok {
		return fmt.Errorf("unexpected supervisor type for spec maintainer")
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
	if err := sched.ResumeTask(ctx, state.TaskID, inputs, lg); err != nil {
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
