package cli

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConsoleOutput_IntakePrompt_TTY validates the TTY mode prompt formatting
func TestConsoleOutput_IntakePrompt_TTY(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("Manage PLAN.md\n"))
	var output bytes.Buffer

	_, err := promptForInstruction(input, &output, true)
	require.NoError(t, err)

	result := output.String()

	// Verify TTY prompt appears with example text
	require.Contains(t, result, "lorch> What should I do?")
	require.Contains(t, result, "(e.g., \"Manage PLAN.md\" or \"Implement section 3.1\")")
	require.NotContains(t, result, "\n\n") // Should not have extra newlines in prompt
}

// TestConsoleOutput_IntakePrompt_NonTTY validates the non-TTY mode (no prompt)
func TestConsoleOutput_IntakePrompt_NonTTY(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("Manage PLAN.md\n"))
	var output bytes.Buffer

	_, err := promptForInstruction(input, &output, false)
	require.NoError(t, err)

	result := output.String()

	// Verify no prompt in non-TTY mode (including no example text)
	require.NotContains(t, result, "lorch>")
	require.NotContains(t, result, "What should I do?")
	require.NotContains(t, result, "(e.g., \"Manage PLAN.md\" or \"Implement section 3.1\")")
}

// TestConsoleOutput_PlanCandidateMenu validates multi-candidate menu display
func TestConsoleOutput_PlanCandidateMenu(t *testing.T) {
	tests := []struct {
		name       string
		candidates []planCandidate
		notes      string
		tty        bool
	}{
		{
			name: "two candidates with confidence and reasons",
			candidates: []planCandidate{
				{Path: "PLAN.md", Confidence: 0.9, Reason: "Main implementation plan"},
				{Path: "docs/plan_v2.md", Confidence: 0.75, Reason: "Alternative approach"},
			},
			notes: "Primary recommendation listed first.",
			tty:   true,
		},
		{
			name: "single candidate without confidence",
			candidates: []planCandidate{
				{Path: "PLAN.md", Confidence: 0, Reason: ""},
			},
			notes: "",
			tty:   false,
		},
		{
			name: "three candidates with mixed data",
			candidates: []planCandidate{
				{Path: "PLAN.md", Confidence: 0.85, Reason: "Primary plan"},
				{Path: "docs/plan.md", Confidence: 0.6, Reason: ""},
				{Path: "specs/SPEC.md", Confidence: 0.4, Reason: "Less relevant"},
			},
			notes: "Multiple options available.",
			tty:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			reader := bufio.NewReader(strings.NewReader("1\n"))

			_, err := promptPlanSelection(reader, &output, tt.tty, tt.candidates, tt.notes)
			require.NoError(t, err)

			result := output.String()

			// Verify header
			require.Contains(t, result, "Plan candidates:")

			// Verify all candidates are displayed
			for idx, candidate := range tt.candidates {
				if candidate.Path == "" {
					continue
				}

				// Check numbered list
				require.Contains(t, result, candidate.Path)

				// Check confidence display (if present)
				if candidate.Confidence > 0 {
					require.Contains(t, result, "score")
				}

				// Check reason display (if present)
				if candidate.Reason != "" {
					require.Contains(t, result, candidate.Reason)
				}

				// Verify numbering
				expectedNum := string(rune('1' + idx))
				require.Contains(t, result, expectedNum+".")
			}

			// Verify notes display
			if tt.notes != "" {
				require.Contains(t, result, "Notes:")
				require.Contains(t, result, tt.notes)
			}

			// Verify selection prompt
			require.Contains(t, result, "Select a plan")
			require.Contains(t, result, "'m' for more")
			require.Contains(t, result, "or '0' to cancel")

			// Verify TTY formatting
			if tt.tty {
				// TTY mode should not print extra newlines after prompt
				require.NotContains(t, result, "or '0' to cancel: \n")
			} else {
				// Non-TTY mode should have newline after prompt
				require.Contains(t, result, "or '0' to cancel: \n")
			}
		})
	}
}

// TestConsoleOutput_TaskSelectionMenu validates task approval menu display
func TestConsoleOutput_TaskSelectionMenu(t *testing.T) {
	tests := []struct {
		name  string
		tasks []derivedTask
		tty   bool
	}{
		{
			name: "two tasks with files",
			tasks: []derivedTask{
				{ID: "TASK-1", Title: "Implement authentication", Files: []string{"src/auth.go", "src/auth_test.go"}},
				{ID: "TASK-2", Title: "Add logging", Files: []string{"src/logger.go"}},
			},
			tty: true,
		},
		{
			name: "single task without files",
			tasks: []derivedTask{
				{ID: "TASK-SIMPLE", Title: "Update README", Files: []string{}},
			},
			tty: false,
		},
		{
			name: "three tasks with varying file counts",
			tasks: []derivedTask{
				{ID: "T-1", Title: "Task one", Files: []string{"file1.go"}},
				{ID: "T-2", Title: "Task two", Files: []string{"file2.go", "file3.go", "file4.go"}},
				{ID: "T-3", Title: "Task three", Files: []string{}},
			},
			tty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			reader := bufio.NewReader(strings.NewReader("\n")) // blank = all tasks

			_, err := promptTaskSelection(reader, &output, tt.tty, tt.tasks)
			require.NoError(t, err)

			result := output.String()

			// Verify header
			require.Contains(t, result, "Derived tasks:")

			// Verify all tasks are displayed
			for idx, task := range tt.tasks {
				// Check numbered list with title and ID
				expectedNum := string(rune('1' + idx))
				require.Contains(t, result, expectedNum+".")
				require.Contains(t, result, task.Title)
				require.Contains(t, result, "("+task.ID+")")

				// Check files display
				if len(task.Files) > 0 {
					require.Contains(t, result, "files:")
					for _, file := range task.Files {
						require.Contains(t, result, file)
					}
				}
			}

			// Verify selection prompt
			require.Contains(t, result, "Select tasks")
			require.Contains(t, result, "1,2,3")
			require.Contains(t, result, "blank for all")
			require.Contains(t, result, "'0' to cancel")

			// Verify TTY formatting
			if tt.tty {
				require.NotContains(t, result, "'0' to cancel]: \n")
			} else {
				require.Contains(t, result, "'0' to cancel]: \n")
			}
		})
	}
}

// TestConsoleOutput_TaskSelectionMenu_EmptyList validates empty task list handling
func TestConsoleOutput_TaskSelectionMenu_EmptyList(t *testing.T) {
	var output bytes.Buffer
	reader := bufio.NewReader(strings.NewReader("\n"))

	result, err := promptTaskSelection(reader, &output, true, []derivedTask{})
	require.NoError(t, err)
	require.Nil(t, result)

	// Should not display any menu for empty list
	outputStr := output.String()
	require.Empty(t, outputStr)
}

// TestConsoleOutput_ClarificationPrompts validates clarification question display
func TestConsoleOutput_ClarificationPrompts(t *testing.T) {
	tests := []struct {
		name      string
		questions []string
		answers   string
		tty       bool
	}{
		{
			name:      "single question TTY",
			questions: []string{"Which section should be implemented first?"},
			answers:   "Section 2\n",
			tty:       true,
		},
		{
			name:      "multiple questions non-TTY",
			questions: []string{"What is the priority?", "Should tests be included?"},
			answers:   "High\nYes\n",
			tty:       false,
		},
		{
			name:      "three questions with long text",
			questions: []string{"Question 1", "A much longer question with more detail", "Question 3"},
			answers:   "Answer 1\nAnswer 2\nAnswer 3\n",
			tty:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			reader := bufio.NewReader(strings.NewReader(tt.answers))

			_, err := promptClarifications(reader, &output, tt.questions, tt.tty)
			require.NoError(t, err)

			result := output.String()

			// Verify all questions are displayed with numbering
			for idx, question := range tt.questions {
				expectedNum := idx + 1
				require.Contains(t, result, "Clarification "+string(rune('0'+expectedNum))+":")
				require.Contains(t, result, question)
			}

			// Verify TTY prompts
			if tt.tty {
				// Should have "> " prompt for each question
				promptCount := strings.Count(result, "> ")
				require.Equal(t, len(tt.questions), promptCount)
			} else {
				// Non-TTY should not have "> " prompts
				require.NotContains(t, result, "> ")
			}
		})
	}
}

// TestConsoleOutput_ClarificationPrompts_EmptyAnswerRetry validates empty answer retry behavior
func TestConsoleOutput_ClarificationPrompts_EmptyAnswerRetry(t *testing.T) {
	var output bytes.Buffer
	// First answer is empty (should retry), second is valid
	input := "\nValid answer\n"
	reader := bufio.NewReader(strings.NewReader(input))

	answers, err := promptClarifications(reader, &output, []string{"Test question?"}, true)
	require.NoError(t, err)
	require.Len(t, answers, 1)
	require.Equal(t, "Valid answer", answers[0])

	result := output.String()

	// Should display error message for empty answer
	require.Contains(t, result, "Please provide an answer.")

	// Should show the question twice (initial + retry after empty)
	questionCount := strings.Count(result, "Test question?")
	require.Equal(t, 2, questionCount, "Question should appear twice: once initially, once after empty retry")

	// Should have two TTY prompts
	promptCount := strings.Count(result, "> ")
	require.Equal(t, 2, promptCount, "TTY prompt should appear twice")
}

// TestConsoleOutput_ClarificationPrompts_MultipleQuestionsWithRetry validates retry doesn't affect subsequent questions
func TestConsoleOutput_ClarificationPrompts_MultipleQuestionsWithRetry(t *testing.T) {
	var output bytes.Buffer
	// Q1: empty, then valid; Q2: valid immediately
	input := "\nAnswer 1\nAnswer 2\n"
	reader := bufio.NewReader(strings.NewReader(input))

	answers, err := promptClarifications(reader, &output, []string{"Question 1?", "Question 2?"}, true)
	require.NoError(t, err)
	require.Len(t, answers, 2)
	require.Equal(t, "Answer 1", answers[0])
	require.Equal(t, "Answer 2", answers[1])

	result := output.String()

	// Should show retry message once
	retryCount := strings.Count(result, "Please provide an answer.")
	require.Equal(t, 1, retryCount, "Should show retry message once for first empty answer")

	// Q1 should appear twice (initial + retry), Q2 should appear once
	q1Count := strings.Count(result, "Question 1?")
	q2Count := strings.Count(result, "Question 2?")
	require.Equal(t, 2, q1Count, "Question 1 should appear twice due to retry")
	require.Equal(t, 1, q2Count, "Question 2 should appear once (no retry)")
}

// TestConsoleOutput_ConflictSummary validates plan conflict display formatting
func TestConsoleOutput_ConflictSummary(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		tty     bool
	}{
		{
			name: "conflict with message and list",
			payload: map[string]any{
				"message":   "Multiple conflicting plan files detected",
				"conflicts": []any{"PLAN.md vs docs/plan_v2.md", "Different priorities"},
			},
			tty: true,
		},
		{
			name:    "empty payload",
			payload: map[string]any{},
			tty:     false,
		},
		{
			name: "conflict with complex nested data",
			payload: map[string]any{
				"message": "Schema mismatch",
				"details": map[string]any{
					"file1": "PLAN.md",
					"file2": "docs/plan.md",
					"issue": "Different versions",
				},
			},
			tty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			reader := bufio.NewReader(strings.NewReader("Use PLAN.md\n"))

			_, _, err := promptPlanConflictResolution(reader, &output, tt.payload, tt.tty)
			require.NoError(t, err)

			result := output.String()

			// Verify conflict header
			require.Contains(t, result, "The orchestration agent detected a plan conflict:")

			// Verify payload is formatted (as JSON)
			if len(tt.payload) == 0 {
				require.Contains(t, result, "(no additional details provided)")
			} else {
				// Should contain at least some of the payload keys
				for key := range tt.payload {
					// JSON formatted output should contain the key
					require.Contains(t, result, key)
				}
			}

			// Verify resolution prompt
			require.Contains(t, result, "How should this be resolved?")
			require.Contains(t, result, "'m' for more options")
			require.Contains(t, result, "'abort' to cancel")

			// Verify TTY formatting
			if tt.tty {
				require.NotContains(t, result, "'abort' to cancel): \n")
			} else {
				require.Contains(t, result, "'abort' to cancel): \n")
			}
		})
	}
}

// TestConsoleOutput_ApprovalConfirmation validates success message formatting
// This test calls the real printApprovalConfirmation helper to ensure snapshot
// tests detect any regressions in the production code (per Phase 2.5 review feedback).
func TestConsoleOutput_ApprovalConfirmation(t *testing.T) {
	tests := []struct {
		name          string
		approvedPlan  string
		approvedTasks []string
		logPath       string
	}{
		{
			name:          "single task",
			approvedPlan:  "PLAN.md",
			approvedTasks: []string{"TASK-1"},
			logPath:       "events/intake-123.ndjson",
		},
		{
			name:          "multiple tasks",
			approvedPlan:  "docs/plan_v2.md",
			approvedTasks: []string{"TASK-1", "TASK-2", "TASK-3"},
			logPath:       "events/intake-456.ndjson",
		},
		{
			name:          "no tasks",
			approvedPlan:  "PLAN.md",
			approvedTasks: []string{},
			logPath:       "events/intake-789.ndjson",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the actual production helper function
			var output bytes.Buffer
			printApprovalConfirmation(&output, tt.approvedPlan, tt.approvedTasks, tt.logPath)

			result := output.String()

			// Verify plan confirmation
			require.Contains(t, result, "Approved plan:")
			require.Contains(t, result, tt.approvedPlan)

			// Verify task list (if present)
			if len(tt.approvedTasks) > 0 {
				require.Contains(t, result, "Approved tasks:")
				for _, taskID := range tt.approvedTasks {
					require.Contains(t, result, taskID)
				}
			}

			// Verify transcript path
			require.Contains(t, result, "Intake transcript written to")
			require.Contains(t, result, tt.logPath)
		})
	}
}

// TestConsoleOutput_DeclineMessage validates decline confirmation display
func TestConsoleOutput_DeclineMessage(t *testing.T) {
	// Test is implied by existing TestRunIntakeFlow_UserDeclineRecorded
	// This validates the error message format when user declines

	// The actual message is in the error return, not console output
	// Just verify the pattern exists in the codebase
	require.Equal(t, "user declined all plan candidates", errUserDeclined.Error())
}

// TestConsoleOutput_DiscoveryMessage validates discovery status messages
// This test calls the real printDiscoveryMessage helper to ensure snapshot
// tests detect any regressions in the production code (per Phase 2.5 review feedback).
func TestConsoleOutput_DiscoveryMessage(t *testing.T) {
	var output bytes.Buffer

	// Call the actual production helper function
	printDiscoveryMessage(&output)

	result := output.String()

	// Verify discovery message format
	require.Contains(t, result, "Discovering plan files in workspace")
	require.True(t, strings.HasPrefix(strings.TrimSpace(result), "Discovering plan files in workspace"))
}

// TestConsoleOutput_ErrorMessages validates error formatting
func TestConsoleOutput_ErrorMessages(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		expectedMsg string
	}{
		{
			name:        "instruction required",
			err:         errInstructionRequired,
			expectedMsg: "instruction is required",
		},
		{
			name:        "user declined",
			err:         errUserDeclined,
			expectedMsg: "user declined all plan candidates",
		},
		{
			name:        "request more options",
			err:         errRequestMoreOptions,
			expectedMsg: "user requested more plan options",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expectedMsg, tt.err.Error())
		})
	}
}

// TestConsoleOutput_MenuOptions validates option keywords across all prompts
func TestConsoleOutput_MenuOptions(t *testing.T) {
	// Verify magic string constants exist
	require.Equal(t, "m", optionMore)
	require.Equal(t, "more", optionMoreWord)
	require.Equal(t, "0", optionNone)
	require.Equal(t, "none", optionNoneWord)
	require.Equal(t, "abort", optionAbort)
	require.Equal(t, "cancel", optionCancel)
	require.Equal(t, "all", optionAll)

	// These constants ensure consistent UX across all prompts
}
