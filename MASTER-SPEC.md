**MASTER-SPEC.md**

**Local Orchestrator for Multi‑Agent Workflows (Filesystem as Shared Memory; NDJSON over stdio)**
**Version:** 1.0 (initial)
**Status:** Draft – implementation-ready

⸻

**1. Purpose & Scope**

**1.1 Problem & Importance**

Teams increasingly coordinate multiple AI-powered agents (builder, reviewer, spec‑maintainer, compliance) to produce code and artifacts. In local-first environments, we need a deterministic, inspectable orchestrator that:
	•	runs on a single machine (macOS/Linux),
	•	uses the filesystem as the source of truth,
	•	coordinates agent processes via stdio (NDJSON),
	•	persists outcomes durably for audit and re-runs,
	•	remains portable with minimal dependencies.

**1.2 Goals**
	•	**Local-first orchestration.** No external brokers or servers in v1.
	•	**Clear agent boundaries.** Orchestrator alone routes messages; agents never talk directly.
	•	**Deterministic persistence.** Filesystem is canonical; atomic writes, receipts, and manifests.
	•	**Robust process lifecycle.** Launch, supervise, heartbeat, restart with backoff.
	•	**Idempotent operations.** Safe to re-run after crash or partial progress.
	•	**Actionable interfaces.** A precise NDJSON protocol, file conventions, and CLI entry points.
	•	**Extensible design.** Add/replace agent types without redesigning the core.

**1.3 Non‑Goals (v1)**
	•	Distributed execution across multiple machines.
	•	External message buses (Redis/NATS/etc.).
	•	Agent-to-agent direct channels.
	•	Rich UI/GUI; v1 is CLI-oriented with file outputs.
	•	Cloud storage or network persistence (local FS only).

**1.4 Success Criteria**
	•	A builder agent can implement tasks; reviewer and compliance agents can validate and drive iterations; spec‑maintainer can update specs.
	•	Three or more end‑to‑end runs complete with deterministic artifacts and receipts.
	•	Re-run after orchestrator crash produces identical artifacts (or cleanly skips via idempotency receipts).
	•	Protocol conformance tests pass for command/event/heartbeat schemas.

⸻

**2. Architecture Overview**

**2.1 Components**

```
+-----------------------------+
|         Orchestrator        |
| - CLI entrypoint            |
| - Agent supervisor          |
| - Router (NDJSON stdio)     |
| - Ledger (events)           |
| - Scheduler                 |
| - State/recovery            |
+--^-----------^-----------^--+
   |           |           |
   | stdio     | stdio     | stdio
   |           |           |
+--+--+     +--+--+     +--+--+
|Agent|     |Agent|     |Agent|
|Build|     |Review|    |Compliance|
+-----+     +------+    +---------+
      \        |             /
       \       |            /
        \      |           /
         v     v          v
          +----------------------+
          |    Filesystem        |
          | /specs  /src /tests  |
          | /reviews /compliance |
          | /events /logs /state |
          +----------------------+

```

**Trust Boundaries**
	•	**Orchestrator** is trusted to enforce protocol, routing, lifecycle, and persistence.
	•	**Agents** are untrusted processes. They must be sandboxable and cannot access each other.
	•	**Filesystem** is the canonical source of truth. The orchestrator verifies and records all changes via manifests/receipts.

**2.2 Agent Roles & Responsibilities**
	•	**Builder**: Implements tasks (write/modify code, tests, docs). Emits granular progress and a builder.completed event with produced artifacts.
	•	**Reviewer**: Reads outputs and emits review.completed with status: approved or changes_requested, including findings and file-level comments.
	•	**Compliance**: Validates policy checks, licensing, tests, lint, security scans; emits compliance.completed with pass|fail and evidence.
	•	**Spec‑Maintainer**: Updates /specs/*.md and task specs; emits spec.updated and/or changes_requested for structure or scope.

**2.3 Orchestrator Responsibilities**
	•	Load configuration (orchestrate.yaml), create run state.
	•	Spawn agents via CLI wrappers; connect stdio; log/stdout capture.
	•	Route *commands* to agents; read *events/heartbeats/logs* from agents.
	•	Maintain a durable **ledger** of all messages and **receipts** for artifacts.
	•	Decide **who acts next** using routing rules & policy.
	•	Handle timeouts, restarts, backoff, and resumability.

⸻

**3. IPC Protocol (NDJSON over stdio)**

**Transport:** UTF‑8 NDJSON, one JSON object per line, no pretty printing.
**Envelope rules:**
	•	kind determines schema: "command" | "event" | "heartbeat" | "log".
	•	message_id is unique (UUIDv4 recommended).
	•	correlation_id ties responses to a particular command (commands set it; events echo it).
	•	task_id scopes work to a single task.
	•	Timestamps: RFC 3339 UTC.

**Line Size Limits:**
	•	Max message size: **256 KiB**.
	•	Larger payloads (diffs, logs, artifacts) must be referenced by file paths and checksums rather than inlined.

**3.1 Message: command**

Used by the orchestrator → agent.

**Actions (enum):**
implement, implement_changes, review, compliance_check, finalize, update_spec

**Fields (summary):**
	•	kind: "command"
	•	message_id, correlation_id, task_id, idempotency_key
	•	to: { agent_type, agent_id? }
	•	action
	•	inputs: freeform JSON (paths, hints)
	•	expected_outputs: artifact descriptors (paths, patterns)
	•	version: { specs_hash?, code_hash?, snapshot_id }
	•	deadline: RFC 3339
	•	retry: { attempt, max_attempts }
	•	priority: integer

**JSON Schema – Command**

```
{
  "$id": "https://local.orchestrator/schemas/command.v1.json",
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
        "agent_type": { "type": "string", "enum": ["builder","reviewer","compliance","spec_maintainer"] },
        "agent_id": { "type": "string" }
      }
    },
    "action": {
      "type": "string",
      "enum": ["implement","implement_changes","review","compliance_check","finalize","update_spec"]
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

**Example Command (implement):**

{"kind":"command","message_id":"6c4a...","correlation_id":"corr-T-0042-1","task_id":"T-0042","idempotency_key":"ik:implement:T-0042:v3","to":{"agent_type":"builder"},"action":"implement","inputs":{"sections":["3.1","3.2","3.3"],"spec_path":"specs/MASTER-SPEC.md"},"expected_outputs":[{"path":"src/foo/bar.js"},{"path":"tests/foo/bar.spec.js"}],"version":{"snapshot_id":"snap-0007","specs_hash":"sha256:...","code_hash":"sha256:..."},"deadline":"2025-10-13T19:00:00Z","retry":{"attempt":0,"max_attempts":3},"priority":5}


⸻

**3.2 Message: event**

Emitted by agents → orchestrator.

**Event Types (non-exhaustive):**
	•	builder.progress, builder.completed
	•	review.completed (status: approved|changes_requested)
	•	compliance.completed (status: pass|fail)
	•	spec.updated, changes.requested
	•	artifact.produced (with path and checksum)
	•	error (machine-readable), log (structured)

**Fields (summary):**
	•	kind: "event"
	•	message_id, correlation_id, task_id
	•	from: { agent_type, agent_id? }
	•	event: string type
	•	status: optional enum depending on type
	•	payload: freeform JSON
	•	artifacts: optional array of { path, sha256, size }
	•	observed_version: mirror of version from command when relevant
	•	occurred_at: RFC 3339

**JSON Schema – Event**

```
{
  "$id": "https://local.orchestrator/schemas/event.v1.json",
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "Event",
  "type": "object",
  "additionalProperties": false,
  "required": [
    "kind","message_id","correlation_id","task_id",
    "from","event","occurred_at"
  ],
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
        "agent_type": { "type": "string", "enum": ["builder","reviewer","compliance","spec_maintainer"] },
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

**Example Event (builder.completed):**

{"kind":"event","message_id":"e1b3...","correlation_id":"corr-T-0042-1","task_id":"T-0042","from":{"agent_type":"builder","agent_id":"builder#1"},"event":"builder.completed","status":"success","payload":{"notes":"Implemented sections 3.1–3.3"},"artifacts":[{"path":"src/foo/bar.js","sha256":"sha256:...","size":1432},{"path":"tests/foo/bar.spec.js","sha256":"sha256:...","size":782}],"observed_version":{"snapshot_id":"snap-0007"},"occurred_at":"2025-10-13T18:10:02Z"}

**Example Event (review.completed changes_requested):**

{"kind":"event","message_id":"a9c2...","correlation_id":"corr-T-0042-2","task_id":"T-0042","from":{"agent_type":"reviewer"},"event":"review.completed","status":"changes_requested","payload":{"review_path":"reviews/T-0042.json","summary":"Edge cases missing in bar.js","required_changes":["handle null inputs","add error branch test"]},"occurred_at":"2025-10-13T18:20:41Z"}

**Example Event (compliance.completed pass):**

{"kind":"event","message_id":"c05d...","correlation_id":"corr-T-0042-3","task_id":"T-0042","from":{"agent_type":"compliance"},"event":"compliance.completed","status":"pass","payload":{"report_path":"compliance/T-0042.json","coverage":0.91},"occurred_at":"2025-10-13T18:30:12Z"}


⸻

**3.3 Message: heartbeat**

Emitted by agent shims on an interval to indicate liveness to the orchestrator.

**Fields (summary):**
	•	kind: "heartbeat"
	•	agent: { agent_type, agent_id }
	•	seq: monotonically increasing integer per process
	•	status: starting|ready|busy|stopping|backoff
	•	pid, ppid
	•	uptime_s, last_activity_at
	•	stats: { cpu_pct, rss_bytes }
	•	task_id?: when busy

**JSON Schema – Heartbeat**

```
{
  "$id": "https://local.orchestrator/schemas/heartbeat.v1.json",
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
        "agent_type": { "type": "string", "enum": ["builder","reviewer","compliance","spec_maintainer"] },
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


⸻

**3.4 Logs & Errors**
	•	**Logs**: {"kind":"log","level":"info|warn|error","message":"...","fields":{...},"timestamp":"..."}
	•	**Errors**: Either as event with event:"error" and machine-readable payload.code, or as log with level:"error".
For conformance, agents **must** use event:"error" for action-level failures.

⸻

**4. Process Flow**

**4.1 Typical Run (init → implement → review → compliance → finalize)**
	1.	**Initialization**
	•	Orchestrator loads orchestrate.yaml, allocates run_id, creates /state/run.json, and takes a **snapshot** (snap-0007) of the workspace manifest.
	•	Spawns agent shims (builder, reviewer, compliance, spec‑maintainer), connects stdio, and begins heartbeat supervision.
	2.	**Implement**
	•	Router issues command(action=implement) to **builder** with idempotency_key derived from task inputs and snapshot (see §5.4).
	•	Builder writes artifacts to temp files, atomically renames to targets, and emits artifact.produced events, then builder.completed.
	3.	**Review**
	•	Router issues command(action=review) to **reviewer** referencing produced artifacts. Reviewer writes /reviews/<task>.json and emits review.completed with approved or changes_requested.
	•	If changes_requested, orchestrator branches to **Implement changes**.
	4.	**Implement changes (if needed)**
	•	Router issues command(action=implement_changes) to **builder** referencing review file; follow deterministic writes; return to **Review**.
	5.	**Compliance**
	•	Router issues command(action=compliance_check) to **compliance**. Compliance writes /compliance/<task>.json and emits compliance.completed pass|fail.
	•	On fail, orchestrator may route back to builder or spec‑maintainer per policy.
	6.	**Finalize**
	•	On review.approved **and** compliance.pass, router issues command(action=finalize) to **spec_maintainer** (optional) to update /specs status sections; orchestrator writes a **closing receipt**, updates /state/run.json to completed.

**4.2 Example Exchanges (3 concrete)**

**Exchange A – Implement**

```
Orchestrator -> Builder: command implement (corr-T-0042-1)
Builder -> Orchestrator: event artifact.produced (src/foo/bar.js)
Builder -> Orchestrator: event artifact.produced (tests/foo/bar.spec.js)
Builder -> Orchestrator: event builder.completed (status=success)

```

**Exchange B – Review requests changes**

```
Orchestrator -> Reviewer: command review (corr-T-0042-2)
Reviewer -> Orchestrator: event review.completed (status=changes_requested, review_path=reviews/T-0042.json)

```
Orchestrator -> Builder: command implement_changes (corr-T-0042-2b, references reviews/T-0042.json)

**Exchange C – Compliance pass & finalize**

```
Orchestrator -> Compliance: command compliance_check (corr-T-0042-3)
Compliance -> Orchestrator: event compliance.completed (status=pass, report_path=compliance/T-0042.json)
Orchestrator -> SpecMaint: command update_spec (corr-T-0042-4)
SpecMaint -> Orchestrator: event spec.updated (payload: updated sections)

```

**4.3 Failure/Retry Branches**
	•	**Agent timeout** → orchestrator sends SIGTERM, waits grace period, escalates to SIGKILL, restarts with exponential backoff; resend command **with same idempotency_key**.
	•	**Validation fail** → orchestrator routes to builder with implement_changes.
	•	**Multiple reviews** → all run; policy: any changes_requested triggers changes; else approved.

⸻

**5. Persistence & State**

**5.1 Durable vs. Ephemeral**
	•	**Durable (files):**
	•	/specs/**, /src/**, /tests/**
	•	/reviews/<task>.json, /compliance/<task>.json
	•	/events/<run_id>.ndjson (append-only ledger)
	•	/receipts/<task>/<step>.json (artifact manifests)
	•	/state/run.json, /state/index.json (current pointers)
	•	/logs/<agent>/<run_id>.ndjson
	•	**Ephemeral:**
	•	In-flight NDJSON streams on stdio
	•	Orchestrator’s in-memory scheduler queues

**5.2 Directories & Conventions**

```
/specs/MASTER-SPEC.md
/src/**            # source
/tests/**          # test files
/reviews/T-0042.json
/compliance/T-0042.json
/events/run-<timestamp>-<shortid>.ndjson
/receipts/T-0042/step-<n>.json
/logs/<agent_type>/<run_id>.ndjson
/state/run.json               # active run snapshot pointer & status
/state/index.json             # task registry (id -> last run_id, snapshot_id)
/snapshots/snap-0007.manifest.json
/tmp-orch/**                  # temp, orchestrator-managed

```

**5.3 Snapshots & Manifests**
	•	A **snapshot** is a manifest file listing all tracked files (paths, sha256, size, mtime).
	•	snapshot_id is content-addressed: snap-<first8(sha256(manifest))>.
	•	Agents echo observed_version.snapshot_id back on events.

**5.4 Idempotency Keys (IK)**
	•	**Definition:** ik = H(action, task_id, snapshot_id, sorted(inputs), sorted(expected_outputs)) (e.g., SHA‑256 hex prefixed with ik:).
	•	**Agent contract:**
	•	If an agent receives a command with an idempotency_key it has **already completed** successfully, it must:
	•	**not** rewrite artifacts,
	•	emit event indicating status:"success" and reference prior receipts (idempotent acknowledgment).
	•	**Orchestrator behavior on retry:** resends same idempotency_key until a different snapshot or inputs change.

**5.5 Deterministic Writes**
	•	Agents must:
	1.	Write to a temp file under the same directory: .<basename>.tmp.<pid>.<rand>.
	2.	fsync(temp_fd); fsync(dir_fd) after atomic rename() to final path.
	3.	Emit artifact.produced (path, sha256, size).
	4.	Orchestrator records a **receipt** including idempotency_key, artifact metadata, and the producing event message IDs.

**5.6 Rehydration After Crash/Restart**
	•	On startup, orchestrator:
	•	Reads /state/run.json and /events/<run_id>.ndjson.
	•	Reconstructs last known step from latest builder.completed|review.completed|compliance.completed.
	•	Verifies artifact receipts exist and files match checksums.
	•	Resends the **next** command; if prior command had no terminal event, resend it with the **same idempotency_key**.

⸻

**6. Routing Rules**

**6.1 Who Acts Next**
	•	Default policy for a task:
	1.	Builder implement
	2.	Reviewer review
	3.	If changes_requested → Builder implement_changes → back to Reviewer
	4.	If approved → Compliance compliance_check
	5.	If pass → Spec‑Maintainer update_spec (optional) → Finalize

**6.2 Correlation & Task IDs**
	•	task_id is user-provided (e.g., T-0042).
	•	correlation_id per command chain; reuses same value across retries of the same logical action.
	•	run_id scoped to orchestrator run; included in logs and ledger filename.

**6.3 Version Pinning**
	•	Commands include version.snapshot_id (and optional code_hash/specs_hash).
	•	Agents must check observed_version.snapshot_id matches the command; otherwise emit event error with code:"version_mismatch".

⸻

**7. Error Handling & Recovery**

**7.1 Timeouts**
	•	**Heartbeat interval:** 10s (configurable per agent).
	•	**Missed heartbeats tolerance:** 3 intervals → agent assumed unhealthy.
	•	**Command execution timeout:** per action (default 10m implement, 5m review, 5m compliance, 2m update_spec).

**7.2 Backoff & Restart**
	•	Exponential backoff: initial 1s, multiplier 2, max 60s, full jitter.
	•	Max restarts per agent per run: 5 (configurable). Exceeding → run fails with diagnostics.

**7.3 Orphaned Processes**
	•	On orchestrator start, adopt children via PID handshake (agent shims emit heartbeat status=starting with ppid). If adoption fails, orchestrator issues termination then clean start.

**7.4 Partial Writes & Conflicts**
	•	Orchestrator validates artifact checksums against receipts; partial or mismatched writes cause command failure with code:"artifact_mismatch", triggering retry.
	•	**Multiple reviews**: merge logic (policy default):
	•	If any changes_requested → route to builder.
	•	If all approved → proceed to compliance.
	•	Ties/errors → escalate to spec‑maintainer for adjudication (changes.requested event allowed).

**7.5 Resumability**
	•	All commands are idempotent (via IK). Re-sent commands must not duplicate artifacts.
	•	Ledger is append-only; reconstruction relies on last terminal event per correlation.

⸻

**8. Security & Safety**
	•	**Filesystem Permissions:** Orchestrator creates directories with umask 077; files default 0600 (override via config).
	•	**Path Sanitization:** Reject paths containing .., absolute paths outside workspace root, or symlinks escaping root.
	•	**Secrets Redaction:** Environment variables matching *_TOKEN|*_KEY|*_SECRET are masked in logs/events.
	•	**Sandboxing (recommended):** Run agent shims under separate users or namespaces (where available), with restricted network access.
	•	**Resource Limits:** Use OS ulimit/rlimit to cap CPU time, file descriptors, and memory per agent.

⸻

**9. Performance & Limits**
	•	**Message size:** 256 KiB max; spill to files for larger content.
	•	**Artifact size:** configurable; default warn >100 MiB; hard cap 1 GiB.
	•	**Concurrency policy:**
	•	Default: **one active agent per task**; multiple tasks can run in parallel (configurable N).
	•	Router ensures no two agents concurrently mutate the same path set.
	•	**Streaming Handling:** Events may stream progress; orchestrator applies backpressure by draining stdout continuously; if agent blocks on full pipe, orchestrator prioritizes reads.

⸻

**10. Extensibility**

**10.1 Adding a New Agent Type**
	•	Implement CLI shim supporting this spec:
	•	Read command NDJSON on stdin.
	•	Emit heartbeat every heartbeat_interval_s.
	•	Emit event/log NDJSON to stdout.
	•	Register the agent in orchestrate.yaml under agents.
	•	Provide smoke tests and schema conformance fixtures.

**10.2 Swapping Models/Tools**
	•	Agent shims wrap any model/tool CLIs. The orchestrator is agnostic to the internals.
	•	Configuration selects binaries, args, and env per agent.

**10.3 Configuration Format (orchestrate.yaml)**

```
version: "1.0"
workspace_root: "."
tasks:
  - id: "T-0042"
    goal: "Implement sections 3.1–3.3 of /specs/MASTER-SPEC.md"
policy:
  max_parallel_tasks: 2
  message_max_bytes: 262144
  artifact_max_bytes: 1073741824
  retry:
    max_attempts: 3
    backoff: { initial_ms: 1000, max_ms: 60000, multiplier: 2.0, jitter: "full" }
agents:
  builder:
    cmd: ["./agents/builder.sh"]
    cwd: "."
    env: { MODEL: "local-llm", LOG_LEVEL: "info" }
    heartbeat_interval_s: 10
    timeouts: { implement_s: 600, implement_changes_s: 600 }
  reviewer:
    cmd: ["./agents/reviewer.sh"]
    heartbeat_interval_s: 10
    timeouts: { review_s: 300 }
  compliance:
    cmd: ["./agents/compliance.sh"]
    timeouts: { compliance_check_s: 300 }
  spec_maintainer:
    cmd: ["./agents/spec-maint.sh"]
feature_flags:
  - strict_version_pinning

```
  - redact_secrets_in_logs

**10.4 Feature Flags**
	•	strict_version_pinning: reject events without matching observed_version.
	•	ledger_checksums: hash each ledger line for tamper-evidence.
	•	parallel_reviews: spawn multiple reviewers and aggregate.

⸻

**11. Testing & Validation**

**11.1 Protocol Conformance**
	•	Validate agent output against JSON Schemas for **command**, **event**, **heartbeat** (this spec).
	•	Negative tests: oversize messages, missing required fields, invalid enums.

**11.2 Agent Smoke Tests**
	•	Builder: receives implement with IK X → writes artifact → emits builder.completed with correct checksum.
	•	Reviewer: accepts inputs → writes /reviews/<task>.json → emits review.completed.
	•	Compliance: runs checks → emits compliance.completed pass|fail.

**11.3 Minimal E2E Scenario**

**Initial Use Case**
Task T-0042: Implement sections 3.1–3.3 of /specs/MASTER-SPEC.md.

**Before (Directory Snapshot)**

```
.
├─ specs/
│  └─ MASTER-SPEC.md        # missing sections 3.1–3.3
├─ src/
├─ tests/
├─ orchestrate.yaml
└─ state/

```

**Run Outline**
	1.	Orchestrator snapshot → snap-0007.
	2.	Command(A): implement → Builder creates:
	•	src/foo/bar.js
	•	tests/foo/bar.spec.js
	3.	Command(B): review → Reviewer writes reviews/T-0042.json with changes_requested.
	4.	Command(C): implement_changes → Builder updates files; emits builder.completed.
	5.	Command(D): review → Reviewer approved.
	6.	Command(E): compliance_check → Compliance pass.
	7.	Command(F): update_spec → Spec‑Maintainer updates specs/MASTER-SPEC.md to mark sections complete.

**After (Directory Snapshot)**

```
.
├─ specs/
│  └─ MASTER-SPEC.md           # sections 3.1–3.3 implemented & marked complete
├─ src/
│  └─ foo/bar.js
├─ tests/
│  └─ foo/bar.spec.js
├─ reviews/
│  └─ T-0042.json
├─ compliance/
│  └─ T-0042.json
├─ events/
│  └─ run-20251013-1810Z-abc123.ndjson
├─ receipts/
│  └─ T-0042/
│     ├─ step-1.json           # implement
│     ├─ step-2.json           # implement_changes
│     └─ finalize.json
├─ logs/
│  ├─ builder/run-...ndjson
│  ├─ reviewer/run-...ndjson
│  ├─ compliance/run-...ndjson
│  └─ spec_maintainer/run-...ndjson
└─ state/
   ├─ run.json                 # status=completed
   └─ index.json               # task->last run, snapshot pointer

```

**Validation Checks**
	•	Checksums of src/foo/bar.js and test file match receipts/*.
	•	Ledger contains terminal events for all correlations.
	•	reviews/T-0042.json shows approved in final iteration.
	•	compliance/T-0042.json status pass.

⸻

**12. Operational Behavior**

**12.1 CLI Entry Points**
	•	orchestrate run --task T-0042 [--config orchestrate.yaml]
	•	orchestrate resume --run <run_id>
	•	orchestrate validate --schemas (schema linting)
	•	orchestrate doctor (env & capability checks)

**12.2 Process Supervision**
	•	Child processes started with inherited environment + overlay:
	•	ORCH_RUN_ID, ORCH_TASK_ID, ORCH_WORKSPACE_ROOT, ORCH_HEARTBEAT_INTERVAL_S
	•	Stdout consumed line-by-line; stderr mirrored to /logs/<agent>/... as log records with level:"error" unless valid NDJSON.

**12.3 Heartbeats & Health**
	•	If no heartbeat for 30s (default), mark agent unhealthy.
	•	On unhealthy during active command → cancel, restart, resend (same IK).

**12.4 Logging**
	•	All incoming/outgoing messages mirrored to:
	•	/events/<run_id>.ndjson (router-level ledger; append-only)
	•	/logs/<agent>/<run_id>.ndjson (raw agent lines, validated or not)

**12.5 Recovery & Resume**
	•	resume reconstructs last stable step (§5.6), validates receipts, and continues.

⸻

**13. Interfaces & File Conventions**

**13.1 Message Schemas**
	•	**Command / Event / Heartbeat** as specified in §3 (JSON Schemas included).
	•	**Error/Log** follow §3.4.

**13.2 Artifact Receipts**

```
/receipts/<task>/<step>.json

```

```
{
  "task_id": "T-0042",
  "step": 1,
  "idempotency_key": "ik:implement:T-0042:v3",
  "artifacts": [
    {"path":"src/foo/bar.js","sha256":"sha256:...","size":1432},
    {"path":"tests/foo/bar.spec.js","sha256":"sha256:...","size":782}
  ],
  "events": ["e1b3...","..."],
  "created_at": "2025-10-13T18:10:04Z"
}

```

**13.3 Review & Compliance File Formats**
	•	/reviews/<task>.json

```
{
  "task_id":"T-0042",
  "status":"changes_requested|approved",
  "findings":[{"path":"src/foo/bar.js","line":42,"comment":"Handle null inputs"}],
  "summary":"...",
  "created_at":"2025-10-13T18:20:40Z"
}

```

	•	/compliance/<task>.json

```
{
  "task_id":"T-0042",
  "status":"pass|fail",
  "checks":[{"name":"unit_tests","status":"pass","coverage":0.91}],
  "created_at":"2025-10-13T18:30:12Z"
}

```


⸻

**14. Pseudocode (Key Mechanisms)**

**14.1 Orchestrator Main Loop**

```
load_config()
run_id = new_run_id()
init_state(run_id)
spawn_agents(config.agents)

while tasks_remaining():
  task = next_task()
  state = restore_state(task)

  if state.needs_implement():
    send_command(builder, implement, ik=compute_ik(...))
    await_terminal_event_or_retry()

  if state.needs_review():
    send_command(reviewer, review, ik=compute_ik(...))
    await_terminal_event_or_retry()

  if state.review_changes_requested():
    send_command(builder, implement_changes, ik=compute_ik(...))
    await_terminal_event_or_retry()
    goto review

  if state.needs_compliance():
    send_command(compliance, compliance_check, ik=compute_ik(...))
    await_terminal_event_or_retry()

  if state.compliance_passed():
    optionally send_command(spec_maintainer, update_spec)
    mark_complete(task)

```

**14.2 Router (Reading Agent Streams)**

```
for each agent_proc in procs:
  on_stdout_line(line):
    msg = parse_json(line)
    validate_schema(msg)
    append_to_ledger(msg)
    update_state_with_event(msg)

```
    maybe_route_next_command()

**14.3 Heartbeat Supervisor**

```
every heartbeat_interval:
  for each agent:
    if now - agent.last_heartbeat > 3 * interval:
      mark_unhealthy(agent)
      cancel_inflight(agent)
      restart(agent)

```
      resend_last_command_with_same_ik()

**14.4 Safe Write Helper (for Agents)**

```
function atomic_write(path, bytes):
  tmp = dirname(path) + "/." + basename(path) + ".tmp." + pid + "." + rand()
  write(tmp, bytes)
  fsync(tmp)
  rename(tmp, path)  // atomic within same filesystem
  fsync(dirname(path))

```


⸻

**15. Compliance & Validation**

**15.1 What Constitutes a Passing Build**
	•	All required artifacts exist and match receipt checksums.
	•	Review status is approved.
	•	Compliance status is pass with evidence.
	•	Ledger shows terminal events for each correlation with no orphaned commands.
	•	State shows completed and last snapshot recorded.

**15.2 Reviewer/Compliance Agent Conformance**
	•	Agents emit events adhering to schemas.
	•	For idempotent re-commands, agents do not duplicate writes and reference prior receipts.

**15.3 Protocol Tests**
	•	Provide fixtures:
	•	Valid/invalid command lines (boundary sizes).
	•	Event sequences including error and log.
	•	Heartbeat cadence and missed-heartbeat scenarios.

⸻

**16. Operational Limits & Defaults (recommended)**

**Setting**	**Default**	**Notes**
Heartbeat interval	10s	agent → orchestrator
Missed heartbeat threshold	3	then restart with backoff
Implement timeout	600s	per command
Review timeout	300s	per command
Compliance timeout	300s	per command
Max message size	256 KiB	larger payloads via files
Artifact hard cap	1 GiB	configurable
Max parallel tasks	2	configurable
Max agent restarts per run	5	per agent


⸻

**17. Example CLI & Message Transcripts**

**Start a run**

```
$ orchestrate run --task T-0042

```

**Ledger excerpts (/events/run-...ndjson)**

```
{"kind":"command","correlation_id":"corr-T-0042-1",...}
{"kind":"event","event":"artifact.produced",...}
{"kind":"event","event":"builder.completed",...}
{"kind":"command","correlation_id":"corr-T-0042-2",...}
{"kind":"event","event":"review.completed","status":"changes_requested",...}
{"kind":"command","correlation_id":"corr-T-0042-2b","action":"implement_changes",...}
{"kind":"event","event":"builder.completed",...}
{"kind":"command","correlation_id":"corr-T-0042-2c","action":"review",...}
{"kind":"event","event":"review.completed","status":"approved",...}
{"kind":"command","correlation_id":"corr-T-0042-3","action":"compliance_check",...}
{"kind":"event","event":"compliance.completed","status":"pass",...}
{"kind":"command","correlation_id":"corr-T-0042-4","action":"update_spec",...}

```
{"kind":"event","event":"spec.updated",...}


⸻

**18. Alternative Design Paths (for awareness)**
	•	**Versioning backends**
	•	*Option A (recommended v1):* Manifest-based snapshots in /snapshots.
	•	*Option B (optional):* Use Git if available; store commit hash in snapshot_id.
**Pros A:** No external dependency; **Cons A:** Larger local storage.
**Pros B:** Native diffs/history; **Cons B:** Introduces tool dependency.
	•	**Multiple reviewers concurrency**
	•	*Option A:* Sequential single reviewer (simpler).
	•	*Option B:* Parallel reviewers with aggregation (feature flag).
Recommendation: Start with A; enable B via parallel_reviews.

⸻

**19. Guidelines for Writing Future Specifications**
	•	Use precise, testable language and define schemas early.
	•	Specify directory layouts, file formats, and naming conventions.
	•	Always define idempotency and recovery behavior.
	•	Provide at least one E2E example with before/after snapshots.
	•	Prefer configuration-driven variability over bespoke code paths.

⸻

**20. Appendix: Message Examples (copy/paste ready)**

**A. artifact.produced**

{"kind":"event","message_id":"m1","correlation_id":"corr-T-0042-1","task_id":"T-0042","from":{"agent_type":"builder"},"event":"artifact.produced","payload":{"description":"main module"},"artifacts":[{"path":"src/foo/bar.js","sha256":"sha256:...","size":1432}],"occurred_at":"2025-10-13T18:09:58Z"}

**B. error (version mismatch)**

{"kind":"event","message_id":"m2","correlation_id":"corr-T-0042-1","task_id":"T-0042","from":{"agent_type":"builder"},"event":"error","status":"failed","payload":{"code":"version_mismatch","expected_snapshot":"snap-0007","observed_snapshot":"snap-0006"},"occurred_at":"2025-10-13T18:10:00Z"}

**C. heartbeat (busy)**

{"kind":"heartbeat","agent":{"agent_type":"reviewer","agent_id":"reviewer#1"},"seq":42,"status":"busy","pid":32100,"ppid":32000,"uptime_s":122.4,"last_activity_at":"2025-10-13T18:20:20Z","stats":{"cpu_pct":23.1,"rss_bytes":73400320},"task_id":"T-0042"}


⸻

**End of MASTER-SPEC.md**

*This document is self-contained, implementation-ready, and verifiable. If you want this tailored to a specific language/toolchain or extended with Git-backed snapshots in v1, I can include those details in a follow-up revision.*
