package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUtilities provides common testing utilities for all test categories
type TestUtilities struct {
	t        *testing.T
	tempDir  string
	workspace string
}

// NewTestUtilities creates a new test utilities instance
func NewTestUtilities(t *testing.T) *TestUtilities {
	tempDir := t.TempDir()
	workspace := filepath.Join(tempDir, "workspace")
	err := os.MkdirAll(workspace, 0700)
	require.NoError(t, err)

	return &TestUtilities{
		t:         t,
		tempDir:   tempDir,
		workspace: workspace,
	}
}

// Cleanup removes temporary test files
func (tu *TestUtilities) Cleanup() {
	os.RemoveAll(tu.tempDir)
}

// GetWorkspace returns the test workspace path
func (tu *TestUtilities) GetWorkspace() string {
	return tu.workspace
}

// CreateTestFile creates a test file in the workspace
func (tu *TestUtilities) CreateTestFile(relativePath, content string) string {
	fullPath := filepath.Join(tu.workspace, relativePath)
	dir := filepath.Dir(fullPath)
	err := os.MkdirAll(dir, 0700)
	require.NoError(tu.t, err)

	err = os.WriteFile(fullPath, []byte(content), 0600)
	require.NoError(tu.t, err)

	return fullPath
}

// CreateTestAgent creates a test agent with mock dependencies
func (tu *TestUtilities) CreateTestAgent(role protocol.AgentType) *LLMAgent {
	cfg := &AgentConfig{
		Role:      role,
		LLMCLI:    "mock-llm",
		Workspace: tu.workspace,
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	agent, err := NewLLMAgent(cfg)
	require.NoError(tu.t, err)

	// Set up mock dependencies
	agent.llmCaller = NewMockLLMCaller()
	agent.receiptStore = NewMockReceiptStore()
	agent.fsProvider = NewMockFSProvider()
	agent.eventEmitter = NewMockEventEmitter()

	return agent
}

// CreateTestCommand creates a test command with default values
func (tu *TestUtilities) CreateTestCommand(action protocol.Action, taskID string) *protocol.Command {
	return &protocol.Command{
		Kind:          protocol.MessageKindCommand,
		MessageID:     "cmd-test001",
		CorrelationID: "corr-test001",
		TaskID:        taskID,
		IdempotencyKey: "ik:test:key:1234567890123456789012345678901234567890123456789012345678901234",
		To: protocol.AgentRef{
			AgentType: protocol.AgentTypeOrchestration,
		},
		Action: action,
		Inputs: map[string]any{
			"user_instruction": "Test instruction",
		},
		ExpectedOutputs: []protocol.ExpectedOutput{},
		Version: protocol.Version{
			SnapshotID: "snap-test001",
		},
		Deadline: time.Now().Add(180 * time.Second),
		Retry: protocol.Retry{
			Attempt:     0,
			MaxAttempts: 3,
		},
		Priority: 5,
	}
}

// CreateTestReceipt creates a test receipt
func (tu *TestUtilities) CreateTestReceipt(taskID, action, ik string) *Receipt {
	return &Receipt{
		TaskID:         taskID,
		Step:           1,
		IdempotencyKey: ik,
		Artifacts: []protocol.Artifact{
			{
				Path:   "test.txt",
				SHA256: "sha256:test123",
				Size:   100,
			},
		},
		Events:    []string{"event-1", "event-2"},
		CreatedAt: time.Now(),
	}
}

// CreateTestEvent creates a test event
func (tu *TestUtilities) CreateTestEvent(eventName string, cmd *protocol.Command) *protocol.Event {
	return &protocol.Event{
		Kind:          protocol.MessageKindEvent,
		MessageID:     "evt-test001",
		CorrelationID: cmd.CorrelationID,
		TaskID:        cmd.TaskID,
		From: protocol.AgentRef{
			AgentType: protocol.AgentTypeOrchestration,
		},
		Event: eventName,
		Status: "success",
		Payload: map[string]any{
			"test": "data",
		},
		ObservedVersion: &protocol.Version{
			SnapshotID: cmd.Version.SnapshotID,
		},
		OccurredAt: time.Now(),
	}
}

// CreateTestHeartbeat creates a test heartbeat
func (tu *TestUtilities) CreateTestHeartbeat(status protocol.HeartbeatStatus, taskID string) *protocol.Heartbeat {
	return &protocol.Heartbeat{
		Kind: protocol.MessageKindHeartbeat,
		Agent: protocol.AgentRef{
			AgentType: protocol.AgentTypeOrchestration,
			AgentID:   "test-agent-001",
		},
		Seq:            1,
		Status:        status,
		PID:           12345,
		PPID:          12340,
		UptimeS:       10.5,
		LastActivityAt: time.Now(),
		TaskID:        taskID,
	}
}

// CreateTestLog creates a test log message
func (tu *TestUtilities) CreateTestLog(level, message string) *protocol.Log {
	return &protocol.Log{
		Kind:      protocol.MessageKindLog,
		Level:     protocol.LogLevel(level),
		Message:   message,
		Fields:    map[string]any{"test": "field"},
		Timestamp: time.Now(),
	}
}

// AssertJSONValid validates that JSON is valid
func (tu *TestUtilities) AssertJSONValid(jsonStr string) {
	var data map[string]any
	err := json.Unmarshal([]byte(jsonStr), &data)
	assert.NoError(tu.t, err, "Invalid JSON: %s", jsonStr)
}

// AssertMessageSize validates message size is under limit
func (tu *TestUtilities) AssertMessageSize(message string, maxSize int) {
	size := len([]byte(message))
	assert.LessOrEqual(tu.t, size, maxSize, "Message size %d exceeds limit %d", size, maxSize)
}

// AssertPathSafe validates that a path is safe (no traversal)
func (tu *TestUtilities) AssertPathSafe(workspace, path string) {
	// This would use the actual resolveWorkspacePath function
	// For now, just check for basic traversal patterns
	assert.NotContains(tu.t, path, "..", "Path contains traversal: %s", path)
	assert.NotContains(tu.t, path, "//", "Path contains double slashes: %s", path)
}

// CreateMockLLMScript creates a mock LLM script for testing
func (tu *TestUtilities) CreateMockLLMScript() string {
	script := `#!/bin/bash
# Mock LLM script for testing
echo "LLM response to: $(cat)"
`

	scriptPath := filepath.Join(tu.tempDir, "mock-llm.sh")
	err := os.WriteFile(scriptPath, []byte(script), 0755)
	require.NoError(tu.t, err)

	return scriptPath
}

// CreateLargeContent creates content that exceeds size limits for testing
func (tu *TestUtilities) CreateLargeContent(size int) string {
	return strings.Repeat("A", size)
}

// AssertReceiptValid validates receipt structure and content
func (tu *TestUtilities) AssertReceiptValid(receipt *Receipt) {
	assert.NotEmpty(tu.t, receipt.TaskID, "Receipt TaskID should not be empty")
	assert.Greater(tu.t, receipt.Step, 0, "Receipt Step should be positive")
	assert.NotEmpty(tu.t, receipt.IdempotencyKey, "Receipt IdempotencyKey should not be empty")
	assert.NotZero(tu.t, receipt.CreatedAt, "Receipt CreatedAt should not be zero")
}

// AssertArtifactValid validates artifact structure and content
func (tu *TestUtilities) AssertArtifactValid(artifact protocol.Artifact) {
	assert.NotEmpty(tu.t, artifact.Path, "Artifact Path should not be empty")
	assert.NotEmpty(tu.t, artifact.SHA256, "Artifact SHA256 should not be empty")
	assert.GreaterOrEqual(tu.t, artifact.Size, int64(0), "Artifact Size should be non-negative")
	assert.True(tu.t, strings.HasPrefix(artifact.SHA256, "sha256:"), "Artifact SHA256 should have sha256: prefix")
}

// AssertEventValid validates event structure and content
func (tu *TestUtilities) AssertEventValid(event *protocol.Event) {
	assert.Equal(tu.t, protocol.MessageKindEvent, event.Kind, "Event Kind should be event")
	assert.NotEmpty(tu.t, event.MessageID, "Event MessageID should not be empty")
	assert.NotEmpty(tu.t, event.Event, "Event Event should not be empty")
	assert.NotZero(tu.t, event.OccurredAt, "Event OccurredAt should not be zero")
	assert.NotNil(tu.t, event.ObservedVersion, "Event ObservedVersion should not be nil")
	assert.NotEmpty(tu.t, event.ObservedVersion.SnapshotID, "Event ObservedVersion.SnapshotID should not be empty")
}

// AssertHeartbeatValid validates heartbeat structure and content
func (tu *TestUtilities) AssertHeartbeatValid(heartbeat *protocol.Heartbeat) {
	assert.Equal(tu.t, protocol.MessageKindHeartbeat, heartbeat.Kind, "Heartbeat Kind should be heartbeat")
	assert.NotEmpty(tu.t, heartbeat.Agent.AgentType, "Heartbeat Agent.AgentType should not be empty")
	assert.GreaterOrEqual(tu.t, heartbeat.Seq, int64(0), "Heartbeat Seq should be non-negative")
	assert.NotEmpty(tu.t, heartbeat.Status, "Heartbeat Status should not be empty")
	assert.Greater(tu.t, heartbeat.PID, 0, "Heartbeat PID should be positive")
	assert.GreaterOrEqual(tu.t, heartbeat.UptimeS, 0.0, "Heartbeat UptimeS should be non-negative")
	assert.NotEmpty(tu.t, heartbeat.LastActivityAt, "Heartbeat LastActivityAt should not be empty")
}

// AssertLogValid validates log structure and content
func (tu *TestUtilities) AssertLogValid(log *protocol.Log) {
	assert.Equal(tu.t, protocol.MessageKindLog, log.Kind, "Log Kind should be log")
	assert.NotEmpty(tu.t, log.Level, "Log Level should not be empty")
	assert.NotEmpty(tu.t, log.Message, "Log Message should not be empty")
	assert.NotZero(tu.t, log.Timestamp, "Log Timestamp should not be zero")
}

// CreateTestContext creates a test context with timeout
func (tu *TestUtilities) CreateTestContext(timeout time.Duration) context.Context {
	ctx, _ := context.WithTimeout(context.Background(), timeout)
	return ctx
}

// AssertSHA256Valid validates SHA256 checksum format
func (tu *TestUtilities) AssertSHA256Valid(checksum string) {
	assert.True(tu.t, strings.HasPrefix(checksum, "sha256:"), "Checksum should have sha256: prefix")
	assert.Len(tu.t, checksum, 71, "Checksum should be 71 characters (sha256: + 64 hex chars)")

	// Extract hex part and validate
	hexPart := strings.TrimPrefix(checksum, "sha256:")
	assert.Len(tu.t, hexPart, 64, "SHA256 hex part should be 64 characters")

	// Validate hex characters
	for _, char := range hexPart {
		assert.True(tu.t, (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f'),
			"Invalid hex character in checksum: %c", char)
	}
}

// AssertIdempotencyKeyValid validates idempotency key format
func (tu *TestUtilities) AssertIdempotencyKeyValid(ik string) {
	assert.True(tu.t, strings.HasPrefix(ik, "ik:"), "Idempotency key should have ik: prefix")
	assert.GreaterOrEqual(tu.t, len(ik), 16, "Idempotency key should be at least 16 characters")
}

// AssertSnapshotIDValid validates snapshot ID format
func (tu *TestUtilities) AssertSnapshotIDValid(snapshotID string) {
	assert.True(tu.t, strings.HasPrefix(snapshotID, "snap-"), "Snapshot ID should have snap- prefix")
	assert.Len(tu.t, snapshotID, 17, "Snapshot ID should be 17 characters (snap- + 12 hex chars)")
}

// AssertTaskIDValid validates task ID format
func (tu *TestUtilities) AssertTaskIDValid(taskID string) {
	assert.True(tu.t, strings.HasPrefix(taskID, "T-"), "Task ID should have T- prefix")
	assert.Len(tu.t, taskID, 6, "Task ID should be 6 characters (T- + 4 digits)")
}

// AssertMessageIDValid validates message ID format
func (tu *TestUtilities) AssertMessageIDValid(messageID string) {
	assert.True(tu.t, strings.HasPrefix(messageID, "cmd-") || strings.HasPrefix(messageID, "evt-") ||
		strings.HasPrefix(messageID, "hb-") || strings.HasPrefix(messageID, "log-"),
		"Message ID should have appropriate prefix")
	assert.Len(tu.t, messageID, 12, "Message ID should be 12 characters (prefix + 8 hex chars)")
}

// CreateTestArtifact creates a test artifact with proper checksum
func (tu *TestUtilities) CreateTestArtifact(path string, content []byte) protocol.Artifact {
	hash := sha256.Sum256(content)
	return protocol.Artifact{
		Path:   path,
		SHA256: fmt.Sprintf("sha256:%x", hash),
		Size:   int64(len(content)),
	}
}

// AssertArtifactChecksumValid validates artifact checksum matches content
func (tu *TestUtilities) AssertArtifactChecksumValid(artifact protocol.Artifact, content []byte) {
	expectedHash := sha256.Sum256(content)
	expectedChecksum := fmt.Sprintf("sha256:%x", expectedHash)
	assert.Equal(tu.t, expectedChecksum, artifact.SHA256, "Artifact checksum should match content")
	assert.Equal(tu.t, int64(len(content)), artifact.Size, "Artifact size should match content length")
}

// CreateTestWorkspaceStructure creates a standard test workspace structure
func (tu *TestUtilities) CreateTestWorkspaceStructure() {
	// Create standard directories
	dirs := []string{
		"specs",
		"src",
		"tests",
		"reviews",
		"spec_notes",
		"receipts",
		"logs",
		"state",
		"snapshots",
		"transcripts",
	}

	for _, dir := range dirs {
		err := os.MkdirAll(filepath.Join(tu.workspace, dir), 0700)
		require.NoError(tu.t, err)
	}

	// Create some test files
	tu.CreateTestFile("specs/MASTER-SPEC.md", "# Test Spec\n\nThis is a test specification.")
	tu.CreateTestFile("PLAN.md", "# Test Plan\n\nThis is a test plan.")
	tu.CreateTestFile("src/main.go", "package main\n\nfunc main() {\n\t// Test code\n}")
	tu.CreateTestFile("tests/main_test.go", "package main\n\nimport \"testing\"\n\nfunc TestMain(t *testing.T) {\n\t// Test code\n}")
}

// AssertWorkspaceStructureValid validates workspace structure
func (tu *TestUtilities) AssertWorkspaceStructureValid() {
	requiredDirs := []string{
		"specs",
		"src",
		"tests",
		"reviews",
		"spec_notes",
		"receipts",
		"logs",
		"state",
		"snapshots",
		"transcripts",
	}

	for _, dir := range requiredDirs {
		dirPath := filepath.Join(tu.workspace, dir)
		info, err := os.Stat(dirPath)
		assert.NoError(tu.t, err, "Required directory should exist: %s", dir)
		assert.True(tu.t, info.IsDir(), "Should be a directory: %s", dir)
	}
}

// CreateTestConfig creates a test configuration
func (tu *TestUtilities) CreateTestConfig() *AgentConfig {
	return &AgentConfig{
		Role:      protocol.AgentTypeOrchestration,
		LLMCLI:    "mock-llm",
		Workspace: tu.workspace,
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}
}

// AssertConfigValid validates configuration structure
func (tu *TestUtilities) AssertConfigValid(config *AgentConfig) {
	assert.NotEmpty(tu.t, config.Role, "Config Role should not be empty")
	assert.NotEmpty(tu.t, config.LLMCLI, "Config LLMCLI should not be empty")
	assert.NotEmpty(tu.t, config.Workspace, "Config Workspace should not be empty")
	assert.NotNil(tu.t, config.Logger, "Config Logger should not be nil")
}
