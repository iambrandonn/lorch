# Phase 2.4 Test Plan — Task Activation Pipeline

**Goal**: Validate that lorch correctly transforms approved intake decisions into executable tasks, drives the builder→reviewer→spec-maintainer pipeline, and preserves traceability back to NL intake decisions.

---

## 1. Scope & Objectives

1. **Task Activation**  
   Ensure approved tasks from intake are converted into actionable scheduler entries with stable IDs, snapshots, and idempotency keys.
2. **Execution Ordering**  
   Enforce implement → review → spec-maintenance sequencing for each activated task, including iterative change-request loops.
3. **Traceability & Receipts**  
   Persist linkage between activated tasks and their intake origin across receipts, events, and run state.
4. **Resilience & Resume**  
   Support crash/restart scenarios after task activation begins without duplicating work.
5. **Integration With Existing Features**  
   Confirm that heartbeat monitoring, idempotency, and ledger persistence continue to operate correctly once activation integrates with the broader pipeline.

---

## 2. Test Matrix

| ID | Area | Scenario | Type |
|----|------|----------|------|
| TA-001 | Task Activation | Single approved task translates into one scheduler job | Unit |
| TA-002 | Task Activation | Multiple approved tasks map to sequential jobs preserving intake order | Unit |
| TA-003 | Task Activation | Task discovery add-on produces additional jobs with new intake metadata | Unit |
| TA-004 | Task Activation | Duplicate approvals (resume after success) do not enqueue twice | Unit |
| TA-005 | Task Activation | Zero approved tasks detected; activation skips scheduler interaction | Unit |
| TA-006 | Task Activation | Plan file missing/corrupted during activation triggers clear failure | Integration |
| TA-007 | Task Activation | Activated task carries complete metadata (instruction, plan, clarifications) | Unit |
| EO-001 | Execution Ordering | Builder→Reviewer→Spec pipeline runs once for happy path | Integration |
| EO-002 | Execution Ordering | Reviewer change request triggers builder implement_changes loop | Integration |
| EO-003 | Execution Ordering | Spec maintainer change request triggers full round-trip loop | Integration |
| EO-004 | Execution Ordering | Sequential processing for multiple activated tasks | Integration |
| EO-005 | Execution Ordering | Snapshot/version mismatch surfaces error per spec §6.2 | Integration |
| EO-006 | Execution Ordering | Builder test enforcement rejects missing/failing test payloads | Integration |
| TR-001 | Traceability | Receipts include `intake_id`, `approved_plan`, and `approved_task_id` metadata | Unit |
| TR-002 | Traceability | Run state stores per-task lineage for resume/reporting | Unit |
| TR-003 | Traceability | Events NDJSON captures task activation command with correct correlation/IK | Integration |
| TR-004 | Traceability | Clarifications/conflict resolutions propagate into task commands | Unit |
| RS-001 | Resume | Crash between activation and builder completion resumes without duplicate builder work | Integration |
| RS-002 | Resume | Crash after reviewer change-request retains pending tasks + lineage | Integration |
| RS-003 | Resume | Intake resume followed by activation resume (stacked) recovers both layers | Integration |
| RS-004 | Resume | State transitions remain valid across crash points | Unit / Integration |
| INT-001 | Intake Interaction | Denied intake produces no activation entries | Unit |
| INT-002 | Intake Interaction | Partial approval (subset of tasks) activates only approved tasks | Unit |
| INT-003 | Intake Interaction | “More options” flow adds new tasks while preserving prior approvals | Integration |
| ERR-001 | Error Handling | Builder failure surfaces error, marks task failed, retains trace metadata | Integration |
| ERR-002 | Error Handling | Reviewer or spec-maintainer failure surfaces error without corrupting queue | Integration |
| ERR-003 | Error Handling | Heartbeat timeout during activation aborts task and leaves resumable state | Integration |
| ERR-004 | Error Handling | User abort (SIGINT) during activation leaves resumable state | Integration |
| PERF-001 | Performance | Large approved task set (20+) activates without degradation | Integration (optional) |

---

## 3. Detailed Test Cases

### TA-001 — Single Task Activation
- **Given** a recorded intake decision with one approved task  
- **When** the activation pipeline runs  
- **Then** the scheduler receives one task with:
  - `TaskID` = approved task ID (e.g., `PLAN-1`)
  - Snapshot ID identical to intake snapshot
  - Command action `implement` and IK derived from intake lineage  
- **Validation**: Assert scheduler queue length, task metadata, IK format  
- **Type**: Unit (mock scheduler)

### TA-002 — Multiple Task Activation Order
- Intake decision includes tasks `[PLAN-1, PLAN-2, PLAN-3]`
- Activation should enqueue them in the order approved
- Validate `runstate` tracks pending tasks and scheduler enqueues sequentially
- Type: Unit

### TA-003 — Task Discovery Expansion
- Simulate initial approval of `[PLAN-1]`, later `task_discovery` adds `[PLAN-4]`
- Ensure activation appends new task with `discovery_id` metadata while preserving processed tasks
- Type: Unit

### TA-004 — Duplicate Activation Guard
- Simulate resume after activation already enqueued tasks  
- Re-run activation; no new scheduler entries should appear (IK reuse)  
- Type: Unit

### TA-005 — Zero Approved Tasks
- Intake decision returns no approved tasks (user declined all)
- Activation should log and exit without touching scheduler
- Run state should mark activation phase complete immediately
- Type: Unit

### TA-006 — Plan File Parsing Failure
- Remove or corrupt approved plan file before activation
- Activation should surface descriptive error, mark run failed, retain intake lineage
- Verify no scheduler commands issued and state/ledger capture failure
- Type: Integration

### TA-007 — Task Metadata Completeness
- Construct intake decision with instruction, clarifications, conflicts
- Activation command should include all metadata in its payload/headers
- Receipts should mirror same metadata for traceability
- Type: Unit

---

### EO-001 — End-to-End Happy Path
- Use fixture agents (`claude-fixture`) for builder/reviewer/spec-maintainer  
- Activation enqueues single task  
- Verify event order: `implement` → `builder.completed` → `review` → `review.completed` (approved) → `update_spec` → `spec.updated`  
- Ensure receipts/artifacts recorded  
- Type: Integration

### EO-002 — Reviewer Change Request Loop
- Reviewer fixture emits `changes_requested` once  
- Activation must enqueue builder `implement_changes`, rerun review until approved  
- Validate final approval recorded and no extra activation jobs remain  
- Type: Integration

### EO-003 — Spec Maintainer Change Request Loop
- Spec fixture emits `spec.changes_requested`, requiring builder/reviewer cycle before final approval  
- Confirm runner schedules implement_changes → review → update_spec sequence automatically  
- Type: Integration

### EO-004 — Sequential Task Execution
- Two tasks `[PLAN-1, PLAN-2]`  
- Ensure builder completes `PLAN-1` before `PLAN-2` implement command is sent  
- Validate scheduler queue respects FIFO semantics and run state updates per task  
- Type: Integration

### EO-005 — Snapshot Version Mismatch
- Mutate workspace after intake approval to change snapshot contents
- Activation should detect mismatch and emit `version_mismatch` error per MASTER-SPEC §6.2
- Confirm run state records failure and no tasks proceed
- Type: Integration

### EO-006 — Builder Test Enforcement Integration
- Builder fixture omits or fails test payload on completion
- Activation must reject completion, surface error, and require builder retry
- Ensures Phase 1 test enforcement still applies through activation layer
- Type: Integration

---

### TR-001 — Receipt Metadata
- After activation, receipts should include:
  - `intake_run_id`
  - `approved_plan`
  - `approved_task_id`
- Verify via JSON inspection of `/receipts/...`  
- Type: Unit (generate receipt, inspect struct)

### TR-002 — Run State Lineage
- `runstate` should record task list with associated approved plan/tasks  
- After completion, ensure state persists lineage for audit  
- Type: Unit

### TR-003 — Event Log Traceability
- Inspect `/events/run-*.ndjson` to confirm activation command (`action: implement`) includes `correlation_id`, `idempotency_key`, and metadata referencing intake  
- Type: Integration

### TR-004 — Clarification & Conflict Metadata Propagation
- Intake captured clarifications/conflict resolutions
- Activation commands should include this context for downstream agents
- Validate via command payload and scheduler mocks
- Type: Unit

---

### RS-001 — Resume After Builder Start
- Crash after `implement` command sent but before `builder.completed`  
- Resume should resend `implement` with same IK and avoid duplicate queue entries  
- Type: Integration

### RS-002 — Resume After Reviewer Change Request
- Crash while awaiting builder response to `review` change request  
- Resume should pick up pending `implement_changes` without re-enqueuing completed tasks  
- Type: Integration

### RS-003 — Stacked Intake + Activation Resume
- Crash after intake resume but before activation completes  
- Ensure combined resume flows: NL intake resumes first, then activation resumes pending tasks  
- Type: Integration

### RS-004 — Run State Transition Validation
- Inject crashes at intake completion, post-activation enqueue, and mid-execution
- After each resume, verify `runstate.CurrentStage` reflects accurate stage and no invalid transitions occur
- Type: Unit / Integration

---

### INT-001 — Denied Intake
- Intake decision with status `denied`  
- Activation should detect and skip task activation altogether  
- Validate scheduler remains empty  
- Type: Unit

### INT-002 — Partial Approval
- Intake approves subset of derived tasks  
- Activation should only enqueue approved subset  
- Type: Unit

### INT-003 — Additional Discovery Tasks
- Intake approves `[PLAN-1]`, later approves `[PLAN-2]` via discovery  
- Activation should add new tasks without disrupting completed ones  
- Type: Integration

---

### ERR-001 — Builder Failure Handling
- Fixture builder emits `error` event  
- Activation should mark task failed, record error in run state, and stop pipeline  
- Verify no further reviewer/spec commands sent  
- Type: Integration

### ERR-002 — Reviewer/Spec Failure
- Reviewer or spec-maintainer emits error  
- Activation should surface error and leave task in resumable state  
- Type: Integration

### ERR-003 — Heartbeat Timeout During Activation
- Simulate builder heartbeat timeout  
- Activation should abort task with timeout error, maintaining lineage  
- Type: Integration

### ERR-004 — Abort/Cancel During Activation
- Trigger SIGINT (or simulated cancel) while activation pipeline is running
- Ensure clean shutdown: no orphaned child processes, resumable state preserved
- On resume, pipeline continues from last safe point
- Type: Integration

---

### PERF-001 — Large Task Set Performance (Optional)
- Approve ≥20 tasks to stress queueing logic
- Measure runtime/memory to ensure no significant degradation
- Verify ordering and metadata integrity for all tasks
- Type: Integration (optional)

---

## 4. Supporting Utilities & Fixtures

- **Fixtures**: Extend `testdata/fixtures` with activation scenarios (happy path, reviewer change request, spec change request, builder failure, heartbeat timeout).  
- **Helpers**:
  - Mock scheduler for unit tests (assert enqueued tasks/metadata)
  - Resume harness to simulate process crash (write ledger/state, restart)
  - Receipt loader for metadata assertions

---

## 5. Acceptance Criteria

1. All unit tests pass (`go test ./internal/activation ./internal/runstate ...` as applicable).  
2. All integration tests pass using fixture agents (`go test ./internal/cli -run Activation`).  
3. No regressions in existing Phase 2.3 tests (`go test ./internal/cli`).  
4. Documentation updated with test results summary post-implementation.

---

## 6. Spec Clarifications (Resolved)

1. **Discovery candidate confidence** – Spec only requires confidence in orchestration outputs (`MASTER-SPEC.md:259-262`); activation metadata needs the approved plan but not the score. Tests should treat confidence propagation as optional.
2. **Conflict resolution storage** – FAQ (§20) states conflicts must be surfaced and recorded as `system.user_decision`; no per-task storage is mandated. Ledger/state verification of that event is sufficient.
3. **Denied approvals** – Process flow (§4.1) treats user denial as a failure/abort. Run state should transition to `aborted` with no activation work queued. Tests must assert the aborted state and empty scheduler.

---

## 7. Test Count Summary

- **Core scenarios** (Table above) = 30 planned tests  
- **High-priority additions (TA-005, TA-006, EO-005, ERR-004, EO-006)** included  
- **Medium-priority additions (TR-004, TA-007, RS-004)** included  
- **Optional** performance test listed for later phases

---

**Prepared For Review**: 2025-10-21  
**Author**: Builder Agent
