# MASTER-SPEC.md
**Local Orchestrator “lorch” for Multi‑Agent Workflows**
**Transport:** Filesystem as shared memory; NDJSON over stdio
**Version:** 1.5 (spec maintainer events, streamlined completion)
**Status:** Draft – implementation‑ready

> **What changed in 1.5 (summary)**
> - Added the `spec.no_changes_needed` terminal event and aligned success criteria to use spec-maintainer signals only.
> - Removed the unused `finalize` action (and related receipts/pseudocode) now that runs end with spec maintenance.
> - Clarified example exchanges and artifacts to reflect the simplified agent set while keeping `lorch` ↔ orchestration flow unchanged.
> - Retained all improvements from v1.4 (builder-driven checks, spec-focused maintenance, CLI alias).

---

## 1. Purpose & Scope

### 1.1 Problem & Importance
We need a local‑first, auditable orchestrator that coordinates multiple AI agents (builder, reviewer, spec‑maintainer, and optionally orchestration/NL intake) to implement spec‑driven development tasks. The system must be deterministic, resilient (idempotent & resumable), and easy to operate from a CLI on a single machine.

### 1.2 Goals
- **Local‑first**: single machine (macOS/Linux); no external brokers in v1.
- **Filesystem‑as‑truth**: all durable artifacts (specs, code, reviews).
- **Strict mediation**: orchestrator is the **only** process that talks to agents.
- **NDJSON over stdio**: newline‑delimited JSON IPC; orchestrator routes all messages.
- **Determinism**: atomic writes, receipts, idempotency keys; repeatable re‑runs.
- **Human‑in‑control**: surface conflicts; do not auto‑edit plans; gate ambiguous steps.
- **Clarity & Extensibility**: minimal deps; generic LLM CLI wrappers; swappable agents.
- **Natural Language intake**: user can say “Manage PLAN.md” — system discovers tasks, asks for approval, then executes (Phase 2).

### 1.3 Non‑Goals (v1)
- Distributed execution, remote brokers, or agent‑to‑agent direct comms.
- GUI dashboards (stdout transcript is sufficient).
- Automatic resolution of plan/spec conflicts (must notify user).

- End‑to‑end runs complete: implement (builder ensures tests/lint pass) → review (iterate until approved) → spec maintenance (iterate until approved).
- Re‑runs after crash are safe and idempotent.
- NL intake can derive tasks from user instructions and request approval (Phase 2).
- Protocol conformance (schemas in §3) passes for all agents.

- **Phase 1 – Core Orchestrator Foundation**: Deliver `lorch run` with builder/reviewer/spec-maintainer agents, deterministic filesystem persistence, append-only ledger, single-agent scheduling, and resumability via idempotency keys.
- **Phase 2 – Natural Language Task Intake**: Introduce the orchestration agent and intake/task discovery commands, human approval loop with `system.user_decision`, transcript printing, and conflict surfacing without auto-editing plans/specs.
- **Phase 3 – Interactive Configuration**: Ship auto-initialized `lorch.json`, the `lorch config` interactive editor with two-tier validation, and generic LLM/tool configuration hooks to keep the orchestrator flexible.
- **Phase 4 – Advanced Error Handling & Conflict Resolution**: Extend recovery to richer diagnostics, structured conflict reporting/escalation, and additional guardrails that keep humans in control while maintaining deterministic outcomes.

---

## 2. Architecture Overview

### 2.1 Components & Trust Boundaries

```
+-----------------------------+
|           lorch             |  (trusted)
| - CLI (run, resume, config) |
| - Scheduler (1-at-a-time)   |
| - Router (NDJSON stdio)     |
| - Agent Supervisor          |
| - Ledger & Receipts         |
| - NL Task Intake (Phase 2)* |
+--^-----------^-----------^--+
   |           |           |
   | stdio     | stdio     | stdio
   v           v           v
+------+   +--------+   +----------------+
|Builder|  |Reviewer|   |Spec-Maintainer |
+------+   +--------+   +----------------+
         (optional Phase 2 agent)
                         +--------------------+
                         | Orchestration (NL) |
                         +--------------------+

                (all untrusted agents)
                    |
                    v
         +---------------------------+
         |  Filesystem (source of    |
         |  truth): /specs /src ...  |
         +---------------------------+
```

\* **Natural Language Task Intake (NLTI)** can be implemented either (a) as a **separate orchestration agent** (recommended, Phase 2) or (b) as built‑in logic in `lorch`. This spec standardizes the **agent variant** to keep the core orchestrator minimal and deterministic.

### 2.2 Agent Roles
- **Builder** — implements tasks; writes/updates code & tests; must run tests/lint before declaring success; emits `builder.completed`.
- **Reviewer** — evaluates outputs for code quality; emits `review.completed` with `approved | changes_requested`.
- **Spec‑Maintainer** — validates implementation against SPEC.md, may update allowed sections, and emits `spec.updated | spec.no_changes_needed | spec.changes_requested`.
- **Orchestration (NL)** — derives a concrete task plan from user instructions; emits `orchestration.proposed_tasks` and `orchestration.needs_clarification`. *Never edits plan/spec files.*

### 2.3 Operational Constraints
- **Concurrency = 1** (one agent active at a time per run).
- **lorch** prints **live transcripts** of conversations with each agent to **stdout**. No other progress indicator is required.

---

## 3. IPC Protocol (NDJSON over stdio)

**Transport**: UTF‑8 NDJSON (one JSON per line).
**Envelope**: `kind` ∈ `command | event | heartbeat | log`.
Common fields: `message_id`, `correlation_id`, `task_id`, timestamps RFC 3339 UTC.
**Max NDJSON line**: 256 KiB. Diffs/logs/artifacts are referenced by **file paths** + checksums.

### 3.1 `command` (lorch → agent)

**Actions (enum)**
`implement | implement_changes | review | update_spec | intake | task_discovery`

- `intake` / `task_discovery` are used with the **Orchestration (NL)** agent.
- `update_spec` targets the spec-maintainer; other agents ignore it.

**Schema**
```json
{
  "$id": "https://lorch/schemas/command.v1.json",
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "Command",
  "type": "object",
  "additionalProperties": false,
  "required": [
    "kind", "message_id", "correlation_id", "task_id",
    "idempotency_key", "to", "action", "inputs", "version",
    "deadline", "retry", "priority"
  ],
  "properties": {
    "kind": { "const": "command" },
    "message_id": { "type": "string" },
    "correlation_id": { "type": "string" },
    "task_id": { "type": "string" },
    "idempotency_key": { "type": "string", "minLength": 16 },
    "to": {
      "type": "object",
      "additionalProperties": false,
      "required": ["agent_type"],
      "properties": {
        "agent_type": {
          "type": "string",
          "enum": ["builder","reviewer","spec_maintainer","orchestration"]
        },
        "agent_id": { "type": "string" }
      }
    },
    "action": {
      "type": "string",
      "enum": [
        "implement","implement_changes","review",
        "update_spec","intake","task_discovery"
      ]
    },
    "inputs": { "type": "object" },
    "expected_outputs": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["path"],
        "properties": {
          "path": { "type": "string" },
          "description": { "type": "string" },
          "required": { "type": "boolean", "default": true }
        }
      },
      "default": []
    },
    "version": {
      "type": "object",
      "additionalProperties": false,
      "required": ["snapshot_id"],
      "properties": {
        "snapshot_id": { "type": "string" },
        "specs_hash": { "type": "string" },
        "code_hash": { "type": "string" }
      }
    },
    "deadline": { "type": "string", "format": "date-time" },
    "retry": {
      "type": "object",
      "additionalProperties": false,
      "required": ["attempt","max_attempts"],
      "properties": {
        "attempt": { "type": "integer", "minimum": 0 },
        "max_attempts": { "type": "integer", "minimum": 1 }
      }
    },
    "priority": { "type": "integer", "minimum": 0 }
  }
}
```

**Example (intake)**
```json
{"kind":"command","message_id":"m-01","correlation_id":"corr-intake-1","task_id":"T-0050","idempotency_key":"ik:intake:T-0050:snap-0009","to":{"agent_type":"orchestration"},"action":"intake","inputs":{"user_instruction":"I've got a PLAN.md. Manage the implementation of it"},"expected_outputs":[{"path":"tasks/T-0050.plan.json"}],"version":{"snapshot_id":"snap-0009"},"deadline":"2025-10-19T19:00:00Z","retry":{"attempt":0,"max_attempts":3},"priority":5}
```

### 3.2 `event` (agent → lorch)

**Representative event types**
- Builder: `builder.progress`, `builder.completed`
- Reviewer: `review.completed` (`approved | changes_requested`)
- Spec‑Maintainer: `spec.updated`, `spec.no_changes_needed`, `spec.changes_requested`
- Orchestration(NL): `orchestration.proposed_tasks`, `orchestration.needs_clarification`, `orchestration.plan_conflict`
- Generic: `artifact.produced`, `error`, `log`
- System (from lorch): `system.user_decision` (records human approvals/denials)

**Schema**
```json
{
  "$id": "https://lorch/schemas/event.v1.json",
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "Event",
  "type": "object",
  "additionalProperties": false,
  "required": ["kind","message_id","correlation_id","task_id","from","event","occurred_at"],
  "properties": {
    "kind": { "const": "event" },
    "message_id": { "type": "string" },
    "correlation_id": { "type": "string" },
    "task_id": { "type": "string" },
    "from": {
      "type": "object",
      "additionalProperties": false,
      "required": ["agent_type"],
      "properties": {
        "agent_type": {
          "type": "string",
          "enum": ["builder","reviewer","spec_maintainer","orchestration","system"]
        },
        "agent_id": { "type": "string" }
      }
    },
    "event": { "type": "string" },
    "status": { "type": "string" },
    "payload": { "type": "object", "default": {} },
    "artifacts": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["path","sha256","size"],
        "properties": {
          "path": { "type": "string" },
          "sha256": { "type": "string" },
          "size": { "type": "integer", "minimum": 0 }
        }
      }
    },
    "observed_version": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "snapshot_id": { "type": "string" },
        "specs_hash": { "type": "string" },
        "code_hash": { "type": "string" }
      }
    },
    "occurred_at": { "type": "string", "format": "date-time" }
  }
}
```

**Example (orchestration.proposed_tasks)**
```json
{"kind":"event","message_id":"e-01","correlation_id":"corr-intake-1","task_id":"T-0050","from":{"agent_type":"orchestration"},"event":"orchestration.proposed_tasks","payload":{"plan_candidates":[{"path":"PLAN.md","confidence":0.82},{"path":"docs/plan_v2.md","confidence":0.61}],"derived_tasks":[{"id":"T-0050-1","title":"Implement sections 1–2","files":["src/a.js","tests/a.spec.js"]},{"id":"T-0050-2","title":"Implement sections 3–4","files":["src/b.js","tests/b.spec.js"]}],"notes":"Found 2 plausible plan files."},"occurred_at":"2025-10-19T18:03:00Z"}
```

**Example (system.user_decision)**
```json
{"kind":"event","message_id":"e-02","correlation_id":"corr-intake-1","task_id":"T-0050","from":{"agent_type":"system"},"event":"system.user_decision","status":"approved","payload":{"approved_plan":"PLAN.md","approved_tasks":["T-0050-1","T-0050-2"],"prompt":"Use PLAN.md and proceed"},"occurred_at":"2025-10-19T18:05:10Z"}
```

### 3.3 `heartbeat` (agent → lorch)
Shim‑level liveness.

**Schema**
```json
{
  "$id": "https://lorch/schemas/heartbeat.v1.json",
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "Heartbeat",
  "type": "object",
  "additionalProperties": false,
  "required": ["kind","agent","seq","status","pid","uptime_s","last_activity_at"],
  "properties": {
    "kind": { "const": "heartbeat" },
    "agent": {
      "type": "object",
      "additionalProperties": false,
      "required": ["agent_type","agent_id"],
      "properties": {
        "agent_type": {
          "type": "string",
          "enum": ["builder","reviewer","spec_maintainer","orchestration"]
        },
        "agent_id": { "type": "string" }
      }
    },
    "seq": { "type": "integer", "minimum": 0 },
    "status": { "type": "string", "enum": ["starting","ready","busy","stopping","backoff"] },
    "pid": { "type": "integer", "minimum": 1 },
    "ppid": { "type": "integer", "minimum": 0 },
    "uptime_s": { "type": "number", "minimum": 0 },
    "last_activity_at": { "type": "string", "format": "date-time" },
    "stats": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "cpu_pct": { "type": "number", "minimum": 0 },
        "rss_bytes": { "type": "integer", "minimum": 0 }
      }
    },
    "task_id": { "type": "string" }
  }
}
```

### 3.4 Logs & Errors
- `log`: `{"kind":"log","level":"info|warn|error","message":"...","fields":{...},"timestamp":"..."}`
- Action failures **must** use `event` with `event:"error"` and machine‑readable `payload.code`.

---

## 4. Process Flow (Single‑Agent‑at‑a‑Time)

### 4.1 Normal Execution
1. **Startup**
   - `lorch` loads or **auto‑creates** `lorch.json` (see §8).
   - Creates `run_id`, snapshot (`snap-XXXX`), initializes `/state/run.json`.
   - Spawns all configured agents (idle) and starts heartbeats.

   > **Invocation note**: running `lorch` with no subcommand is identical to executing `lorch run`.

2. **(Phase 2) Natural Language Intake (optional but specified)**
   - If user starts with **no explicit task**: `lorch` (alias `lorch run`) prompts:
     ```
     lorch> What should I do? (e.g., "Use PLAN.md and manage its implementation")
     ```
   - `lorch` sends `command(action=intake)` to **orchestration** agent.
   - Orchestration returns `orchestration.proposed_tasks` with plan candidates and derived tasks.
   - **lorch prints transcript** and asks user to **approve/modify** plan selection and tasks.
   - `lorch` records decision as `system.user_decision`.
   - If ambiguous, orchestration may emit `orchestration.needs_clarification` → `lorch` prompts human for answers and re‑issues `intake` with same `idempotency_key`.

3. **Implement → Review (iterate) → Spec Maintenance (iterate) → Complete**
   - **Implement**: send `implement` to **builder**; wait for `builder.completed` (must include passing test/lint summaries).
   - **Review**: send `review` to **reviewer`. If `changes_requested`, loop: `implement_changes` → `review` until `approved`.
   - **Spec Maintenance**: send `update_spec` to **spec‑maintainer**. If they emit `spec.changes_requested`, loop: `implement_changes` → `review` → `update_spec` until ready.
   - Run completion is signaled by `spec.updated` or `spec.no_changes_needed`.

4. **Completion**
   - Success = `review.completed: approved` **and** either `spec.updated` or `spec.no_changes_needed`.
   - Failure = user aborts, agent exhausts retries, or persistent conflicts; notify the user and stop.

### 4.2 Required Iteration Order (hard‑coded)
```
Implement → Review
  ↳ if changes_requested → Implement Changes → Review (repeat)
Approved → Spec Maintenance (update_spec)
  ↳ if spec.changes_requested → Implement Changes → Review → Spec Maintenance (repeat)
Spec Updated/No Changes Needed → Done
```

### 4.3 Example Exchanges (≥3)

**Exchange 0 – NL Intake & Approval**
```
lorch → Orchestration: command(intake)
Orchestration → lorch: event(orchestration.proposed_tasks)
lorch → user (console): prints candidates & derived tasks → ask approve?
lorch → lorch ledger: event(system.user_decision, status=approved)
```

**Exchange A – Implement**
```
lorch → Builder: command(implement)
Builder → lorch: event(artifact.produced)
Builder → lorch: event(builder.completed, tests={"status":"pass"})
```

**Exchange B – Review requests changes**
```
lorch → Reviewer: command(review)
Reviewer → lorch: event(review.completed: changes_requested, reviews/T-0042.json)
lorch → Builder: command(implement_changes)
```

**Exchange C – Spec maintenance approves**
```
lorch → SpecMaint: command(update_spec)
SpecMaint → lorch: event(spec.updated)
```

**Exchange C′ – Spec maintenance confirms no edits required**
```
lorch → SpecMaint: command(update_spec)
SpecMaint → lorch: event(spec.no_changes_needed)
```

**Exchange D – Spec maintainer asks for changes**
```
lorch → SpecMaint: command(update_spec)
SpecMaint → lorch: event(spec.changes_requested, spec_notes/T-0042.json)
lorch → Builder: command(implement_changes)
Builder → lorch: event(builder.completed, tests={"status":"pass"})
lorch → Reviewer: command(review)
Reviewer → lorch: event(review.completed: approved)
lorch → SpecMaint: command(update_spec)
SpecMaint → lorch: event(spec.updated)
```

---

## 5. Persistence & State

### 5.1 Durable vs Ephemeral
- **Durable**
  - `/specs/**`, `/src/**`, `/tests/**`
  - `/reviews/<task>.json`, `/spec_notes/<task>.json`
  - `/events/run-<id>.ndjson` (append‑only ledger)
  - `/receipts/<task>/<step>.json` (artifact manifests)
  - `/logs/<agent>/<run_id>.ndjson`
  - `/state/run.json`, `/state/index.json`
  - `/snapshots/snap-XXXX.manifest.json`
  - `/transcripts/<run_id>.txt` (optional human‑readable transcript derived from events)

- **Ephemeral**
  - In‑flight stdio streams, in‑memory scheduler.

### 5.2 Directory Layout
```
/specs/MASTER-SPEC.md
/src/**                 /tests/**
/reviews/T-0042.json    /spec_notes/T-0042.json
/events/run-20251019-...ndjson
/receipts/T-0042/step-<n>.json
/logs/builder/run-...ndjson (etc.)
/state/run.json         /state/index.json
/snapshots/snap-0009.manifest.json
/transcripts/run-...txt (optional pretty print)
```

### 5.3 Snapshots & Manifests
- Snapshot lists tracked files (path, sha256, size, mtime).
- `snapshot_id = "snap-" + first8(sha256(manifest_bytes))`.
- Commands carry `version.snapshot_id`; agents echo `observed_version.snapshot_id`.

### 5.4 Idempotency Keys (IK)
```
ik = SHA256(
  action + '\n' + task_id + '\n' + snapshot_id + '\n' +
  canonical_json(inputs) + '\n' + canonical_json(expected_outputs)
)
```
- Agents **must** treat repeated commands with the same IK as **already handled**: no rewrites; emit success referencing prior receipts.
- lorch reuses IK across retries until inputs or snapshot change.

### 5.5 Deterministic Writes (agents)
1. Write to `.<basename>.tmp.<pid>.<rand>` in same dir.
2. `fsync(tmp)` → atomic `rename(tmp, final)` → `fsync(dir)`.
3. Emit `artifact.produced` with `{path, sha256, size}`.
4. lorch records a **receipt** linking artifacts to IK and message IDs.

### 5.6 Crash/Restart Rehydration
- On startup: read `/state/run.json` and ledger; rebuild last terminal events; verify receipts & checksums; resend the **next** command.
- If a prior command has no terminal event, **resend it with the same IK**.

---

## 6. Routing Rules

### 6.1 Who Acts Next (policy)
1. (Optional) Orchestration(NL) to intake/derive tasks → require **user approval**.
2. Builder `implement`.
3. Reviewer `review`. If `changes_requested` → Builder `implement_changes` → Reviewer `review` (repeat).
4. Spec‑Maintainer `update_spec` (allowed sections only). If `spec.changes_requested` → Builder `implement_changes` → Reviewer → Spec‑Maintainer (repeat).
5. Run completes.

### 6.2 Correlation, Tasks, Versions
- `task_id` is stable per user scenario (e.g., `T-0042`).
- `correlation_id` per command chain; preserved across retries.
- **Version pinning** via `snapshot_id` (and optional `code_hash`, `specs_hash`). Agents must error with `version_mismatch` if snapshots differ.

---

## 7. Error Handling & Recovery

### 7.1 Timeouts (defaults; config in §8)
- Heartbeat interval: 10s; tolerate 3 misses → unhealthy.
- Command timeouts: implement 600s; review 300s; update_spec 180s; intake 180s.

### 7.2 Backoff & Restart
- Exponential backoff: initial 1s, x2, max 60s, full jitter; max 5 restarts per agent per run.

### 7.3 Orphaned or Stuck Processes
- On resume, attempt adoption via heartbeat; else terminate & clean start.
- If stdout stalls (pipe full), lorch prioritizes draining.

### 7.4 Conflict Handling Philosophy
- **Never auto‑modify plan/spec content.**
- Orchestration agent emits `orchestration.plan_conflict` or `needs_clarification`.
- lorch prints the issue, requests a human decision, records `system.user_decision`, and proceeds or aborts accordingly.

### 7.5 Resumability
- Entire workflow is idempotent via IKs and append‑only ledger.
- Partial writes are detected by checksum mismatch → retry same IK.

---

## 8. Configuration Management (`lorch.json`)

### 8.1 Auto‑Creation & Validation
- On first run, if `lorch.json` absent, lorch **auto‑creates** it with sensible defaults.
- **Two‑tier validation**: on `lorch config` changes and at startup.

### 8.2 Example `lorch.json`
```json
{
  "version": "1.0",
  "workspace_root": ".",
  "policy": {
    "concurrency": 1,
    "message_max_bytes": 262144,
    "artifact_max_bytes": 1073741824,
    "retry": { "max_attempts": 3, "backoff": { "initial_ms": 1000, "max_ms": 60000, "multiplier": 2.0, "jitter": "full" } },
    "strict_version_pinning": true,
    "parallel_reviews": false,
    "redact_secrets_in_logs": true
  },
  "agents": {
    "builder": {
      "cmd": ["claude"],
      "heartbeat_interval_s": 10,
      "timeouts_s": { "implement": 600, "implement_changes": 600 },
      "env": { "CLAUDE_AGENT_ROLE": "builder", "LOG_LEVEL": "info" }
    },
    "reviewer": {
      "cmd": ["claude"],
      "heartbeat_interval_s": 10,
      "timeouts_s": { "review": 300 },
      "env": { "CLAUDE_AGENT_ROLE": "reviewer" }
    },
    "spec_maintainer": {
      "cmd": ["claude"],
      "timeouts_s": { "update_spec": 180 },
      "env": { "CLAUDE_AGENT_ROLE": "spec_maintainer" }
    },
    "orchestration": {
      "enabled": true,
      "cmd": ["claude"],
      "timeouts_s": { "intake": 180, "task_discovery": 180 },
      "env": { "CLAUDE_AGENT_ROLE": "orchestration" }
    }
  },
  "tasks": [
    { "id": "T-0042", "goal": "Implement sections 3.1–3.3 of /specs/MASTER-SPEC.md" }
  ]
}
```

### 8.3 CLI
- `lorch` — shorthand for `lorch run`; supports the same flags and defaults to NL intake when no task is specified.
- `lorch run [--task T-0042]` — start a run. If `--task` omitted, prompt for NL instruction (Phase 2).
- `lorch resume --run <run_id>` — resume from ledger/state.
- `lorch config` — interactive editor with validation (Phase 3).
- `lorch validate --schemas` — schema compliance for agents.
- `lorch doctor` — environment checks.
- `lorch help` — *(planned, low priority; may arrive in a later phase for discoverability only).*

---

## 9. Routing & Console Transcript

- lorch **prints all agent messages** (commands and events) to console in human‑readable lines:
  ```
  [builder] artifact.produced src/foo/bar.js (1.4 KiB)
  [reviewer] review.completed changes_requested (see reviews/T-0042.json)
  ```
- The same content is recorded in `/events/run-*.ndjson`.
- Optional pretty transcript at `/transcripts/run-*.txt` may be generated from ledger.

---

## 10. Extensibility & Determinism

### 10.1 Adding/Swapping Agents
- Each agent is a **CLI wrapper** speaking NDJSON on stdio; internals (LLM/tool) are pluggable.
- Default model/tool is configured by env in `lorch.json` (e.g., `"MODEL":"common-llm"`). No per‑LLM customization required by lorch.

### 10.2 Orchestration Agent (NL) Contract
- **Inputs**: user instruction text; workspace context.
- **Outputs**: `orchestration.proposed_tasks` (list of candidate plan/spec files + derived task list), or `needs_clarification` with questions.
- **Constraints**:
  - MUST NOT modify plan/spec files.
  - MUST route through lorch; never prompt user directly.
  - MUST accept stable IK behavior for repeatability.

### 10.3 Event Types (additions)
- `orchestration.proposed_tasks`, `orchestration.needs_clarification`, `orchestration.plan_conflict`.
- `system.user_decision` (originated by lorch).

### 10.4 File Discovery (for NL intake)
- Search paths: `[".", "docs", "specs", "plans"]` (recursive, ignore `node_modules`, `.git`, hidden dirs).
- Candidate extensions: `.md`, `.rst`, `.txt`.
- Basic heuristics: rank by filename tokens (`plan`, `spec`, `proposal`) and heading similarity; **present to user for approval**.

### 10.5 SPEC.md Allowed Edits (spec‑maintainer)
- May edit **only** these sections/markers to keep runs deterministic:
  - `## Status` table (task IDs, states, timestamps).
  - `## Changelog` (append entries).
  - `## Completion` markers (checkboxes or status tags).
  - `## Open Questions` (append new items only).
- **Must NOT** change requirements text, acceptance criteria, or instructions outside these areas.

---

## 11. Run Validation

### 11.1 Passing Run
- Artifacts exist & match receipt checksums.
- Final `review.completed: approved`.
- Spec maintainer emitted `spec.updated` (or `spec.no_changes_needed`) after verifying requirements.
- SPEC.md updates confined to allowed sections.
- Ledger contains terminal events for each correlation; `/state/run.json` shows `completed`.

### 11.2 Protocol Conformance
- Validate agent IO against **command**, **event**, **heartbeat** schemas.
- Negative tests: oversize messages, invalid enums, missing required fields.

### 11.3 Agent Smoke Tests
- Builder: `implement` → runs tests/lint, produces `/receipts/...` + `builder.completed` summarizing results.
- Reviewer: `review` → `/reviews/<task>.json` + `review.completed`.
- Spec‑Maintainer: `update_spec` → `/spec_notes/<task>.json` (optional) + `spec.updated` or `spec.no_changes_needed`.
- Orchestration: `intake` → `orchestration.proposed_tasks` with ≥1 candidate, or `needs_clarification`.

---

## 12. Performance & Limits (defaults)

| Setting                      | Default | Notes                               |
|-----------------------------|---------|-------------------------------------|
| Concurrency                 | 1       | Exactly one active agent at a time  |
| Max NDJSON message          | 256 KiB | Larger content via files            |
| Artifact hard cap           | 1 GiB   | Configurable                        |
| Heartbeat interval          | 10 s    | Miss 3 → unhealthy                  |
| Max restarts per agent/run  | 5       | With exponential backoff            |

---

## 13. Security & Safety

- **Permissions**: create files with `0600`, dirs `0700` (configurable via umask).
- **Path safety**: reject `..`, absolute paths outside workspace, or escaping symlinks.
- **Secrets**: redact env ending in `_TOKEN|_KEY|_SECRET` from logs and ledger.
- **Sandboxing**: recommend separate OS users/namespaces for each agent; network‑off unless required by the agent.

---

## 14. Pseudocode (Key Mechanisms)

### 14.1 lorch Main Loop (single‑agent scheduler)
```pseudo
load_or_init_config()
run_id = new_run_id()
snapshot = take_snapshot()
spawn_agents()

if no --task:
  // Phase 2 path: NL intake
  cmd = mk_command(to=orchestration, action=intake, inputs={user_instruction})
  send(cmd); await events until proposed_tasks or needs_clarification
  while event == needs_clarification:
    ask_user_questions(); resend same IK with updated inputs
  present_candidates_and_tasks_to_user()
  record_event(system.user_decision)
  if user_denies: abort_run()

for each approved_task in order:
  implement_review_spec_complete(approved_task)

complete_run()
```

### 14.2 Implement → Review → Spec Maintenance loop
```pseudo
function implement_review_spec_complete(task):
  send(builder, implement); await builder.completed or retry
  ensure(builder.report.tests.status == "pass" or builder.report.tests.allowed_failures)
  do:
    send(reviewer, review); await review.completed
    if status == "changes_requested":
      send(builder, implement_changes); await builder.completed
  while status == "changes_requested"

  send(spec_maintainer, update_spec); await spec.updated or spec.no_changes_needed or spec.changes_requested
  while last_event == "spec.changes_requested":
    send(builder, implement_changes); await builder.completed
    send(reviewer, review); await review.completed (must be approved)
    send(spec_maintainer, update_spec); await spec.updated or spec.no_changes_needed or spec.changes_requested
```

### 14.3 Heartbeat Supervisor
```pseudo
every interval:
  for agent in agents:
    if now - last_heartbeat(agent) > 3 * interval:
      mark_unhealthy(agent)
      cancel_inflight(agent)
      restart_with_backoff(agent)
      resend_last_command_same_IK(agent)
```

### 14.4 Agent Safe Write
```pseudo
atomic_write(path, bytes):
  tmp = dirname(path) + "/." + basename(path) + ".tmp." + pid + "." + rand()
  write(tmp, bytes); fsync(tmp)
  rename(tmp, path); fsync(dirname(path))
```
---

## 15. Example End‑to‑End Scenario (grounded)

**Initial Use Case**
Task `T-0042`: Implement sections 3.1–3.3 of `/specs/MASTER-SPEC.md`.

**Before (snapshot)**
```
.
├─ specs/
│  └─ MASTER-SPEC.md          # missing sections 3.1–3.3
├─ src/
├─ tests/
├─ lorch.json                 # auto-created on first run (defaults)
└─ state/
```

**Run (console sketch)**
```
$ lorch run --task T-0042
[lorch] snapshot snap-0007
[lorch→builder] command implement (corr T-0042-1)
[builder] artifact.produced src/foo/bar.js (1.4 KiB)
[builder] artifact.produced tests/foo/bar.spec.js (0.8 KiB)
[builder] builder.completed success
[lorch→reviewer] command review (corr T-0042-2)
[reviewer] review.completed changes_requested (reviews/T-0042.json)
[lorch→builder] command implement_changes (corr T-0042-2b)
[builder] builder.completed success
[lorch→reviewer] command review (corr T-0042-2c)
[reviewer] review.completed approved
[lorch→spec_maintainer] command update_spec
[spec_maintainer] spec.updated (status table, completion marker)
[lorch] DONE
```

**After (snapshot)**
```
.
├─ specs/
│  └─ MASTER-SPEC.md           # sections 3.1–3.3 marked complete (allowed areas only)
├─ src/
│  └─ foo/bar.js
├─ tests/
│  └─ foo/bar.spec.js
├─ reviews/
│  └─ T-0042.json
├─ spec_notes/
│  └─ T-0042.json
├─ events/
│  └─ run-20251019-...ndjson
├─ receipts/
│  └─ T-0042/
│     ├─ step-1.json
│     ├─ step-2.json
│     └─ update_spec.json
├─ logs/
│  ├─ builder/run-...ndjson
│  ├─ reviewer/run-...ndjson
│  └─ spec_maintainer/run-...ndjson
└─ state/
   ├─ run.json  (status=completed)
   └─ index.json
```

**Validation**
- Checksums in receipts match actual files.
- `review.completed` final event is `approved`.
- Spec maintainer emits `spec.updated` or `spec.no_changes_needed` to confirm requirements satisfied.
- SPEC.md changes limited to allowed sections.

---

## 16. Interfaces & File Conventions

### 16.1 Artifact Receipts
`/receipts/<task>/<step>.json`
```json
{
  "task_id": "T-0042",
  "step": 1,
  "idempotency_key": "ik:implement:T-0042:snap-0007:...",
  "artifacts": [
    {"path":"src/foo/bar.js","sha256":"sha256:...","size":1432},
    {"path":"tests/foo/bar.spec.js","sha256":"sha256:...","size":782}
  ],
  "events": ["msg-e1","msg-e2","msg-e3"],
  "created_at": "2025-10-19T18:10:04Z"
}
```

### 16.2 Review & Spec Notes
- `/reviews/<task>.json`
```json
{
  "task_id":"T-0042",
  "status":"changes_requested|approved",
  "findings":[{"path":"src/foo/bar.js","line":42,"comment":"Handle null inputs"}],
  "summary":"...",
  "created_at":"2025-10-19T18:20:40Z"
}
- `/spec_notes/<task>.json`
```json
{
  "task_id":"T-0042",
  "status":"approved|changes_requested",
  "summary":"Implementation covers sections 3.1–3.3 exactly; updated status table.",
  "created_at":"2025-10-19T18:30:12Z"
}
```

---

## 17. Testing

### 17.1 Schema Tests
- Valid/invalid instances for **command**, **event**, **heartbeat**.
- Ensure `to.agent_type` and `from.agent_type` enums enforce allowed values.

### 17.2 NL Intake Tests
- Given instruction “Manage PLAN.md”, orchestration returns at least one candidate and a task list.
- When ambiguous, orchestration returns `needs_clarification` with ≥1 question; lorch prompts user; repeat with same IK.

### 17.3 Smoke E2E
- Run with `--task T-0042` (no NL intake) → should complete and update SPEC.md allowed areas.
- Run with **no** `--task`; provide instruction; approve a candidate; then complete.

---

## 18. Operational Limits & Defaults (quick table)

| Setting                        | Default |
|--------------------------------|---------|
| Implement timeout              | 600 s   |
| Review timeout                 | 300 s   |
| Compliance timeout             | 300 s   |
| Update_spec timeout            | 120 s   |
| Intake/task_discovery timeout  | 180 s   |
| Heartbeat interval             | 10 s    |
| Missed heartbeat threshold     | 3       |
| Max restarts per agent         | 5       |
| Max message size               | 256 KiB |
| Artifact size cap              | 1 GiB   |

---

## 19. Example Messages (copy/paste)

**artifact.produced**
```json
{"kind":"event","message_id":"m1","correlation_id":"corr-T-0042-1","task_id":"T-0042","from":{"agent_type":"builder"},"event":"artifact.produced","payload":{"description":"main module"},"artifacts":[{"path":"src/foo/bar.js","sha256":"sha256:...","size":1432}],"occurred_at":"2025-10-19T18:09:58Z"}
```

**error (version mismatch)**
```json
{"kind":"event","message_id":"m2","correlation_id":"corr-T-0042-1","task_id":"T-0042","from":{"agent_type":"builder"},"event":"error","status":"failed","payload":{"code":"version_mismatch","expected_snapshot":"snap-0007","observed_snapshot":"snap-0006"},"occurred_at":"2025-10-19T18:10:00Z"}
```

**heartbeat (busy)**
```json
{"kind":"heartbeat","agent":{"agent_type":"reviewer","agent_id":"reviewer#1"},"seq":42,"status":"busy","pid":32100,"ppid":32000,"uptime_s":122.4,"last_activity_at":"2025-10-19T18:20:20Z","stats":{"cpu_pct":23.1,"rss_bytes":73400320},"task_id":"T-0042"}
```

---

## 20. FAQ (Decisions to Align with Feedback)

- **Protocol for new orchestration component?**
  Yes—**NDJSON** remains the interface; orchestration is just another agent type.

- **Process architecture: in‑process vs. separate agent?**
  **Separate agent recommended** (Phase 2) to keep lorch core deterministic/minimal.

- **Conflict surfacing pattern?**
  Orchestration emits `orchestration.plan_conflict`/`needs_clarification`; lorch **prints** the issue, asks the user, records `system.user_decision`, and continues or aborts.

- **Persist transcripts?**
  Console transcript is primary; the ledger is canonical. An optional prettified transcript is saved as `/transcripts/<run_id>.txt`.

- **Task translation from conversation to concrete tasks?**
  Orchestration proposes task objects (IDs, titles, likely file sets). **User approves**; lorch then issues normal `command` messages per this spec.

---

**End of MASTER-SPEC.md**
