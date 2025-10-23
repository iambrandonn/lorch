# LLM Agents Plan — Spec Compliance Review (Review 3)

**Documents reviewed**: `docs/development/LLM-AGENTS-PLAN.md`, `docs/development/llm-agents-plan-review-2-response.md`
**Primary reference**: `MASTER-SPEC.md`
**Status**: Changes requested (final polish)
**Date**: 2025-10-23

---

## Executive Summary

The plan is strong and nearly spec‑tight. This pass lists final polish items to ensure strict adherence to `MASTER-SPEC.md` and robust behavior in edge cases: heartbeat envelope compliance, complete event fields, explicit `needs_clarification` payload, proper `log` envelope, optional IK index for O(1) replays, artifact and event size guards, deterministic ordering, and secret redaction in logs.

---

## Required Changes

1) Heartbeat envelope compliance (Spec §3.3)
- Ensure heartbeats use the `heartbeat` envelope (not `event`), with all required fields: `agent`, `seq`, `status`, `pid`, `ppid`, `uptime_s`, `last_activity_at`, optional `stats`, and `task_id`.

```go
func (a *LLMAgent) sendHeartbeat(status protocol.HeartbeatStatus, taskID string) error {
    a.hbSeq++
    hb := protocol.Heartbeat{
        Kind: "heartbeat",
        Agent: protocol.AgentRef{ AgentType: a.agentType, AgentID: a.agentID },
        Seq: a.hbSeq,
        Status: string(status),
        Pid: os.Getpid(),
        Ppid: os.Getppid(),
        UptimeS: time.Since(a.startTime).Seconds(),
        LastActivityAt: time.Now().UTC().Format(time.RFC3339),
        TaskID: taskID,
        // Stats optional
    }
    return a.encoder.Encode(hb)
}
```

2) Event completeness: `message_id` and `occurred_at` (Spec §3.2)
- Standardize an event builder that always sets `message_id`, `occurred_at`, and `observed_version.snapshot_id`.

```go
func (a *LLMAgent) newEvent(cmd *protocol.Command, name string) protocol.Event {
    return protocol.Event{
        Kind:          protocol.MessageKindEvent,
        MessageID:     uuid.New().String(),
        CorrelationID: cmd.CorrelationID,
        TaskID:        cmd.TaskID,
        From:          protocol.AgentRef{AgentType: a.agentType, AgentID: a.agentID},
        Event:         name,
        ObservedVersion: &protocol.Version{ SnapshotID: cmd.Version.SnapshotID },
        OccurredAt:    time.Now().UTC(),
    }
}
```

3) Explicit `orchestration.needs_clarification` payload (Spec §3.2)
- Define payload shape and include example.

```json
{"kind":"event","event":"orchestration.needs_clarification","payload":{
  "questions":[
    "Which plan file should be used (PLAN.md vs docs/plan_v2.md)?",
    "Should we implement phases A and B together or separately?"
  ],
  "notes":"Ambiguous instruction; multiple plausible interpretations"
}}
```

4) Proper `log` envelope (Spec §3.4)
- Use `kind:"log"` with `level`, `message`, optional `fields`, `timestamp`.

```go
func (a *LLMAgent) sendLog(level, message string, fields map[string]any) error {
    l := protocol.Log{
        Kind:     "log",
        Level:    level,    // "info" | "warn" | "error"
        Message:  message,
        Fields:   fields,
        Timestamp: time.Now().UTC(),
    }
    return a.encoder.Encode(l)
}
```

5) Optional IK index for O(1) replays (Spec §5.4, §5.6)
- Directory scan is correct but O(n). Add a best‑effort index: `/receipts/<task>/index/by-ik/<first8(sha256(ik))>.json` with `{receipt_path}`. Use if present; fall back to scan.

```go
indexDir := filepath.Join(a.workspace, "receipts", cmd.TaskID, "index", "by-ik")
_ = os.MkdirAll(indexDir, 0700)
_ = os.WriteFile(filepath.Join(indexDir, first8(sha256(ik))+".json"),
    []byte(fmt.Sprintf("{\"receipt_path\":%q}", newReceiptPath)), 0600)
```

6) Artifact size cap (Spec §12)
- Before emitting `artifact.produced`, enforce configured cap (default 1 GiB). If exceeded, remove temp file and emit `error: artifact_write_failed`.

```go
if int64(len(fileData)) > a.cfg.ArtifactMaxBytes {
    os.Remove(fullPath)
    return protocol.Artifact{}, fmt.Errorf("artifact exceeds size cap: %d > %d", len(fileData), a.cfg.ArtifactMaxBytes)
}
```

7) Event size guard for `orchestration.proposed_tasks` (Spec §12)
- Ensure payload stays under 256 KiB. If too large, write full set to a file (when permitted via `expected_outputs`) and include only a truncated preview plus artifact reference in the event.

```go
payloadBytes := mustJSONMarshal(payload)
if len(payloadBytes) > a.cfg.MaxMessageBytes { // default 256*1024
    // Write full tasks to tasks/<task_id>.derived.json (atomic),
    // include first N tasks + note with artifact path in the event
}
```

8) Deterministic ordering
- Sort `plan_candidates` by path asc then confidence desc; sort `derived_tasks` by id asc before emitting.

```go
sort.SliceStable(planCandidates, func(i, j int) bool {
    if planCandidates[i].Path == planCandidates[j].Path {
        return planCandidates[i].Confidence > planCandidates[j].Confidence
    }
    return planCandidates[i].Path < planCandidates[j].Path
})
sort.SliceStable(derivedTasks, func(i, j int) bool { return derivedTasks[i].ID < derivedTasks[j].ID })
```

9) Secret redaction in logs (Spec §13)
- Redact fields with keys ending `_TOKEN`, `_KEY`, `_SECRET` (case‑insensitive) before emitting logs.

```go
func redactSecrets(m map[string]any) map[string]any {
    out := map[string]any{}
    for k, v := range m {
        kUp := strings.ToUpper(k)
        if strings.HasSuffix(kUp, "_TOKEN") || strings.HasSuffix(kUp, "_KEY") || strings.HasSuffix(kUp, "_SECRET") {
            out[k] = "[REDACTED]"
        } else {
            out[k] = v
        }
    }
    return out
}
```

10) Clarify orchestration never emits `system.user_decision` (Spec §3.2)
- Add a note under orchestration events: only `lorch` emits `system.user_decision`; orchestration never does.

---

## Nice‑to‑Have
- Validate `log` and `heartbeat` against `/schemas/v1/*.json` in tests (in addition to events).
- Include the exact `artifact.produced` event shape used by the agent to match Spec §19 examples.
- Mark the timeline section as non‑binding (user indicated AI will implement; timelines are irrelevant).

---

## Verdict

Apply the above edits to finalize spec alignment. With these changes, the plan is ready for implementation against `MASTER-SPEC.md` without gaps.
