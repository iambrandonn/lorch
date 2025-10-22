package activation

import (
	"context"
	"fmt"
)

// TaskExecutor encapsulates the ability to execute a task end-to-end.
type TaskExecutor interface {
	ExecuteTask(ctx context.Context, taskID string, goal string) error
}

// Activate sequentially executes the provided tasks using the supplied executor.
// Tasks are processed in the order they were approved during intake.
func Activate(ctx context.Context, exec TaskExecutor, tasks []Task) error {
	for _, task := range tasks {
		if err := exec.ExecuteTask(ctx, task.ID, task.Title); err != nil {
			return fmt.Errorf("activation: executing task %s: %w", task.ID, err)
		}
	}
	return nil
}
