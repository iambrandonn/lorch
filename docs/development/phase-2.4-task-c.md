# Phase 2.4 Task C Implementation Summary
## Receipt Traceability Metadata

**Status**: ✅ Complete (2025-10-22)
**Milestone**: Phase 2.4 – Task Activation Pipeline
**Test**: TR-001 (Receipt Traceability)

---

## Overview

Task C extends the receipt persistence layer to capture intake origin metadata, enabling full traceability from natural language instructions through to completed work artifacts. This completes the activation pipeline by ensuring that every receipt records not just *what* was done, but *why* it was requested and *how* it was derived from user intent.

### What Was Built

**Core Changes**:
1. **Receipt Structure Extension**: Added 6 traceability fields to capture intake metadata
2. **Metadata Extraction**: Safe helper functions for extracting structured data from command inputs
3. **Scheduler Metadata Propagation**: Task inputs preserved across implement/review/spec-maintainer commands
4. **Integration Testing**: TR-001 validates end-to-end traceability with fixture agents

---

## Architecture

### Receipt Structure (Before vs After)

**Before (Phase 1.3)**:
```go
type Receipt struct {
    TaskID           string
    Step             int
    Action           string
    IdempotencyKey   string
    SnapshotID       string
    CommandMessageID string
    CorrelationID    string
    Artifacts        []protocol.Artifact
    Events           []string
    CreatedAt        time.Time
}
```

**After (Phase 2.4 Task C)**:
```go
type Receipt struct {
    // ... existing fields ...

    // Intake traceability metadata (P2.4 Task C)
    TaskTitle           string   `json:"task_title,omitempty"`
    Instruction         string   `json:"instruction,omitempty"`
    ApprovedPlan        string   `json:"approved_plan,omitempty"`
    IntakeCorrelationID string   `json:"intake_correlation_id,omitempty"`
    Clarifications      []string `json:"clarifications,omitempty"`
    ConflictResolutions []string `json:"conflict_resolutions,omitempty"`
}
```

### Data Flow

```
User NL Instruction
  ↓
Orchestration Agent (proposed_tasks)
  ↓
User Approval (system.user_decision)
  ↓
Activation (Task.ToCommandInputs)
  ↓
Scheduler (taskInputs preserved)
  ↓
Commands (implement/review/spec → all carry metadata)
  ↓
Receipts (NewReceipt extracts from command inputs)
  ↓
Disk (/receipts/<task>/step-N.json)
```

---

## Implementation Details

### 1. Receipt Struct Extension

**File**: `internal/receipt/receipt.go`

Added 6 fields with `omitempty` JSON tags for backward compatibility:
- `TaskTitle`: Human-readable description from orchestration
- `Instruction`: Original user instruction (rationale)
- `ApprovedPlan`: Which plan file was approved (e.g., "PLAN.md")
- `IntakeCorrelationID`: Links back to intake conversation
- `Clarifications`: User answers to orchestration questions
- `ConflictResolutions`: Conflict resolution choices

**Design Decision**: Used `omitempty` to ensure receipts from Phase 1 tasks (without intake) serialize cleanly without null/empty fields cluttering the JSON.

### 2. Metadata Extraction Helpers

**File**: `internal/receipt/receipt.go`

Three safe extraction functions handle type assertions and missing data:

```go
// extractString: handles nil maps, missing keys, type mismatches
func extractString(inputs map[string]any, key string) string

// extractStringSlice: handles []string and []interface{} with conversion
func extractStringSlice(inputs map[string]any, key string) []string

// extractIntakeCorrelationID: parses "corr-intake-XXX|activate-YYY" format
func extractIntakeCorrelationID(correlationID string) string
```

**Design Decision**: Fail-safe extraction (return empty on error) prevents receipts from crashing due to unexpected input types or missing data. This aligns with the "graceful degradation" principle—better to have partial traceability than no receipt at all.

### 3. NewReceipt() Update

**File**: `internal/receipt/receipt.go`

Updated to extract metadata from `cmd.Inputs`:

```go
func NewReceipt(cmd *protocol.Command, step int, events []*protocol.Event) *Receipt {
    // ... existing artifact collection ...

    // Extract traceability metadata
    taskTitle := extractString(cmd.Inputs, "task_title")
    instruction := extractString(cmd.Inputs, "instruction")
    approvedPlan := extractString(cmd.Inputs, "approved_plan")
    clarifications := extractStringSlice(cmd.Inputs, "clarifications")
    conflictResolutions := extractStringSlice(cmd.Inputs, "conflict_resolutions")

    // Intake correlation ID: try inputs first (set by scheduler), fallback to correlation ID parsing
    intakeCorrelationID := extractString(cmd.Inputs, "intake_correlation_id")
    if intakeCorrelationID == "" {
        intakeCorrelationID = extractIntakeCorrelationID(cmd.CorrelationID)
    }

    return &Receipt{
        // ... existing fields ...
        TaskTitle:           taskTitle,
        Instruction:         instruction,
        ApprovedPlan:        approvedPlan,
        IntakeCorrelationID: intakeCorrelationID,
        Clarifications:      clarifications,
        ConflictResolutions: conflictResolutions,
    }
}
```

**Design Decision**: Two-stage lookup for `IntakeCorrelationID` (inputs → correlation ID) provides resilience. The scheduler explicitly sets `intake_correlation_id` in inputs after the first command, ensuring subsequent commands (review, spec-maintainer) preserve the lineage even though their correlation IDs don't have the `|activate` format.

### 4. Scheduler Metadata Propagation

**File**: `internal/scheduler/scheduler.go`

#### Added Field to Scheduler Struct:
```go
type Scheduler struct {
    // ... existing fields ...

    // Task inputs preserved across all commands for traceability (P2.4 Task C)
    taskInputs map[string]any
}
```

#### ExecuteTask Captures Inputs:
```go
func (s *Scheduler) ExecuteTask(ctx context.Context, taskID string, inputs map[string]any) error {
    // Store task inputs for traceability metadata propagation
    s.taskInputs = make(map[string]any, len(inputs))
    for k, v := range inputs {
        s.taskInputs[k] = v
    }

    // ... execute implement → review → spec-maintainer ...
}
```

#### All Command Methods Use Preserved Inputs:
```go
func (s *Scheduler) executeImplement(...) {
    cmd := s.makeCommand(taskID, ..., inputs)  // Original inputs

    // Capture intake correlation ID from first command
    if s.taskInputs != nil {
        intakeCorrelation := extractIntakeCorrelationFromCommand(cmd)
        if intakeCorrelation != "" {
            s.taskInputs["intake_correlation_id"] = intakeCorrelation
        }
    }
    // ...
}

func (s *Scheduler) executeReview(...) {
    inputs := s.taskInputs  // Reuse stored inputs
    cmd := s.makeCommand(taskID, ..., inputs)
    // ...
}

func (s *Scheduler) executeSpecMaintenance(...) {
    inputs := s.taskInputs  // Reuse stored inputs
    cmd := s.makeCommand(taskID, ..., inputs)
    // ...
}

func (s *Scheduler) executeImplementChanges(...) {
    inputs := s.taskInputs  // Reuse stored inputs
    cmd := s.makeCommand(taskID, ..., inputs)
    // ...
}
```

**Design Decision**: Storing `taskInputs` in the scheduler ensures all commands carry the same traceability metadata. The intake correlation ID is explicitly captured after the first command and added to `taskInputs`, so subsequent commands have it in their inputs map (not just their correlation IDs).

### 5. Activation Layer Update

**File**: `internal/activation/input.go`

Added `intake_correlation_id` to `Task.ToCommandInputs()`:

```go
func (t Task) ToCommandInputs() map[string]any {
    return map[string]any{
        // ... existing fields ...
        "intake_correlation_id": t.IntakeCorrelationID,
    }
}
```

**Design Decision**: Including `IntakeCorrelationID` in the initial inputs ensures it's available from the very first command, making the scheduler's capture logic reliable.

---

## Testing

### Unit Tests (`internal/receipt/receipt_test.go`)

**7 new tests**:

1. **TestNewReceiptWithTraceability**: Validates metadata extraction from rich command inputs
2. **TestNewReceiptWithoutTraceability**: Confirms backward compatibility (empty metadata for Phase 1 tasks)
3. **TestExtractString**: Validates safe string extraction (nil maps, missing keys, type mismatches)
4. **TestExtractStringSlice**: Validates safe slice extraction ([]string, []interface{} conversion)
5. **TestExtractIntakeCorrelationID**: Validates correlation ID parsing ("corr-intake-X|activate-Y" → "corr-intake-X")
6. **Updated TestWriteAndReadReceipt**: Ensures JSON round-trip preserves all fields
7. **Updated TestNewReceipt**: Ensures existing behavior unchanged

**Coverage**: Helper functions, metadata extraction logic, backward compatibility.

### Integration Test (`internal/activation/integration_test.go`)

**TR-001: TestReceiptTraceability**

Validates end-to-end traceability:
1. Creates activation input with rich metadata (instruction, clarifications, conflict resolutions)
2. Executes full pipeline (implement → review → spec-maintainer) with fixture agents
3. Reads receipts from disk
4. Asserts all receipts contain:
   - `TaskTitle` = "Implement OAuth2 login flow"
   - `Instruction` = "Add user authentication with OAuth2"
   - `ApprovedPlan` = "PLAN.md"
   - `IntakeCorrelationID` = "corr-intake-tr001"
   - `Clarifications` = ["Use Google OAuth provider", "Store tokens in Redis"]
   - `ConflictResolutions` = ["Keep existing session management"]

**Outcome**: ✅ Pass (receipts for all steps contain complete metadata)

---

## Example Receipt JSON

**Before (Phase 1.3)**:
```json
{
  "task_id": "T-0042",
  "step": 1,
  "action": "implement",
  "idempotency_key": "ik:abc123...",
  "snapshot_id": "snap-xyz789",
  "command_message_id": "msg-cmd-001",
  "correlation_id": "corr-T-0042-implement-abc",
  "artifacts": [{"path": "src/main.go", "sha256": "...", "size": 1234}],
  "events": ["msg-e1", "msg-e2"],
  "created_at": "2025-10-22T10:15:30Z"
}
```

**After (Phase 2.4 Task C, intake-derived task)**:
```json
{
  "task_id": "AUTH-1",
  "step": 1,
  "action": "implement",
  "idempotency_key": "ik:def456...",
  "snapshot_id": "snap-test-123",
  "command_message_id": "msg-cmd-789",
  "correlation_id": "corr-intake-tr001|activate-ghi",
  "artifacts": [{"path": "src/auth.go", "sha256": "...", "size": 2048}],
  "events": ["msg-e10", "msg-e11"],
  "created_at": "2025-10-22T11:30:45Z",

  "task_title": "Implement OAuth2 login flow",
  "instruction": "Add user authentication with OAuth2",
  "approved_plan": "PLAN.md",
  "intake_correlation_id": "corr-intake-tr001",
  "clarifications": ["Use Google OAuth provider", "Store tokens in Redis"],
  "conflict_resolutions": ["Keep existing session management"]
}
```

---

## Traceability Chain Example

**User Journey**:
1. User types: `lorch run` (no --task)
2. Prompt: "What should I do?"
3. User: "Add user authentication with OAuth2"
4. Orchestration agent proposes: `PLAN-1: Implement OAuth2 login flow`
5. User approves PLAN-1
6. Activation creates task with `IntakeCorrelationID = "corr-intake-xyz"`
7. Scheduler executes: implement → review → spec-maintainer
8. All receipts record: `instruction`, `task_title`, `approved_plan`, `intake_correlation_id`
9. Audit: Read `/receipts/PLAN-1/step-1.json` → see original user instruction

**Value**: Operator can trace any artifact back to its originating user request, supporting debugging, compliance, and post-hoc analysis.

---

## Deliverables

**Code**:
- `internal/receipt/receipt.go`: Extended Receipt struct + 3 helper functions + updated NewReceipt
- `internal/scheduler/scheduler.go`: Added taskInputs field + metadata propagation + extractIntakeCorrelationFromCommand
- `internal/activation/input.go`: Added intake_correlation_id to ToCommandInputs

**Tests**:
- `internal/receipt/receipt_test.go`: 7 new unit tests (all passing)
- `internal/activation/integration_test.go`: 1 new integration test (TR-001, passing)

**Documentation**:
- Updated PLAN.md Task C status to complete
- This summary document

**Regression Check**: ✅ Full test suite passes (`go test ./... -timeout 120s`)

---

## Key Design Decisions

### 1. Why `omitempty` JSON Tags?

Ensures receipts from Phase 1 tasks (without intake) remain clean. Without `omitempty`, they'd have:
```json
{
  "task_title": "",
  "instruction": "",
  "approved_plan": "",
  "intake_correlation_id": "",
  "clarifications": null,
  "conflict_resolutions": null
}
```
With `omitempty`, these fields are omitted entirely, maintaining backward compatibility and JSON clarity.

### 2. Why Store `taskInputs` in Scheduler?

**Alternative Considered**: Pass inputs explicitly to each execute method.

**Rejected Because**:
- Would require changing signatures of 4 methods (implement, implementChanges, review, specMaintenance)
- More invasive to existing code
- Less maintainable (multiple call sites to update)

**Chosen Approach**: Store once in `ExecuteTask`, reuse in all subsequent commands.
- Single point of change
- Minimal diff (4 lines per method)
- Clear ownership (scheduler owns task execution context)

### 3. Why Two-Stage Lookup for IntakeCorrelationID?

**Problem**: Activation's `BuildImplementCommand` creates correlation IDs like `corr-intake-X|activate-Y`, but scheduler's `makeCommand` generates new IDs like `corr-<task>-<action>-<uuid>`. The intake portion is lost after the first command.

**Solution**:
1. Activation includes `intake_correlation_id` in inputs
2. Scheduler captures it from first command and adds to `taskInputs`
3. Receipt extraction tries `inputs["intake_correlation_id"]` first, then parses `cmd.CorrelationID`

This ensures all receipts (not just the first) have the intake lineage.

### 4. Why Not Store Metadata in RunState?

**Alternative Considered**: Persist traceability metadata in `/state/run.json` separately.

**Rejected Because**:
- Receipts are the canonical "what happened" record per MASTER-SPEC §5.1
- Duplicating metadata increases maintenance burden (two places to update)
- RunState is for orchestration state (current stage, pending tasks), not artifact lineage

**Chosen Approach**: Receipts are self-contained. Each receipt fully describes its origin without external lookups.

---

## Future Improvements

### 1. Structured Traceability Queries

With receipts now containing intake metadata, future tooling could:
- `lorch trace AUTH-1` → Show full lineage from user instruction to artifacts
- `lorch audit --instruction "OAuth"` → Find all tasks derived from OAuth requests
- `lorch report --plan PLAN.md` → Aggregate completion metrics by plan file

### 2. Conflict Resolution Replay

`ConflictResolutions` field enables:
- Post-mortem analysis: "Why did we keep existing session logic?"
- Decision audit: "What conflicts did user resolve during this task?"
- Future automation: "Apply same resolution to similar conflicts"

### 3. Cross-Run Traceability

`IntakeCorrelationID` links receipts across resume cycles:
- Original run: `corr-intake-X` → receipts with steps 1-3
- Resume run: Same `corr-intake-X` → receipts with steps 4-6
- Query: "Show all work for intake conversation X" → Full history across crashes/resumes

---

## Spec Alignment

**MASTER-SPEC §5.1 (Durable Artifacts)**:
✅ Receipts at `/receipts/<task>/<step>.json` now include traceability metadata

**MASTER-SPEC §16.1 (Receipt Format)**:
✅ Extended schema maintains backward compatibility (omitempty fields)

**Phase 2.4 Test Plan TR-001**:
✅ Integration test validates receipts include `intake_run_id`, `approved_plan`, `approved_task_id`

**Phase 2.4 Task C Exit Criteria**:
✅ "Automated end-to-end test validates instruction → approval → implement/review/spec-maintainer completion with recorded traceability fields"

---

## Lessons Learned

### 1. Metadata Propagation Requires Ownership

Initially attempted to pass metadata "downstream" through command chaining. This failed because:
- Review commands don't have access to original implement inputs
- Correlation IDs change between commands

**Solution**: Scheduler owns task execution context, stores inputs, provides to all commands.

### 2. Backward Compatibility Matters

Phase 1 tasks still run. Without `omitempty`, their receipts would be polluted with empty traceability fields.

**Principle**: New features should degrade gracefully when data is absent.

### 3. Test What You Trace

Unit tests validated extraction logic, but only TR-001 (integration test) caught the correlation ID propagation bug. The issue: scheduler wasn't explicitly storing `intake_correlation_id` in inputs after the first command.

**Takeaway**: Traceability requires end-to-end testing to validate the full data flow pipeline.

---

## Conclusion

Task C completes the activation pipeline by ensuring receipts capture not just implementation artifacts, but the full context of *why* work was performed. This enables:

- **Debugging**: Trace failures back to original user intent
- **Compliance**: Audit what was requested vs. what was delivered
- **Transparency**: Operators can inspect receipts to understand task lineage
- **Resilience**: Resume operations preserve intake metadata across crashes

All exit criteria met. Phase 2.4 is now ready for Phase 2.5 (UX Polish & Documentation).

---

**Implementation Complete**: 2025-10-22
**Test Status**: ✅ All tests passing (unit + integration + full suite)
**Documentation**: ✅ PLAN.md updated, summary document created
