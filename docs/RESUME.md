# Resume & Crash Recovery in lorch

## Overview

**Resume** is lorch's crash recovery mechanism that allows you to restart an interrupted run and continue from where it left off—without redoing completed work.

Every `lorch run` execution creates persistent state:
- **Run state** (`/state/run.json`) tracks overall progress
- **Event ledger** (`/events/run-XXXX.ndjson`) records all commands and events
- **Snapshot** (`/snapshots/snap-XXXX.manifest.json`) pins the workspace version

If lorch crashes, `lorch resume --run <run-id>` will:
1. Load the run state and ledger
2. Identify which commands haven't completed
3. Restart agents with the same snapshot
4. Continue execution (agents recognize duplicate IKs and skip completed work)

---

## Why Resume Matters

### Without Resume

```
User: lorch run --task T-0042
lorch: Starting build...
builder: Implementing feature X...
[POWER OUTAGE - Machine dies]

User: [restarts machine]
User: lorch run --task T-0042
lorch: Starting NEW run
builder: Implementing feature X AGAIN (wasting time!)
Result: Duplicate work, wasted resources, frustration
```

### With Resume

```
User: lorch run --task T-0042
lorch: Starting run-20251020-140818-abc123...
builder: Implementing feature X...
[POWER OUTAGE - Machine dies]

User: [restarts machine]
User: lorch resume --run run-20251020-140818-abc123
lorch: Loading state... found builder.completed
lorch: Continuing from review stage...
reviewer: Starting review (builder work reused!)
Result: Fast recovery, no duplicate work, progress preserved
```

---

## How Resume Works

### Step-by-Step Flow

#### 1. Initial Run
```bash
$ lorch run --task T-0042
[lorch] Capturing workspace snapshot...
[lorch] Snapshot: snap-d0ab7e60b764
[lorch] Run ID: run-20251020-140818-4f9f29a1
[lorch→builder] implement (IK: ik:abc123...)
[builder] builder.completed
[lorch→reviewer] review (IK: ik:def456...)
[CRASH - Process killed]
```

**State after crash:**
- `/state/run.json` shows status: "running", last stage: "review"
- `/events/run-20251020-140818-4f9f29a1.ndjson` has:
  - Command: implement (IK: ik:abc123...)
  - Event: builder.completed
  - Command: review (IK: ik:def456...)
  - ❌ No terminal event for review command
- `/snapshots/snap-d0ab7e60b764.manifest.json` preserved

#### 2. Resume Execution

```bash
$ lorch resume --run run-20251020-140818-4f9f29a1
[lorch] Loading run state...
[lorch] Run: run-20251020-140818-4f9f29a1
[lorch] Task: T-0042
[lorch] Snapshot: snap-d0ab7e60b764 (pinned)
[lorch] Status: running
[lorch] Stage: review
[lorch] Loading event ledger...
[lorch] Commands: 2, Events: 1, Heartbeats: 3
[lorch] Analyzing pending commands...
[lorch] Found 1 pending command (review)
[lorch] Restarting agents...
[lorch] Resuming task execution...
[lorch→builder] implement (IK: ik:abc123... SAME!)
[builder] Duplicate IK detected, returning cached result
[builder] builder.completed
[lorch→reviewer] review (IK: ik:def456... SAME!)
[reviewer] Executing review...
[reviewer] reviewer.completed
[lorch→spec_maintainer] update_spec (IK: ik:789xyz...)
[spec_maintainer] spec_maintainer.completed
[lorch] Run completed successfully
```

**Key insight**: Builder recognizes `ik:abc123...` and returns cached result immediately. No duplicate work!

---

## The Event Ledger

### What Gets Logged

Every `lorch run` creates an append-only ledger at `/events/<run-id>.ndjson`:

```json
{"kind":"command","message_id":"cmd-001","task_id":"T-0042","action":"implement","idempotency_key":"ik:abc123..."}
{"kind":"heartbeat","message_id":"hb-001","source":"builder","status":"healthy"}
{"kind":"event","message_id":"evt-001","correlation_id":"cmd-001","event_type":"builder.completed"}
{"kind":"command","message_id":"cmd-002","task_id":"T-0042","action":"review","idempotency_key":"ik:def456..."}
```

**Format**: Newline-delimited JSON (NDJSON), one message per line

### Ledger Replay

When you run `lorch resume`, the ledger is replayed to reconstruct state:

```go
func ReadLedger(path string) (*Ledger, error) {
    // Read entire ledger file
    // Parse each line into Command, Event, Heartbeat, or Log
    // Return structured ledger
}
```

**Performance**: Replaying ~1000 ledger entries takes < 100ms

### Terminal Events

A **terminal event** indicates a command has completed. Examples:
- `builder.completed` → Builder finished implementing
- `reviewer.completed` → Reviewer finished reviewing
- `spec_maintainer.completed` → Spec maintainer finished updating

**Pending commands** are commands without terminal events:

```go
func (l *Ledger) GetPendingCommands() []*Command {
    terminals := l.GetTerminalEvents()

    pending := []
    for _, cmd := range l.Commands {
        if _, completed := terminals[cmd.MessageID]; !completed {
            pending = append(pending, cmd)
        }
    }

    return pending
}
```

---

## Run State

### Run State Schema

The file `/state/run.json` tracks the current run:

```json
{
  "run_id": "run-20251020-140818-4f9f29a1",
  "status": "running",
  "task_id": "T-0042",
  "snapshot_id": "snap-d0ab7e60b764",
  "current_stage": "review",
  "started_at": "2025-10-20T14:08:18Z",
  "terminal_events": {
    "builder": "builder.completed"
  }
}
```

### Status Values

| Status | Meaning | Can Resume? |
|--------|---------|-------------|
| `running` | Task in progress | ✅ Yes |
| `completed` | Task finished successfully | ⚠️ No-op (already done) |
| `failed` | Task failed permanently | ❌ No (needs investigation) |
| `aborted` | User aborted run | ❌ No (intentional stop) |

### Stage Values

| Stage | Meaning |
|-------|---------|
| `implement` | Builder working on implementation |
| `review` | Reviewer assessing changes |
| `spec_maintain` | Spec maintainer updating documentation |
| `complete` | All stages finished |

---

## Resume Strategies

### Full Task Re-Execution (Phase 1.3)

**Current implementation**: Resume re-executes the entire task flow from the beginning.

**How it works:**
1. Load snapshot ID from run state
2. Restart scheduler with same snapshot
3. Call `ExecuteTask()` with same task ID and goal
4. Scheduler regenerates commands with **same idempotency keys**
5. Agents recognize duplicate IKs and return cached results

**Advantages:**
- Simple to implement
- Agents handle idempotency transparently
- No complex resume logic needed

**Trade-off:**
- Regenerates already-completed commands (but agents skip them)
- Slight overhead from re-sending commands

**Example:**

```go
// In internal/cli/resume.go
sched := scheduler.NewScheduler(builder, reviewer, specMaintainer, logger)
sched.SetSnapshotID(state.SnapshotID)  // SAME snapshot as original run

// This will regenerate ALL commands, but with same IKs
if err := sched.ExecuteTask(ctx, state.TaskID, task.Goal); err != nil {
    return err
}
```

### Future: Incremental Resume

**Phase 2+ optimization**: Resume from exact failure point.

**How it would work:**
1. Analyze ledger to find last successful stage
2. Skip to that stage directly
3. Only send pending commands

**Advantages:**
- Minimal redundant work
- Faster resume for large tasks

**Complexity:**
- Must handle partial stage completion
- Requires stage-aware resume logic

---

## Using `lorch resume`

### Basic Usage

```bash
# Resume a specific run
lorch resume --run run-20251020-140818-4f9f29a1

# Short flag
lorch resume -r run-20251020-140818-4f9f29a1
```

### Finding Run IDs

**Option 1: Check run state**
```bash
cat state/run.json | jq -r '.run_id'
```

**Option 2: List event logs**
```bash
ls events/
# Output: run-20251020-140818-4f9f29a1.ndjson
```

**Option 3: From lorch output**
```
[lorch] Run ID: run-20251020-140818-4f9f29a1
```

---

## Common Resume Scenarios

### Scenario 1: Power Outage Mid-Build

**Situation**: Machine loses power while builder is working

**State:**
- Run state: status=running, stage=implement
- Ledger: implement command sent, no terminal event

**Resume:**
```bash
lorch resume --run <run-id>
```

**Outcome:**
- Builder restarts
- Receives implement command (same IK)
- If builder had completed before crash: returns cached result
- If builder hadn't finished: re-executes from scratch

### Scenario 2: User Hits Ctrl+C

**Situation**: User interrupts lorch during review

**State:**
- Run state: status=running, stage=review
- Ledger: implement completed, review in progress

**Resume:**
```bash
lorch resume --run <run-id>
```

**Outcome:**
- Builder recognizes completed implement (cached)
- Reviewer resumes review work
- Spec maintainer proceeds normally

### Scenario 3: Agent Crashes

**Situation**: Builder process dies unexpectedly

**State:**
- Run state: status=running
- Ledger: command sent, no heartbeats, no terminal event

**Resume:**
```bash
lorch resume --run <run-id>
```

**Outcome:**
- lorch restarts builder agent
- Re-sends command with same IK
- Builder re-executes (no cached result from crashed process)

**Note**: Phase 1.3 agents don't persist cache across restarts. Future phases may add persistent caching.

### Scenario 4: Resume Already Completed Run

**Situation**: User runs resume on a finished run

**State:**
- Run state: status=completed
- Ledger: All commands have terminal events

**Resume:**
```bash
lorch resume --run <run-id>
[lorch] Run already completed
```

**Outcome:**
- Immediate exit (no-op)
- Run state unchanged

### Scenario 5: Disk Full During Run

**Situation**: Disk fills up while writing artifacts

**State:**
- Run state: May be corrupted
- Ledger: Partial final entry possible

**Troubleshooting:**
1. Free up disk space
2. Check ledger integrity: `tail events/<run-id>.ndjson`
3. If last line is malformed JSON, remove it
4. Run `lorch resume --run <run-id>`

---

## Snapshot Pinning

### Why Snapshots Matter for Resume

Every command includes a `snapshot_id` field:

```json
{
  "kind": "command",
  "action": "implement",
  "version": {
    "snapshot_id": "snap-d0ab7e60b764"
  }
}
```

This ensures:
1. **Version consistency**: Commands reference a specific workspace state
2. **Conflict detection**: If workspace changed, snapshot ID would differ
3. **Safe retries**: Agents can assume inputs haven't changed

### Resume Uses Original Snapshot

```go
// From internal/cli/resume.go
state, err := runstate.LoadRunState(statePath)

// Later...
sched.SetSnapshotID(state.SnapshotID)  // Original snapshot, not new one!
```

**Critical**: Resume MUST use the original snapshot ID. If you captured a new snapshot, commands would have different IKs and agents wouldn't recognize them.

### What If Workspace Changed?

**Q**: What if I edited files after the crash?

**A**: Phase 1.3 doesn't detect this—resume uses the original snapshot ID regardless of current workspace state.

**Implication**: If you made significant changes, you may want to start a new run instead of resuming.

**Future**: Phase 2+ may add snapshot drift detection and offer to restart with new snapshot.

---

## Agent Idempotency Cache

### In-Memory Cache (Phase 1.3)

Agents cache completed work by idempotency key:

```python
# Pseudocode for agent behavior
completed_work = {}  # IK → result mapping

def handle_command(command):
    ik = command['idempotency_key']

    if ik in completed_work:
        log(f"Duplicate IK {ik}, returning cached result")
        return completed_work[ik]

    # Do the work
    result = execute_work(command)

    # Cache by IK
    completed_work[ik] = result

    return result
```

**Limitation**: Cache is lost if agent process dies.

### Future: Persistent Cache

**Phase 2+ enhancement**: Agents persist cache to disk

```
workspace/
  cache/
    builder/
      ik-abc123.json  → Cached result for IK abc123
      ik-def456.json  → Cached result for IK def456
```

**Benefit**: Resume works even if agent crashes (not just lorch orchestrator).

---

## Troubleshooting Resume

### Error: "run state mismatch"

**Problem**: Wrong run ID provided

**Solution:**
```bash
# Check actual run ID in state file
cat state/run.json | jq -r '.run_id'

# Use correct ID
lorch resume --run <correct-run-id>
```

### Error: "run was aborted, cannot resume"

**Problem**: Run was intentionally aborted

**Solution:**
- If abort was a mistake, manually edit `state/run.json` to change status from `aborted` to `running`
- Or start a fresh run: `lorch run --task <task-id>`

### Error: "task T-0042 not found in config"

**Problem**: Task was removed from `lorch.json` after run started

**Solution:**
- Re-add task to `lorch.json`
- Or start new run with a different task

### Error: "failed to read ledger"

**Problem**: Ledger file is missing or corrupted

**Diagnosis:**
```bash
# Check if ledger exists
ls events/<run-id>.ndjson

# Check ledger integrity (should be valid NDJSON)
cat events/<run-id>.ndjson | jq -c . > /dev/null
```

**Solution:**
- If ledger is missing: Cannot resume (start new run)
- If ledger is corrupted: Remove malformed lines at end, then retry resume

### Resume Hangs or Times Out

**Problem**: Agent not responding to duplicate IK

**Diagnosis:**
- Check if agent process started: `ps aux | grep mockagent`
- Check agent stderr for errors
- Verify IK in command matches original: `grep idempotency_key events/<run-id>.ndjson`

**Solution:**
- Increase timeout in resume code (default 30 minutes)
- Check agent implementation for IK handling bugs
- Verify agents are configured correctly in `lorch.json`

### No Pending Commands But Run Not Complete

**Problem**: All commands have terminal events but status is "running"

**Solution:**
```bash
lorch resume --run <run-id>
# lorch will detect no pending commands and mark run complete
```

**Explanation**: Edge case where final state update was lost. Resume handles this gracefully.

---

## When to Resume vs. Start Fresh

### Resume When:
- ✅ Crash/interruption during execution
- ✅ Want to save completed work
- ✅ Workspace hasn't significantly changed
- ✅ Run state is valid

### Start Fresh When:
- ❌ Workspace has major changes since crash
- ❌ Task definition changed
- ❌ Agent configuration changed
- ❌ Run was aborted intentionally
- ❌ Ledger is corrupted
- ❌ You want a clean slate

---

## Resume and Idempotency Keys

Resume relies entirely on **idempotency keys** to avoid duplicate work. See `docs/IDEMPOTENCY.md` for details.

**Key principle**: Same inputs → Same IK → Same cached result

When resume re-executes a task:
1. Scheduler regenerates commands with same task ID, snapshot ID, and inputs
2. Idempotency key generation is deterministic
3. Result: **Exact same IKs** as original run
4. Agents recognize IKs and return cached results

**Example:**

```
# Original run
IK = SHA256("implement\nT-0042\nsnap-d0ab7e60b764\n{...inputs...}")
   = "ik:abc123..."

# Resume (same task, same snapshot, same inputs)
IK = SHA256("implement\nT-0042\nsnap-d0ab7e60b764\n{...inputs...}")
   = "ik:abc123..."  ← IDENTICAL!
```

---

## Testing Resume

### Manual Testing

```bash
# 1. Start a run
lorch run --task T-0042

# 2. Interrupt mid-execution (Ctrl+C)

# 3. Verify state
cat state/run.json | jq '.status'
# Should show: "running"

# 4. Resume
lorch resume --run <run-id>

# 5. Verify completion
cat state/run.json | jq '.status'
# Should show: "completed"
```

### Automated Testing

See `internal/scheduler/crash_test.go` for integration tests:

```go
func TestCrashAndResumeAfterBuilderCompleted(t *testing.T) {
    // 1. Start run, wait for builder.completed
    // 2. Simulate crash (stop agents, close ledger)
    // 3. Load state and ledger
    // 4. Resume execution
    // 5. Verify no duplicate work, correct completion
}
```

**Run tests:**
```bash
go test ./internal/scheduler -v -run TestCrash
```

---

## Implementation Reference

### Key Files

| File | Purpose |
|------|---------|
| `internal/cli/resume.go` | Resume command implementation |
| `internal/runstate/runstate.go` | Run state persistence |
| `internal/ledger/ledger.go` | Ledger replay logic |
| `internal/idempotency/idempotency.go` | IK generation |
| `internal/snapshot/snapshot.go` | Snapshot capture |
| `internal/scheduler/scheduler.go` | Command scheduling with IKs |

### Resume Command Flow

```go
func runResume(cmd *cobra.Command, args []string) error {
    // 1. Load config and workspace
    cfg, cfgPath, err := loadOrCreateConfig(configPath, logger)
    workspaceRoot := determineWorkspaceRoot(cfg, cfgPath)

    // 2. Load run state
    state, err := runstate.LoadRunState(statePath)

    // 3. Verify resumable
    if state.Status == StatusCompleted {
        return nil  // Already done
    }

    // 4. Load ledger
    lg, err := ledger.ReadLedger(ledgerPath)
    pending := lg.GetPendingCommands()

    // 5. If no pending, mark complete
    if len(pending) == 0 {
        state.MarkCompleted()
        return runstate.SaveRunState(state, statePath)
    }

    // 6. Restart agents
    builder.Start(ctx)
    reviewer.Start(ctx)
    specMaintainer.Start(ctx)

    // 7. Resume execution with same snapshot
    sched := scheduler.NewScheduler(...)
    sched.SetSnapshotID(state.SnapshotID)
    sched.ExecuteTask(ctx, state.TaskID, task.Goal)

    // 8. Mark complete
    state.MarkCompleted()
    runstate.SaveRunState(state, statePath)

    return nil
}
```

---

## Advanced Topics

### Concurrent Resumes

**Q**: What if I run `lorch resume` twice simultaneously?

**A**: Phase 1.3 doesn't prevent this. Possible outcomes:
- Both processes fight for agent connections
- Duplicate commands sent
- Undefined behavior

**Recommendation**: Don't do this. Future phases may add run locking.

### Resume Across Machines

**Q**: Can I resume a run on a different machine?

**A**: Theoretically yes, if you copy:
- `/state/run.json`
- `/events/<run-id>.ndjson`
- `/snapshots/<snapshot-id>.manifest.json`
- Workspace files

**Caveats:**
- Agents cache in memory (won't transfer)
- Absolute paths may differ
- Not officially supported in Phase 1.3

### Resume with Config Changes

**Q**: What if I change `lorch.json` between crash and resume?

**A**: Depends on what changed:
- ✅ **Safe**: Changing agent environment variables
- ⚠️ **Risky**: Changing task goals (may cause confusion)
- ❌ **Unsafe**: Removing the task being resumed
- ❌ **Unsafe**: Changing agent commands (may break agent startup)

### Ledger Compaction

**Q**: Do ledgers grow forever?

**A**: Phase 1.3 doesn't compact ledgers. A long run could produce a large ledger file.

**Future**: Phase 2+ may add ledger compaction (remove heartbeats, deduplicate, etc.).

**Workaround**: Completed runs can have their ledgers archived/compressed:
```bash
# After run completes
gzip events/run-20251020-140818-4f9f29a1.ndjson
```

---

## References

- **MASTER-SPEC §5.6**: Crash/Restart Rehydration
- **MASTER-SPEC §5.4**: Idempotency Keys
- **Implementation**: `internal/cli/resume.go`
- **Tests**: `internal/scheduler/crash_test.go`
- **Related Docs**: `docs/IDEMPOTENCY.md`

---

## Summary

**Resume enables crash recovery by**:
1. ✅ Persisting run state and event ledger
2. ✅ Pinning workspace snapshots for version consistency
3. ✅ Using idempotency keys for duplicate detection
4. ✅ Replaying ledgers to identify pending work
5. ✅ Restarting execution with same snapshot ID

**Key Commands**:
- `lorch run --task T-0042` → Start new run
- `lorch resume --run <run-id>` → Resume interrupted run

**Key Concepts**:
- **Run state**: Persistent execution status
- **Event ledger**: Append-only log of all commands/events
- **Terminal events**: Indicators of command completion
- **Snapshot pinning**: Version consistency via snapshot IDs
- **Idempotency keys**: Deterministic command identifiers

**In Practice**:
- Resume is automatic and transparent
- Agents handle duplicate IKs without orchestrator intervention
- Users simply run `lorch resume` after crashes
- No manual recovery steps needed
