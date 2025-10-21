package testharness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/iambrandonn/lorch/internal/config"
	"github.com/iambrandonn/lorch/internal/fsutil"
	"github.com/iambrandonn/lorch/internal/runstate"
)

// Scenario defines a deterministic smoke-test flow driven by mockagent scripts.
type Scenario struct {
	Name           string
	TaskID         string
	BuilderScript  string
	ReviewerScript string
	SpecScript     string
	BuilderArgs    []string
	ReviewerArgs   []string
	SpecArgs       []string
}

var (
	// ScenarioSimpleSuccess exercises the happy path implement -> review -> spec-update flow.
	ScenarioSimpleSuccess = Scenario{
		Name:           "simple-success",
		TaskID:         "T-SMOKE-0001",
		BuilderScript:  "testdata/fixtures/simple-success.json",
		ReviewerScript: "testdata/fixtures/simple-success.json",
		SpecScript:     "testdata/fixtures/simple-success.json",
	}
	// ScenarioReviewChanges validates the review change-request loop.
	ScenarioReviewChanges = Scenario{
		Name:          "review-changes-loop",
		TaskID:        "T-SMOKE-REVIEW",
		BuilderScript: "testdata/fixtures/simple-success.json",
		ReviewerArgs:  []string{"-review-changes-count", "1"},
		SpecScript:    "testdata/fixtures/simple-success.json",
	}
	// ScenarioSpecChanges validates spec-maintainer change requests.
	ScenarioSpecChanges = Scenario{
		Name:          "spec-changes-loop",
		TaskID:        "T-SMOKE-SPEC",
		BuilderScript: "testdata/fixtures/spec-changes-requested.json",
		SpecArgs:      []string{"-spec-changes-count", "1"},
	}
)

// SmokeOptions configures RunSmoke.
type SmokeOptions struct {
	Scenario        Scenario
	LorchBinary     string
	MockAgentBinary string
	WorkspaceDir    string
	Env             map[string]string
}

// SmokeResult captures the outcome of a smoke scenario.
type SmokeResult struct {
	Scenario   Scenario
	Workspace  string
	Stdout     string
	Stderr     string
	RunErr     error
	RunState   *runstate.RunState
	ConfigPath string
}

// RunSmoke executes a smoke scenario using the provided binaries and scripts.
func RunSmoke(ctx context.Context, opts SmokeOptions) (*SmokeResult, error) {
	if opts.LorchBinary == "" {
		return nil, fmt.Errorf("lorch binary path is required")
	}
	if opts.MockAgentBinary == "" {
		return nil, fmt.Errorf("mockagent binary path is required")
	}
	if opts.Scenario.TaskID == "" {
		return nil, fmt.Errorf("scenario task ID is required")
	}

	repoRoot, err := DetectRepoRoot()
	if err != nil {
		return nil, err
	}

	workspace := opts.WorkspaceDir
	if workspace == "" {
		workspace, err = os.MkdirTemp("", "lorch-smoke-")
		if err != nil {
			return nil, fmt.Errorf("failed to create workspace: %w", err)
		}
	} else {
		if err := os.MkdirAll(workspace, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create workspace directory: %w", err)
		}
	}

	cfg := config.GenerateDefault()
	cfg.WorkspaceRoot = "."
	cfg.Agents.Orchestration = nil
	builderScript, err := resolveScenarioPath(repoRoot, opts.Scenario.BuilderScript)
	if err != nil {
		return nil, err
	}
	reviewerScript, err := resolveScenarioPath(repoRoot, opts.Scenario.ReviewerScript)
	if err != nil {
		return nil, err
	}
	specScript, err := resolveScenarioPath(repoRoot, opts.Scenario.SpecScript)
	if err != nil {
		return nil, err
	}

	cfg.Agents.Builder.Cmd = buildAgentCommand(opts.MockAgentBinary, "builder", builderScript, opts.Scenario.BuilderArgs)
	cfg.Agents.Reviewer.Cmd = buildAgentCommand(opts.MockAgentBinary, "reviewer", reviewerScript, opts.Scenario.ReviewerArgs)
	cfg.Agents.SpecMaintainer.Cmd = buildAgentCommand(opts.MockAgentBinary, "spec_maintainer", specScript, opts.Scenario.SpecArgs)
	cfg.Tasks = []config.Task{{
		ID:   opts.Scenario.TaskID,
		Goal: fmt.Sprintf("Smoke scenario: %s", opts.Scenario.Name),
	}}

	configPath := filepath.Join(workspace, "lorch-smoke.json")
	if err := writeConfig(configPath, cfg); err != nil {
		return nil, err
	}

	stdOut := &bytes.Buffer{}
	stdErr := &bytes.Buffer{}

	cmd := exec.CommandContext(ctx, opts.LorchBinary, "run", "--config", configPath, "--task", opts.Scenario.TaskID)
	cmd.Dir = workspace
	cmd.Stdout = stdOut
	cmd.Stderr = stdErr
	cmd.Env = mergeEnv(os.Environ(), opts.Env)

	runErr := cmd.Run()

	result := &SmokeResult{
		Scenario:   opts.Scenario,
		Workspace:  workspace,
		Stdout:     stdOut.String(),
		Stderr:     stdErr.String(),
		RunErr:     runErr,
		ConfigPath: configPath,
	}

	runStatePath := runstate.GetRunStatePath(workspace)
	if st, err := runstate.LoadRunState(runStatePath); err == nil {
		result.RunState = st
	}

	return result, nil
}

func resolveScenarioPath(root, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if filepath.IsAbs(path) {
		return path, nil
	}
	abs := filepath.Join(root, path)
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("scenario script %s not found: %w", abs, err)
	}
	return abs, nil
}

func writeConfig(path string, cfg *config.Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	return fsutil.AtomicWrite(path, append(data, '\n'))
}

// DetectRepoRoot locates the repository root by searching for go.mod.
func DetectRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found (starting from %s)", dir)
		}
		dir = parent
	}
}

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	result := append([]string{}, base...)
	for k, v := range overrides {
		result = setEnv(result, k, v)
	}
	return result
}

func buildAgentCommand(binary, agentType, scriptPath string, extra []string) []string {
	cmd := []string{binary, "-type", agentType, "-no-heartbeat"}
	if scriptPath != "" {
		cmd = append(cmd, "-script", scriptPath)
	}
	if len(extra) > 0 {
		cmd = append(cmd, extra...)
	}
	return cmd
}
