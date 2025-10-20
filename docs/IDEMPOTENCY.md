# Idempotency in lorch

## Overview

**Idempotency** is the property that an operation can be applied multiple times without changing the result beyond the initial application. In `lorch`, idempotency is critical for **crash recovery** and **reliable distributed execution**.

Every command sent to an agent carries an **idempotency key (IK)** that uniquely identifies the work to be done. If `lorch` crashes and resumes, it may resend the same command—but the agent recognizes the duplicate IK and returns the cached result instead of redoing the work.

---

## Why Idempotency Matters

### Without Idempotency
```
lorch sends: implement task T-0042
agent starts: writing code...
[CRASH - lorch dies]
lorch resumes: sends implement task T-0042 again
agent starts: writing code AGAIN (duplicate work!)
Result: Conflicting changes, wasted time, inconsistent state
```

### With Idempotency
```
lorch sends: implement task T-0042 (IK: ik:abc123...)
agent starts: writing code...
[CRASH - lorch dies]
lorch resumes: sends implement task T-0042 (IK: ik:abc123... SAME!)
agent checks IK: "I've seen this before"
agent returns: cached result from first attempt
Result: No duplicate work, consistent state, fast recovery
```

---

## Idempotency Keys (IK)

### Format

```
ik:<64-hex-characters>
```

**Example**: `ik:7a3f8e9c2d1b5a6e4f7c8d9e0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c`

### Generation Algorithm

Per MASTER-SPEC §5.4, an IK is computed as:

```
IK = "ik:" + SHA256(
  action + '\n' +
  task_id + '\n' +
  snapshot_id + '\n' +
  canonical_json(inputs) + '\n' +
  canonical_json(expected_outputs)
)
```

**Components**:
- **action**: The command type (`implement`, `review`, `update_spec`, etc.)
- **task_id**: The task identifier (e.g., `T-0042`)
- **snapshot_id**: The workspace snapshot ID (e.g., `snap-d0ab7e60b764`)
- **inputs**: Command parameters (JSON object)
- **expected_outputs**: Expected artifact paths (JSON array)

**Critical**: `canonical_json` ensures deterministic serialization by recursively sorting all map keys.

---

## Implementation Details

### Canonical JSON

Standard `encoding/json` in Go doesn't guarantee key ordering in maps. We implement **canonical JSON** that:

1. **Recursively sorts all map keys** lexicographically
2. **Preserves array ordering** (arrays are not sorted)
3. **Uses compact format** (no extra whitespace)

**Example**:
```go
// Input (arbitrary order)
map[string]any{
    "z_param": "value",
    "a_param": 123,
    "m_param": []string{"x", "y"},
}

// Canonical JSON (sorted keys)
{"a_param":123,"m_param":["x","y"],"z_param":"value"}
```

This ensures that two logically identical maps always produce the same JSON bytes, which is essential for deterministic IK generation.

### IK Properties

1. **Deterministic**: Same inputs always produce the same IK
2. **Collision-resistant**: SHA256 makes collisions astronomically unlikely
3. **Verifiable**: Anyone can recompute an IK from the command
4. **Opaque**: The IK doesn't expose sensitive data (it's a hash)

---

## What Changes the IK?

### Changes That Produce Different IKs

```go
// Different action
implement  → ik:abc123...
review     → ik:def456...  // DIFFERENT

// Different task
T-0042 → ik:abc123...
T-0043 → ik:def456...  // DIFFERENT

// Different snapshot
snap-v1 → ik:abc123...
snap-v2 → ik:def456...  // DIFFERENT

// Different inputs
{"goal": "A"} → ik:abc123...
{"goal": "B"} → ik:def456...  // DIFFERENT
```

### Changes That DON'T Affect IK

```go
// Different message ID (ephemeral)
message_id: "msg-001" → ik:abc123...
message_id: "msg-002" → ik:abc123...  // SAME

// Different correlation ID
correlation_id: "corr-1" → ik:abc123...
correlation_id: "corr-2" → ik:abc123...  // SAME

// Different deadline
deadline: T+10min → ik:abc123...
deadline: T+20min → ik:abc123...  // SAME

// Different priority
priority: 5 → ik:abc123...
priority: 10 → ik:abc123...  // SAME
```

**Rationale**: Only the **actual work content** affects the IK. Ephemeral metadata (message IDs, deadlines, priorities) can change without invalidating cached results.

---

## Agent Responsibilities

### Recognizing Duplicate IKs

When an agent receives a command, it should:

1. **Extract the IK** from the command
2. **Check if it has seen this IK before**
3. **If yes**: Return the cached result (no re-execution)
4. **If no**: Execute the command and cache the result by IK

### Example Agent Logic

```python
def handle_command(command):
    ik = command['idempotency_key']

    # Check cache
    if ik in completed_work:
        log(f"Duplicate IK {ik}, returning cached result")
        return completed_work[ik]

    # Do the work
    result = execute_work(command)

    # Cache by IK
    completed_work[ik] = result

    return result
```

### Caching Strategy

**Phase 1.3 Recommendation**:
- **In-memory cache** during agent lifetime
- **Persist to disk** for cross-restart persistence (optional)
- **Cache invalidation**: Clear cache when workspace snapshot changes

---

## Crash Recovery Flow

### Step-by-Step

1. **Normal Execution**:
   ```
   lorch → agent: command (IK: ik:abc123...)
   agent: executes work
   agent → lorch: result + artifacts
   lorch: logs to ledger
   ```

2. **Crash Occurs**:
   ```
   [lorch process terminates unexpectedly]
   ```

3. **Resume**:
   ```
   lorch resume --run <run-id>
   lorch: loads /state/run.json
   lorch: replays /events/run-XXXX.ndjson
   lorch: identifies pending commands
   lorch: restarts agents
   lorch → agent: command (IK: ik:abc123... SAME!)
   agent: "I've seen this IK"
   agent → lorch: cached result (no re-execution)
   lorch: continues from where it left off
   ```

### Key Insight

Because the IK is deterministic, `lorch` will regenerate the **exact same IK** when it resumes. The agent recognizes it and avoids duplicate work.

---

## Testing Idempotency

### Unit Test Pattern

```go
func TestIdempotencyKeyDeterminism(t *testing.T) {
    cmd1 := makeCommand("T-0042", "implement", inputs)
    cmd2 := makeCommand("T-0042", "implement", inputs)

    ik1, _ := idempotency.GenerateIK(cmd1)
    ik2, _ := idempotency.GenerateIK(cmd2)

    if ik1 != ik2 {
        t.Error("IKs should be identical for same inputs")
    }
}
```

### Integration Test Pattern

```go
func TestCrashAndResume(t *testing.T) {
    // Start run
    runFirstHalf()
    simulateCrash()

    // Resume
    resumeRun()

    // Verify: no duplicate work, correct final state
    verifyLedger()
    verifyArtifacts()
}
```

---

## Common Pitfalls

### ❌ Non-Deterministic Inputs

```go
// BAD: Timestamp in inputs
inputs := map[string]any{
    "goal": "implement X",
    "started_at": time.Now(), // CHANGES EVERY TIME!
}
// Result: Different IK on resume, duplicate work
```

```go
// GOOD: Deterministic inputs
inputs := map[string]any{
    "goal": "implement X",
    "spec_version": "v1.2.3",
}
// Result: Same IK on resume, idempotent
```

### ❌ Unsorted Maps

```go
// BAD: Using standard json.Marshal
data, _ := json.Marshal(map[string]any{"b": 2, "a": 1})
// May produce: {"b":2,"a":1} or {"a":1,"b":2}
// Result: Non-deterministic IK
```

```go
// GOOD: Using canonical JSON
data, _ := idempotency.CanonicalJSON(map[string]any{"b": 2, "a": 1})
// Always produces: {"a":1,"b":2}
// Result: Deterministic IK
```

### ❌ Including Ephemeral Data

```go
// BAD: Including message ID in IK calculation
ik := hash(action + taskID + messageID)
// Result: Different IK every time, cache never hits
```

```go
// GOOD: Only including work content
ik := hash(action + taskID + snapshotID + inputs)
// Result: Same IK for same work, cache effective
```

---

## Advanced Topics

### Snapshot Pinning

Every IK includes the **snapshot ID**. This ensures that:

1. **Version consistency**: Commands reference a specific workspace state
2. **Conflict detection**: If workspace changes, new snapshot → new IKs
3. **Safe retries**: Agents can assume the snapshot hasn't changed

### Expected Outputs

The `expected_outputs` field in commands lists anticipated artifacts:

```json
{
  "expected_outputs": [
    {"path": "src/main.go", "required": true},
    {"path": "tests/main_test.go", "required": true}
  ]
}
```

Including this in the IK ensures that **changing output expectations** produces a new IK, triggering re-execution.

### IK Collisions

**Q**: What if two different commands produce the same IK?

**A**: With SHA256, the probability of a collision is ~2^-256 (effectively zero). You're more likely to experience hardware failure than an IK collision.

**If paranoid**: Use SHA512 instead (just change the hash algorithm in `internal/idempotency`).

---

## References

- **MASTER-SPEC §5.4**: Idempotency Keys specification
- **MASTER-SPEC §5.5**: Deterministic Writes
- **MASTER-SPEC §5.6**: Crash/Restart Rehydration
- **Implementation**: `internal/idempotency/idempotency.go`
- **Tests**: `internal/idempotency/idempotency_test.go`
- **Scheduler Integration**: `internal/scheduler/scheduler.go` (makeCommand)

---

## Summary

**Idempotency Keys Enable**:
- ✅ Crash recovery without duplicate work
- ✅ Safe command retries
- ✅ Distributed execution (future)
- ✅ Audit trails (every command has unique IK)

**Key Principles**:
1. IKs are deterministic (same inputs → same IK)
2. IKs include only work content (not ephemeral metadata)
3. Agents cache results by IK
4. Canonical JSON ensures determinism

**In Practice**:
- `lorch` generates IKs automatically
- Agents handle duplicate IKs transparently
- Users benefit from crash resilience without manual intervention
