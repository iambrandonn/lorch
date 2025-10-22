package receipt

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
)

func TestWriteAndReadReceipt(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test receipt
	receipt := &Receipt{
		TaskID:           "T-0042",
		Step:             1,
		Action:           string(protocol.ActionImplement),
		IdempotencyKey:   "ik:abc123...",
		SnapshotID:       "snap-xyz789",
		CommandMessageID: "msg-cmd-001",
		CorrelationID:    "corr-001",
		Artifacts: []protocol.Artifact{
			{
				Path:   "src/main.go",
				SHA256: "sha256:abc...",
				Size:   1234,
			},
			{
				Path:   "tests/main_test.go",
				SHA256: "sha256:def...",
				Size:   567,
			},
		},
		Events:    []string{"msg-e1", "msg-e2", "msg-e3"},
		CreatedAt: time.Now().UTC(),
	}

	// Write receipt
	receiptPath := filepath.Join(tmpDir, "receipts", "T-0042", "step-1.json")
	if err := WriteReceipt(receipt, receiptPath); err != nil {
		t.Fatalf("WriteReceipt() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(receiptPath); os.IsNotExist(err) {
		t.Fatal("receipt file not created")
	}

	// Read back
	loaded, err := ReadReceipt(receiptPath)
	if err != nil {
		t.Fatalf("ReadReceipt() error = %v", err)
	}

	// Verify fields
	if loaded.TaskID != receipt.TaskID {
		t.Errorf("TaskID = %s, want %s", loaded.TaskID, receipt.TaskID)
	}

	if loaded.Step != receipt.Step {
		t.Errorf("Step = %d, want %d", loaded.Step, receipt.Step)
	}

	if loaded.IdempotencyKey != receipt.IdempotencyKey {
		t.Errorf("IdempotencyKey = %s, want %s", loaded.IdempotencyKey, receipt.IdempotencyKey)
	}

	if len(loaded.Artifacts) != len(receipt.Artifacts) {
		t.Errorf("Artifacts count = %d, want %d", len(loaded.Artifacts), len(receipt.Artifacts))
	}

	if len(loaded.Events) != len(receipt.Events) {
		t.Errorf("Events count = %d, want %d", len(loaded.Events), len(receipt.Events))
	}
}

func TestNewReceipt(t *testing.T) {
	// Create test command
	cmd := &protocol.Command{
		TaskID:         "T-0042",
		Action:         protocol.ActionImplement,
		IdempotencyKey: "ik:test123",
		MessageID:      "msg-cmd-001",
		CorrelationID:  "corr-001",
		Version: protocol.Version{
			SnapshotID: "snap-abc123",
		},
	}

	// Create test events
	events := []*protocol.Event{
		{
			MessageID: "msg-e1",
			Event:     protocol.EventBuilderProgress,
		},
		{
			MessageID: "msg-e2",
			Event:     protocol.EventArtifactProduced,
			Artifacts: []protocol.Artifact{
				{
					Path:   "src/main.go",
					SHA256: "sha256:abc...",
					Size:   1234,
				},
			},
		},
		{
			MessageID: "msg-e3",
			Event:     protocol.EventBuilderCompleted,
		},
	}

	// Create receipt
	receipt := NewReceipt(cmd, 1, events)

	// Verify fields
	if receipt.TaskID != "T-0042" {
		t.Errorf("TaskID = %s, want T-0042", receipt.TaskID)
	}

	if receipt.Step != 1 {
		t.Errorf("Step = %d, want 1", receipt.Step)
	}

	if receipt.Action != string(protocol.ActionImplement) {
		t.Errorf("Action = %s, want implement", receipt.Action)
	}

	if receipt.IdempotencyKey != "ik:test123" {
		t.Errorf("IdempotencyKey = %s, want ik:test123", receipt.IdempotencyKey)
	}

	if len(receipt.Events) != 3 {
		t.Errorf("Events count = %d, want 3", len(receipt.Events))
	}

	// Verify artifacts were collected from events
	if len(receipt.Artifacts) != 1 {
		t.Errorf("Artifacts count = %d, want 1", len(receipt.Artifacts))
	} else {
		if receipt.Artifacts[0].Path != "src/main.go" {
			t.Errorf("Artifact path = %s, want src/main.go", receipt.Artifacts[0].Path)
		}
	}
}

func TestGetReceiptPath(t *testing.T) {
	tests := []struct {
		name         string
		workspaceRoot string
		taskID       string
		step         int
		expected     string
	}{
		{
			name:         "simple case",
			workspaceRoot: "/workspace",
			taskID:       "T-0042",
			step:         1,
			expected:     "/workspace/receipts/T-0042/step-1.json",
		},
		{
			name:         "multi-digit step",
			workspaceRoot: "/workspace",
			taskID:       "T-0042",
			step:         15,
			expected:     "/workspace/receipts/T-0042/step-15.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetReceiptPath(tt.workspaceRoot, tt.taskID, tt.step)
			if result != tt.expected {
				t.Errorf("GetReceiptPath() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestListReceipts(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple receipts for a task
	taskID := "T-0042"

	receipts := []*Receipt{
		{
			TaskID:    taskID,
			Step:      1,
			Action:    string(protocol.ActionImplement),
			CreatedAt: time.Now().UTC(),
		},
		{
			TaskID:    taskID,
			Step:      2,
			Action:    string(protocol.ActionReview),
			CreatedAt: time.Now().UTC(),
		},
	}

	// Write receipts
	for _, r := range receipts {
		path := GetReceiptPath(tmpDir, taskID, r.Step)
		if err := WriteReceipt(r, path); err != nil {
			t.Fatalf("WriteReceipt() error = %v", err)
		}
	}

	// List receipts
	loaded, err := ListReceipts(tmpDir, taskID)
	if err != nil {
		t.Fatalf("ListReceipts() error = %v", err)
	}

	if len(loaded) != 2 {
		t.Errorf("ListReceipts() count = %d, want 2", len(loaded))
	}

	// Test empty task
	empty, err := ListReceipts(tmpDir, "T-9999")
	if err != nil {
		t.Fatalf("ListReceipts() error for empty = %v", err)
	}

	if len(empty) != 0 {
		t.Errorf("ListReceipts() for nonexistent task = %d, want 0", len(empty))
	}
}

// TestNewReceiptWithTraceability validates that intake metadata is extracted correctly
// from command inputs (P2.4 Task C - TR-001).
func TestNewReceiptWithTraceability(t *testing.T) {
	// Create command with intake traceability metadata
	cmd := &protocol.Command{
		TaskID:         "PLAN-1",
		Action:         protocol.ActionImplement,
		IdempotencyKey: "ik:test123",
		MessageID:      "msg-cmd-001",
		CorrelationID:  "corr-intake-abc|activate-def",
		Version: protocol.Version{
			SnapshotID: "snap-xyz",
		},
		Inputs: map[string]any{
			"task_title":           "Implement user authentication",
			"instruction":          "Add OAuth2 login flow",
			"approved_plan":        "PLAN.md",
			"clarifications":       []string{"Use Google OAuth", "Store tokens in Redis"},
			"conflict_resolutions": []string{"Keep existing session logic"},
		},
	}

	events := []*protocol.Event{
		{
			MessageID: "msg-e1",
			Event:     protocol.EventBuilderCompleted,
		},
	}

	receipt := NewReceipt(cmd, 1, events)

	// Verify core fields
	if receipt.TaskID != "PLAN-1" {
		t.Errorf("TaskID = %s, want PLAN-1", receipt.TaskID)
	}

	// Verify traceability metadata extraction
	if receipt.TaskTitle != "Implement user authentication" {
		t.Errorf("TaskTitle = %q, want %q", receipt.TaskTitle, "Implement user authentication")
	}

	if receipt.Instruction != "Add OAuth2 login flow" {
		t.Errorf("Instruction = %q, want %q", receipt.Instruction, "Add OAuth2 login flow")
	}

	if receipt.ApprovedPlan != "PLAN.md" {
		t.Errorf("ApprovedPlan = %q, want %q", receipt.ApprovedPlan, "PLAN.md")
	}

	if receipt.IntakeCorrelationID != "corr-intake-abc" {
		t.Errorf("IntakeCorrelationID = %q, want %q", receipt.IntakeCorrelationID, "corr-intake-abc")
	}

	if len(receipt.Clarifications) != 2 {
		t.Errorf("Clarifications length = %d, want 2", len(receipt.Clarifications))
	} else {
		if receipt.Clarifications[0] != "Use Google OAuth" {
			t.Errorf("Clarifications[0] = %q, want %q", receipt.Clarifications[0], "Use Google OAuth")
		}
	}

	if len(receipt.ConflictResolutions) != 1 {
		t.Errorf("ConflictResolutions length = %d, want 1", len(receipt.ConflictResolutions))
	} else {
		if receipt.ConflictResolutions[0] != "Keep existing session logic" {
			t.Errorf("ConflictResolutions[0] = %q", receipt.ConflictResolutions[0])
		}
	}
}

// TestNewReceiptWithoutTraceability validates backward compatibility when
// intake metadata is absent (e.g., Phase 1 tasks without intake).
func TestNewReceiptWithoutTraceability(t *testing.T) {
	cmd := &protocol.Command{
		TaskID:         "T-0042",
		Action:         protocol.ActionImplement,
		IdempotencyKey: "ik:test123",
		MessageID:      "msg-cmd-001",
		CorrelationID:  "corr-T-0042-implement-abc",
		Version: protocol.Version{
			SnapshotID: "snap-xyz",
		},
		Inputs: map[string]any{
			"goal": "Implement sections 3.1-3.3",
		},
	}

	events := []*protocol.Event{
		{
			MessageID: "msg-e1",
			Event:     protocol.EventBuilderCompleted,
		},
	}

	receipt := NewReceipt(cmd, 1, events)

	// Verify core fields still work
	if receipt.TaskID != "T-0042" {
		t.Errorf("TaskID = %s, want T-0042", receipt.TaskID)
	}

	// Verify traceability fields are empty (no panic, no error)
	if receipt.TaskTitle != "" {
		t.Errorf("TaskTitle should be empty for non-intake task, got %q", receipt.TaskTitle)
	}

	if receipt.IntakeCorrelationID != "" {
		t.Errorf("IntakeCorrelationID should be empty for non-intake task, got %q", receipt.IntakeCorrelationID)
	}
}

// TestExtractString validates the helper function for safe string extraction.
func TestExtractString(t *testing.T) {
	tests := []struct {
		name     string
		inputs   map[string]any
		key      string
		expected string
	}{
		{
			name:     "valid string",
			inputs:   map[string]any{"foo": "bar"},
			key:      "foo",
			expected: "bar",
		},
		{
			name:     "missing key",
			inputs:   map[string]any{"foo": "bar"},
			key:      "baz",
			expected: "",
		},
		{
			name:     "nil inputs",
			inputs:   nil,
			key:      "foo",
			expected: "",
		},
		{
			name:     "wrong type",
			inputs:   map[string]any{"foo": 123},
			key:      "foo",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractString(tt.inputs, tt.key)
			if result != tt.expected {
				t.Errorf("extractString() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExtractStringSlice validates the helper for safe slice extraction.
func TestExtractStringSlice(t *testing.T) {
	tests := []struct {
		name     string
		inputs   map[string]any
		key      string
		expected []string
	}{
		{
			name:     "valid string slice",
			inputs:   map[string]any{"items": []string{"a", "b"}},
			key:      "items",
			expected: []string{"a", "b"},
		},
		{
			name:     "interface slice",
			inputs:   map[string]any{"items": []interface{}{"a", "b"}},
			key:      "items",
			expected: []string{"a", "b"},
		},
		{
			name:     "missing key",
			inputs:   map[string]any{"foo": []string{"a"}},
			key:      "items",
			expected: nil,
		},
		{
			name:     "nil inputs",
			inputs:   nil,
			key:      "items",
			expected: nil,
		},
		{
			name:     "wrong type",
			inputs:   map[string]any{"items": "not a slice"},
			key:      "items",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractStringSlice(tt.inputs, tt.key)
			if len(result) != len(tt.expected) {
				t.Errorf("extractStringSlice() length = %d, want %d", len(result), len(tt.expected))
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("extractStringSlice()[%d] = %q, want %q", i, result[i], tt.expected[i])
				}
			}
		})
	}
}

// TestExtractIntakeCorrelationID validates correlation ID extraction logic.
func TestExtractIntakeCorrelationID(t *testing.T) {
	tests := []struct {
		name          string
		correlationID string
		expected      string
	}{
		{
			name:          "activation format",
			correlationID: "corr-intake-abc|activate-def",
			expected:      "corr-intake-abc",
		},
		{
			name:          "intake only",
			correlationID: "corr-intake-xyz",
			expected:      "corr-intake-xyz",
		},
		{
			name:          "non-intake correlation",
			correlationID: "corr-T-0042-implement-abc",
			expected:      "",
		},
		{
			name:          "empty",
			correlationID: "",
			expected:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractIntakeCorrelationID(tt.correlationID)
			if result != tt.expected {
				t.Errorf("extractIntakeCorrelationID() = %q, want %q", result, tt.expected)
			}
		})
	}
}
