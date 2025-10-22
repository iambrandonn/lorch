package receipt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/iambrandonn/lorch/internal/fsutil"
	"github.com/iambrandonn/lorch/internal/protocol"
)

// Receipt represents a record of completed work for a command
// Based on MASTER-SPEC §16.1 and P1.3-REVIEW-ANSWERS #3
// Extended in P2.4 Task C to include intake traceability metadata
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

	// Intake traceability metadata (P2.4 Task C)
	// These fields link receipts back to their natural language intake origins
	TaskTitle           string   `json:"task_title,omitempty"`            // Human-readable task description from orchestration
	Instruction         string   `json:"instruction,omitempty"`           // Original user instruction (rationale)
	ApprovedPlan        string   `json:"approved_plan,omitempty"`         // Plan file approved during intake (e.g., "PLAN.md")
	IntakeCorrelationID string   `json:"intake_correlation_id,omitempty"` // Links back to intake conversation
	Clarifications      []string `json:"clarifications,omitempty"`        // User clarifications from intake negotiation
	ConflictResolutions []string `json:"conflict_resolutions,omitempty"`  // Conflict resolution choices
}

// extractString safely extracts a string value from the inputs map.
// Returns empty string if key is missing or value is not a string.
func extractString(inputs map[string]any, key string) string {
	if inputs == nil {
		return ""
	}
	val, ok := inputs[key]
	if !ok {
		return ""
	}
	str, ok := val.(string)
	if !ok {
		return ""
	}
	return str
}

// extractStringSlice safely extracts a string slice from the inputs map.
// Returns nil if key is missing or value is not a string slice.
func extractStringSlice(inputs map[string]any, key string) []string {
	if inputs == nil {
		return nil
	}
	val, ok := inputs[key]
	if !ok {
		return nil
	}

	// Try direct string slice
	if slice, ok := val.([]string); ok {
		return slice
	}

	// Try []interface{} and convert each element
	if interfaceSlice, ok := val.([]interface{}); ok {
		result := make([]string, 0, len(interfaceSlice))
		for _, item := range interfaceSlice {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	}

	return nil
}

// extractIntakeCorrelationID extracts the intake correlation ID from a command's correlation ID.
// Activation commands have format: "corr-intake-XXX|activate-YYY" or similar.
// Returns the intake portion if found, otherwise empty string.
func extractIntakeCorrelationID(correlationID string) string {
	if correlationID == "" {
		return ""
	}

	// Look for pipe separator indicating intake lineage
	parts := strings.Split(correlationID, "|")
	if len(parts) > 0 {
		first := parts[0]
		// If it starts with "corr-intake" or contains "intake", it's the intake correlation
		if strings.Contains(first, "intake") {
			return first
		}
	}

	return ""
}

// NewReceipt creates a new receipt from a command and its resulting events.
// Extracts intake traceability metadata from command inputs if present (P2.4 Task C).
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

	// Extract intake traceability metadata from command inputs
	// These fields are populated by the activation layer (activation.Task.ToCommandInputs)
	// and provide linkage back to the natural language intake conversation.
	taskTitle := extractString(cmd.Inputs, "task_title")
	instruction := extractString(cmd.Inputs, "instruction")
	approvedPlan := extractString(cmd.Inputs, "approved_plan")
	clarifications := extractStringSlice(cmd.Inputs, "clarifications")
	conflictResolutions := extractStringSlice(cmd.Inputs, "conflict_resolutions")

	// Extract intake correlation ID - try inputs first (set by scheduler), then command correlation ID
	intakeCorrelationID := extractString(cmd.Inputs, "intake_correlation_id")
	if intakeCorrelationID == "" {
		// Fallback: extract from command correlation ID
		// Format: "corr-intake-XXX|activate-YYY" → extract "corr-intake-XXX"
		intakeCorrelationID = extractIntakeCorrelationID(cmd.CorrelationID)
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

		// Intake traceability (P2.4 Task C)
		TaskTitle:           taskTitle,
		Instruction:         instruction,
		ApprovedPlan:        approvedPlan,
		IntakeCorrelationID: intakeCorrelationID,
		Clarifications:      clarifications,
		ConflictResolutions: conflictResolutions,
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
