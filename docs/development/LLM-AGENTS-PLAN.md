# LLM Agent Implementation Plan

**Status**: Updated after Review 1 - Ready for implementation
**Author**: Generated from implementation discussion
**Date**: 2025-10-22 (Updated 2025-10-23 per review)
**Purpose**: Design and implementation plan for building real LLM agents in Go
**Review Status**: Incorporates feedback from `llm-agents-plan-review-1.md`

---

## Executive Summary

This document outlines the plan to build `cmd/llm-agent` - a Go binary that speaks the NDJSON protocol and calls external LLM CLI tools (like `claude`, `codex`, etc.). The first implementation will focus on the **orchestration agent**, which translates natural language instructions into concrete task plans.

---

## Background & Motivation

### Current State
- ✅ `cmd/mockagent` - Full NDJSON agent for testing with canned responses
- ✅ `cmd/claude-agent` - Shim wrapper that sets env vars and passes through to underlying binary
- ✅ Complete orchestrator infrastructure (protocol, discovery, scheduling)
- ❌ **Missing**: Actual agents that translate NDJSON ↔ LLM calls

### The Problem
When users run lorch with natural language prompts, the orchestration agent needs to:
1. Parse the user's intent ("Implement authentication from PLAN.md")
2. Read relevant plan files from the workspace
3. Understand the plan content
4. Propose specific, actionable tasks

Mockagent returns canned responses and can't actually understand prompts or read files intelligently.

### The Solution
Build real agents that:
- Maintain the NDJSON protocol interface (spec compliance)
- Call external LLM CLI tools for intelligence
- Access the filesystem as "shared memory" (per spec)
- Support any LLM via pluggable CLI interface

---

## Architecture Design

### High-Level Flow

```
lorch → NDJSON command → llm-agent → read files → call LLM CLI → parse response → NDJSON event → lorch
```

### Directory Structure

```
cmd/llm-agent/
├── main.go           # Entry point, CLI flags, agent initialization
├── agent.go          # Core agent with NDJSON I/O loop
├── llm.go            # LLM CLI caller (external process management)
├── orchestration.go  # Orchestration-specific logic
├── builder.go        # Builder-specific logic (future)
├── reviewer.go       # Reviewer-specific logic (future)
└── spec.go           # Spec-maintainer logic (future)
```

### Key Design Principles

1. **NDJSON Protocol Compliance**
   - Uses existing `internal/protocol` and `internal/ndjson` packages
   - Follows same patterns as mockagent
   - Speaks identical wire format

2. **External LLM CLI Interface**
   - Calls configurable CLI tool via stdin/stdout
   - No direct API dependencies (Go code stays generic)
   - Supports: `claude`, `codex`, custom scripts, etc.

3. **Filesystem as Shared Memory**
   - Agents read workspace files directly (MASTER-SPEC.md line 3)
   - Builder/spec-maintainer write files using atomic writes (spec §14.4)
   - Orchestration must not edit plan/spec content, but may produce planning artifacts when requested via `expected_outputs`, using the safe atomic write pattern (§14.4) and emitting `artifact.produced` (spec §2.2, §3.1)

4. **Role-Based Behavior**
   - Single binary with `--role` flag
   - Switch logic based on action type
   - Future-proof for all 4 agent roles

---

## Critical Spec Compliance Requirements

These requirements are **mandatory** for spec compliance (per MASTER-SPEC.md and review feedback):

### 1. Idempotency & Deterministic Replays (Spec §5.4)

**Requirement**: Agents must treat repeated commands with the same `idempotency_key` (IK) as "already handled."

**Implementation**:
- Persist results as receipts: `/receipts/<task_id>/<action>-<step>.json` (per spec §16.1)
- Receipt contains: `task_id`, `step`, `idempotency_key`, `artifacts` array, `events` (message IDs), `created_at`
- On repeated IK: Agent **scans** `/receipts/<task_id>/` directory for any receipt matching the IK (not tied to attempt number)
- **Storage location**: Inside workspace at `/receipts/` (visible to lorch for verification)
- **Lifecycle**: Receipts persist across runs; lorch uses them for resumability (spec §5.6)
- **Lookup**: **Critical** - Must scan directory, not construct path from attempt number, since retries reuse same IK but have different attempt numbers

**Example Flow**:
```go
// Helper function to find receipt by IK (scans directory)
func (a *LLMAgent) findReceiptByIK(taskID, action, ik string) (*Receipt, string, error) {
    dir := filepath.Join(a.workspace, "receipts", taskID)
    entries, err := os.ReadDir(dir)
    if err != nil {
        if errors.Is(err, os.ErrNotExist) {
            return nil, "", nil // No receipts yet
        }
        return nil, "", err
    }

    for _, e := range entries {
        if e.IsDir() {
            continue
        }

        // Only consider receipts for this action
        if !strings.HasPrefix(e.Name(), string(action)+"-") {
            continue
        }

        rp := filepath.Join(dir, e.Name())
        r, err := a.loadReceipt(rp)
        if err == nil && r.IdempotencyKey == ik {
            return r, rp, nil
        }
    }

    return nil, "", nil // No matching receipt
}

func (a *LLMAgent) handleOrchestration(cmd *protocol.Command) error {
    // 1. Check for existing receipt with matching IK (scan directory)
    receipt, receiptPath, err := a.findReceiptByIK(cmd.TaskID, string(cmd.Action), cmd.IdempotencyKey)
    if err != nil {
        return a.sendErrorEvent(cmd, "receipt_lookup_failed", err.Error())
    }

    if receipt != nil {
        // Cache hit - replay without calling LLM
        a.logger.Info("replaying cached result", "ik", cmd.IdempotencyKey, "receipt", receiptPath)

        // Re-emit artifact.produced events for each artifact
        for _, artifact := range receipt.Artifacts {
            a.sendArtifactProducedEvent(cmd, artifact)
        }

        // Re-emit the terminal event (orchestration.proposed_tasks)
        return a.replayTerminalEvent(cmd, receipt.Events)
    }

    // 2. Cache miss - process normally with LLM
    result, err := a.callOrchestrationLLM(cmd)
    if err != nil {
        return a.sendErrorEvent(cmd, "llm_call_failed", err.Error())
    }

    // 3. Check if expected_outputs are specified
    var artifacts []protocol.Artifact
    if len(cmd.ExpectedOutputs) > 0 {
        // Write planning artifacts (e.g., tasks/T-0050.plan.json)
        for _, expectedOut := range cmd.ExpectedOutputs {
            artifact, err := a.writeArtifactAtomic(expectedOut.Path, result.PlanningData)
            if err != nil {
                // Check if this output is required (default: true)
                isRequired := true
                if expectedOut.Required != nil {
                    isRequired = *expectedOut.Required
                }

                if !isRequired {
                    // Optional output - log warning and continue
                    a.sendLog("warn", "optional artifact write failed",
                        map[string]any{"path": expectedOut.Path, "error": err.Error()})
                    continue
                }

                // Required output - send error and stop
                return a.sendErrorEvent(cmd, "artifact_write_failed",
                    fmt.Sprintf("failed to write required artifact %s: %v", expectedOut.Path, err))
            }

            artifacts = append(artifacts, artifact)

            // Emit artifact.produced event
            a.sendArtifactProducedEvent(cmd, artifact)
        }
    }

    // 4. Emit orchestration.proposed_tasks event
    evt := a.buildProposedTasksEvent(cmd, result)
    if err := a.encoder.Encode(evt); err != nil {
        return err
    }

    // 5. Write receipt for idempotency
    // Determine step number by counting existing receipts for this action
    stepNum, err := a.getNextStepNumber(cmd.TaskID, string(cmd.Action))
    if err != nil {
        a.logger.Warn("failed to determine step number", "error", err)
        stepNum = 1
    }

    newReceipt := Receipt{
        TaskID:          cmd.TaskID,
        Step:            stepNum,
        IdempotencyKey:  cmd.IdempotencyKey,
        Artifacts:       artifacts,
        Events:          []string{evt.MessageID},
        CreatedAt:       time.Now().UTC(),
    }

    receiptDir := filepath.Join(a.workspace, "receipts", cmd.TaskID)
    if err := os.MkdirAll(receiptDir, 0700); err != nil {
        return fmt.Errorf("failed to create receipt directory: %w", err)
    }

    newReceiptPath := filepath.Join(receiptDir, fmt.Sprintf("%s-%d.json", cmd.Action, stepNum))
    return a.saveReceipt(newReceiptPath, newReceipt)
}
```

**Receipt Structure** (per spec §16.1):
```go
type Receipt struct {
    TaskID         string              `json:"task_id"`
    Step           int                 `json:"step"`
    IdempotencyKey string              `json:"idempotency_key"`
    Artifacts      []protocol.Artifact `json:"artifacts"`
    Events         []string            `json:"events"` // Message IDs
    CreatedAt      time.Time           `json:"created_at"`
}
```

### 2. Observed Version Echo (Spec §3.2, §6.2)

**Requirement**: All events must include `observed_version.snapshot_id` echoing the command's version.

**Implementation**:
```go
evt := protocol.Event{
    // ... other fields
    ObservedVersion: &protocol.Version{
        SnapshotID: cmd.Version.SnapshotID, // Echo from command
    },
}
```

**Version Mismatch Handling** (Spec §6.2):
- Per spec: "Agents must error with `version_mismatch` if snapshots differ"
- **Implementation**: Agent records the first `version.snapshot_id` seen during its run
- For each subsequent command, if `cmd.Version.SnapshotID != firstObservedSnapshotID`, emit `error` event with `version_mismatch`
- This detects snapshot changes **within a single agent run** (e.g., if lorch sends commands from different snapshots)
- Agent always echoes `observed_version.snapshot_id` in all events

**Code Example**:
```go
type LLMAgent struct {
    // ... other fields
    firstObservedSnapshotID string // Set on first command
    mu                      sync.Mutex
}

func (a *LLMAgent) handleCommand(cmd *protocol.Command) error {
    // Version mismatch detection
    a.mu.Lock()
    if a.firstObservedSnapshotID == "" {
        a.firstObservedSnapshotID = cmd.Version.SnapshotID
        a.logger.Info("recorded initial snapshot", "snapshot_id", a.firstObservedSnapshotID)
    } else if cmd.Version.SnapshotID != a.firstObservedSnapshotID {
        a.mu.Unlock()
        return a.sendErrorEvent(cmd, "version_mismatch",
            fmt.Sprintf("expected snapshot %s, received %s", a.firstObservedSnapshotID, cmd.Version.SnapshotID))
    }
    a.mu.Unlock()

    // ... route to action handler
}
```

### 3. Heartbeat Lifecycle (Spec §3.3, §7.1)

**Requirement**: Maintain heartbeat with correct status transitions and all required fields.

**Status Transitions**:
```
starting → ready → busy (during command) → ready → stopping (on shutdown)
```

**Required Fields**:
- `seq`: Monotonic sequence number
- `status`: One of `starting`, `ready`, `busy`, `stopping`, `backoff`
- `pid`, `ppid`: Process identifiers
- `uptime_s`: Time since agent start
- `last_activity_at`: RFC3339 timestamp
- `task_id`: Current task (if applicable)
- `stats`: Optional CPU/memory stats

**Critical**: Heartbeats must continue during long LLM calls to show liveness.

**Architecture**: Use a single background goroutine that sends heartbeats at regular intervals (10s). The agent updates an atomic `lastActivityAt` timestamp whenever it does work. The heartbeat goroutine reads this timestamp and sends heartbeat messages with current status.

**Implementation**:
```go
// Background heartbeat loop (started once at agent startup)
func (a *LLMAgent) heartbeatLoop(ctx context.Context) {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            a.mu.Lock()
            status := a.currentStatus
            taskID := a.currentTaskID
            a.mu.Unlock()

            a.sendHeartbeat(status, taskID)
        case <-ctx.Done():
            return
        }
    }
}

// Update activity timestamp (called before/during long operations)
func (a *LLMAgent) updateActivity() {
    a.mu.Lock()
    a.lastActivityAt = time.Now()
    a.mu.Unlock()
}

// Set status (called when transitioning between command states)
func (a *LLMAgent) setStatus(status protocol.HeartbeatStatus, taskID string) {
    a.mu.Lock()
    a.currentStatus = status
    a.currentTaskID = taskID
    a.lastActivityAt = time.Now()
    a.mu.Unlock()
}

// When calling LLM, just update activity periodically
func (a *LLMAgent) callLLM(ctx context.Context, prompt string) (string, error) {
    a.setStatus(protocol.HeartbeatStatusBusy, a.currentTaskID)
    defer a.setStatus(protocol.HeartbeatStatusReady, "")

    // Periodically update activity while LLM is working
    activityTicker := time.NewTicker(5 * time.Second)
    defer activityTicker.Stop()

    done := make(chan struct{})
    go func() {
        for {
            select {
            case <-activityTicker.C:
                a.updateActivity()
            case <-done:
                return
            }
        }
    }()

    result, err := a.executeLLMCall(ctx, prompt)
    close(done)
    return result, err
}
```

**Rationale**: Single background loop is simpler and ensures consistent heartbeat intervals regardless of what the agent is doing. Status and activity are updated via synchronized state, not by sending heartbeats from multiple places.

**Heartbeat Emission** (Spec §3.3):
```go
func (a *LLMAgent) sendHeartbeat(status protocol.HeartbeatStatus, taskID string) error {
    a.mu.Lock()
    a.hbSeq++
    seq := a.hbSeq
    a.mu.Unlock()

    hb := protocol.Heartbeat{
        Kind: "heartbeat",
        Agent: protocol.AgentRef{
            AgentType: a.agentType,
            AgentID:   a.agentID,
        },
        Seq:            seq,
        Status:         string(status),
        Pid:            os.Getpid(),
        Ppid:           os.Getppid(),
        UptimeS:        time.Since(a.startTime).Seconds(),
        LastActivityAt: a.lastActivityAt.Format(time.RFC3339),
        TaskID:         taskID,
        // Stats optional - can add CPU/memory monitoring
    }
    return a.encoder.Encode(hb)
}
```

**Note**: Heartbeat uses `kind: "heartbeat"` envelope (not `event`), per spec §3.3.

### 3.1. Event Builder (Ensures Completeness)

**Problem**: Events must consistently include `message_id`, `occurred_at`, and `observed_version.snapshot_id`.

**Solution**: Standardized builder that all event constructors use.

**Implementation** (Spec §3.2):
```go
// newEvent creates a base event with all required fields
func (a *LLMAgent) newEvent(cmd *protocol.Command, eventName string) protocol.Event {
    return protocol.Event{
        Kind:          protocol.MessageKindEvent,
        MessageID:     uuid.New().String(),
        CorrelationID: cmd.CorrelationID,
        TaskID:        cmd.TaskID,
        From: protocol.AgentRef{
            AgentType: a.agentType,
            AgentID:   a.agentID,
        },
        Event: eventName,
        ObservedVersion: &protocol.Version{
            SnapshotID: cmd.Version.SnapshotID,
        },
        OccurredAt: time.Now().UTC(),
    }
}

// Usage in event constructors:
// evt := a.newEvent(cmd, "orchestration.proposed_tasks")
// evt.Status = "success"
// evt.Payload = map[string]any{...}
// return a.encoder.Encode(evt)
```

**Benefit**: Ensures no event is emitted without required fields; single source of truth for event structure.

### 4. Structured Error Events (Spec §3.4)

**Requirement**: Use `event` with `event:"error"` and machine-readable `payload.code`.

**Error Codes**:
- `invalid_inputs`: Command inputs failed validation
- `llm_call_failed`: LLM CLI subprocess failed
- `invalid_llm_response`: Could not parse LLM output
- `version_mismatch`: Snapshot mismatch detected
- `file_read_failed`: Could not read workspace file
- `artifact_write_failed`: Could not write artifact
- `receipt_lookup_failed`: Failed to scan or load receipt

**Implementation**:
```go
func (a *LLMAgent) sendErrorEvent(cmd *protocol.Command, code, message string) error {
    evt := a.newEvent(cmd, "error")
    evt.Status = "failed"
    evt.Payload = map[string]any{
        "code":    code,
        "message": message,
    }
    return a.encoder.Encode(evt)
}
```

### 4.1. Orchestration-Specific Events

The orchestration agent emits three terminal event types (per spec §3.2):

**4.1.1. orchestration.proposed_tasks** (Normal Case)
Emitted when agent successfully derives tasks from user instruction and plan files.

**4.1.2. orchestration.needs_clarification**
Emitted when user instruction is ambiguous or unclear. Includes questions for the user.

**Payload Structure**:
- `questions`: Array of strings (questions for user)
- `notes`: String explaining why clarification is needed

**Implementation**:
```go
func (a *LLMAgent) sendNeedsClarificationEvent(cmd *protocol.Command, questions []string, notes string) error {
    evt := a.newEvent(cmd, "orchestration.needs_clarification")
    evt.Status = "needs_input"
    evt.Payload = map[string]any{
        "questions": questions,
        "notes":     notes,
    }
    return a.encoder.Encode(evt)
}
```

**Example JSON Event**:
```json
{
  "kind": "event",
  "event": "orchestration.needs_clarification",
  "status": "needs_input",
  "payload": {
    "questions": [
      "Which plan file should be used (PLAN.md vs docs/plan_v2.md)?",
      "Should we implement phases A and B together or separately?"
    ],
    "notes": "Ambiguous instruction; multiple plausible interpretations"
  }
}
```

**4.1.3. orchestration.plan_conflict** (Spec §3.2)
Emitted when multiple high-confidence plan candidates contain contradictory information requiring human selection.

**Conflict Detection Criteria**:
- Multiple candidates with confidence > 0.75
- Plans contain contradictory scopes, requirements, or architectures
- Cannot safely merge or select programmatically

**Implementation Example**:
```go
func (a *LLMAgent) sendPlanConflictEvent(cmd *protocol.Command, candidates []PlanCandidate, reason string) error {
    evt := a.newEvent(cmd, "orchestration.plan_conflict")
    evt.Status = "needs_input"
    evt.Payload = map[string]any{
        "candidates": candidates,
        "reason":     reason,
    }
    return a.encoder.Encode(evt)
}
```

**Example JSON Event**:
```json
{
  "kind": "event",
  "event": "orchestration.plan_conflict",
  "payload": {
    "candidates": [
      {"path": "PLAN.md", "confidence": 0.81},
      {"path": "docs/plan_v2.md", "confidence": 0.80}
    ],
    "reason": "Two high-confidence plans diverge in scope; human selection required."
  }
}
```

**4.1.4. Important: system.user_decision is lorch-only**

The orchestration agent **NEVER** emits `system.user_decision` events. Per spec §3.2, only `lorch` (the orchestrator) emits this event to record human approval/denial decisions.

Orchestration role:
- ✅ Emit `orchestration.proposed_tasks`, `orchestration.needs_clarification`, or `orchestration.plan_conflict`
- ❌ Never emit `system.user_decision` (that's lorch's responsibility)

### 5. Message Size Limits (Spec §3, §12)

**Requirement**: Keep NDJSON messages under 256 KiB. Reference large content via artifacts.

**Strategy for Orchestration**:
- **Don't** embed full plan file contents in events
- **Do** read files to build LLM prompt
- **Do** write planning artifacts to disk if needed
- **Do** reference artifacts by path in events

### 5.1. Prompt Size Budget (Deterministic Summarization)

**Problem**: Unbounded prompt construction can exceed LLM context limits or cause performance issues.

**Solution**: Implement total prompt size budget with per-file caps and deterministic summarization.

**Defaults** (configurable via env vars):
- Total prompt budget: 256 KiB
- Per-file content limit: 32 KiB
- Summarization strategy: Structural extraction (headings, first/last paragraphs)

**Implementation**:
```go
const (
    maxPromptBytesDefault = 256 * 1024 // 256 KiB total budget
    maxPerFileBytes       = 32 * 1024  // 32 KiB per file
)

func (a *LLMAgent) buildContextForLLM(candidates []protocol.DiscoveryCandidate) (string, error) {
    var sb strings.Builder

    // Get prompt budget from config or use default
    budget := a.cfg.MaxPromptBytes
    if budget == 0 {
        budget = maxPromptBytesDefault
    }

    budgetUsed := 0

    for _, candidate := range candidates {
        // Read file with size limit
        content, err := readFileSafe(candidate.Path, maxPerFileBytes)
        if err != nil {
            a.logger.Warn("failed to read candidate", "path", candidate.Path, "error", err)
            continue
        }

        // Apply deterministic summarization if content exceeds per-file limit
        if len(content) > maxPerFileBytes {
            content = summarizeContent(content, maxPerFileBytes)
            a.logger.Info("summarized large file", "path", candidate.Path,
                "original_bytes", len(content), "summarized_bytes", maxPerFileBytes)
        }

        // Check if adding this file would exceed total budget
        if budgetUsed+len(content) > budget {
            remaining := budget - budgetUsed
            if remaining > 1024 { // Only include if at least 1KB remains
                content = content[:remaining]
                a.logger.Warn("truncated file due to budget", "path", candidate.Path,
                    "budget", budget, "used", budgetUsed, "remaining", remaining)
            } else {
                a.logger.Warn("skipped file due to budget exhaustion", "path", candidate.Path)
                break
            }
        }

        sb.WriteString(fmt.Sprintf("File: %s\n%s\n\n", candidate.Path, content))
        budgetUsed += len(content)
    }

    a.logger.Info("prompt context built", "total_bytes", budgetUsed, "budget", budget,
        "files_included", len(candidates))

    return sb.String(), nil
}

// summarizeContent extracts structural elements (headings, first/last paragraphs)
// Deterministic: same input always produces same output
func summarizeContent(content string, maxBytes int) string {
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
```

**Configuration** (via environment variables):
- `LLM_PROMPT_MAX_BYTES`: Total prompt budget (default: 262144)
- `LLM_PROMPT_MAX_PER_FILE`: Per-file limit (default: 32768)

### 6. Artifact Production (Spec §3.1, §5.5)

**Requirement**: When `expected_outputs` are provided, write artifacts using atomic pattern.

**For Orchestration**:
- If command specifies `expected_outputs` (e.g., `tasks/T-0050.plan.json`)
- Write planning artifacts using safe atomic write (§14.4)
- Emit `artifact.produced` with `{path, sha256, size}`

**Implementation**:
```go
func (a *LLMAgent) writeArtifactAtomic(relativePath string, content []byte) (protocol.Artifact, error) {
    // Security: validate path with symlink-safe resolution
    fullPath, err := resolveWorkspacePath(a.workspace, relativePath)
    if err != nil {
        return protocol.Artifact{}, fmt.Errorf("invalid artifact path: %w", err)
    }

    // Atomic write pattern per spec §14.4
    dir := filepath.Dir(fullPath)
    if err := os.MkdirAll(dir, 0700); err != nil { // 0700 per spec §13
        return protocol.Artifact{}, err
    }

    tmpFile := fmt.Sprintf("%s/.%s.tmp.%d.%s",
        dir, filepath.Base(fullPath), os.Getpid(), uuid.New().String()[:8])

    if err := os.WriteFile(tmpFile, content, 0600); err != nil { // 0600 per spec §13
        return protocol.Artifact{}, err
    }

    // Sync temp file
    f, err := os.Open(tmpFile)
    if err != nil {
        os.Remove(tmpFile)
        return protocol.Artifact{}, err
    }
    f.Sync()
    f.Close()

    // Atomic rename
    if err := os.Rename(tmpFile, fullPath); err != nil {
        os.Remove(tmpFile)
        return protocol.Artifact{}, err
    }

    // Sync directory
    d, err := os.Open(dir)
    if err != nil {
        return protocol.Artifact{}, err
    }
    d.Sync()
    d.Close()

    // Compute checksum
    fileData, err := os.ReadFile(fullPath)
    if err != nil {
        return protocol.Artifact{}, err
    }
    hash := sha256.Sum256(fileData)

    return protocol.Artifact{
        Path:   relativePath, // Return relative path in artifact
        SHA256: fmt.Sprintf("sha256:%x", hash),
        Size:   int64(len(fileData)),
    }, nil
}
```

### 7. Timeouts & Defaults (Spec §7.1, §18)

**Defaults**:
- `intake`: 180s
- `task_discovery`: 180s
- Heartbeat interval: 10s (tolerate 3 misses)

**Implementation**: Use context deadlines from command.Deadline field.

---

## Orchestration Actions: intake vs task_discovery

The orchestration agent supports two distinct actions (per spec §3.1 and internal/protocol/types.go):

### `intake` Action
**Purpose**: Translate an initial natural-language instruction into concrete plan candidates and derived task objects.

**Use case**: User starts with no explicit tasks, provides instruction like "Manage PLAN.md implementation"

**Inputs**:
- `user_instruction`: Natural language string
- `discovery`: Plan file candidates with scores

**Outputs**:
- `orchestration.proposed_tasks` with plan_candidates and derived_tasks (normal case)
- OR `orchestration.needs_clarification` if ambiguous/unclear user intent
- OR `orchestration.plan_conflict` if multiple high-confidence plans conflict

**Example**: "I've got a PLAN.md. Manage the implementation of it" → agent discovers PLAN.md, extracts tasks T-001, T-002, etc.

**Conflict Detection**: If multiple plan files have similar confidence scores (e.g., both > 0.75) but contain contradictory sections or scopes, emit `plan_conflict` to require human selection.

### `task_discovery` Action
**Purpose**: Perform incremental task expansion mid-run, leveraging existing context (approved plans, current task state) to suggest next actions.

**Use case**: During an active run, lorch needs additional tasks beyond the initial set

**Inputs**:
- `discovery`: Updated file context
- `context`: Current task status, approved plans from earlier in run

**Outputs**:
- `orchestration.proposed_tasks` with additional tasks based on current state

**Example**: After completing T-001, system discovers new requirements in SPEC.md updates → suggests T-003, T-004

**Key Difference**: `intake` is first-time planning; `task_discovery` is incremental expansion with run context.

**Implementation Note**: Both actions share similar LLM calling logic but differ in prompt construction. `intake` starts fresh; `task_discovery` includes run history.

---

## Phase 1: Orchestration Agent

### Responsibilities

Per MASTER-SPEC.md §2.2:
> **Orchestration (NL)** — derives a concrete task plan from user instructions; emits `orchestration.proposed_tasks` and `orchestration.needs_clarification`. *Never edits plan/spec files.*

### Input (Command)

```json
{
  "kind": "command",
  "action": "intake",
  "inputs": {
    "user_instruction": "Implement authentication from PLAN.md",
    "discovery": {
      "root": "/workspace",
      "strategy": "heuristic:v1",
      "candidates": [
        {"path": "PLAN.md", "score": 0.9, "reason": "filename contains 'plan'"},
        {"path": "docs/spec.md", "score": 0.6}
      ]
    }
  }
}
```

### Processing Steps

1. **Parse Inputs**
   - Extract `user_instruction` and `discovery` from command.Inputs
   - Validate using `protocol.ParseOrchestrationInputs()`

2. **Read Plan Files**
   - For each discovery candidate (sorted by score):
     - Read file from workspace: `filepath.Join(workspace, candidate.Path)`
     - Extract headings and structure
     - Limit read size (safety)

3. **Build LLM Prompt**
   ```
   You are an orchestration agent for a multi-agent development workflow.

   User instruction: {user_instruction}

   Discovered plan files:
   1. PLAN.md (score: 0.9) - filename contains 'plan'
      Content:
      {file contents}

   2. docs/spec.md (score: 0.6)
      Content:
      {file contents}

   Your task:
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
   }
   ```

4. **Call LLM CLI**
   ```go
   response, err := callLLM(prompt, llmCLIPath)
   ```

5. **Parse LLM Response**
   - Extract JSON from LLM output
   - Validate structure
   - Map to protocol types

6. **Emit Event**
   ```json
   {
     "kind": "event",
     "event": "orchestration.proposed_tasks",
     "payload": {
       "plan_candidates": [...],
       "derived_tasks": [...]
     }
   }
   ```

### Output (Event)

See MASTER-SPEC.md §3.2 example (lines 259-262).

---

## Implementation Tasks

### Task 1: Core Agent Scaffold

**Goal**: Create basic llm-agent binary with NDJSON I/O

**Files**:
- `cmd/llm-agent/main.go`
- `cmd/llm-agent/agent.go`

**Implementation**:
```go
// main.go
func main() {
    var (
        role         = flag.String("role", "", "Agent role (builder, reviewer, spec_maintainer, orchestration)")
        llmCLI       = flag.String("llm-cli", "claude", "LLM CLI command (claude, codex, etc.)")
        workspace    = flag.String("workspace", ".", "Workspace root")
        logLevel     = flag.String("log-level", "info", "Log level")
    )
    flag.Parse()

    // Create agent
    agent := NewLLMAgent(*role, *llmCLI, *workspace, logger)

    // Run NDJSON I/O loop
    if err := agent.Run(ctx, os.Stdin, os.Stdout, os.Stderr); err != nil {
        logger.Error("agent failed", "error", err)
        os.Exit(1)
    }
}
```

**Features**:
- CLI flag parsing
- Logger setup (stderr)
- Context with signal handling
- Basic error handling

**Testing**:
- Unit test for flag parsing
- Integration test with echo stdin/stdout

---

### Task 2: LLM CLI Caller

**Goal**: Function to call external LLM CLI and capture response

**File**: `cmd/llm-agent/llm.go`

**Implementation**:
```go
// LLMConfig holds LLM CLI configuration
type LLMConfig struct {
    CLIPath string        // Path to LLM CLI (e.g., "claude", "codex")
    Timeout time.Duration // Command timeout
    MaxOutputBytes int    // Safety limit
}

// callLLM sends a prompt to the LLM CLI and returns the response
func callLLM(ctx context.Context, cfg LLMConfig, prompt string) (string, error) {
    // Create command
    cmd := exec.CommandContext(ctx, cfg.CLIPath)

    // Set up pipes
    stdin, _ := cmd.StdinPipe()
    stdout, _ := cmd.StdoutPipe()
    stderr, _ := cmd.StderrPipe()

    // Start process
    if err := cmd.Start(); err != nil {
        return "", fmt.Errorf("failed to start LLM CLI: %w", err)
    }

    // Write prompt to stdin
    go func() {
        defer stdin.Close()
        io.WriteString(stdin, prompt)
    }()

    // Read stdout with size limit
    var output bytes.Buffer
    limited := io.LimitReader(stdout, int64(cfg.MaxOutputBytes))
    if _, err := io.Copy(&output, limited); err != nil {
        return "", fmt.Errorf("failed to read LLM output: %w", err)
    }

    // Capture stderr for diagnostics (log, don't fail)
    go io.Copy(os.Stderr, stderr)

    // Wait for completion
    if err := cmd.Wait(); err != nil {
        return "", fmt.Errorf("LLM CLI failed: %w", err)
    }

    return output.String(), nil
}
```

**Features**:
- Context-aware (respects timeouts)
- Size limits for safety
- Stderr passthrough for diagnostics
- Error handling with context

**Testing**:
- Mock LLM CLI (bash script that echoes input)
- Timeout test
- Size limit test
- Error propagation test

---

### Task 3: Orchestration Logic

**Goal**: Implement `handleOrchestration()` supporting both `intake` and `task_discovery` actions

**File**: `cmd/llm-agent/orchestration.go`

**Note**: Both actions share core logic; primary difference is prompt construction (intake = fresh start, task_discovery = includes run context)

**Implementation**:
```go
func (a *LLMAgent) handleOrchestration(cmd *protocol.Command) error {
    // 1. Parse inputs
    inputs, err := protocol.ParseOrchestrationInputs(cmd.Inputs)
    if err != nil {
        return a.sendErrorEvent(cmd, "invalid_inputs", err.Error())
    }

    // 2. Determine if this is intake or task_discovery
    // (affects prompt construction below)
    isTaskDiscovery := cmd.Action == protocol.ActionTaskDiscovery

    // 2. Read plan files from workspace
    planContents := make(map[string]string)
    for _, candidate := range inputs.Discovery.Candidates {
        path := filepath.Join(a.workspace, candidate.Path)
        content, err := readFileSafe(path, 1024*1024) // 1MB limit
        if err != nil {
            a.logger.Warn("failed to read candidate", "path", path, "error", err)
            continue
        }
        planContents[candidate.Path] = content
    }

    // 3. Build prompt
    prompt := buildOrchestrationPrompt(inputs.UserInstruction, inputs.Discovery.Candidates, planContents)

    // 4. Call LLM
    response, err := callLLM(a.ctx, a.llmConfig, prompt)
    if err != nil {
        return a.sendErrorEvent(cmd, "llm_call_failed", err.Error())
    }

    // 5. Parse LLM response
    result, err := parseOrchestrationResponse(response)
    if err != nil {
        return a.sendErrorEvent(cmd, "invalid_llm_response", err.Error())
    }

    // 6. Emit orchestration.proposed_tasks event
    return a.sendProposedTasksEvent(cmd, result)
}

func buildOrchestrationPrompt(action protocol.Action, instruction string, candidates []protocol.DiscoveryCandidate, contents map[string]string, runContext map[string]any) string {
    var sb strings.Builder

    sb.WriteString("You are an orchestration agent for a multi-agent development workflow.\n\n")

    if action == protocol.ActionTaskDiscovery {
        sb.WriteString("## Task Discovery (Incremental Expansion)\n\n")
        sb.WriteString("You are expanding an existing task plan mid-run.\n\n")

        if runContext != nil {
            if approvedPlan, ok := runContext["approved_plan"].(string); ok {
                sb.WriteString(fmt.Sprintf("Approved plan: %s\n", approvedPlan))
            }
            if completedTasks, ok := runContext["completed_tasks"].([]string); ok {
                sb.WriteString(fmt.Sprintf("Completed tasks: %v\n", completedTasks))
            }
        }
        sb.WriteString("\n")
    } else {
        sb.WriteString("## Initial Task Intake\n\n")
    }

    sb.WriteString(fmt.Sprintf("User instruction: %s\n\n", instruction))
    sb.WriteString("Discovered plan files:\n")

    for i, candidate := range candidates {
        sb.WriteString(fmt.Sprintf("%d. %s (score: %.2f)\n", i+1, candidate.Path, candidate.Score))
        if content, ok := contents[candidate.Path]; ok {
            sb.WriteString(fmt.Sprintf("   Content:\n%s\n\n", indentContent(content)))
        }
    }

    sb.WriteString(promptInstructions) // Template constant (same for both actions)

    return sb.String()
}

func parseOrchestrationResponse(response string) (*OrchestrationResult, error) {
    // Extract JSON from LLM response (may have markdown fences, etc.)
    jsonStr := extractJSON(response)

    var result OrchestrationResult
    if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
        return nil, fmt.Errorf("failed to parse JSON: %w", err)
    }

    // Validate
    if err := result.Validate(); err != nil {
        return nil, err
    }

    return &result, nil
}
```

**Features**:
- Safe file reading with size limits
- Structured prompt building
- Robust JSON extraction from LLM response
- Validation before emitting events

**Testing**:
- Unit tests for prompt building
- JSON extraction tests (with/without markdown)
- Integration test with mock LLM
- End-to-end test with real lorch

---

### Task 4: NDJSON Protocol Implementation

**Goal**: Implement full NDJSON I/O loop (copy pattern from mockagent)

**File**: `cmd/llm-agent/agent.go`

**Key Methods**:
```go
type LLMAgent struct {
    role        protocol.AgentType
    agentID     string
    workspace   string
    llmConfig   LLMConfig
    logger      *slog.Logger
    encoder     *ndjson.Encoder
    decoder     *ndjson.Decoder

    // Heartbeat fields
    startTime              time.Time
    hbSeq                  int
    currentStatus          protocol.HeartbeatStatus
    currentTaskID          string
    lastActivityAt         time.Time

    // Version tracking
    firstObservedSnapshotID string

    // Synchronization
    mu sync.Mutex
}

func (a *LLMAgent) Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) error {
    // Set up encoder/decoder
    a.encoder = ndjson.NewEncoder(stdout)
    a.decoder = ndjson.NewDecoder(stdin)

    // Initialize heartbeat timers before first emission
    a.startTime = time.Now()
    a.lastActivityAt = time.Now()
    a.currentStatus = protocol.HeartbeatStatusStarting

    // Start heartbeat goroutine
    go a.heartbeatLoop(ctx)

    // Send initial heartbeat (status: starting)
    a.sendHeartbeat(protocol.HeartbeatStatusStarting)

    // Mark ready
    a.sendHeartbeat(protocol.HeartbeatStatusReady)

    // Process commands from stdin
    for {
        var cmd protocol.Command
        if err := a.decoder.Decode(&cmd); err != nil {
            if err == io.EOF {
                return nil // Clean shutdown
            }
            return fmt.Errorf("failed to decode command: %w", err)
        }

        // Route to handler
        if err := a.handleCommand(&cmd); err != nil {
            a.logger.Error("command failed", "action", cmd.Action, "error", err)
            // Send error event but continue processing
        }
    }
}

func (a *LLMAgent) handleCommand(cmd *protocol.Command) error {
    a.logger.Info("handling command", "action", cmd.Action, "task_id", cmd.TaskID)

    switch cmd.Action {
    case protocol.ActionIntake, protocol.ActionTaskDiscovery:
        if a.role != protocol.AgentTypeOrchestration {
            return fmt.Errorf("action %s not supported for role %s", cmd.Action, a.role)
        }
        // Both actions use similar logic but different prompt construction
        // task_discovery includes run context; intake starts fresh
        return a.handleOrchestration(cmd)

    case protocol.ActionImplement, protocol.ActionImplementChanges:
        if a.role != protocol.AgentTypeBuilder {
            return fmt.Errorf("action %s not supported for role %s", cmd.Action, a.role)
        }
        return fmt.Errorf("builder not yet implemented")

    // ... other actions

    default:
        return fmt.Errorf("unknown action: %s", cmd.Action)
    }
}
```

**Features**:
- Proper NDJSON encoding/decoding
- Heartbeat lifecycle (starting → ready → busy → stopping)
- Role-based command routing
- Error handling that doesn't crash agent

---

### Task 5: Configuration & Environment

**Goal**: Support env vars and CLI flags for configuration

**Environment Variables**:
- `WORKSPACE_ROOT` - Workspace directory (set by lorch)
- `LLM_CLI_PATH` - Override LLM CLI command
- `LLM_TIMEOUT` - LLM call timeout in seconds
- `LOG_LEVEL` - Agent log level

**CLI Flags**:
- `--role` - Agent role (required)
- `--llm-cli` - LLM CLI command (default: "claude")
- `--workspace` - Workspace root (default: ".")
- `--log-level` - Log level (default: "info")
- `--help` - Usage documentation

**Priority**: CLI flags > Environment variables > Defaults

**lorch.json Configuration**:
```json
{
  "agents": {
    "orchestration": {
      "cmd": [
        "/path/to/llm-agent",
        "--role", "orchestration",
        "--llm-cli", "claude"
      ],
      "env": {
        "WORKSPACE_ROOT": ".",
        "LLM_TIMEOUT": "180"
      }
    }
  }
}
```

---

### Task 6: Testing Strategy

**Unit Tests**:
- Prompt building functions
- JSON extraction from various LLM response formats
- File reading with safety limits
- Role-based command routing
- IK cache storage and retrieval
- Observed version echo
- Error event construction with proper codes
- Atomic artifact write pattern

**Protocol Compliance Tests** (Critical):
- **Schema Validation**: Validate all outbound command/event/heartbeat against protocol schemas
- **Message Size**: Negative test for oversized NDJSON messages (> 256 KiB)
- **Invalid Enums**: Negative tests for invalid action/status/event types
- **Required Fields**: Ensure all required protocol fields are present

**Idempotency Tests** (Critical):
```go
func TestIdempotencyReplay(t *testing.T) {
    // Send intake command with IK-1
    cmd1 := buildIntakeCommand("IK-1", "Implement auth")
    result1, err := agent.handleIntake(cmd1)
    require.NoError(t, err)

    // Verify LLM was called (check mock call count)
    assert.Equal(t, 1, mockLLM.CallCount())

    // Send same command again with IK-1
    cmd2 := buildIntakeCommand("IK-1", "Implement auth") // Same IK
    result2, err := agent.handleIntake(cmd2)
    require.NoError(t, err)

    // Verify:
    // 1. LLM was NOT called again
    assert.Equal(t, 1, mockLLM.CallCount())

    // 2. Events are identical
    assert.JSONEq(t, result1.Event, result2.Event)

    // 3. Artifacts match
    assert.Equal(t, result1.Artifacts, result2.Artifacts)
}
```

**Version Mismatch Tests**:
```go
func TestSnapshotMismatch(t *testing.T) {
    // Send command with snapshot-A
    cmd := buildIntakeCommand("IK-1", "Implement auth")
    cmd.Version.SnapshotID = "snap-A"

    // Agent detects it's running in snap-B context
    agent.currentSnapshot = "snap-B"

    result, err := agent.handleIntake(cmd)

    // Should emit error event
    assert.NoError(t, err) // No Go error
    assert.Equal(t, "error", result.Event)
    assert.Equal(t, "version_mismatch", result.Payload["code"])
}
```

**Heartbeat Lifecycle Tests**:
- Verify status transitions: starting → ready → busy → ready
- Check all required fields present (seq, pid, ppid, uptime_s, etc.)
- Verify heartbeats continue during long LLM calls
- Test heartbeat during timeout scenarios

**Integration Tests**:
- Mock LLM CLI (bash script):
  ```bash
  #!/bin/bash
  # mock-llm.sh - Returns canned JSON response
  cat <<EOF
  {
    "plan_file": "PLAN.md",
    "confidence": 0.95,
    "tasks": [
      {"id": "T-001", "title": "Test task", "files": ["test.go"]}
    ]
  }
  EOF
  ```
- Test with mock LLM CLI
- Verify NDJSON protocol compliance
- Test error scenarios (LLM failure, timeout, invalid response)

**End-to-End Tests**:
- Real lorch + llm-agent + mock LLM
- Verify full intake flow with idempotency
- Check event log persistence
- Validate transcript output
- Test artifact production and checksums
- Verify crash/resume with IK replay

**Artifact Tests**:
- Verify atomic write pattern (temp → fsync → rename → fsync dir)
- Check SHA256 checksums match file contents
- Verify artifact.produced events emitted
- Test artifact reference in idempotency cache

**Manual Testing**:
- Test with actual `claude` CLI (if available)
- Test with `codex` CLI (if available)
- Verify workspace file access
- Test with large plan files (size limit handling)
- Test timeout behavior with slow LLM responses

---

## LLM CLI Interface Specification

### Supported CLIs

The agent supports any CLI that follows this simple interface:

**Input**: Prompt sent to stdin
**Output**: Response printed to stdout
**Errors**: Diagnostic messages to stderr (optional)

**Examples**:

```bash
# Anthropic Claude (hypothetical NDJSON mode)
echo "prompt" | claude

# OpenAI Codex
echo "prompt" | codex --model gpt-4

# Custom Python wrapper
echo "prompt" | python llm_wrapper.py --provider anthropic

# Local bash script
echo "prompt" | ./my_llm.sh
```

### Response Format Expectations

**For Orchestration**:
- JSON object with `tasks` array
- Each task has: `id`, `title`, optional `files`
- May include `needs_clarification` and `clarification_questions`

**Flexible Parsing**:
- Handles markdown code fences: \`\`\`json ... \`\`\`
- Handles plain JSON
- Handles JSON embedded in natural language response

---

## Future Extensions

### Phase 2: Builder Agent

**Responsibilities**:
- Read specs/tasks from workspace
- Write/modify code files
- Run tests and lint
- Report results with test summaries

**Key Differences from Orchestration**:
- **Writes files** (using atomic write pattern per spec §14.4)
- **Executes commands** (go test, npm test, etc.)
- **Parses test output** into structured format

**Prompt Template**:
```
You are a builder agent. Implement the following task:

Task: {task_title}
Goal: {task_goal}

Relevant files:
{current file contents if exists}

Spec requirements:
{relevant spec sections}

Instructions:
1. Write/modify the necessary code files
2. Follow project conventions
3. Ensure tests pass
4. Return JSON with file paths and contents
```

### Phase 3: Reviewer Agent

**Responsibilities**:
- Read implemented code
- Evaluate quality, style, correctness
- Provide structured feedback
- Approve or request changes

### Phase 4: Spec-Maintainer Agent

**Responsibilities**:
- Read SPEC.md and implementation
- Verify requirements coverage
- Update allowed sections (status table, changelog)
- Emit approval or request changes

---

## Filesystem Access Patterns

### Read Operations

**All agents can read**:
```go
func readWorkspaceFile(workspace, relativePath string) (string, error) {
    // Security: validate path with symlink-safe resolution
    fullPath, err := resolveWorkspacePath(workspace, relativePath)
    if err != nil {
        return "", err
    }

    // Safety: limit file size
    const maxFileSize = 10 * 1024 * 1024 // 10MB

    file, err := os.Open(fullPath)
    if err != nil {
        return "", err
    }
    defer file.Close()

    limited := io.LimitReader(file, maxFileSize)
    content, err := io.ReadAll(limited)
    if err != nil {
        return "", err
    }

    return string(content), nil
}
```

### Write Operations

**Builder and Spec-Maintainer write** (per MASTER-SPEC.md §14.4):
```go
func writeWorkspaceFile(workspace, relativePath string, content []byte) error {
    // Security: validate path with symlink-safe resolution
    fullPath, err := resolveWorkspacePath(workspace, relativePath)
    if err != nil {
        return err
    }

    // Atomic write pattern (spec §14.4)
    dir := filepath.Dir(fullPath)
    if err := os.MkdirAll(dir, 0700); err != nil { // 0700 per spec §13
        return err
    }

    tmpFile := fmt.Sprintf("%s/.%s.tmp.%d.%s",
        dir, filepath.Base(fullPath), os.Getpid(), uuid.New().String()[:8])

    // Write to temp file with restrictive permissions
    if err := os.WriteFile(tmpFile, content, 0600); err != nil { // 0600 per spec §13
        return err
    }

    // Sync
    f, err := os.Open(tmpFile)
    if err != nil {
        os.Remove(tmpFile)
        return err
    }
    f.Sync()
    f.Close()

    // Atomic rename
    if err := os.Rename(tmpFile, fullPath); err != nil {
        os.Remove(tmpFile)
        return err
    }

    // Sync directory
    d, err := os.Open(dir)
    if err != nil {
        return err // Return error but file is written
    }
    d.Sync()
    d.Close()

    return nil
}
```

**Orchestration never edits plan/spec files** (per spec §2.2) but may produce planning artifacts via `expected_outputs` (e.g., `/tasks/T-0050.plan.json`) using the atomic write pattern.

---

## Additional Implementation Details

### Log Envelope (Spec §3.4)

**Requirement**: Use `kind: "log"` envelope for diagnostic output (not `event`).

**Implementation**:
```go
func (a *LLMAgent) sendLog(level, message string, fields map[string]any) error {
    // Redact secrets before logging
    if fields != nil {
        fields = redactSecrets(fields)
    }

    l := protocol.Log{
        Kind:      "log",
        Level:     level,    // "info" | "warn" | "error"
        Message:   message,
        Fields:    fields,
        Timestamp: time.Now().UTC(),
    }
    return a.encoder.Encode(l)
}
```

**Usage**:
- Info logs: Major steps (IK hit/miss, LLM invoked, JSON parsed)
- Warn logs: Optional artifact failures, summarization, budget exhaustion
- Error logs: Internal errors that don't halt processing

**Note**: Keep logs concise to respect NDJSON 256 KiB caps.

### Idempotency Index (Optional O(1) Optimization)

**Problem**: Directory scan for IK lookup is O(n) in number of receipts.

**Solution**: Best-effort index at `/receipts/<task>/index/by-ik/<first8(sha256(ik))>.json`.

**Implementation**:
```go
// When saving receipt, also write index
func (a *LLMAgent) saveReceiptWithIndex(taskID, action, ik string, receipt Receipt, receiptPath string) error {
    // Save main receipt
    if err := a.saveReceipt(receiptPath, receipt); err != nil {
        return err
    }

    // Best-effort index (failures don't block receipt save)
    indexDir := filepath.Join(a.workspace, "receipts", taskID, "index", "by-ik")
    if err := os.MkdirAll(indexDir, 0700); err == nil {
        ikHash := fmt.Sprintf("%x", sha256.Sum256([]byte(ik)))[:8]
        indexPath := filepath.Join(indexDir, ikHash+".json")
        indexData := map[string]string{"receipt_path": receiptPath}
        if data, err := json.Marshal(indexData); err == nil {
            _ = os.WriteFile(indexPath, data, 0600) // Best effort
        }
    }

    return nil
}

// Lookup checks index first, falls back to scan
func (a *LLMAgent) findReceiptByIK(taskID, action, ik string) (*Receipt, string, error) {
    // Try index first (O(1))
    ikHash := fmt.Sprintf("%x", sha256.Sum256([]byte(ik)))[:8]
    indexPath := filepath.Join(a.workspace, "receipts", taskID, "index", "by-ik", ikHash+".json")
    if data, err := os.ReadFile(indexPath); err == nil {
        var idx map[string]string
        if json.Unmarshal(data, &idx) == nil {
            if receiptPath, ok := idx["receipt_path"]; ok {
                if receipt, err := a.loadReceipt(receiptPath); err == nil {
                    if receipt.IdempotencyKey == ik {
                        return receipt, receiptPath, nil
                    }
                }
            }
        }
    }

    // Fall back to directory scan (existing code)
    return a.findReceiptByIKScan(taskID, action, ik)
}
```

**Benefit**: Faster replays in large task histories; gracefully degrades to scan if index missing.

### Artifact Size Cap (Spec §12)

**Requirement**: Enforce configured artifact size limit (default 1 GiB).

**Implementation** (in `writeArtifactAtomic`):
```go
func (a *LLMAgent) writeArtifactAtomic(relativePath string, content []byte) (protocol.Artifact, error) {
    // Check artifact size cap BEFORE writing
    maxSize := a.cfg.ArtifactMaxBytes
    if maxSize == 0 {
        maxSize = 1 * 1024 * 1024 * 1024 // 1 GiB default
    }

    if int64(len(content)) > maxSize {
        return protocol.Artifact{}, fmt.Errorf("artifact exceeds size cap: %d > %d bytes",
            len(content), maxSize)
    }

    // ... existing atomic write code ...

    // After successful write, verify final size
    fileData, err := os.ReadFile(fullPath)
    if err != nil {
        return protocol.Artifact{}, err
    }

    if int64(len(fileData)) > maxSize {
        os.Remove(fullPath) // Cleanup oversized artifact
        return protocol.Artifact{}, fmt.Errorf("artifact exceeds size cap after write: %d > %d",
            len(fileData), maxSize)
    }

    // ... rest of existing code ...
}
```

**Configuration**: `ARTIFACT_MAX_BYTES` environment variable.

### Event Size Guard (Spec §12)

**Requirement**: Ensure `orchestration.proposed_tasks` payload stays under 256 KiB.

**Implementation**:
```go
func (a *LLMAgent) sendProposedTasksEvent(cmd *protocol.Command, result OrchestrationResult) error {
    evt := a.newEvent(cmd, "orchestration.proposed_tasks")
    evt.Status = "success"
    evt.Payload = map[string]any{
        "plan_candidates": result.PlanCandidates,
        "derived_tasks":   result.DerivedTasks,
        "notes":           result.Notes,
    }

    // Check payload size
    payloadBytes, _ := json.Marshal(evt.Payload)
    maxSize := a.cfg.MaxMessageBytes
    if maxSize == 0 {
        maxSize = 256 * 1024 // 256 KiB default
    }

    if len(payloadBytes) > maxSize {
        // Payload too large - truncate and reference artifact
        if len(cmd.ExpectedOutputs) > 0 {
            // Write full task list to artifact
            fullPath := cmd.ExpectedOutputs[0].Path
            fullData, _ := json.Marshal(result)
            artifact, err := a.writeArtifactAtomic(fullPath, fullData)
            if err != nil {
                return err
            }
            a.sendArtifactProducedEvent(cmd, artifact)

            // Include only truncated preview in event
            evt.Payload = map[string]any{
                "plan_candidates": result.PlanCandidates,
                "derived_tasks":   result.DerivedTasks[:min(5, len(result.DerivedTasks))],
                "notes":           fmt.Sprintf("Full task list in %s (truncated for size)", fullPath),
                "artifact_ref":    fullPath,
            }
        } else {
            // No expected_outputs - just truncate
            evt.Payload = map[string]any{
                "plan_candidates": result.PlanCandidates[:min(3, len(result.PlanCandidates))],
                "derived_tasks":   result.DerivedTasks[:min(5, len(result.DerivedTasks))],
                "notes":           "Truncated due to size limit",
            }
        }
    }

    return a.encoder.Encode(evt)
}
```

### Deterministic Ordering (Spec §5.4)

**Requirement**: Sort arrays for deterministic output (aids idempotency verification).

**Implementation** (before emitting events):
```go
func (a *LLMAgent) normalizeOrchestrationResult(result *OrchestrationResult) {
    // Sort plan_candidates by path (asc), then confidence (desc)
    sort.SliceStable(result.PlanCandidates, func(i, j int) bool {
        if result.PlanCandidates[i].Path == result.PlanCandidates[j].Path {
            return result.PlanCandidates[i].Confidence > result.PlanCandidates[j].Confidence
        }
        return result.PlanCandidates[i].Path < result.PlanCandidates[j].Path
    })

    // Sort derived_tasks by ID (asc)
    sort.SliceStable(result.DerivedTasks, func(i, j int) bool {
        return result.DerivedTasks[i].ID < result.DerivedTasks[j].ID
    })
}

// Call before emitting:
// normalizeOrchestrationResult(&result)
// return a.sendProposedTasksEvent(cmd, result)
```

**Benefit**: Same input always produces same output order, aiding debugging and testing.

---

## Security Considerations

### Secret Redaction (Spec §13)

**Requirement**: Redact fields ending with `_TOKEN`, `_KEY`, `_SECRET` (case-insensitive) in logs.

**Implementation**:
```go
func redactSecrets(m map[string]any) map[string]any {
    if m == nil {
        return nil
    }

    out := make(map[string]any, len(m))
    for k, v := range m {
        kUp := strings.ToUpper(k)
        if strings.HasSuffix(kUp, "_TOKEN") ||
           strings.HasSuffix(kUp, "_KEY") ||
           strings.HasSuffix(kUp, "_SECRET") {
            out[k] = "[REDACTED]"
        } else {
            // Recursively redact nested maps
            if nested, ok := v.(map[string]any); ok {
                out[k] = redactSecrets(nested)
            } else {
                out[k] = v
            }
        }
    }
    return out
}
```

**Usage**: Automatically called by `sendLog()` before emitting logs.

**Examples**:
- `ANTHROPIC_API_KEY` → `[REDACTED]`
- `GITHUB_TOKEN` → `[REDACTED]`
- `database_secret` → `[REDACTED]`
- `user_name` → unchanged

### Path Validation (Spec §13)

**Critical**: Use symlink-safe path resolution for all workspace file access.

**Implementation**:
```go
// resolveWorkspacePath validates and resolves a relative path within workspace
// Returns canonical absolute path or error if path escapes workspace
func resolveWorkspacePath(workspace, relative string) (string, error) {
    // Get canonical workspace root
    rootAbs, err := filepath.EvalSymlinks(filepath.Clean(workspace))
    if err != nil {
        return "", fmt.Errorf("failed to resolve workspace: %w", err)
    }

    // Join and resolve symlinks in target path
    joined := filepath.Join(rootAbs, relative)
    fullAbs, err := filepath.EvalSymlinks(filepath.Clean(joined))
    if err != nil {
        return "", fmt.Errorf("failed to resolve path: %w", err)
    }

    // Ensure fullAbs is within rootAbs (with path separator guard)
    rootWithSep := rootAbs
    if !strings.HasSuffix(rootWithSep, string(os.PathSeparator)) {
        rootWithSep += string(os.PathSeparator)
    }

    if fullAbs != rootAbs && !strings.HasPrefix(fullAbs, rootWithSep) {
        return "", fmt.Errorf("path escapes workspace: %s", relative)
    }

    return fullAbs, nil
}
```

**Requirements**:
- All filesystem operations must use `resolveWorkspacePath()` before access
- Rejects `..` traversal attempts
- Rejects absolute paths outside workspace
- Rejects symlinks that escape workspace
- Returns canonical absolute path for safe file I/O

### Size Limits
- File reads limited to 10MB by default
- LLM output limited to 1MB by default
- Configurable via env vars

### Timeout Protection
- LLM calls have configurable timeout (default: 180s)
- Context cancellation propagates to subprocess
- Prevents hung agents

### Error Handling
- LLM errors don't crash agent
- Malformed responses emit error events
- Agent continues processing after errors

---

## Documentation Deliverables

### docs/LLM-AGENTS.md
Usage guide for configuring and running llm-agent

### docs/development/LLM-AGENTS-IMPLEMENTATION.md
Implementation notes and patterns for developers

### README.md Updates
- Section on real agent usage
- Configuration examples
- LLM CLI requirements

---

## Open Questions

1. **Prompt Engineering**: Should prompts be configurable or hardcoded?
   - **Recommendation**: Start hardcoded, make configurable later

2. **Response Validation**: How strict should JSON parsing be?
   - **Recommendation**: Lenient parsing (extract JSON from markdown), strict validation

3. **Error Recovery**: Should agents retry LLM calls on failure?
   - **Recommendation**: No automatic retries, let lorch handle via idempotency

4. **Streaming**: Should we support streaming LLM responses?
   - **Recommendation**: Phase 2 feature, start with complete responses

5. **Model Selection**: Should agents support model-specific behavior?
   - **Recommendation**: Keep agents model-agnostic, pass model flags to LLM CLI

---

## Success Criteria

### Orchestration Agent Complete When:
- ✅ Binary builds and runs
- ✅ Speaks NDJSON protocol correctly
- ✅ Reads workspace files safely
- ✅ Calls configurable LLM CLI
- ✅ Parses LLM responses robustly
- ✅ Emits valid `orchestration.proposed_tasks` events
- ✅ End-to-end test passes with real lorch
- ✅ Works with mock LLM for deterministic testing
- ✅ Documentation complete

### Ready for Production When:
- ✅ Tested with real Claude/Codex CLI
- ✅ Error handling covers edge cases
- ✅ Security review passed (path validation, size limits)
- ✅ Performance acceptable (< 3 minute timeout)
- ✅ Multiple users have validated design

---

## Timeline Estimate

**Phase 1 (Orchestration Agent)** - Updated after Review 1 and plan refinement:

### Day-by-Day Breakdown
- **Day 1**: Project scaffold, main.go, CLI flags, basic NDJSON I/O loop
- **Day 2**: Receipt system (load/save), idempotency key matching, replay logic
- **Day 3**: Heartbeat manager with synchronized state, status transitions
- **Day 4**: LLM CLI caller, subprocess management, timeout handling
- **Day 5**: Orchestration logic for intake action, prompt building, response parsing
- **Day 6**: task_discovery support, expected_outputs handling, atomic artifact writes
- **Day 7-8**: Error handling (structured codes), observed version echo, validation
- **Day 9-10**: Comprehensive testing:
  - Unit tests (prompt building, JSON extraction, receipt storage)
  - Idempotency replay test (verify no duplicate LLM calls)
  - Protocol compliance (schema validation, message size, field completeness)
  - Integration tests (mock LLM, end-to-end with lorch)
  - Heartbeat lifecycle tests
  - Version mismatch tests

**Total**: ~10 days for fully spec-compliant orchestration agent with comprehensive tests

**Feature Complexity Assessment**:
- Core NDJSON I/O loop: 1 day (straightforward)
- Receipt-based idempotency: 1.5 days (file I/O, matching logic, replay)
- Heartbeat with synchronized state: 1 day (concurrency, proper transitions)
- LLM calling: 1 day (subprocess, pipes, timeouts)
- Orchestration logic (both actions): 2 days (prompt engineering, JSON parsing)
- Artifact production (atomic writes): 0.5 days (straightforward with spec pattern)
- Error handling & validation: 1 day (structured codes, input validation)
- Comprehensive testing: 2 days (multiple test categories, fixtures)

**Risk Factors**:
- LLM CLI integration may need iteration for different tools (claude, codex)
- Receipt replay logic needs careful testing for edge cases
- Heartbeat synchronization could have subtle race conditions
- Buffer: 1-2 extra days recommended

**Note**: Original 5-day estimate (pre-review) → 8 days (after review 1) → 10 days (after plan refinement). The additional time accounts for receipt-based idempotency, synchronized heartbeat management, both orchestration actions, and truly comprehensive testing.

**Future Phases**:
- Builder: 3-4 days
- Reviewer: 2-3 days
- Spec-Maintainer: 2-3 days

---

## Ready-To-Build Checklist

Before starting implementation, ensure the plan addresses all of these requirements:

### Spec Compliance (Mandatory)
- [ ] **Idempotency**: IK cache implemented; repeated IKs do not call LLM
- [ ] **Orchestration artifacts**: Writes artifacts only via `expected_outputs` using safe atomic writes
- [ ] **Version echo**: `observed_version.snapshot_id` included on all events
- [ ] **Heartbeats**: Correct status transitions (starting → ready → busy → ready) with all required fields (seq, pid, ppid, task_id, stats)
- [ ] **Heartbeat continuity**: Heartbeats continue during long LLM calls
- [ ] **Structured errors**: `error` events with machine-readable `payload.code`
- [ ] **Message size**: NDJSON messages respect 256 KiB limit; artifacts used for large data
- [ ] **Timeouts**: Aligned with spec defaults (intake: 180s, heartbeat: 10s)

### Testing Requirements
- [ ] **Schema validation**: All outbound messages validated against protocol schemas
- [ ] **Idempotency replay**: Test verifies no LLM calls on repeated IK, identical events
- [ ] **Version mismatch**: Test for structured error event on snapshot mismatch
- [ ] **Message size**: Negative test for oversized messages
- [ ] **Invalid enums**: Negative tests for protocol violations
- [ ] **Heartbeat lifecycle**: Status transitions and field completeness verified
- [ ] **Artifact production**: Atomic write pattern, checksums, event emission tested
- [ ] **Error scenarios**: LLM failure, timeout, invalid response all tested

### Implementation Completeness
- [ ] IK cache storage and retrieval logic
- [ ] Observed version echo in all event constructors
- [ ] Continuous heartbeat mechanism during LLM calls
- [ ] Error event constructor with standardized codes
- [ ] Message size detection and handling
- [ ] Atomic artifact write with SHA256 checksums
- [ ] Timeout handling with context deadlines

### Documentation
- [ ] All code examples include required protocol fields
- [ ] Error codes documented and standardized
- [ ] Idempotency behavior explained with examples
- [ ] Artifact production flow documented
- [ ] Testing strategy includes all critical tests

**Verdict**: When all boxes are checked, the plan is implementation-ready and spec-compliant.

---

### Generic Event Size Capping Helper (apply to all emitters)

To ensure no event exceeds the NDJSON message cap (256 KiB by default), centralize size enforcement in a single helper and use it for all event emissions (errors, orchestration events, etc.). Prefer calling this helper instead of the raw encoder.

```go
// encodeEventCapped marshals and emits an event, enforcing the NDJSON message size cap.
// If the marshaled event exceeds the configured cap, the payload is replaced with a
// truncated preview to maintain protocol compliance while preserving debuggability.
func (a *LLMAgent) encodeEventCapped(evt protocol.Event) error {
    maxSize := a.cfg.MaxMessageBytes
    if maxSize == 0 {
        maxSize = 256 * 1024 // 256 KiB default (Spec §12)
    }

    b, err := json.Marshal(evt)
    if err != nil {
        return err
    }
    if len(b) <= maxSize {
        return a.encoder.Encode(evt)
    }

    // Fallback: stringify payload preview under "_truncated" and clear original payload
    preview := ""
    if pb, err := json.Marshal(evt.Payload); err == nil {
        if len(pb) > 2048 {
            preview = string(pb[:2048]) + "…"
        } else {
            preview = string(pb)
        }
    }
    evt.Payload = map[string]any{"_truncated": preview}
    return a.encoder.Encode(evt)
}

// Usage
//   evt := a.newEvent(cmd, "orchestration.proposed_tasks")
//   evt.Payload = ...
//   return a.encodeEventCapped(evt)
```

Note: For known large payloads (e.g., `orchestration.proposed_tasks`), prefer first writing the full payload to an artifact (when permitted via `expected_outputs`) and include only a truncated preview plus an artifact reference in the event, then still pass through `encodeEventCapped` as a final guard.

Updated example (use `encodeEventCapped` instead of `encoder.Encode`):

```go
func (a *LLMAgent) sendErrorEvent(cmd *protocol.Command, code, message string) error {
    evt := a.newEvent(cmd, "error")
    evt.Status = "failed"
    evt.Payload = map[string]any{
        "code":    code,
        "message": message,
    }
    return a.encodeEventCapped(evt)
}
```

Apply the same pattern to other emitters (e.g., `sendProposedTasksEvent`, `sendPlanConflictEvent`, `sendNeedsClarificationEvent`).

## References

- MASTER-SPEC.md §2.2 (Agent Roles)
- MASTER-SPEC.md §3 (IPC Protocol)
- MASTER-SPEC.md §5.4 (Idempotency Keys)
- MASTER-SPEC.md §5.5 (Deterministic Writes)
- MASTER-SPEC.md §7.1 (Timeouts)
- MASTER-SPEC.md §14.4 (Agent Safe Write)
- cmd/mockagent - Reference implementation
- internal/protocol - Protocol types
- docs/AGENT-SHIMS.md - Agent interface docs
- docs/development/llm-agents-plan-review-1.md - Review feedback incorporated
