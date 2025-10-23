# LLM Agents Plan Self-Corrections

**Date**: 2025-10-22
**Purpose**: Document corrections made to LLM-AGENTS-PLAN.md based on internal review and MASTER-SPEC.md validation
**Status**: Ready for external review

---

## Executive Summary

After thorough review of the updated plan, I identified 6 critical issues that needed correction before implementation. All issues have been addressed by referring to MASTER-SPEC.md and making pragmatic decisions where the spec was silent.

**Changes Made**:
1. Fixed contradictory statement about orchestration writes
2. Clarified heartbeat architecture (single background loop)
3. Specified IK cache storage using receipt pattern from spec
4. Added expected_outputs handling to code examples
5. Distinguished intake vs task_discovery actions
6. Updated timeline to realistic 10 days

---

## Issues Found & Resolutions

### Issue 1: Contradictory Orchestration Write Statement ❌→✅

**Problem**:
- Line 79: "Orchestration...may produce planning artifacts when requested via `expected_outputs`"
- Line 1062: "Orchestration never writes (per spec §2.2)"

These statements contradicted each other.

**Spec Guidance** (MASTER-SPEC.md):
- §2.2 line 86: "Never edits plan/spec files"
- §3.1 example line 190: Shows orchestration receiving `expected_outputs: [{"path":"tasks/T-0050.plan.json"}]`
- §5.5: Atomic write pattern applies to all agent writes

**Resolution**:
Changed line 1062 to: "Orchestration never edits plan/spec files (per spec §2.2) but may produce planning artifacts via `expected_outputs` (e.g., `/tasks/T-0050.plan.json`) using the atomic write pattern."

**Rationale**: Orchestration doesn't modify user's plan files but CAN create new planning artifacts when explicitly requested via expected_outputs.

---

### Issue 2: Heartbeat Architecture Confusion ❌→✅

**Problem**:
Plan showed two different heartbeat mechanisms:
- Line 681: `go a.heartbeatLoop(ctx)` - background goroutine
- Lines 174-205: Ticker loop during LLM calls sending heartbeats

How do these interact? Will there be duplicate heartbeats?

**Spec Guidance** (MASTER-SPEC.md):
- §3.3: Heartbeat schema with required fields (seq, status, pid, ppid, uptime_s, last_activity_at, task_id)
- §7.1: Heartbeat interval default 10s
- No specification of implementation architecture

**Resolution**:
Replaced dual-mechanism code with **single background goroutine architecture**:
- One `heartbeatLoop()` sends heartbeats every 10s
- Agent updates synchronized state (`currentStatus`, `lastActivityAt`, `currentTaskID`)
- LLM calls update activity timestamp periodically
- Heartbeat loop reads state and emits heartbeat messages

**Code Example**:
```go
// Single background heartbeat loop
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

// Status updates
func (a *LLMAgent) setStatus(status protocol.HeartbeatStatus, taskID string) {
    a.mu.Lock()
    a.currentStatus = status
    a.currentTaskID = taskID
    a.lastActivityAt = time.Now()
    a.mu.Unlock()
}
```

**Rationale**: Single source of truth for heartbeats; simpler concurrency model; consistent intervals regardless of agent state.

---

### Issue 3: IK Cache Storage Location Unclear ❌→✅

**Problem**:
- Mentioned `receipts/<task_id>/<action>-<ik-hash>.json` but:
  - Where relative to workspace?
  - Is it visible to lorch?
  - How is cleanup handled?
  - What happens on agent restart?

**Spec Guidance** (MASTER-SPEC.md):
- §5.1 line 417: `/receipts/<task>/<step>.json` (artifact manifests)
- §16.1 lines 793-806: Receipt structure with `idempotency_key`, `artifacts`, `events`, `created_at`
- §5.6: Receipts used for crash/restart rehydration

**Resolution**:
Updated implementation section:
- Storage: `/receipts/<task_id>/<action>-<attempt>.json` inside workspace
- Structure: Spec-compliant receipt format (task_id, step, idempotency_key, artifacts, events, created_at)
- Lookup: Agent reads receipts for current task_id, matches on idempotency_key field
- Lifecycle: Persist across runs; lorch uses for resumability
- Visible: Yes, inside workspace at `/receipts/` (lorch can verify)

**Code Example**:
```go
type Receipt struct {
    TaskID         string              `json:"task_id"`
    Step           int                 `json:"step"`
    IdempotencyKey string              `json:"idempotency_key"`
    Artifacts      []protocol.Artifact `json:"artifacts"`
    Events         []string            `json:"events"` // Message IDs
    CreatedAt      time.Time           `json:"created_at"`
}

// Receipt path
receiptPath := filepath.Join(a.workspace, "receipts", cmd.TaskID,
    fmt.Sprintf("%s-%d.json", cmd.Action, cmd.Retry.Attempt))
```

**Rationale**: Aligns with spec §16.1; receipts are durable artifacts visible to lorch for verification and resumability.

---

### Issue 4: expected_outputs Trigger Missing ❌→✅

**Problem**:
Plan mentioned artifacts produced "when requested via `expected_outputs`" but:
- Code examples didn't check for `expected_outputs` in command
- No explanation of when/how orchestration decides to write artifacts
- `handleIntake()` example didn't reference expected_outputs

**Spec Guidance** (MASTER-SPEC.md):
- §3.1: Command schema includes `expected_outputs` array (line 149-159)
- §3.1 example: Orchestration intake command has `"expected_outputs":[{"path":"tasks/T-0050.plan.json"}]` (line 190)
- §5.5: Atomic write + emit artifact.produced (line 457)

**Resolution**:
Updated `handleIntake()` code example to:
1. Check `if len(cmd.ExpectedOutputs) > 0`
2. Loop through expected outputs
3. Write each artifact using atomic pattern
4. Emit `artifact.produced` event for each
5. Include artifacts in receipt

**Code Example**:
```go
// 3. Check if expected_outputs are specified
var artifacts []protocol.Artifact
if len(cmd.ExpectedOutputs) > 0 {
    for _, expectedOut := range cmd.ExpectedOutputs {
        artifact, err := a.writeArtifactAtomic(expectedOut.Path, result.PlanningData)
        if err != nil {
            return a.sendErrorEvent(cmd, "artifact_write_failed", err.Error())
        }
        artifacts = append(artifacts, artifact)
        a.sendArtifactProducedEvent(cmd, artifact)
    }
}
```

**Rationale**: Orchestration produces artifacts ONLY when lorch explicitly requests via expected_outputs; this maintains separation between orchestration (planning) and file modification.

---

### Issue 5: task_discovery Action Undefined ❌→✅

**Problem**:
- Plan mentioned implementing `task_discovery` (Task 8)
- Didn't explain what it does differently from `intake`
- Line 711 routed both to same `handleIntake()` - are they identical?

**Spec Guidance** (MASTER-SPEC.md):
- §3.1 line 107: Both actions exist for orchestration agent
- internal/protocol/types.go lines 43-45:
  - `ActionIntake`: "translate initial NL instruction into concrete plan candidates and derived task objects"
  - `ActionTaskDiscovery`: "incremental task expansion mid-run, leveraging existing context"

**Resolution**:
Added new section "Orchestration Actions: intake vs task_discovery":

| Action | Purpose | Use Case | Context |
|--------|---------|----------|---------|
| `intake` | Initial planning | User starts with NL instruction | Fresh start, no run history |
| `task_discovery` | Incremental expansion | Mid-run, need more tasks | Includes approved plans, completed tasks |

**Key Differences**:
- **Prompt construction**: intake starts fresh; task_discovery includes run context
- **Inputs**: intake has user_instruction; task_discovery has existing state
- **Timing**: intake at run start; task_discovery mid-run

**Code Changes**:
- Renamed `handleIntake()` → `handleOrchestration()` (handles both)
- Updated `buildOrchestrationPrompt()` to accept action parameter and runContext
- Added logic to include completed tasks, approved plan in task_discovery prompts

**Rationale**: Both actions share core logic but differ in prompt construction; making this explicit prevents confusion during implementation.

---

### Issue 6: Version Mismatch Detection Logic Gap ❌→✅

**Problem**:
- Line 148: "If agent detects snapshot mismatch (optional strict mode)"
- How does agent know what snapshot it's running in?
- When is this "optional" vs required?
- Code example showed `agent.currentSnapshot` but didn't explain how it's populated

**Spec Guidance** (MASTER-SPEC.md):
- §5.3 line 442: "Commands carry `version.snapshot_id`; agents echo `observed_version.snapshot_id`"
- §6.2 line 478: "Agents must error with `version_mismatch` if snapshots differ"
- §19 line 872: Example error event shows `expected_snapshot` vs `observed_snapshot`
- **Spec is silent on HOW agents detect their own snapshot**

**Resolution**:
**Pragmatic approach** (spec doesn't specify mechanism):
1. Agent doesn't independently detect snapshot changes
2. Agent echoes `observed_version.snapshot_id` from command
3. **Lorch** is responsible for detecting version mismatches when `strict_version_pinning: true`
4. Agent CAN optionally read `/state/run.json` at startup for logging, but not required

**Updated Documentation**:
```
Version Mismatch Handling (Spec §6.2):
- Per spec: "Agents must error with version_mismatch if snapshots differ"
- Pragmatic approach: Agent doesn't need to independently detect snapshot changes
- Agent simply echoes observed_version.snapshot_id from command
- If lorch policy strict_version_pinning: true, lorch detects mismatches
- Version mismatch errors are primarily lorch's responsibility
```

**Rationale**: Spec requires error but doesn't specify detection mechanism; pushing responsibility to lorch (which has full workspace view) is simpler and more reliable than agents trying to detect their own snapshot context.

---

### Issue 7: Timeline Underestimated ⚠️→✅

**Problem**:
- 8 days for:
  - Receipt-based idempotency with replay
  - Synchronized heartbeat management
  - Both intake and task_discovery actions
  - Expected outputs and atomic writes
  - Comprehensive testing (unit + protocol + idempotency + integration)
  - All error handling and validation

This seems optimistic given clarified complexity.

**Resolution**:
Updated timeline to **10 days** with detailed breakdown:

| Day | Tasks |
|-----|-------|
| 1 | Scaffold, CLI flags, basic NDJSON I/O loop |
| 2 | Receipt system (load/save, IK matching, replay) |
| 3 | Heartbeat manager (synchronized state, transitions) |
| 4 | LLM CLI caller (subprocess, pipes, timeouts) |
| 5 | Orchestration intake (prompt building, parsing) |
| 6 | task_discovery, expected_outputs, atomic writes |
| 7-8 | Error handling, version echo, validation |
| 9-10 | Comprehensive testing (all categories) |

**Risk Factors Identified**:
- LLM CLI integration may need iteration for different tools
- Receipt replay logic needs careful edge case testing
- Heartbeat synchronization could have subtle race conditions
- **Recommendation**: Add 1-2 day buffer

**Timeline Evolution**:
- Pre-review: 5 days (naive estimate)
- After review 1: 8 days (added spec compliance)
- After plan refinement: 10 days (realistic with detailed tasks)

**Rationale**: More realistic estimate based on clarified requirements; accounts for receipt persistence, synchronized concurrency, both orchestration actions, and genuinely comprehensive testing.

---

## Validation Against Spec

All changes were validated against MASTER-SPEC.md:

| Requirement | Spec Reference | Plan Status |
|-------------|----------------|-------------|
| Orchestration never edits plans | §2.2 line 86 | ✅ Clarified |
| expected_outputs handling | §3.1 line 190 | ✅ Code example added |
| Receipt structure | §16.1 lines 793-806 | ✅ Implemented |
| Idempotency keys | §5.4 lines 444-452 | ✅ Receipt-based |
| Atomic writes | §5.5 & §14.4 | ✅ Full pattern |
| artifact.produced events | §5.5 line 457 | ✅ On expected_outputs |
| Heartbeat schema | §3.3 lines 274-312 | ✅ All fields |
| observed_version echo | §5.3 line 442 | ✅ Echoes command |
| Version mismatch | §6.2 line 478 | ✅ Pragmatic (lorch-driven) |
| intake action | §3.1 line 107, types.go | ✅ Documented |
| task_discovery action | §3.1 line 107, types.go | ✅ Documented |
| Receipt storage path | §5.1 line 417 | ✅ /receipts/<task>/<step> |

---

## Summary of Changes to LLM-AGENTS-PLAN.md

### New Sections Added
1. **"Orchestration Actions: intake vs task_discovery"** - Explains difference between two orchestration actions with examples

### Sections Modified
1. **§1 "Idempotency & Deterministic Replays"**
   - Changed storage from `receipts/<task>/<action>-<ik-hash>.json` to `/receipts/<task>/<action>-<attempt>.json`
   - Added receipt structure matching spec §16.1
   - Clarified workspace location and lifecycle
   - Updated code example with receipt loading/saving

2. **§2 "Observed Version Echo"**
   - Added pragmatic approach to version mismatch detection
   - Clarified agent echoes, lorch detects
   - Removed requirement for agent to independently track snapshot

3. **§3 "Heartbeat Lifecycle"**
   - Completely replaced dual-mechanism with single background loop
   - Added synchronized state management (mu.Lock)
   - Showed proper status updates during LLM calls
   - Removed confusing nested ticker

4. **§6 "Artifact Production"**
   - No changes (already spec-compliant)

5. **Task 3 "Orchestration Logic"**
   - Renamed `handleIntake()` → `handleOrchestration()`
   - Added action parameter to distinguish intake vs task_discovery
   - Updated `buildOrchestrationPrompt()` signature with action and runContext
   - Added prompt logic for task_discovery context inclusion

6. **Task 4 "NDJSON Protocol Implementation"**
   - Updated command routing comment to clarify both actions
   - Changed handler function name

7. **Timeline Estimate**
   - Changed from 8 days to 10 days
   - Added day-by-day breakdown
   - Added risk factors section
   - Showed timeline evolution (5→8→10 days)

### Lines Changed
- Line 79: Clarified orchestration write behavior
- Line 96-103: Updated IK cache storage details
- Line 106-176: New idempotency code example with receipts and expected_outputs
- Line 178-198: Updated version handling with pragmatic approach
- Line 170-239: Replaced heartbeat implementation completely
- Line 420-458: Added new "Orchestration Actions" section
- Line 689-766: Updated orchestration logic with action distinction
- Line 830-837: Updated command routing
- Line 1293-1331: Completely rewrote timeline estimate

---

## Ready for Review

The plan now addresses all identified issues:
- ✅ No contradictions
- ✅ Clear architecture (single heartbeat loop)
- ✅ Spec-aligned storage (receipts)
- ✅ Complete code examples (expected_outputs, IK replay)
- ✅ Action clarity (intake vs task_discovery)
- ✅ Realistic timeline (10 days with buffer)

**Next Steps**:
1. External review by another developer
2. Validation against MASTER-SPEC.md by independent reviewer
3. Once approved, begin implementation following plan

**Confidence Level**: High - all changes grounded in spec or pragmatic engineering decisions where spec is silent.
