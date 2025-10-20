package receipt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/iambrandonn/lorch/internal/fsutil"
	"github.com/iambrandonn/lorch/internal/protocol"
)

// Receipt represents a record of completed work for a command
// Based on MASTER-SPEC §16.1 and P1.3-REVIEW-ANSWERS #3
type Receipt struct {
	TaskID           string               `json:"task_id"`
	Step             int                  `json:"step"`
	Action           string               `json:"action"`
	IdempotencyKey   string               `json:"idempotency_key"`
	SnapshotID       string               `json:"snapshot_id"`
	CommandMessageID string               `json:"command_message_id"`
	CorrelationID    string               `json:"correlation_id"`
	Artifacts        []protocol.Artifact  `json:"artifacts"`
	Events           []string             `json:"events"`
	CreatedAt        time.Time            `json:"created_at"`
}

// NewReceipt creates a new receipt from a command and its resulting events
func NewReceipt(cmd *protocol.Command, step int, events []*protocol.Event) *Receipt {
	// Collect all artifacts from events
	var artifacts []protocol.Artifact
	eventIDs := make([]string, 0, len(events))

	for _, evt := range events {
		eventIDs = append(eventIDs, evt.MessageID)
		if evt.Artifacts != nil {
			artifacts = append(artifacts, evt.Artifacts...)
		}
	}

	return &Receipt{
		TaskID:           cmd.TaskID,
		Step:             step,
		Action:           string(cmd.Action),
		IdempotencyKey:   cmd.IdempotencyKey,
		SnapshotID:       cmd.Version.SnapshotID,
		CommandMessageID: cmd.MessageID,
		CorrelationID:    cmd.CorrelationID,
		Artifacts:        artifacts,
		Events:           eventIDs,
		CreatedAt:        time.Now().UTC(),
	}
}

// WriteReceipt writes a receipt to disk atomically
func WriteReceipt(receipt *Receipt, path string) error {
	return fsutil.AtomicWriteJSON(path, receipt)
}

// ReadReceipt reads a receipt from disk
func ReadReceipt(path string) (*Receipt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read receipt: %w", err)
	}

	var receipt Receipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return nil, fmt.Errorf("failed to unmarshal receipt: %w", err)
	}

	return &receipt, nil
}

// GetReceiptPath returns the standard path for a receipt
// Format: <workspace_root>/receipts/<task_id>/step-<step>.json
func GetReceiptPath(workspaceRoot, taskID string, step int) string {
	return filepath.Join(workspaceRoot, "receipts", taskID, fmt.Sprintf("step-%d.json", step))
}

// ListReceipts returns all receipts for a task
func ListReceipts(workspaceRoot, taskID string) ([]*Receipt, error) {
	receiptsDir := filepath.Join(workspaceRoot, "receipts", taskID)

	// Check if directory exists
	if _, err := os.Stat(receiptsDir); os.IsNotExist(err) {
		return []*Receipt{}, nil
	}

	entries, err := os.ReadDir(receiptsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read receipts directory: %w", err)
	}

	var receipts []*Receipt
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		path := filepath.Join(receiptsDir, entry.Name())
		receipt, err := ReadReceipt(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read receipt %s: %w", entry.Name(), err)
		}

		receipts = append(receipts, receipt)
	}

	return receipts, nil
}
