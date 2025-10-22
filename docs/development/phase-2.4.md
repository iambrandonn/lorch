# Phase 2.4 Summary – Task Activation Pipeline

**Milestone**: Phase 2.4
**Completed**: 2025-10-22
**Status**: ✅ Delivered

---

## Overview

Phase 2.4 completes the natural language intake pipeline by bridging approved orchestration decisions to executable scheduler tasks. The activation layer transforms user-approved plans into concrete work items with idempotency keys, validates task metadata, enqueues tasks into the existing implement → review → spec-maintainer scheduler, and ensures full traceability from NL instructions through to persisted receipts.

---

## Key Deliverables

### Task A – Task Activation Mapping ✅
- **Activation pipeline** (`internal/activation/`)
  - `Input`, `DerivedTask`, `Task` data structures with rich metadata
  - `PrepareTasks()` validation: decision status (fail-closed), instruction, plan path (traversal protection), task titles
  - `BuildImplementCommand()` with idempotency key generation and intake correlation threading
  - `Activate()` function for sequential task execution via `TaskExecutor` interface
- **Integration test** (`TestActivationEndToEnd`)
  - Validates orchestration → builder → reviewer → spec-maintainer pipeline with fixture agents
  - Ensures events flow correctly through all stages
- **Unit tests** (13 tests covering edge cases)
  - Approved/denied decisions, duplicate activation guards, plan validation, discovery expansion

### Task B – Scheduler Integration ✅
- **Scheduler enhancements** (`internal/scheduler/scheduler.go`)
  - Updated `ExecuteTask()` signature to accept `inputs map[string]any` for richer metadata
  - Preserved implement → review → spec-maintainer ordering
  - Support for iterative change-request loops (review/spec feedback)
- **Run state tracking** (`internal/runstate/runstate.go`)
  - Added `ActivatedTaskIDs []string` for multi-task run tracking
  - Prevents duplicate activation on resume
- **CLI integration** (`internal/cli/run.go`)
  - `executeApprovedTasks()` bridges intake → activation → execution
  - Extracted `setupExecutionEnvironment()` helper for reusable agent/scheduler initialization
- **End-to-end flow**
  - `lorch run` (no --task) → NL intake → approval → execution of all approved tasks sequentially

### Task C – Receipt Traceability ✅
- **Receipt structure extension** (`internal/receipt/receipt.go`)
  - Added 6 traceability fields: `TaskTitle`, `Instruction`, `ApprovedPlan`, `IntakeCorrelationID`, `Clarifications`, `ConflictResolutions`
  - All fields use `omitempty` for backward compatibility with Phase 1 tasks
- **Metadata extraction helpers**
  - `extractString()`, `extractStringSlice()`, `extractIntakeCorrelationID()` for safe type handling
- **Scheduler metadata propagation**
  - Added `taskInputs map[string]any` to preserve metadata across all commands
  - Updated all command methods (`executeImplement`, `executeReview`, `executeSpecMaintenance`, `executeImplementChanges`) to use preserved inputs
- **Activation integration**
  - `Task.ToCommandInputs()` includes `intake_correlation_id` for immediate availability
- **TR-001 integration test** (`TestReceiptTraceability`)
  - Validates end-to-end traceability from orchestration through all receipts
  - Confirms implement, review, and spec-maintainer receipts all contain complete metadata

---

## Architecture

### Data Flow

```
User NL Instruction
  ↓
Orchestration Agent (proposed_tasks)
  ↓
User Approval (system.user_decision)
  ↓
Activation (PrepareTasks → BuildImplementCommand)
  ↓
Scheduler (ExecuteTask with inputs map)
  ↓
Commands (implement → review → spec-maintainer, metadata preserved)
  ↓
Receipts (NewReceipt extracts from command inputs)
  ↓
Disk (/receipts/<task>/step-N.json with traceability)
```

### Key Components

- **`internal/activation`**: Input validation, task preparation, command building
- **`internal/scheduler`**: Task execution orchestration with metadata preservation
- **`internal/receipt`**: Receipt persistence with intake traceability
- **`internal/cli`**: Intake → activation → execution bridge

---

## Testing

### Unit Tests
- **Activation**: 13 tests (validation, deduplication, edge cases)
- **Receipt**: 11 tests (7 new for traceability + 4 existing updated)
- **Scheduler**: Updated existing tests for new `ExecuteTask` signature
- **RunState**: Tests for `ActivatedTaskIDs` tracking

### Integration Tests
- **`TestActivationEndToEnd`**: Full orchestration → scheduler pipeline with fixture agents
- **`TestReceiptTraceability` (TR-001)**: End-to-end traceability validation
  - Creates activation with rich metadata (instruction, clarifications, conflicts)
  - Executes full pipeline (implement → review → spec-maintainer)
  - Reads receipts from disk
  - Asserts all receipts contain complete traceability metadata

### Regression Check
✅ Full test suite passes: `go test ./... -timeout 120s`

---

## Example Receipt (with traceability)

**Before Phase 2.4**:
```json
{
  "task_id": "T-0042",
  "step": 1,
  "action": "implement",
  "artifacts": [...],
  "events": [...]
}
```

**After Phase 2.4** (intake-derived task):
```json
{
  "task_id": "AUTH-1",
  "step": 1,
  "action": "implement",
  "artifacts": [...],
  "events": [...],

  "task_title": "Implement OAuth2 login flow",
  "instruction": "Add user authentication with OAuth2",
  "approved_plan": "PLAN.md",
  "intake_correlation_id": "corr-intake-abc",
  "clarifications": ["Use Google OAuth provider"],
  "conflict_resolutions": ["Keep existing session management"]
}
```

---

## Design Decisions

### 1. Interface-Based Executor (Task A)
**Decision**: Use `TaskExecutor` interface instead of concrete `*scheduler.Scheduler`.

**Rationale**:
- Enables unit testing with simple mocks
- Supports future distributed executors
- Clear boundary between activation (preparation) and execution (scheduling)

### 2. Fail-Closed Validation (Task A)
**Decision**: Reject empty/whitespace strings; require explicit "approved" status.

**Rationale**:
- Prevents accidental activation from incomplete intake flows
- Aligns with MASTER-SPEC human-in-control principle (§1.2)
- Explicit better than implicit for task execution decisions

### 3. taskInputs Storage in Scheduler (Task C)
**Decision**: Store inputs once in `ExecuteTask`, reuse across all commands.

**Alternative Considered**: Pass inputs explicitly to each execute method.

**Rationale**:
- Single point of change (vs. updating 4 method signatures)
- Minimal diff (4 lines per method)
- Clear ownership (scheduler owns task execution context)
- Ensures metadata propagates to all receipts (implement, review, spec-maintainer)

### 4. Two-Stage IntakeCorrelationID Lookup (Task C)
**Problem**: Activation creates correlation IDs like `corr-intake-X|activate-Y`, but scheduler generates new IDs for subsequent commands. The intake portion would be lost.

**Solution**:
1. Activation includes `intake_correlation_id` in inputs
2. Scheduler captures it from first command and adds to `taskInputs`
3. Receipt extraction tries `inputs["intake_correlation_id"]` first, then parses correlation ID

**Result**: All receipts (not just the first) preserve intake lineage.

---

## Integration Notes for Future Phases

### Phase 2.5 (UX Polish)
- Task titles and instructions now available in receipts for user-facing displays
- Clarifications/conflicts can be surfaced in verbose logging modes
- Intake correlation IDs enable "show work history for conversation X" queries

### Phase 3 (Configuration)
- Activation validation hooks could be configurable (e.g., custom plan path rules)
- Receipt metadata fields could be extended via config without code changes

### Phase 4 (Advanced Error Handling)
- Traceability metadata supports richer diagnostics (e.g., "Task AUTH-1 failed; original instruction: 'Add OAuth2'")
- Conflict resolutions in receipts enable replay/audit workflows

---

## Follow-Up / Technical Debt

1. ✅ Event log traceability – receipts now contain full metadata; event log augmentation noted for future phases
2. ✅ Run-state traceability – `ActivatedTaskIDs` tracks completed tasks; full lineage in receipts
3. Document activation best practices for agent implementers (future Phase 2.5 task)

---

## Spec Alignment

**MASTER-SPEC §4.1 (Process Flow)**:
✅ Intake → Activation → Implement → Review → Spec Maintenance flow implemented

**MASTER-SPEC §5.1 (Durable Artifacts)**:
✅ Receipts at `/receipts/<task>/<step>.json` include traceability metadata

**MASTER-SPEC §16.1 (Receipt Format)**:
✅ Extended schema maintains backward compatibility (omitempty fields)

**Phase 2.4 Test Plan**:
✅ TA-001 through TA-007 (activation), TR-001 (traceability), EO-001 (end-to-end) validated

---

## Contributors

- Builder Agent (implementation & tests)
- Code Review Agent (Task A, Task B, Task C reviews)

---

**Next Phase**: Phase 2.5 – UX Polish & Documentation
