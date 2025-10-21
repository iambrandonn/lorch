package testharness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iambrandonn/lorch/internal/runstate"
)

func TestRunSmokeSimpleSuccess(t *testing.T) {
	result, workspace := runSmokeScenario(t, ScenarioSimpleSuccess)

	receiptsGlob := filepath.Join(workspace, "receipts", ScenarioSimpleSuccess.TaskID, "*.json")
	matches, err := filepath.Glob(receiptsGlob)
	if err != nil {
		t.Fatalf("failed to glob receipts: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("expected receipts to be written in %s", receiptsGlob)
	}

	if result.RunState.TaskID != ScenarioSimpleSuccess.TaskID {
		t.Fatalf("unexpected task id: %s", result.RunState.TaskID)
	}
}

func TestRunSmokeReviewChangesLoop(t *testing.T) {
	result, workspace := runSmokeScenario(t, ScenarioReviewChanges)

	logPath := filepath.Join(workspace, "events", result.RunState.RunID+".ndjson")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read event log: %v", err)
	}

	text := string(data)
	if !strings.Contains(text, "\"review.completed\"") {
		t.Fatalf("expected review.completed events in log:\n%s", text)
	}
	if !strings.Contains(text, "\"changes_requested\"") {
		t.Fatalf("expected review changes_requested in log:\n%s", text)
	}
	if !strings.Contains(text, "\"approved\"") {
		t.Fatalf("expected review approved status in log:\n%s", text)
	}

	receiptsGlob := filepath.Join(workspace, "receipts", ScenarioReviewChanges.TaskID, "*.json")
	matches, err := filepath.Glob(receiptsGlob)
	if err != nil {
		t.Fatalf("failed to glob receipts: %v", err)
	}
	if len(matches) < 2 {
		t.Fatalf("expected multiple receipts for iteration loop, found %d", len(matches))
	}
}

func TestRunSmokeSpecChangesLoop(t *testing.T) {
	result, workspace := runSmokeScenario(t, ScenarioSpecChanges)

	logPath := filepath.Join(workspace, "events", result.RunState.RunID+".ndjson")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read event log: %v", err)
	}

	text := string(data)
	if !strings.Contains(text, "\"spec.changes_requested\"") {
		t.Fatalf("expected spec.changes_requested in log:\n%s", text)
	}
	if !strings.Contains(text, "\"spec.updated\"") {
		t.Fatalf("expected spec.updated in log:\n%s", text)
	}
}

func runSmokeScenario(t *testing.T, scenario Scenario) (*SmokeResult, string) {
	t.Helper()

	repoRoot, err := DetectRepoRoot()
	if err != nil {
		t.Fatalf("failed to locate repo root: %v", err)
	}

	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	cacheDir := filepath.Join(tempDir, "gocache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("failed to create gocache: %v", err)
	}
	t.Setenv("GOCACHE", cacheDir)

	ctx := context.Background()
	lorchBin, mockagentBin, err := BuildBinaries(ctx, repoRoot, binDir)
	if err != nil {
		t.Fatalf("failed to build binaries: %v", err)
	}

	workspace := filepath.Join(tempDir, "workspace")

	result, err := RunSmoke(ctx, SmokeOptions{
		Scenario:        scenario,
		LorchBinary:     lorchBin,
		MockAgentBinary: mockagentBin,
		WorkspaceDir:    workspace,
	})
	if err != nil {
		t.Fatalf("RunSmoke returned error: %v", err)
	}
	if result.RunErr != nil {
		t.Fatalf("lorch run returned error: %v\nstdout:%s\nstderr:%s", result.RunErr, result.Stdout, result.Stderr)
	}
	if result.RunState == nil {
		t.Fatal("expected run state to be captured")
	}
	if result.RunState.Status != runstate.StatusCompleted {
		t.Fatalf("expected run to complete, got status %s", result.RunState.Status)
	}

	return result, workspace
}
