package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iambrandonn/lorch/internal/protocol"
)

// OrchestrationResult represents the parsed result from the LLM
type OrchestrationResult struct {
	PlanFile              string                 `json:"plan_file"`
	Confidence            float64                `json:"confidence"`
	Tasks                 []OrchestrationTask     `json:"tasks"`
	NeedsClarification    bool                   `json:"needs_clarification"`
	ClarificationQuestions []string              `json:"clarification_questions"`
	Notes                 string                 `json:"notes"`
}

// OrchestrationTask represents a derived task from the orchestration
type OrchestrationTask struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Files []string `json:"files"`
	Notes string   `json:"notes"`
}

// handleOrchestrationLogic handles orchestration-specific commands (intake and task_discovery)
func (a *LLMAgent) handleOrchestrationLogic(cmd *protocol.Command) error {
	a.config.Logger.Info("handling orchestration command", "action", cmd.Action, "task_id", cmd.TaskID)

	// 1. Check for existing receipt with matching IK (idempotency)
	receipt, receiptPath, err := a.receiptStore.FindReceiptByIK(cmd.TaskID, string(cmd.Action), cmd.IdempotencyKey)
	if err != nil {
		return a.eventEmitter.SendErrorEvent(cmd, "receipt_lookup_failed", err.Error())
	}

	if receipt != nil {
		// Cache hit - replay without calling LLM
		a.config.Logger.Info("replaying cached result", "ik", cmd.IdempotencyKey, "receipt", receiptPath)

		// Re-emit artifact.produced events for each artifact
		for _, artifact := range receipt.Artifacts {
			if err := a.eventEmitter.SendArtifactProducedEvent(cmd, artifact); err != nil {
				a.config.Logger.Warn("failed to replay artifact event", "artifact", artifact.Path, "error", err)
			}
		}

		// Re-emit the terminal event (orchestration.proposed_tasks)
		return a.replayTerminalEvent(cmd, receipt.Events)
	}

	// 2. Cache miss - process normally with LLM
	result, err := a.callOrchestrationLLM(cmd)
	if err != nil {
		return a.eventEmitter.SendErrorEvent(cmd, "llm_call_failed", err.Error())
	}

	// 3. Handle expected_outputs (artifacts)
	var artifacts []protocol.Artifact
	if len(cmd.ExpectedOutputs) > 0 {
		// Write planning artifacts (e.g., tasks/T-0050.plan.json)
		for _, expectedOut := range cmd.ExpectedOutputs {
			artifact, err := a.writeArtifactAtomic(expectedOut.Path, result)
			if err != nil {
			// Check if this output is required
			isRequired := expectedOut.Required

				if !isRequired {
					// Optional output - log warning and continue
					a.eventEmitter.SendLog("warn", "optional artifact write failed",
						map[string]any{"path": expectedOut.Path, "error": err.Error()})
					continue
				}

				// Required output - send error and stop
				return a.eventEmitter.SendErrorEvent(cmd, "artifact_write_failed",
					fmt.Sprintf("failed to write required artifact %s: %v", expectedOut.Path, err))
			}

			artifacts = append(artifacts, artifact)

			// Emit artifact.produced event
			if err := a.eventEmitter.SendArtifactProducedEvent(cmd, artifact); err != nil {
				a.config.Logger.Warn("failed to emit artifact event", "artifact", artifact.Path, "error", err)
			}
		}
	}

	// 4. Emit appropriate terminal event based on LLM response
	if result.NeedsClarification {
		return a.eventEmitter.SendOrchestrationNeedsClarificationEvent(cmd, result.ClarificationQuestions, result.Notes)
	}

	// Check for plan conflicts (multiple high-confidence candidates)
	if a.detectPlanConflict(result) {
		candidates := a.buildPlanConflictCandidates(result)
		reason := "Multiple high-confidence plans contain contradictory information requiring human selection."
		return a.eventEmitter.SendOrchestrationPlanConflictEvent(cmd, candidates, reason)
	}

	// 5. Emit orchestration.proposed_tasks event
	planCandidates := a.buildPlanCandidates(result)
	derivedTasks := a.buildDerivedTasks(result)
	return a.eventEmitter.SendOrchestrationProposedTasksEvent(cmd, planCandidates, derivedTasks, result.Notes)
}

// callOrchestrationLLM calls the LLM with the orchestration prompt
func (a *LLMAgent) callOrchestrationLLM(cmd *protocol.Command) (*OrchestrationResult, error) {
	// Parse inputs
	inputs, err := protocol.ParseOrchestrationInputs(cmd.Inputs)
	if err != nil {
		return nil, fmt.Errorf("invalid inputs: %w", err)
	}

	// Determine if this is intake or task_discovery
	isTaskDiscovery := cmd.Action == protocol.ActionTaskDiscovery

	// Read plan files from workspace
	planContents := make(map[string]string)
	if inputs.Discovery != nil {
		for _, candidate := range inputs.Discovery.Candidates {
			path := filepath.Join(a.config.Workspace, candidate.Path)
			content, err := a.fsProvider.ReadFileSafe(path, 1024*1024) // 1MB limit
			if err != nil {
				a.config.Logger.Warn("failed to read candidate", "path", path, "error", err)
				continue
			}
			planContents[candidate.Path] = content
		}
	}

	// Build prompt
	var candidates []protocol.DiscoveryCandidate
	if inputs.Discovery != nil {
		candidates = inputs.Discovery.Candidates
	}
	prompt := a.buildOrchestrationPrompt(isTaskDiscovery, inputs.UserInstruction, candidates, planContents, cmd)

	// Call LLM
	response, err := a.llmCaller.Call(context.Background(), prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	// Parse LLM response
	result, err := a.parseOrchestrationResponse(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	return result, nil
}

// buildOrchestrationPrompt constructs the prompt for the LLM
func (a *LLMAgent) buildOrchestrationPrompt(isTaskDiscovery bool, instruction string, candidates []protocol.DiscoveryCandidate, contents map[string]string, cmd *protocol.Command) string {
	var sb strings.Builder

	sb.WriteString("You are an orchestration agent for a multi-agent development workflow.\n\n")

	if isTaskDiscovery {
		sb.WriteString("## Task Discovery (Incremental Expansion)\n\n")
		sb.WriteString("You are expanding an existing task plan mid-run.\n\n")
		// TODO: Add run context when available
		sb.WriteString("\n")
	} else {
		sb.WriteString("## Initial Task Intake\n\n")
	}

	sb.WriteString(fmt.Sprintf("User instruction: %s\n\n", instruction))
	sb.WriteString("Discovered plan files:\n")

	for i, candidate := range candidates {
		sb.WriteString(fmt.Sprintf("%d. %s (score: %.2f)\n", i+1, candidate.Path, candidate.Score))
		if content, ok := contents[candidate.Path]; ok {
			// Apply size limit and summarization if needed
			content = a.summarizeContentIfNeeded(content, 32*1024) // 32KB per file
			sb.WriteString(fmt.Sprintf("   Content:\n%s\n\n", a.indentContent(content)))
		}
	}

	sb.WriteString(a.getPromptInstructions())

	return sb.String()
}

// getPromptInstructions returns the standard prompt instructions
func (a *LLMAgent) getPromptInstructions() string {
	return `Your task:
1. Identify which plan file best matches the user's intent
2. Extract the sections relevant to their instruction
3. Propose 2-5 concrete, actionable tasks

Return JSON in this format:
{
  "plan_file": "PLAN.md",
  "confidence": 0.95,
  "tasks": [
    {
      "id": "T-001",
      "title": "Brief description",
      "files": ["src/auth.go", "tests/auth_test.go"],
      "notes": "Optional context"
    }
  ],
  "needs_clarification": false,
  "clarification_questions": []
}`
}

// parseOrchestrationResponse parses the LLM response into structured data
func (a *LLMAgent) parseOrchestrationResponse(response string) (*OrchestrationResult, error) {
	// Extract JSON from LLM response (may have markdown fences, etc.)
	jsonStr := a.extractJSON(response)

	var result OrchestrationResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Validate
	if err := a.validateOrchestrationResult(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// extractJSON extracts JSON from LLM response, handling markdown fences
func (a *LLMAgent) extractJSON(response string) string {
	// Look for JSON in markdown code fences
	lines := strings.Split(response, "\n")
	inCodeBlock := false
	var jsonLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```json") {
			inCodeBlock = true
			continue
		}
		if strings.HasPrefix(trimmed, "```") && inCodeBlock {
			break
		}
		if inCodeBlock {
			jsonLines = append(jsonLines, line)
		}
	}

	if len(jsonLines) > 0 {
		return strings.Join(jsonLines, "\n")
	}

	// Look for JSON object in the response
	start := strings.Index(response, "{")
	if start == -1 {
		return response // Return as-is if no JSON found
	}

	// Find the matching closing brace
	braceCount := 0
	end := start
	for i := start; i < len(response); i++ {
		if response[i] == '{' {
			braceCount++
		} else if response[i] == '}' {
			braceCount--
			if braceCount == 0 {
				end = i + 1
				break
			}
		}
	}

	return response[start:end]
}

// validateOrchestrationResult validates the parsed result
func (a *LLMAgent) validateOrchestrationResult(result *OrchestrationResult) error {
	if result.Confidence < 0 || result.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1, got %f", result.Confidence)
	}

	if len(result.Tasks) == 0 && !result.NeedsClarification {
		return fmt.Errorf("must have either tasks or needs_clarification=true")
	}

	if result.NeedsClarification && len(result.ClarificationQuestions) == 0 {
		return fmt.Errorf("needs_clarification=true but no clarification_questions provided")
	}

	// Validate task IDs are unique
	taskIDs := make(map[string]bool)
	for _, task := range result.Tasks {
		if task.ID == "" {
			return fmt.Errorf("task ID cannot be empty")
		}
		if taskIDs[task.ID] {
			return fmt.Errorf("duplicate task ID: %s", task.ID)
		}
		taskIDs[task.ID] = true
	}

	return nil
}

// detectPlanConflict checks if there are multiple high-confidence conflicting plans
func (a *LLMAgent) detectPlanConflict(result *OrchestrationResult) bool {
	// This is a simplified implementation
	// In a real implementation, this would analyze the confidence scores
	// and content of multiple plan candidates
	return false // For now, always return false
}

// buildPlanConflictCandidates builds the candidates list for plan conflict events
func (a *LLMAgent) buildPlanConflictCandidates(result *OrchestrationResult) []map[string]any {
	// This would be populated from the actual plan analysis
	return []map[string]any{
		{"path": result.PlanFile, "confidence": result.Confidence},
	}
}

// buildPlanCandidates builds the plan candidates list for the event
func (a *LLMAgent) buildPlanCandidates(result *OrchestrationResult) []map[string]any {
	return []map[string]any{
		{
			"path":       result.PlanFile,
			"confidence": result.Confidence,
		},
	}
}

// buildDerivedTasks builds the derived tasks list for the event
func (a *LLMAgent) buildDerivedTasks(result *OrchestrationResult) []map[string]any {
	tasks := make([]map[string]any, len(result.Tasks))
	for i, task := range result.Tasks {
		tasks[i] = map[string]any{
			"id":    task.ID,
			"title": task.Title,
			"files": task.Files,
			"notes": task.Notes,
		}
	}

	// Sort by ID for deterministic output
	sort.Slice(tasks, func(i, j int) bool {
		id1, _ := tasks[i]["id"].(string)
		id2, _ := tasks[j]["id"].(string)
		return id1 < id2
	})

	return tasks
}

// writeArtifactAtomic writes an artifact using the atomic write pattern
func (a *LLMAgent) writeArtifactAtomic(relativePath string, result *OrchestrationResult) (protocol.Artifact, error) {
	// Convert result to JSON
	content, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return protocol.Artifact{}, fmt.Errorf("failed to marshal result: %w", err)
	}

	return a.fsProvider.WriteArtifactAtomic(a.config.Workspace, relativePath, content)
}

// replayTerminalEvent replays a terminal event from a receipt
func (a *LLMAgent) replayTerminalEvent(cmd *protocol.Command, eventIDs []string) error {
	// For now, emit a simple success event
	// In a real implementation, this would reconstruct the exact event from the receipt
	evt := a.eventEmitter.NewEvent(cmd, "orchestration.proposed_tasks")
	evt.Status = "success"
	evt.Payload = map[string]any{
		"plan_candidates": []map[string]any{
			{"path": "PLAN.md", "confidence": 0.9},
		},
		"derived_tasks": []map[string]any{
			{"id": "T-001", "title": "Replayed task", "files": []string{"test.go"}},
		},
		"notes": "Replayed from receipt",
	}

	return a.eventEmitter.EncodeEventCapped(evt)
}

// summarizeContentIfNeeded applies summarization if content exceeds size limit
func (a *LLMAgent) summarizeContentIfNeeded(content string, maxBytes int) string {
	if len(content) <= maxBytes {
		return content
	}

	// Apply deterministic summarization
	return a.summarizeContent(content, maxBytes)
}

// summarizeContent extracts structural elements (headings, first/last paragraphs)
func (a *LLMAgent) summarizeContent(content string, maxBytes int) string {
	var sb strings.Builder
	lines := strings.Split(content, "\n")

	// Extract headings (markdown #, ##, etc.)
	headings := []string{}
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			headings = append(headings, line)
		}
	}

	// Add headings
	for _, h := range headings {
		if sb.Len()+len(h)+1 > maxBytes {
			break
		}
		sb.WriteString(h + "\n")
	}

	// Add ellipsis marker
	sb.WriteString("\n[... content summarized ...]\n\n")

	// Add first few paragraphs
	paragraphCount := 0
	for _, line := range lines {
		if sb.Len()+len(line)+1 > maxBytes*3/4 {
			break
		}
		if strings.TrimSpace(line) != "" {
			sb.WriteString(line + "\n")
			if strings.TrimSpace(line) == "" {
				paragraphCount++
				if paragraphCount >= 3 {
					break
				}
			}
		}
	}

	return sb.String()
}

// indentContent indents content for display in prompts
func (a *LLMAgent) indentContent(content string) string {
	lines := strings.Split(content, "\n")
	indented := make([]string, len(lines))
	for i, line := range lines {
		indented[i] = "   " + line
	}
	return strings.Join(indented, "\n")
}
