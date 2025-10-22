package activation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	errDecisionNotApproved = fmt.Errorf("activation: intake decision not approved")
)

// PrepareTasks maps intake approvals into concrete activation tasks and enforces
// the edge cases covered by TA-001–TA-007.
func PrepareTasks(input Input) ([]Task, error) {
	if strings.TrimSpace(input.DecisionStatus) != "approved" {
		return nil, errDecisionNotApproved
	}

	if len(input.ApprovedTaskIDs) == 0 {
		return nil, nil
	}

	if input.WorkspaceRoot == "" {
		return nil, fmt.Errorf("activation: workspace root required")
	}

	if input.ApprovedPlan == "" {
		return nil, fmt.Errorf("activation: approved plan path required")
	}

	relPlan := filepath.Clean(input.ApprovedPlan)
	if filepath.IsAbs(relPlan) || strings.HasPrefix(relPlan, "..") {
		return nil, fmt.Errorf("activation: approved plan path escapes workspace: %s", input.ApprovedPlan)
	}

	planPath := filepath.Join(input.WorkspaceRoot, filepath.FromSlash(relPlan))
	if _, err := os.Stat(planPath); err != nil {
		return nil, fmt.Errorf("activation: approved plan %s unavailable: %w", input.ApprovedPlan, err)
	}

	if strings.TrimSpace(input.Instruction) == "" {
		return nil, fmt.Errorf("activation: instruction required")
	}

	derivedMap := make(map[string]DerivedTask, len(input.DerivedTasks))
	for _, task := range input.DerivedTasks {
		if task.ID != "" {
			derivedMap[task.ID] = task
		}
	}

	already := make(map[string]struct{}, len(input.AlreadyActivated))
	for id := range input.AlreadyActivated {
		already[id] = struct{}{}
	}

	var tasks []Task
	for _, taskID := range input.ApprovedTaskIDs {
		if _, seen := already[taskID]; seen {
			continue
		}

		derived, ok := derivedMap[taskID]
		if !ok {
			return nil, fmt.Errorf("activation: approved task %s missing from derived task list", taskID)
		}

		if strings.TrimSpace(derived.Title) == "" {
			return nil, fmt.Errorf("activation: derived task %s missing title", taskID)
		}

		task := Task{
			ID:                  taskID,
			Title:               derived.Title,
			Files:               append([]string(nil), derived.Files...),
			Instruction:         input.Instruction,
			ApprovedPlan:        input.ApprovedPlan,
			Clarifications:      append([]string(nil), input.Clarifications...),
			ConflictResolutions: append([]string(nil), input.ConflictResolutions...),
			SnapshotID:          input.SnapshotID,
			RunID:               input.RunID,
			IntakeCorrelationID: input.IntakeCorrelationID,
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}
