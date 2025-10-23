package main

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrchestrationLogic(t *testing.T) {
	t.Run("IntakeAction", func(t *testing.T) {
		// Create mock components
		mockLLM := NewMockLLMCaller()
		mockReceipt := NewMockReceiptStore()
		mockFS := NewMockFSProvider()
		mockEvents := NewMockEventEmitter()

		// Set up mock LLM response
		mockLLM.SetResponse("test prompt", `{
			"plan_file": "PLAN.md",
			"confidence": 0.95,
			"tasks": [
				{
					"id": "T-001",
					"title": "Test task",
					"files": ["test.go"],
					"notes": "Test notes"
				}
			],
			"needs_clarification": false,
			"clarification_questions": []
		}`)

		// Set up mock file content
		mockFS.SetFile("/workspace/PLAN.md", "# Test Plan\n\nThis is a test plan.")

		// Create agent with mocks
		agent := &LLMAgent{
			config: AgentConfig{
				Role:      protocol.AgentTypeOrchestration,
				Workspace: "/workspace",
				Logger:    slog.Default(), // Add logger
			},
			llmCaller:    mockLLM,
			receiptStore: mockReceipt,
			fsProvider:   mockFS,
			eventEmitter: mockEvents,
		}

		// Create test command with proper discovery metadata
		cmd := &protocol.Command{
			Action: protocol.ActionIntake,
			TaskID: "T-001",
			IdempotencyKey: "test-ik",
			Inputs: map[string]any{
				"user_instruction": "Implement test feature",
				"discovery": map[string]any{
					"root":        "/workspace",
					"strategy":    "heuristic:v1",
					"search_paths": []string{".", "docs"},
					"generated_at": time.Now().Format(time.RFC3339),
					"candidates": []map[string]any{
						{"path": "PLAN.md", "score": 0.9, "reason": "test candidate"},
					},
				},
			},
			Version: protocol.Version{
				SnapshotID: "snap-001",
			},
		}

		// Execute orchestration logic
		err := agent.handleOrchestrationLogic(cmd)
		require.NoError(t, err)

		// Check error log for debugging
		errorLog := mockEvents.GetErrorLog()
		if len(errorLog) > 0 {
			t.Logf("Error log: %v", errorLog)
		}

		// Verify LLM was called
		assert.Equal(t, 1, mockLLM.CallCount())

		// Verify events were emitted
		events := mockEvents.GetEvents()
		assert.Len(t, events, 1)
		assert.Equal(t, "orchestration.proposed_tasks", events[0].Event)
		assert.Equal(t, "success", events[0].Status)
	})

	t.Run("TaskDiscoveryAction", func(t *testing.T) {
		// Create mock components
		mockLLM := NewMockLLMCaller()
		mockReceipt := NewMockReceiptStore()
		mockFS := NewMockFSProvider()
		mockEvents := NewMockEventEmitter()

		// Set up mock LLM response for task discovery
		mockLLM.SetResponse("test prompt", `{
			"plan_file": "PLAN.md",
			"confidence": 0.85,
			"tasks": [
				{
					"id": "T-002",
					"title": "Additional task",
					"files": ["additional.go"],
					"notes": "Found during discovery"
				}
			],
			"needs_clarification": false,
			"clarification_questions": []
		}`)

		// Create agent with mocks
		agent := &LLMAgent{
			config: AgentConfig{
				Role:      protocol.AgentTypeOrchestration,
				Workspace: "/workspace",
				Logger:    slog.Default(), // Add logger
			},
			llmCaller:    mockLLM,
			receiptStore: mockReceipt,
			fsProvider:   mockFS,
			eventEmitter: mockEvents,
		}

		// Create test command for task discovery
		cmd := &protocol.Command{
			Action: protocol.ActionTaskDiscovery,
			TaskID: "T-001",
			IdempotencyKey: "test-ik-discovery",
			Inputs: map[string]any{
				"user_instruction": "Find additional tasks",
				"discovery": map[string]any{
					"root":        "/workspace",
					"strategy":    "heuristic:v1",
					"search_paths": []string{".", "docs"},
					"generated_at": time.Now().Format(time.RFC3339),
					"candidates": []map[string]any{
						{"path": "PLAN.md", "score": 0.9, "reason": "test candidate"},
					},
				},
			},
			Version: protocol.Version{
				SnapshotID: "snap-001",
			},
		}

		// Execute orchestration logic
		err := agent.handleOrchestrationLogic(cmd)
		require.NoError(t, err)

		// Verify LLM was called
		assert.Equal(t, 1, mockLLM.CallCount())

		// Verify events were emitted
		events := mockEvents.GetEvents()
		assert.Len(t, events, 1)
		assert.Equal(t, "orchestration.proposed_tasks", events[0].Event)
	})

	t.Run("NeedsClarification", func(t *testing.T) {
		// Create mock components
		mockLLM := NewMockLLMCaller()
		mockReceipt := NewMockReceiptStore()
		mockFS := NewMockFSProvider()
		mockEvents := NewMockEventEmitter()

		// Set up mock LLM response that needs clarification
		// Use a wildcard approach since the prompt is long
		mockLLM.SetResponse("", `{
			"plan_file": "PLAN.md",
			"confidence": 0.5,
			"tasks": [],
			"needs_clarification": true,
			"clarification_questions": [
				"Which specific features should be implemented?",
				"Should we prioritize performance or simplicity?"
			]
		}`)

		// Create agent with mocks
		agent := &LLMAgent{
			config: AgentConfig{
				Role:      protocol.AgentTypeOrchestration,
				Workspace: "/workspace",
				Logger:    slog.Default(), // Add logger
			},
			llmCaller:    mockLLM,
			receiptStore: mockReceipt,
			fsProvider:   mockFS,
			eventEmitter: mockEvents,
		}

		// Create test command
		cmd := &protocol.Command{
			Action: protocol.ActionIntake,
			TaskID: "T-001",
			IdempotencyKey: "test-ik-clarification",
			Inputs: map[string]any{
				"user_instruction": "Implement something",
				"discovery": map[string]any{
					"root":        "/workspace",
					"strategy":    "heuristic:v1",
					"search_paths": []string{".", "docs"},
					"generated_at": time.Now().Format(time.RFC3339),
					"candidates": []map[string]any{
						{"path": "PLAN.md", "score": 0.9, "reason": "test candidate"},
					},
				},
			},
			Version: protocol.Version{
				SnapshotID: "snap-001",
			},
		}

		// Execute orchestration logic
		err := agent.handleOrchestrationLogic(cmd)
		require.NoError(t, err)

		// Verify events were emitted
		events := mockEvents.GetEvents()
		assert.Len(t, events, 1)
		assert.Equal(t, "orchestration.needs_clarification", events[0].Event)
		assert.Equal(t, "needs_input", events[0].Status)
	})

	t.Run("IdempotencyReplay", func(t *testing.T) {
		// Create mock components
		mockLLM := NewMockLLMCaller()
		mockReceipt := NewMockReceiptStore()
		mockFS := NewMockFSProvider()
		mockEvents := NewMockEventEmitter()

		// Set up existing receipt
		receipt := &Receipt{
			TaskID:         "T-001",
			Step:           1,
			IdempotencyKey: "test-ik-replay",
			Artifacts: []protocol.Artifact{
				{Path: "tasks/T-001.plan.json", SHA256: "sha256:test", Size: 100},
			},
			Events:    []string{"msg-001"},
			CreatedAt: time.Now(),
		}
		mockReceipt.SetReceipt("/receipts/T-001/intake-1.json", receipt)

		// Create agent with mocks
		agent := &LLMAgent{
			config: AgentConfig{
				Role:      protocol.AgentTypeOrchestration,
				Workspace: "/workspace",
				Logger:    slog.Default(), // Add logger
			},
			llmCaller:    mockLLM,
			receiptStore: mockReceipt,
			fsProvider:   mockFS,
			eventEmitter: mockEvents,
		}

		// Create test command with same IK
		cmd := &protocol.Command{
			Action: protocol.ActionIntake,
			TaskID: "T-001",
			IdempotencyKey: "test-ik-replay",
			Inputs: map[string]any{
				"user_instruction": "Implement test feature",
				"discovery": map[string]any{
					"root":        "/workspace",
					"strategy":    "heuristic:v1",
					"search_paths": []string{".", "docs"},
					"generated_at": time.Now().Format(time.RFC3339),
					"candidates": []map[string]any{
						{"path": "PLAN.md", "score": 0.9, "reason": "test candidate"},
					},
				},
			},
			Version: protocol.Version{
				SnapshotID: "snap-001",
			},
		}

		// Execute orchestration logic
		err := agent.handleOrchestrationLogic(cmd)
		require.NoError(t, err)

		// Verify LLM was NOT called (idempotency)
		assert.Equal(t, 0, mockLLM.CallCount())

		// Verify artifact events were replayed
		artifactLog := mockEvents.GetArtifactLog()
		assert.Len(t, artifactLog, 1)
		assert.Contains(t, artifactLog[0], "tasks/T-001.plan.json")
	})

	t.Run("LLMCallFailure", func(t *testing.T) {
		// Create mock components
		mockLLM := NewMockLLMCaller()
		mockReceipt := NewMockReceiptStore()
		mockFS := NewMockFSProvider()
		mockEvents := NewMockEventEmitter()

		// Set up mock LLM to return error
		mockLLM.SetError("", fmt.Errorf("LLM call failed"))

		// Create agent with mocks
		agent := &LLMAgent{
			config: AgentConfig{
				Role:      protocol.AgentTypeOrchestration,
				Workspace: "/workspace",
				Logger:    slog.Default(), // Add logger
			},
			llmCaller:    mockLLM,
			receiptStore: mockReceipt,
			fsProvider:   mockFS,
			eventEmitter: mockEvents,
		}

		// Create test command
		cmd := &protocol.Command{
			Action: protocol.ActionIntake,
			TaskID: "T-001",
			IdempotencyKey: "test-ik-error",
			Inputs: map[string]any{
				"user_instruction": "Implement test feature",
				"discovery": map[string]any{
					"candidates": []map[string]any{
						{"path": "PLAN.md", "score": 0.9},
					},
				},
			},
			Version: protocol.Version{
				SnapshotID: "snap-001",
			},
		}

		// Execute orchestration logic
		err := agent.handleOrchestrationLogic(cmd)
		require.Error(t, err)

		// Verify error event was emitted
		errorLog := mockEvents.GetErrorLog()
		assert.Len(t, errorLog, 1)
		assert.Contains(t, errorLog[0], "llm_call_failed")
	})

	t.Run("InvalidInputs", func(t *testing.T) {
		// Create mock components
		mockLLM := NewMockLLMCaller()
		mockReceipt := NewMockReceiptStore()
		mockFS := NewMockFSProvider()
		mockEvents := NewMockEventEmitter()

		// Create agent with mocks
		agent := &LLMAgent{
			config: AgentConfig{
				Role:      protocol.AgentTypeOrchestration,
				Workspace: "/workspace",
				Logger:    slog.Default(), // Add logger
			},
			llmCaller:    mockLLM,
			receiptStore: mockReceipt,
			fsProvider:   mockFS,
			eventEmitter: mockEvents,
		}

		// Create test command with invalid inputs
		cmd := &protocol.Command{
			Action: protocol.ActionIntake,
			TaskID: "T-001",
			IdempotencyKey: "test-ik-invalid",
			Inputs: map[string]any{
				// Missing required fields
			},
			Version: protocol.Version{
				SnapshotID: "snap-001",
			},
		}

		// Execute orchestration logic
		err := agent.handleOrchestrationLogic(cmd)
		require.Error(t, err)

		// Verify error event was emitted
		errorLog := mockEvents.GetErrorLog()
		assert.Len(t, errorLog, 1)
		assert.Contains(t, errorLog[0], "llm_call_failed")
	})
}

func TestOrchestrationPromptBuilding(t *testing.T) {
	t.Run("IntakePrompt", func(t *testing.T) {
		agent := &LLMAgent{
			config: AgentConfig{
				Workspace: "/workspace",
			},
		}

		candidates := []protocol.DiscoveryCandidate{
			{Path: "PLAN.md", Score: 0.9},
			{Path: "docs/spec.md", Score: 0.6},
		}

		contents := map[string]string{
			"PLAN.md": "# Test Plan\n\nThis is a test plan.",
			"docs/spec.md": "# Specification\n\nThis is a spec.",
		}

		cmd := &protocol.Command{
			Action: protocol.ActionIntake,
			TaskID: "T-001",
		}

		prompt := agent.buildOrchestrationPrompt(false, "Implement test feature", candidates, contents, cmd)

		// Verify prompt contains expected elements
		assert.Contains(t, prompt, "Initial Task Intake")
		assert.Contains(t, prompt, "Implement test feature")
		assert.Contains(t, prompt, "PLAN.md (score: 0.90)")
		assert.Contains(t, prompt, "docs/spec.md (score: 0.60)")
		assert.Contains(t, prompt, "Test Plan")
		assert.Contains(t, prompt, "Your task:")
	})

	t.Run("TaskDiscoveryPrompt", func(t *testing.T) {
		agent := &LLMAgent{
			config: AgentConfig{
				Workspace: "/workspace",
			},
		}

		candidates := []protocol.DiscoveryCandidate{
			{Path: "PLAN.md", Score: 0.9},
		}

		contents := map[string]string{
			"PLAN.md": "# Test Plan\n\nThis is a test plan.",
		}

		cmd := &protocol.Command{
			Action: protocol.ActionTaskDiscovery,
			TaskID: "T-001",
		}

		prompt := agent.buildOrchestrationPrompt(true, "Find additional tasks", candidates, contents, cmd)

		// Verify prompt contains expected elements
		assert.Contains(t, prompt, "Task Discovery (Incremental Expansion)")
		assert.Contains(t, prompt, "Find additional tasks")
		assert.Contains(t, prompt, "You are expanding an existing task plan mid-run")
	})
}

func TestOrchestrationResponseParsing(t *testing.T) {
	t.Run("ValidJSONResponse", func(t *testing.T) {
		agent := &LLMAgent{}

		response := `{
			"plan_file": "PLAN.md",
			"confidence": 0.95,
			"tasks": [
				{
					"id": "T-001",
					"title": "Test task",
					"files": ["test.go"],
					"notes": "Test notes"
				}
			],
			"needs_clarification": false,
			"clarification_questions": []
		}`

		result, err := agent.parseOrchestrationResponse(response)
		require.NoError(t, err)

		assert.Equal(t, "PLAN.md", result.PlanFile)
		assert.Equal(t, 0.95, result.Confidence)
		assert.Len(t, result.Tasks, 1)
		assert.Equal(t, "T-001", result.Tasks[0].ID)
		assert.Equal(t, "Test task", result.Tasks[0].Title)
		assert.False(t, result.NeedsClarification)
	})

	t.Run("MarkdownFencedJSON", func(t *testing.T) {
		agent := &LLMAgent{}

		response := `Here's the response:

` + "```json" + `
{
	"plan_file": "PLAN.md",
	"confidence": 0.95,
	"tasks": [
		{
			"id": "T-001",
			"title": "Test task",
			"files": ["test.go"],
			"notes": "Test notes"
		}
	],
	"needs_clarification": false,
	"clarification_questions": []
}
` + "```" + `

This should work.`

		result, err := agent.parseOrchestrationResponse(response)
		require.NoError(t, err)

		assert.Equal(t, "PLAN.md", result.PlanFile)
		assert.Equal(t, 0.95, result.Confidence)
		assert.Len(t, result.Tasks, 1)
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		agent := &LLMAgent{}

		response := `This is not valid JSON`

		_, err := agent.parseOrchestrationResponse(response)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse JSON")
	})

	t.Run("InvalidConfidence", func(t *testing.T) {
		agent := &LLMAgent{}

		response := `{
			"plan_file": "PLAN.md",
			"confidence": 1.5,
			"tasks": [],
			"needs_clarification": false,
			"clarification_questions": []
		}`

		_, err := agent.parseOrchestrationResponse(response)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "confidence must be between 0 and 1")
	})

	t.Run("NeedsClarificationWithoutQuestions", func(t *testing.T) {
		agent := &LLMAgent{}

		response := `{
			"plan_file": "PLAN.md",
			"confidence": 0.5,
			"tasks": [],
			"needs_clarification": true,
			"clarification_questions": []
		}`

		_, err := agent.parseOrchestrationResponse(response)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "needs_clarification=true but no clarification_questions provided")
	})
}

func TestContentSummarization(t *testing.T) {
	t.Run("SmallContent", func(t *testing.T) {
		agent := &LLMAgent{}

		content := "This is a short content."
		result := agent.summarizeContentIfNeeded(content, 1000)

		assert.Equal(t, content, result)
	})

	t.Run("LargeContent", func(t *testing.T) {
		agent := &LLMAgent{}

		content := strings.Repeat("This is a long line of content. ", 1000)
		result := agent.summarizeContentIfNeeded(content, 100)

		assert.Less(t, len(result), len(content))
		assert.Contains(t, result, "[... content summarized ...]")
	})
}

func TestArtifactWriting(t *testing.T) {
	t.Run("WriteArtifact", func(t *testing.T) {
		agent := &LLMAgent{
			config: AgentConfig{
				Workspace: "/workspace",
			},
			fsProvider: NewMockFSProvider(),
		}

		result := &OrchestrationResult{
			PlanFile:   "PLAN.md",
			Confidence: 0.95,
			Tasks: []OrchestrationTask{
				{ID: "T-001", Title: "Test task", Files: []string{"test.go"}},
			},
		}

		artifact, err := agent.writeArtifactAtomic("tasks/T-001.plan.json", result)
		require.NoError(t, err)

		assert.Equal(t, "tasks/T-001.plan.json", artifact.Path)
		assert.NotEmpty(t, artifact.SHA256)
		assert.Greater(t, artifact.Size, int64(0))
	})
}
