package scheduler

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/eventlog"
	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/iambrandonn/lorch/internal/supervisor"
	"github.com/iambrandonn/lorch/internal/transcript"
)

func TestSchedulerWithLoggingAndTranscripts(t *testing.T) {
	mockAgentPath, err := buildMockAgent(t)
	if err != nil {
		t.Fatalf("failed to build mock agent: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create supervisors
	builder := supervisor.NewAgentSupervisor(
		protocol.AgentTypeBuilder,
		[]string{mockAgentPath, "-type", "builder", "-no-heartbeat"},
		map[string]string{},
		logger,
	)

	reviewer := supervisor.NewAgentSupervisor(
		protocol.AgentTypeReviewer,
		[]string{mockAgentPath, "-type", "reviewer", "-no-heartbeat"},
		map[string]string{},
		logger,
	)

	specMaintainer := supervisor.NewAgentSupervisor(
		protocol.AgentTypeSpecMaintainer,
		[]string{mockAgentPath, "-type", "spec_maintainer", "-no-heartbeat"},
		map[string]string{},
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start agents
	if err := builder.Start(ctx); err != nil {
		t.Fatalf("failed to start builder: %v", err)
	}
	defer builder.Stop(context.Background())

	if err := reviewer.Start(ctx); err != nil {
		t.Fatalf("failed to start reviewer: %v", err)
	}
	defer reviewer.Stop(context.Background())

	if err := specMaintainer.Start(ctx); err != nil {
		t.Fatalf("failed to start spec maintainer: %v", err)
	}
	defer specMaintainer.Stop(context.Background())

	// Create temporary event log
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "events", "test-run.ndjson")

	evtLog, err := eventlog.NewEventLog(logPath, logger)
	if err != nil {
		t.Fatalf("failed to create event log: %v", err)
	}
	defer evtLog.Close()

	// Create transcript formatter
	formatter := transcript.NewFormatter()

	// Create scheduler with logging/transcripts
	scheduler := NewScheduler(builder, reviewer, specMaintainer, logger)
	scheduler.SetEventLogger(evtLog)
	scheduler.SetTranscriptFormatter(formatter)

	// Capture console output by temporarily redirecting stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	outputDone := make(chan string)
	go func() {
		var buf []byte
		buf, _ = io.ReadAll(r)
		outputDone <- string(buf)
	}()

	// Execute task
	taskID := "T-TEST-INTEGRATION"
	goal := "test with logging and transcripts"

	if err := scheduler.ExecuteTask(ctx, taskID, map[string]any{"goal": goal}); err != nil {
		t.Fatalf("ExecuteTask failed: %v", err)
	}

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout
	output := <-outputDone

	// Verify console output contains formatted messages
	if output == "" {
		t.Error("expected console output, got empty string")
	}

	t.Logf("Console output:\n%s", output)

	// Verify output contains expected messages
	expectedPhrases := []string{
		"[lorch→builder]",
		"[builder]",
		"[reviewer]",
		"[spec_maintainer]",
	}

	for _, phrase := range expectedPhrases {
		if !contains(output, phrase) {
			t.Errorf("expected console output to contain %q, but it didn't", phrase)
		}
	}

	// Close event log to flush
	if err := evtLog.Close(); err != nil {
		t.Fatalf("failed to close event log: %v", err)
	}

	// Verify event log was written
	stat, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("event log file not found: %v", err)
	}

	if stat.Size() == 0 {
		t.Error("event log file is empty")
	}

	t.Logf("Event log size: %d bytes", stat.Size())

	// Read and verify event log contents
	file, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("failed to open event log: %v", err)
	}
	defer file.Close()

	// Count lines (each line is a message)
	lineCount := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lineCount++
	}

	if lineCount == 0 {
		t.Error("event log has no lines")
	}

	t.Logf("Event log has %d messages", lineCount)

	// We expect: multiple commands (3 minimum: implement, review, update_spec)
	// and multiple events (3 minimum: builder.completed, review.completed, spec.updated)
	// So minimum 6 messages
	if lineCount < 6 {
		t.Errorf("expected at least 6 messages in event log, got %d", lineCount)
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) >= len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
