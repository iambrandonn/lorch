package cli

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPromptPlanSelection_InvalidInputRetry validates invalid selection handling with retry
func TestPromptPlanSelection_InvalidInputRetry(t *testing.T) {
	var output bytes.Buffer
	candidates := []planCandidate{
		{Path: "PLAN.md", Confidence: 0.9},
		{Path: "docs/plan.md", Confidence: 0.8},
	}

	// Input: "invalid" then "999" (out of range) then "2" (valid)
	reader := bufio.NewReader(strings.NewReader("invalid\n999\n2\n"))

	index, err := promptPlanSelection(reader, &output, false, candidates, "")
	require.NoError(t, err)
	require.Equal(t, 1, index) // Second candidate (index 1)

	result := output.String()
	// Should see error messages for invalid attempts
	invalidCount := strings.Count(result, "Invalid selection")
	require.GreaterOrEqual(t, invalidCount, 2, "Should show invalid selection message for both bad inputs")
}

// TestPromptPlanSelection_OutOfRange validates out-of-range handling
func TestPromptPlanSelection_OutOfRange(t *testing.T) {
	var output bytes.Buffer
	candidates := []planCandidate{
		{Path: "PLAN.md", Confidence: 0.9},
	}

	// Try indices 0, 2, then 1 (valid)
	reader := bufio.NewReader(strings.NewReader("0\n2\n1\n"))

	// Note: "0" is a special case (cancel), so let's use different values
	reader = bufio.NewReader(strings.NewReader("2\n1\n"))

	index, err := promptPlanSelection(reader, &output, false, candidates, "")
	require.NoError(t, err)
	require.Equal(t, 0, index)

	result := output.String()
	require.Contains(t, result, "Invalid selection")
}

// TestPromptPlanSelection_EmptyInput validates empty input retry
func TestPromptPlanSelection_EmptyInput(t *testing.T) {
	var output bytes.Buffer
	candidates := []planCandidate{
		{Path: "PLAN.md", Confidence: 0.9},
	}

	// Empty input, then valid
	reader := bufio.NewReader(strings.NewReader("\n1\n"))

	index, err := promptPlanSelection(reader, &output, false, candidates, "")
	require.NoError(t, err)
	require.Equal(t, 0, index)

	result := output.String()
	require.Contains(t, result, "Please enter a selection")
}

// TestPromptPlanSelection_WhitespaceHandling validates whitespace normalization
func TestPromptPlanSelection_WhitespaceHandling(t *testing.T) {
	var output bytes.Buffer
	candidates := []planCandidate{
		{Path: "PLAN.md", Confidence: 0.9},
		{Path: "docs/plan.md", Confidence: 0.8},
	}

	// Input with extra whitespace: "  2  "
	reader := bufio.NewReader(strings.NewReader("  2  \n"))

	index, err := promptPlanSelection(reader, &output, false, candidates, "")
	require.NoError(t, err)
	require.Equal(t, 1, index) // Should parse "2" correctly despite whitespace
}

// TestPromptPlanSelection_SpecialOptions validates "m" and "0" options
func TestPromptPlanSelection_SpecialOptions(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedError error
	}{
		{
			name:          "more options lowercase",
			input:         "m\n",
			expectedError: errRequestMoreOptions,
		},
		{
			name:          "more options word",
			input:         "more\n",
			expectedError: errRequestMoreOptions,
		},
		{
			name:          "decline with zero",
			input:         "0\n",
			expectedError: errUserDeclined,
		},
		{
			name:          "decline with word",
			input:         "none\n",
			expectedError: errUserDeclined,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			candidates := []planCandidate{
				{Path: "PLAN.md", Confidence: 0.9},
			}

			reader := bufio.NewReader(strings.NewReader(tt.input))

			_, err := promptPlanSelection(reader, &output, false, candidates, "")
			require.Error(t, err)
			require.ErrorIs(t, err, tt.expectedError)
		})
	}
}

// TestPromptTaskSelection_InvalidFormat validates malformed comma-separated input
func TestPromptTaskSelection_InvalidFormat(t *testing.T) {
	var output bytes.Buffer
	tasks := []derivedTask{
		{ID: "TASK-1", Title: "One"},
		{ID: "TASK-2", Title: "Two"},
		{ID: "TASK-3", Title: "Three"},
	}

	// Input: "1,abc,2" (contains non-number), then "1,2" (valid)
	reader := bufio.NewReader(strings.NewReader("1,abc,2\n1,2\n"))

	selected, err := promptTaskSelection(reader, &output, false, tasks)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"TASK-1", "TASK-2"}, selected)

	result := output.String()
	require.Contains(t, result, "Invalid selection")
}

// TestPromptTaskSelection_Duplicates validates duplicate selection handling
func TestPromptTaskSelection_Duplicates(t *testing.T) {
	var output bytes.Buffer
	tasks := []derivedTask{
		{ID: "TASK-1", Title: "One"},
		{ID: "TASK-2", Title: "Two"},
	}

	// Input with duplicates: "1,2,1,2"
	reader := bufio.NewReader(strings.NewReader("1,2,1,2\n"))

	selected, err := promptTaskSelection(reader, &output, false, tasks)
	require.NoError(t, err)
	// Should return unique IDs only
	require.Len(t, selected, 2)
	require.ElementsMatch(t, []string{"TASK-1", "TASK-2"}, selected)
}

// TestPromptTaskSelection_MixedValidInvalid validates partial valid selections
func TestPromptTaskSelection_MixedValidInvalid(t *testing.T) {
	var output bytes.Buffer
	tasks := []derivedTask{
		{ID: "TASK-1", Title: "One"},
		{ID: "TASK-2", Title: "Two"},
	}

	// Input: "1,999" (one valid, one out of range), then "1" (valid)
	reader := bufio.NewReader(strings.NewReader("1,999\n1\n"))

	selected, err := promptTaskSelection(reader, &output, false, tasks)
	require.NoError(t, err)
	require.Equal(t, []string{"TASK-1"}, selected)

	result := output.String()
	// Should see error message for invalid input
	require.Contains(t, result, "Invalid selection")
}

// TestPromptTaskSelection_BlankForAll validates blank input selects all
func TestPromptTaskSelection_BlankForAll(t *testing.T) {
	var output bytes.Buffer
	tasks := []derivedTask{
		{ID: "TASK-1", Title: "One"},
		{ID: "TASK-2", Title: "Two"},
		{ID: "TASK-3", Title: "Three"},
	}

	// Blank input
	reader := bufio.NewReader(strings.NewReader("\n"))

	selected, err := promptTaskSelection(reader, &output, false, tasks)
	require.NoError(t, err)
	require.Len(t, selected, 3)
	require.ElementsMatch(t, []string{"TASK-1", "TASK-2", "TASK-3"}, selected)
}

// TestPromptTaskSelection_AllKeyword validates "all" keyword
func TestPromptTaskSelection_AllKeyword(t *testing.T) {
	var output bytes.Buffer
	tasks := []derivedTask{
		{ID: "TASK-1", Title: "One"},
		{ID: "TASK-2", Title: "Two"},
	}

	// "all" keyword
	reader := bufio.NewReader(strings.NewReader("all\n"))

	selected, err := promptTaskSelection(reader, &output, false, tasks)
	require.NoError(t, err)
	require.Len(t, selected, 2)
	require.ElementsMatch(t, []string{"TASK-1", "TASK-2"}, selected)
}

// TestPromptTaskSelection_DeclineOptions validates "0" and "none"
func TestPromptTaskSelection_DeclineOptions(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "zero", input: "0\n"},
		{name: "none", input: "none\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			tasks := []derivedTask{
				{ID: "TASK-1", Title: "One"},
			}

			reader := bufio.NewReader(strings.NewReader(tt.input))

			_, err := promptTaskSelection(reader, &output, false, tasks)
			require.Error(t, err)
			require.ErrorIs(t, err, errUserDeclined)
		})
	}
}

// TestParseNumberList_EdgeCases validates number list parsing edge cases
func TestParseNumberList_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  []int
		expectErr bool
	}{
		{
			name:     "single number",
			input:    "1",
			expected: []int{1},
		},
		{
			name:     "comma separated",
			input:    "1,2,3",
			expected: []int{1, 2, 3},
		},
		{
			name:     "with spaces",
			input:    "1, 2, 3",
			expected: []int{1, 2, 3},
		},
		{
			name:     "mixed delimiters",
			input:    "1  2,3",
			expected: []int{1, 2, 3},
		},
		{
			name:      "contains non-number",
			input:     "1,abc,3",
			expectErr: true,
		},
		{
			name:      "empty string",
			input:     "",
			expectErr: true,
		},
		{
			name:     "negative numbers allowed",
			input:    "-1,2",
			expected: []int{-1, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseNumberList(tt.input)

			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestPromptConflictResolution_SpecialOptions validates conflict resolution special inputs
func TestPromptConflictResolution_SpecialOptions(t *testing.T) {
	tests := []struct {
		name                 string
		input                string
		expectedResolution   string
		expectedRequestMore  bool
		expectedEmptyNoError bool
	}{
		{
			name:               "normal resolution",
			input:              "Use PLAN.md\n",
			expectedResolution: "Use PLAN.md",
		},
		{
			name:                "more options lowercase",
			input:               "m\n",
			expectedRequestMore: true,
		},
		{
			name:                "more options word",
			input:               "more\n",
			expectedRequestMore: true,
		},
		{
			name:                 "abort",
			input:                "abort\n",
			expectedEmptyNoError: true,
		},
		{
			name:                 "cancel",
			input:                "cancel\n",
			expectedEmptyNoError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			payload := map[string]any{
				"message": "Test conflict",
			}

			reader := bufio.NewReader(strings.NewReader(tt.input))

			resolution, requestMore, err := promptPlanConflictResolution(reader, &output, payload, false)
			require.NoError(t, err)

			if tt.expectedRequestMore {
				require.True(t, requestMore)
				require.Empty(t, resolution)
			} else if tt.expectedEmptyNoError {
				require.False(t, requestMore)
				require.Empty(t, resolution)
			} else {
				require.False(t, requestMore)
				require.Equal(t, tt.expectedResolution, resolution)
			}
		})
	}
}

// TestPromptConflictResolution_EmptyInputRetry validates empty input handling
func TestPromptConflictResolution_EmptyInputRetry(t *testing.T) {
	var output bytes.Buffer
	payload := map[string]any{
		"message": "Test conflict",
	}

	// Empty input, then valid
	reader := bufio.NewReader(strings.NewReader("\nUse PLAN.md\n"))

	resolution, requestMore, err := promptPlanConflictResolution(reader, &output, payload, false)
	require.NoError(t, err)
	require.False(t, requestMore)
	require.Equal(t, "Use PLAN.md", resolution)

	result := output.String()
	require.Contains(t, result, "Please enter a clarification")
}

// TestParsePlanResponse_MalformedPayload validates malformed response handling
func TestParsePlanResponse_MalformedPayload(t *testing.T) {
	tests := []struct {
		name      string
		payload   map[string]any
		expectErr bool
	}{
		{
			name: "valid payload",
			payload: map[string]any{
				"plan_candidates": []any{
					map[string]any{"path": "PLAN.md", "confidence": 0.9},
				},
				"derived_tasks": []any{
					map[string]any{"id": "TASK-1", "title": "Task"},
				},
			},
			expectErr: false,
		},
		{
			name: "missing plan_candidates",
			payload: map[string]any{
				"derived_tasks": []any{},
			},
			expectErr: true,
		},
		{
			name: "empty plan_candidates",
			payload: map[string]any{
				"plan_candidates": []any{},
			},
			expectErr: true,
		},
		{
			name: "wrong type for plan_candidates",
			payload: map[string]any{
				"plan_candidates": "not an array",
			},
			expectErr: true,
		},
		{
			name: "malformed candidate object with valid one",
			payload: map[string]any{
				"plan_candidates": []any{
					"not a map",
					map[string]any{"path": "PLAN.md", "confidence": 0.9},
				},
			},
			expectErr: false, // Function is lenient, skips bad entries but has one valid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates, tasks, notes, err := parsePlanResponse(tt.payload)

			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				// Function returns successfully - actual values depend on payload quality
				// Just verify no panic and err is nil
				_ = candidates
				_ = tasks
				_ = notes
			}
		})
	}
}
