# Phase 2.4 Task A: Task Activation Mapping

**Status**: ✅ Complete (2025-10-21)
**Milestone**: Phase 2.4 – Task Activation Pipeline
**Deliverables**: Activation mapping layer, integration test, validation framework

---

## Overview

Task A delivers the activation pipeline that transforms approved intake decisions into concrete, executable tasks with idempotency keys and scheduler integration. This layer bridges the natural language intake (Phase 2.1-2.3) with the existing implement → review → spec-maintainer scheduler (Phase 1).

### What Was Built

**Core Components**:
- `internal/activation` package: Data structures, mapping logic, command building
- Integration test: End-to-end orchestration → builder → reviewer → spec-maintainer validation
- Validation framework: Fail-closed checks for decision status, instructions, plan paths, task titles

**Key Files**:
- `input.go`: `Input`, `DerivedTask`, `Task` structs with metadata helpers
- `prepare.go`: `PrepareTasks()` function with validation and deduplication
- `commands.go`: `BuildImplementCommand()` with idempotency key generation
- `enqueue.go`: `TaskExecutor` interface and `Activate()` function
- `integration_test.go`: Full pipeline test with mock agents
- `activation_test.go`: 13 unit tests covering edge cases

---

## Key Design Decisions

### 1. Interface-Based Executor Design

**Decision**: Use `TaskExecutor` interface instead of concrete `*scheduler.Scheduler`.

**Rationale**:
- Enables unit testing with simple mocks (`recordingExecutor`)
- Supports integration testing with full scheduler
- Future extensibility (distributed executors, custom routing)
- Clear boundary between activation (task preparation) and execution (scheduling)

**Interface**:
```go
type TaskExecutor interface {
    ExecuteTask(ctx context.Context, taskID string, goal string) error
}
```

**Implementation**: `scheduler.Scheduler.ExecuteTask()` runs full implement → review → spec loop.

---

### 2. Three-Layer Data Model

**Decision**: Separate intake data (`Input`), concrete tasks (`Task`), and protocol commands (`Command`).

**Rationale**:
- Clear transformation pipeline: Intake → Activation → Scheduler
- Each layer owns specific concerns:
  - `Input`: Intake decisions, user approvals, clarifications
  - `Task`: Executable work with files, snapshot, correlation
  - `Command`: Wire protocol with idempotency keys, deadlines, retry policy
- Testability: Each transformation isolated and unit-testable

**Flow**:
```
OrchestrationEvent → Input → PrepareTasks() → []Task → BuildImplementCommand() → Command → Scheduler
```

---

### 3. Fail-Closed Validation Philosophy

**Decision**: Reject empty/whitespace strings; require explicit "approved" status.

**Rationale**:
- Prevents accidental activation from incomplete intake flows
- Aligns with MASTER-SPEC human-in-control principle (§1.2)
- Explicit better than implicit for task execution decisions

**Examples**:
```go
// Decision status: Must be exactly "approved"
if strings.TrimSpace(input.DecisionStatus) != "approved" {
    return nil, errDecisionNotApproved
}

// Instruction: Cannot be empty/whitespace
if strings.TrimSpace(input.Instruction) == "" {
    return nil, fmt.Errorf("activation: instruction required")
}
```

**Security**: Path traversal protection with `filepath.Clean()` and `..` detection.

---

### 4. Correlation ID Threading Strategy

**Decision**: Thread intake correlation ID through activation commands with `|` separator.

**Format**:
- Without intake: `corr-activate-{taskID}-{uuid8}`
- With intake: `{intakeCorrelationID}|activate-{uuid8}`

**Rationale**:
- Maintains full event traceability from intake → activation → builder → reviewer → spec-maintainer
- Parseable format for log analysis and debugging
- Supports multi-round intake (intake1 → activate → intake2 → activate chains)

**MASTER-SPEC Compliance**: §6.2 correlation_id per command chain.

---

### 5. Expected Outputs Derivation

**Decision**: Populate `ExpectedOutputs` from `task.Files` rather than leaving `nil`.

**Rationale**:
- Idempotency keys include `canonical_json(expected_outputs)` per MASTER-SPEC §5.4
- Commands declare anticipated artifacts for downstream validation
- Resumability guaranteed: IK remains stable across runs

**Implementation**:
```go
func (t Task) ToExpectedOutputs() []protocol.ExpectedOutput {
    outputs := make([]protocol.ExpectedOutput, 0, len(t.Files))
    for _, path := range t.Files {
        outputs = append(outputs, protocol.ExpectedOutput{
            Path:        path,
            Description: fmt.Sprintf("expected artifact: %s", path),
            Required:    true,
        })
    }
    return outputs
}
```

**Refinement Opportunity**: Task B may enhance descriptions with orchestration metadata.

---

## Integration Points

### For Phase 1 Components

**Scheduler** (`internal/scheduler`):
- Implements `TaskExecutor` interface
- Receives activation tasks via `ExecuteTask()`
- Runs full implement → review → spec-maintainer loop
- No changes required; interface already compatible

**Idempotency** (`internal/idempotency`):
- Reused for command IK generation
- Activation commands follow same IK rules as Phase 1
- Deterministic hashing includes snapshot, inputs, expected outputs

**Protocol** (`internal/protocol`):
- `ExpectedOutput` struct now populated by activation
- `AgentTypeBuilder` command routing works unchanged
- Event handling (builder.completed, review.completed, spec.updated) unchanged

---

### For Phase 2 Components

**Intake** (P2.1-P2.3):
- Produces `orchestration.proposed_tasks` events
- Activation consumes `DerivedTask` payloads
- `system.user_decision` events provide approval context

**Bridge Code Needed** (Task B):
```go
// Example: CLI layer maps intake events → activation.Input
func buildActivationInput(
    intakeEvents []protocol.Event,
    decision *protocol.Event, // system.user_decision
    workspace string,
) (*activation.Input, error) {
    // Extract derived tasks, approved plan, clarifications from events
    // Build Input struct
    // Call activation.PrepareTasks()
}
```

---

### For Future Phases

**Task B – Scheduler Enqueueing**:
- Wire `Activate()` into CLI `lorch run` flow
- Map intake decision events → `activation.Input`
- Handle multi-round `task_discovery` with `AlreadyActivated` tracking

**Task C – Traceability**:
- Pass `Task.ToActivationMetadata()` to receipt writer
- Record intake lineage in `/receipts/<task>/<step>.json`
- Update run state with per-task activation timestamps

**Resume Logic**:
- Ledger replay detects pending activation commands
- `AlreadyActivated` map prevents duplicate work
- Idempotency keys ensure safe re-execution

---

## Testing Strategy

### Unit Tests (13 passing)

**Coverage**:
- Single/multiple task activation
- Order preservation (`ApprovedTaskIDs` sequence)
- Duplicate prevention (`AlreadyActivated` map)
- Zero tasks (no-op)
- Plan file validation (missing file detection)
- Metadata completeness (all fields threaded)
- Command building (IK determinism, field mapping)
- Validation (decision status, instruction, path traversal)

**Example**:
```go
func TestTaskActivationDiscoveryExpansion(t *testing.T) {
    // Scenario: TASK-1 already activated, TASK-2 new
    input := Input{
        ApprovedTaskIDs:  []string{"TASK-1", "TASK-2"},
        AlreadyActivated: map[string]struct{}{"TASK-1": {}},
        // ...
    }
    tasks, err := PrepareTasks(input)
    // Validates: Only TASK-2 returned
}
```

---

### Integration Test

**Test**: `TestActivationEndToEnd`

**Setup**:
- Builds real mock agent binary (`cmd/mockagent`)
- Spawns supervisors for builder, reviewer, spec-maintainer
- Creates scheduler instance with event handler

**Flow**:
1. Build `Input` with approved task
2. Call `PrepareTasks()` → produces `[]Task`
3. Call `Activate(ctx, scheduler, tasks)` → enqueues
4. Scheduler executes full pipeline
5. Validate terminal events:
   - `builder.completed`
   - `review.completed`
   - `spec.updated` or `spec.no_changes_needed`

**Duration**: ~0.8s (subprocess spawn + NDJSON communication)

**Value**: Proves end-to-end orchestration → activation → scheduler → agents integration.

---

### Deferred Tests (22 skipped)

**Appropriate Scope**: The following tests are correctly deferred to Task B/C:

- **Execution Ordering** (EO-001–006): Scheduler behavior under review/spec loops (tested in Phase 1.4)
- **Traceability** (TR-001–004): Receipt/run state metadata wiring (Task C)
- **Resume** (RS-001–004): Crash recovery flows (Task B integration)
- **Intake Interaction** (INT-001–003): Multi-round intake flows (Task B)
- **Error Handling** (ERR-001–004): Agent failure surfacing (delegated to scheduler)
- **Performance** (PERF-001): Large task set optimization (post-Phase 2)

---

## Validation Framework

### Checks Implemented

1. **Decision Status**: Must be exactly "approved" (whitespace trimmed)
2. **Instruction**: Cannot be empty or whitespace-only
3. **Plan Path**: Must exist on filesystem, no path traversal (`..` or absolute paths)
4. **Derived Task Titles**: Cannot be empty or whitespace-only
5. **Snapshot ID**: Required (command building validation)
6. **Task ID**: Required (command building validation)

### Security Considerations

**Path Traversal Protection**:
```go
relPlan := filepath.Clean(input.ApprovedPlan)
if filepath.IsAbs(relPlan) || strings.HasPrefix(relPlan, "..") {
    return nil, fmt.Errorf("activation: approved plan path escapes workspace")
}
```

**Defensive Copying**:
- All slice fields defensively copied: `append([]string(nil), input.Clarifications...)`
- Prevents mutation of shared intake data

**Fail-Closed Philosophy**:
- Empty strings rejected, not treated as valid
- Missing fields caught before command building
- Clear error messages with context

---

## Lessons Learned

### What Worked Well

1. **Interface-Based Design**: `TaskExecutor` interface enabled isolated unit testing while supporting real scheduler integration.

2. **Tests-First Approach**: Writing unit tests before implementation caught edge cases early (empty decision status, path traversal).

3. **Incremental Review**: Initial review identified 7 critical issues; all resolved in ~2 hours with focused changes.

4. **Defensive Programming**: Nil checks, defensive copying, path validation prevented entire classes of bugs.

5. **Integration Test Value**: End-to-end test caught scheduler wiring assumption that unit tests missed.

### Improvements Made During Development

**Initial Implementation → Final**:
- Decision validation: `!= ""` → `TrimSpace() != "approved"` (fail-closed)
- Expected outputs: `nil` → populated from `task.Files` (idempotency correctness)
- Correlation IDs: Random → threaded from intake (traceability)
- Path validation: None → traversal protection (security)
- Test coverage: 10 tests → 13 tests + integration (completeness)

### Design Trade-Offs

**Trade-Off 1: Generic vs. Specific Executor**
- **Chose**: Generic `TaskExecutor` interface
- **Cost**: Slightly more abstract; requires type assertions for scheduler-specific features
- **Benefit**: Testability, extensibility, clear boundaries

**Trade-Off 2: Expected Outputs Now vs. Later**
- **Chose**: Populate from `task.Files` immediately
- **Cost**: Generic descriptions (`"expected artifact: {path}"`)
- **Benefit**: Idempotency keys stable, no future migration needed

**Trade-Off 3: Validation Strictness**
- **Chose**: Fail-closed (reject empty/whitespace)
- **Cost**: More verbose intake code (must always set fields)
- **Benefit**: Prevents silent failures, clear error messages

---

## MASTER-SPEC Alignment

### Requirements Met

| Requirement | Section | Implementation |
|-------------|---------|----------------|
| Idempotency keys include snapshot | §5.4 | `Command.Version.SnapshotID` |
| Idempotency keys include expected outputs | §5.4 | `ToExpectedOutputs()` populated |
| Task IDs stable per scenario | §6.2 | Preserved from intake |
| Correlation IDs chain | §6.2 | `{intake}\|activate-{uuid}` |
| Duplicate prevention | §5.4 | `AlreadyActivated` map |
| Implement → Review → Spec loop | §4.2 | Scheduler integration |
| Command timeout: 10 minutes | §18 | `DefaultCommandTimeout` |
| Command retry: 3 attempts | §18 | `Retry.MaxAttempts = 3` |

### Human-in-Control Principle (§1.2)

**Compliance**:
- Activation only proceeds for `DecisionStatus == "approved"`
- Empty/ambiguous decisions rejected
- Plan file must exist (fail-fast on missing context)
- Traceability maintained (intake correlation threaded)

---

## File Inventory

### New Packages
- `internal/activation/` (6 files)

### New Files
| File | Lines | Purpose |
|------|-------|---------|
| `doc.go` | 4 | Package documentation |
| `input.go` | 92 | Data structures, metadata helpers |
| `prepare.go` | 90 | Mapping logic, validation |
| `commands.go` | 55 | Protocol command building |
| `enqueue.go` | 23 | Executor interface, sequential activation |
| `activation_test.go` | 391 | Unit tests, test scaffolding |
| `integration_test.go` | 151 | End-to-end pipeline test |

**Total New Code**: ~800 lines (excluding tests: ~400 lines)

---

## Dependencies

### Internal Packages Used
- `internal/protocol`: Command/Event structs, agent types, constants
- `internal/idempotency`: IK generation (`GenerateIK()`)
- `internal/scheduler`: TaskExecutor implementation
- `internal/supervisor`: Agent lifecycle (integration test)

### External Packages
- `github.com/google/uuid`: Message/correlation IDs
- `github.com/stretchr/testify/require`: Test assertions

---

## Next Steps

### Task B: Scheduler Enqueueing

**Objective**: Wire activation into `lorch run` CLI flow.

**Work Items**:
1. CLI layer: Map intake events → `activation.Input`
2. Call `PrepareTasks()` after `system.user_decision`
3. Call `Activate(ctx, scheduler, tasks)` to enqueue
4. Handle multi-round `task_discovery`:
   - Track activated tasks in run state
   - Pass `AlreadyActivated` map on subsequent approvals
5. Integration test: Full NL instruction → approval → execution flow

**Integration Points**:
- `cmd/lorch/run.go`: Add activation call after intake approval
- `internal/runstate`: Persist activated task IDs for resume
- `internal/scheduler`: Already compatible via `TaskExecutor`

---

### Task C: Traceability Wiring

**Objective**: Flow activation metadata into receipts, run state, event logs.

**Work Items**:
1. Receipt metadata: Wire `Task.ToActivationMetadata()` into receipt writes
2. Run state: Record intake correlation, approved plan, clarifications per task
3. Event log: Add activation context to command/event entries
4. Tests: TR-001–004 validate lineage tracking

**Data Flow**:
```
Task.ToActivationMetadata() → {
    "intake_run_id": "...",
    "approved_plan": "PLAN.md",
    "approved_task_id": "TASK-1",
    "clarifications": [...],
    "conflict_resolutions": [...]
}
→ /receipts/<task>/<step>.json
→ /state/run.json
→ /events/<run>.ndjson
```

---

## References

- **PLAN.md**: Phase 2.4 Task A requirements
- **MASTER-SPEC.md**: §3.1 (Commands), §4.2 (Execution Flow), §5.4 (Idempotency), §6.2 (Correlation)
- **Phase 1.4**: Builder test enforcement, spec loop closure
- **Phase 2.1-2.3**: Intake foundations, approval loop

---

## Conclusion

Phase 2.4 Task A delivers a production-ready activation pipeline that bridges natural language intake with deterministic task execution. The interface-based design, comprehensive validation, and integration test provide a solid foundation for Task B (scheduler enqueueing) and Task C (traceability wiring).

**Key Achievement**: End-to-end flow from user instruction → orchestration → activation → scheduler → agents now validated with passing integration test.

**Ready For**: Task B implementation can begin immediately; all dependencies resolved.
