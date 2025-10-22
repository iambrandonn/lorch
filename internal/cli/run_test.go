package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/config"
	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/iambrandonn/lorch/internal/workspace"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestPromptForInstructionTTY(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("Manage PLAN.md\n"))
	var output bytes.Buffer

	instruction, err := promptForInstruction(input, &output, true)
	require.NoError(t, err)
	require.Equal(t, "Manage PLAN.md", instruction)
	require.Contains(t, output.String(), "lorch> What should I do?")
}

func TestPromptForInstructionNonTTY(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("Implement PLAN.md\n"))
	var output bytes.Buffer

	instruction, err := promptForInstruction(input, &output, false)
	require.NoError(t, err)
	require.Equal(t, "Implement PLAN.md", instruction)
	require.NotContains(t, output.String(), "lorch>")
}

func TestPromptForInstructionEmpty(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("\n"))
	var output bytes.Buffer

	_, err := promptForInstruction(input, &output, true)
	require.Error(t, err)
	require.ErrorIs(t, err, errInstructionRequired)
}

func TestPromptForInstructionEOFWithoutNewline(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("Manage PLAN.md"))
	var output bytes.Buffer

	instruction, err := promptForInstruction(input, &output, false)
	require.NoError(t, err)
	require.Equal(t, "Manage PLAN.md", instruction)
}

func TestPromptForInstructionImmediateEOF(t *testing.T) {
	input := bufio.NewReader(strings.NewReader(""))
	var output bytes.Buffer

	_, err := promptForInstruction(input, &output, false)
	require.Error(t, err)
	require.ErrorIs(t, err, errInstructionRequired)
}

func TestBuildIntakeCommand(t *testing.T) {
	cfg := config.GenerateDefault()
	inputs := map[string]any{
		"user_instruction": "Manage PLAN.md",
	}

	runID := "intake-20250101-000000-abcdef"
	now := time.Unix(1735689600, 0).UTC()

	cmd, err := buildIntakeCommand(runID, inputs, now, cfg, "", "")
	require.NoError(t, err)

	require.Equal(t, protocol.MessageKindCommand, cmd.Kind)
	require.Equal(t, protocol.ActionIntake, cmd.Action)
	require.Equal(t, runID, cmd.TaskID)
	require.Equal(t, fmt.Sprintf("snap-%s", runID), cmd.Version.SnapshotID)
	require.NotEmpty(t, cmd.IdempotencyKey)
	require.Len(t, cmd.ExpectedOutputs, 0)

	timeout := resolveTimeout(cfg, "intake", 180)
	require.WithinDuration(t, now.Add(time.Duration(timeout)*time.Second), cmd.Deadline, time.Second)
}

func TestPromptPlanSelectionNonTTY(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("2\n"))
	var out bytes.Buffer
	candidates := []planCandidate{
		{Path: "PLAN.md", Confidence: 0.8},
		{Path: "docs/plan_v2.md", Confidence: 0.9},
	}

	index, err := promptPlanSelection(reader, &out, false, candidates, "")
	require.NoError(t, err)
	require.Equal(t, 1, index)
	require.Contains(t, out.String(), "Select a plan")
}

func TestPromptTaskSelectionNonTTY(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("1,2\n"))
	var out bytes.Buffer
	tasks := []derivedTask{
		{ID: "TASK-1", Title: "One"},
		{ID: "TASK-2", Title: "Two"},
	}

	selected, err := promptTaskSelection(reader, &out, false, tasks)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"TASK-1", "TASK-2"}, selected)
	require.Contains(t, out.String(), "Select tasks")
}

func TestRunIntakeFlowSuccess(t *testing.T) {
	binary := buildHelperBinary(t, "./cmd/claude-fixture")
	fixturePath := filepath.Join(repoRoot(t), "testdata", "fixtures", "orchestration-simple.json")

	workspaceRoot := t.TempDir()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Implementation Plan\n"), 0o644))

	cfg := config.GenerateDefault()
	cfg.Agents.Orchestration.Cmd = []string{
		binary,
		"--role", "orchestration",
		"--fixture", fixturePath,
		"--no-heartbeat",
	}
	cfg.Agents.Orchestration.TimeoutsS["intake"] = 10

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))

	var output bytes.Buffer
	command := &cobra.Command{}
	command.SetContext(context.Background())
	command.SetIn(strings.NewReader("Manage PLAN.md\n1\n\n"))
	command.SetOut(&output)
	command.SetErr(&output)

	outcome, err := runIntakeFlow(command, cfg, workspaceRoot, logger)
	require.NoError(t, err, output.String())
	require.NotNil(t, outcome)
	require.Equal(t, "Manage PLAN.md", outcome.Instruction)
	require.NotEmpty(t, outcome.RunID)
	require.NotNil(t, outcome.FinalEvent)
	require.NotEmpty(t, outcome.LogPath)
	require.NotNil(t, outcome.Decision)
	require.Equal(t, "approved", outcome.Decision.Status)
	require.Equal(t, "PLAN.md", outcome.Decision.ApprovedPlan)

	data := output.String()
	require.Contains(t, data, "Discovering plan files in workspace")
	require.Contains(t, data, "Plan candidates:")
	require.Contains(t, data, "Approved plan: PLAN.md")
	require.Contains(t, data, "Intake transcript written")

	eventsDir := filepath.Join(workspaceRoot, "events")
	entries, err := os.ReadDir(eventsDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Contains(t, entries[0].Name(), "-intake.ndjson")
	require.Equal(t, outcome.LogPath, filepath.Join(workspaceRoot, "events", entries[0].Name()))

	stateDir := filepath.Join(workspaceRoot, "state", "intake")
	require.DirExists(t, stateDir)
	stateFiles, err := os.ReadDir(stateDir)
	require.NoError(t, err)
	require.Len(t, stateFiles, 2)
	require.FileExists(t, filepath.Join(stateDir, outcome.RunID+".json"))
	require.FileExists(t, filepath.Join(stateDir, "latest.json"))
}

func TestRunIntakeFlowRequiresInstruction(t *testing.T) {
	cfg := config.GenerateDefault()
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))

	command := &cobra.Command{}
	command.SetContext(context.Background())
	command.SetIn(strings.NewReader("\n"))
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)

	result, err := runIntakeFlow(command, cfg, t.TempDir(), logger)
	require.Error(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "instruction required")
}

func TestRunIntakeFlowContextCancellation(t *testing.T) {
	binary := buildHelperBinary(t, "./cmd/claude-fixture")
	fixturePath := buildSlowFixture(t)

	workspaceRoot := t.TempDir()
	require.NoError(t, workspace.Initialize(workspaceRoot))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "PLAN.md"), []byte("# Implementation Plan\n"), 0o644))

	cfg := config.GenerateDefault()
	cfg.Agents.Orchestration.Cmd = []string{
		binary,
		"--role", "orchestration",
		"--fixture", fixturePath,
		"--no-heartbeat",
	}
	cfg.Agents.Orchestration.TimeoutsS["intake"] = 10

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, cancel := context.WithCancel(context.Background())
	command := &cobra.Command{}
	command.SetContext(ctx)
	command.SetIn(strings.NewReader("Manage PLAN.md\n1\n"))
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)

	type result struct {
		outcome *IntakeOutcome
		err     error
	}
	resultCh := make(chan result, 1)
	go func() {
		out, err := runIntakeFlow(command, cfg, workspaceRoot, logger)
		resultCh <- result{outcome: out, err: err}
	}()

	time.Sleep(300 * time.Millisecond)
	cancel()

	res := <-resultCh
	require.Nil(t, res.outcome)
	require.Error(t, res.err)
	require.ErrorIs(t, res.err, context.Canceled)

	entries, readErr := os.ReadDir(filepath.Join(workspaceRoot, "events"))
	require.NoError(t, readErr)
	require.Len(t, entries, 1)
	require.Contains(t, entries[0].Name(), "-intake.ndjson")
	stateDir := filepath.Join(workspaceRoot, "state", "intake")
	_, statErr := os.Stat(stateDir)
	if statErr == nil {
		files, err := os.ReadDir(stateDir)
		require.NoError(t, err)
		require.Empty(t, files)
	}
}

func buildHelperBinary(t *testing.T, pkg string) string {
	t.Helper()
	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, filepath.Base(pkg))

	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "go build failed: %s", string(output))
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Join(wd, "..", "..")
}

func buildSlowFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "slow.json")

	fixture := map[string]any{
		"responses": map[string]any{
			"intake": map[string]any{
				"delay_ms": 2000,
				"events": []any{
					map[string]any{
						"type": protocol.EventOrchestrationProposedTasks,
						"payload": map[string]any{
							"plan_candidates": []any{
								map[string]any{"path": "PLAN.md", "confidence": 0.8},
							},
							"derived_tasks": []any{
								map[string]any{"id": "T-CANCEL", "title": "Cancelled flow"},
							},
						},
					},
				},
			},
		},
	}

	data, err := json.MarshalIndent(fixture, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}
