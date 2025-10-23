# LLM Agents Plan — Spec Compliance Review (Review 1)

**Document under review**: `docs/development/LLM-AGENTS-PLAN.md`
**Primary reference**: `MASTER-SPEC.md`
**Status**: Changes requested
**Date**: 2025-10-23

---

## Executive Summary

The overall approach (single binary with `--role`, NDJSON over stdio, external LLM CLI, filesystem as shared memory, orchestration-first) aligns well with `MASTER-SPEC.md`. To be fully spec-compliant and robust, the plan needs explicit handling for idempotency (IK replay), event/schema fields (observed version echo, structured errors), heartbeat status transitions, message size limits, and the nuance that orchestration may produce artifacts (planning outputs) even though it must not edit plan/spec content.

---

## Required Corrections (Spec-Tightening)

1. Idempotency and deterministic replays (Spec §5.4)
   - Persist results keyed by `idempotency_key` (IK). On repeat IK, skip LLM calls and re-emit the prior outcome (including `artifact.produced` references if applicable).
   - Store a small planning artifact when requested via `expected_outputs` and emit `artifact.produced`; this also serves as the source of truth for replays.

2. Orchestration writes vs. edits (Spec §3.1 example, §10.3)
   - Clarify: orchestration must not modify plan/spec files, but it may write planning artifacts when `expected_outputs` are provided. Use the safe atomic write pattern (Spec §14.4) and keep NDJSON messages small by referencing files.

3. Observed version and version pinning (Spec §6.2, §3.2 `observed_version`)
   - Echo `observed_version.snapshot_id` in all events. Optionally error with `version_mismatch` if the command’s snapshot differs from what the agent observes.

4. Heartbeat lifecycle (Spec §3.3)
   - Send `starting → ready` at boot; `busy` while handling a command; `ready` after. Include `seq`, `pid`, `ppid`, and `task_id` when applicable.

5. Structured error events (Spec §3.4)
   - On failures (invalid inputs, LLM call, invalid response, version mismatch), emit `event:"error"` with machine-readable `payload.code` (e.g., `invalid_inputs`, `llm_call_failed`, `invalid_llm_response`, `version_mismatch`).

6. Message size limits and artifacts (Spec §12, §3)
   - Keep NDJSON messages under 256 KiB. Avoid embedding large file contents in events; put content on disk and reference via `artifacts` when needed.

7. Timeouts and defaults (Spec §7.1, §18)
   - Align orchestration `intake`/`task_discovery` default timeout to 180s (configurable). Ensure heartbeats continue during long LLM calls.

---

## Concrete Plan Updates (Edit Suggestions)

Update `docs/development/LLM-AGENTS-PLAN.md` in these areas:

- Filesystem as shared memory (clarification)
  - Replace: “Orchestration reads only (never edits per spec §2.2)”
  - With: “Orchestration must not edit plan/spec content, but may produce planning artifacts when requested via `expected_outputs`, using the safe atomic write pattern (§14.4) and emitting `artifact.produced`.”

- NDJSON Protocol Implementation
  - Add: Event emission includes `observed_version.snapshot_id` echo. On snapshot mismatch, emit `error` with `payload.code = "version_mismatch"`.
  - Add: Heartbeat status transitions (`starting`, `ready`, `busy`, `ready`) with `seq`, `pid`, `ppid`, `task_id`.

- Orchestration Logic
  - Add: IK cache: persist `{ik → result + artifact paths}`. On repeated IK, re-emit previous `orchestration.proposed_tasks` and any `artifact.produced` without calling the LLM.
  - Add: Enforce NDJSON 256 KiB cap; if prompt context is large, summarize/structurally extract instead of embedding raw contents.
  - Add: Standardized error events with `payload.code` per failure mode.

- LLM CLI Caller
  - Add: Continuous heartbeat while waiting for the subprocess (e.g., a ticker that updates last activity) to maintain liveness.
  - Add: Output size limit and timeout already noted—ensure these are configurable and logged succinctly.

- Testing Strategy
  - Add: Schema validation for outbound `command`/`event`/`heartbeat` using repo schemas; negative tests for oversize messages and invalid enums.
  - Add: Idempotency replay test—send the same `intake` command twice with the same IK and assert no new artifacts or LLM calls, identical events.
  - Add: Snapshot/version mismatch test—assert structured `error`.

---

## Implementation Notes (Grounded in Spec)

- Safe write pattern for artifacts (Spec §14.4)
  - Use atomic temp file → `fsync` → `rename` → dir `fsync`. Emit `artifact.produced {path, sha256, size}` for each output.

- Idempotency (Spec §5.4)
  - Treat IK as the replay key. Store a small receipt-like record in a deterministic path (e.g., `receipts/<task_id>/intake.json`) that references produced artifacts and the event payload used.

- Version echo (Spec §3.2)
  - Include `observed_version.snapshot_id` in all events to let `lorch` verify consistency.

- Log vs. Event (Spec §3.4)
  - Prefer `log` for human diagnostics; keep `event` payloads minimal and machine-readable. Do not exceed NDJSON caps.

---

## Minimal API/Behavior Sketches

Suggested orchestration action flow:

1. Parse and validate inputs → if invalid, `event:error` (`invalid_inputs`).
2. Build prompt and bound context size.
3. Check IK cache → if hit, re-emit cached `orchestration.proposed_tasks` (+ any `artifact.produced`) and return.
4. Call LLM (with timeout); stream heartbeats while waiting.
5. Extract and validate JSON; on failure, `event:error` (`invalid_llm_response`).
6. If `expected_outputs` present, safe-write planning artifact(s) and emit `artifact.produced`.
7. Emit `orchestration.proposed_tasks` with `observed_version`.

---

## Ready-To-Build Checklist

- [ ] IK cache implemented; repeated IKs do not call LLM
- [ ] Orchestration writes artifacts only via `expected_outputs` and safe writes
- [ ] `observed_version.snapshot_id` included on all events
- [ ] Heartbeats: correct status transitions and fields
- [ ] Structured `error` events with `payload.code`
- [ ] NDJSON message size respected; artifacts used for large data
- [ ] Timeouts aligned with defaults; heartbeats continue during waits
- [ ] Tests: schema validation, idempotency replay, version mismatch, oversize negative

---

## Verdict

The plan is directionally correct and close to spec. Incorporate the above corrections to ensure strict compliance and resilient behavior before proceeding to implementation.


