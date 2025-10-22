package activation

import (
	"context"
	"fmt"
)

// TaskExecutor encapsulates the ability to execute a task end-to-end.
// Updated for P2.4 Task B to accept inputs map instead of just goal string.
type TaskExecutor interface {
	ExecuteTask(ctx context.Context, taskID string, inputs map[string]any) error
}

// Activate sequentially executes the provided tasks using the supplied executor.
// Tasks are processed in the order they were approved during intake.
func Activate(ctx context.Context, exec TaskExecutor, tasks []Task) error {
	for _, task := range tasks {
		inputs := task.ToCommandInputs()
		if err := exec.ExecuteTask(ctx, task.ID, inputs); err != nil {
			return fmt.Errorf("activation: executing task %s: %w", task.ID, err)
		}
	}
	return nil
}
