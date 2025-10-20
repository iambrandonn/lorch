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
