package activation

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/iambrandonn/lorch/internal/idempotency"
	"github.com/iambrandonn/lorch/internal/protocol"
)

// DefaultCommandTimeout specifies how long activation commands are valid.
const DefaultCommandTimeout = 10 * time.Minute

// BuildImplementCommand constructs a builder implement command from an
// activation task, including idempotency metadata and intake traceability.
func BuildImplementCommand(task Task) (*protocol.Command, error) {
	if task.ID == "" {
		return nil, fmt.Errorf("activation: task ID required")
	}
	if task.SnapshotID == "" {
		return nil, fmt.Errorf("activation: snapshot ID required")
	}

	prefix := fmt.Sprintf("corr-activate-%s", task.ID)
	if task.IntakeCorrelationID != "" {
		prefix = fmt.Sprintf("%s|activate", task.IntakeCorrelationID)
	}

	command := &protocol.Command{
		Kind:          protocol.MessageKindCommand,
		MessageID:     uuid.New().String(),
		CorrelationID: fmt.Sprintf("%s-%s", prefix, uuid.New().String()[:8]),
		TaskID:        task.ID,
		To: protocol.AgentRef{
			AgentType: protocol.AgentTypeBuilder,
		},
		Action:          protocol.ActionImplement,
		Inputs:          task.ToCommandInputs(),
		ExpectedOutputs: task.ToExpectedOutputs(),
		Version: protocol.Version{
			SnapshotID: task.SnapshotID,
		},
		Deadline: time.Now().Add(DefaultCommandTimeout).UTC(),
		Retry: protocol.Retry{
			Attempt:     0,
			MaxAttempts: 3,
		},
		Priority: 5,
	}

	ik, err := idempotency.GenerateIK(command)
	if err != nil {
		return nil, err
	}
	command.IdempotencyKey = ik

	return command, nil
}
