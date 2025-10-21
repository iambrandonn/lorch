# Implementation Plan (High-Level)

- **Implementation stack**: ✅ Go (target macOS + Linux equally, statically linked binaries). Assume latest stable Go toolchain (≥1.22) with modules.
- **Agent strategy**: need to build agent shims from scratch; initial implementation wraps Claude Code via CLI (`claude` on `$PATH`). Roles passed via CLI prompt interpolation (e.g., `claude "You are the $ROLE agent"`); design shims so alternative CLIs can be swapped in later.
- **Workspace baseline**: ✅ greenfield repository.
- **Platform priority**: treat macOS and Linux as first-class targets from Phase 1 onward (tooling and packaging should support both).
- **Testing expectations**: aim for high-quality, meaningful coverage (unit + integration); use mocks for deterministic tests and supplement with optional LLM-in-the-loop smoke runs; no artificial 100% threshold.

> Pending answers feed back into the detailed sub-plans for each phase.

## Phase 1 – Core Orchestrator Foundation
Deliver `lorch run` with builder/reviewer/spec-maintainer agents, deterministic persistence, and resumable runs.

### P1.1 Milestone – Workspace & CLI Skeleton ✅ **COMPLETE**
- **Tests first**: define unit specs for config generation/validation and golden snapshot for default `lorch.json`.
- **Task A**: scaffold Go module (`go mod init`), create `cmd/lorch` entrypoint with subcommands (`lorch`, `lorch run`, `lorch resume`). ✅
- **Task B**: implement bootstrap flow to detect/create workspace directories (`state/`, `events/`, `receipts/`, `logs/`, etc.). ✅
- **Task C**: implement config loader/auto-generator matching defaults (Claude agents, policy settings). ✅
- **Task D**: add validation layer (struct tags or JSON schema) with user-friendly errors. ✅
- **Exit criteria** ✅: tests from step one pass, CLI can initialize a fresh workspace, `lorch.json` matches expected golden file.

### P1.2 Milestone – Agent Supervisor & IPC Core ✅ **COMPLETE**
- **Tests first**: craft mock-agent harness specs (echo NDJSON, heartbeat simulation) and scheduler sequencing tests.
- **Task A**: implement agent registry with subprocess lifecycle management (start/stop, restart hooks). ✅
- **Task B**: build NDJSON framing utilities (encoder/decoder, size enforcement, structured logging). ✅
- **Task C**: implement single-agent scheduler enforcing Implement → Review → Spec Maintenance order with back-pressure. ✅
- **Task D**: pipe transcripts to console and persist raw streams to `/events/<run>.ndjson`. ✅
- **Exit criteria** ✅: mock-agent tests pass; transcripts and event logs show correct sequencing.

### P1.3 Milestone – Idempotency & Persistence ✅ **COMPLETE**
> **Status**: Completed 2025-10-20
> **Summary**: Full implementation of idempotency keys, workspace snapshots, event ledger, run state persistence, and crash recovery via `lorch resume`. All core functionality tested and working end-to-end.

- **Tests first** ✅
  - Unit tests for canonical command hashing → deterministic idempotency keys.
  - Ledger append/replay tests (including checksum verification and out-of-order rejection).
  - Receipt-writing golden tests (JSON structure, checksum fields, atomic write behaviour).
  - Integration test simulating crash/restart with mock agents (re-run command with same IK, ensure no duplicates).
- **Task A – Idempotency Key Generator & Snapshot Metadata** ✅
  - Implement snapshot capture stub (record `snapshot_id`, placeholder hashes) invoked at run start.
  - Build canonical serialization + SHA256 hashing to derive IK per command (action/task/snapshot/inputs).
  - Persist snapshot metadata to `/snapshots/snap-XXXX.manifest.json` (minimal schema for now).
  - **Delivered**: `internal/idempotency`, `internal/snapshot`, `internal/checksum` packages with full test coverage.
- **Task B – Ledger Writer & Event Persistence** ✅
  - Extend event logging to append commands/events/heartbeats to `/events/<run>.ndjson` with metadata entries (timestamp, IK, checksum) as per spec §5.
  - Ensure append-only semantics, verify writes with checksum/hmac when reading.
  - Implement ledger reader that can reconstruct in-flight state on resume.
  - **Delivered**: `internal/ledger` package with replay logic and 256 KiB message support.
- **Task C – Receipt Pipeline** ✅
  - Write receipts to `/receipts/<task>/<step>.json` capturing artifacts, IK, message IDs.
  - Use atomic write pattern (tmp file + fsync + rename) and include SHA256 of artifact payloads (use `crypto/sha256`).
  - Add helper to compute file checksums for verification.
  - **Delivered**: `internal/receipt` and `internal/fsutil` packages with atomic write utilities.
- **Task D – Resume & Crash Recovery** ✅
  - Implement `/state/run.json` tracking run status, current stage, last message IDs.
  - On `lorch resume --run <id>`, reload state, replay ledger, send pending commands with original IKs.
  - Add crash simulation test: start run, stop mid-flight, restart via resume, ensure no duplicate work and consistent transcripts/ledger.
  - **Delivered**: `internal/runstate` package, `lorch resume` command, crash/restart integration tests.
- **Task E – CLI Wiring & Regression Harness** ✅
  - Wire `lorch run` to instantiate scheduler + supervisors, attach event logger/transcript formatter, and kick off Phase 1 pipeline using real components.
  - Provide flag/option to run with mock agents for local smoke tests (optional dev tool).
  - Update documentation (`README`, PLAN next steps) to reflect runnable path.
  - **Delivered**: Fully integrated `lorch run` command with snapshot capture, IK generation, and persistence.
- **Exit criteria** ✅ **MET**
  - All new tests (unit + crash/restart integration) pass. ✅ (~30 new tests, all passing)
  - Running `lorch run` produces event log, receipts, and snapshot metadata; resuming after simulated crash succeeds without duplicate work. ✅ (Verified end-to-end)
  - CLI transcripts/event logs align with spec formatting and content. ✅ (Matches MASTER-SPEC §5)

**Additional Deliverables**:
- 📦 7 new packages: `checksum`, `fsutil`, `idempotency`, `snapshot`, `receipt`, `ledger`, `runstate`
- 📝 Documentation: `docs/IDEMPOTENCY.md`, `docs/RESUME.md`
- 🧪 Test coverage: ~1,500 lines of test code across all packages
- 🔧 Bug fixes: Ledger scanner buffer sizing, snapshot determinism
- 📊 Implementation summary: `P1.3-IMPLEMENTATION-SUMMARY.md`

### P1.4 Milestone – Builder/Test Enforcement & Spec Loop Closure ✅ **COMPLETE**
> **Status**: Completed 2025-10-20
> **Summary**: Builder test enforcement, spec_notes artifact handling, and granular spec loop resumption fully implemented and tested. All exit criteria met. See `P1.4-IMPLEMENTATION-SUMMARY.md` for details.

- **Tests first** ✅
  - ✅ Scheduler integration tests cover all required scenarios
  - ✅ Builder test validation: missing payload, invalid payload, failing tests, allowed_failures
  - ✅ Spec-maintainer loop: spec.changes_requested with spec_notes artifacts, spec.no_changes_needed
  - ✅ Receipt tests verify test summaries and spec-note artifacts persist
  - ✅ Crash/resume test verifies granular spec loop continuation

- **Task A – Builder result contract enforcement** ✅
  - ✅ `validateBuilderTestResults()` helper in `scheduler.go:358-424`
  - ✅ Rejects builder completions without valid `tests` payload with clear error messages
  - ✅ Structured test metadata persisted in receipts and event log
  - ✅ Test fixtures created: missing-tests, invalid-tests, tests-failed, tests-failed-allowed

- **Task B – Spec-maintainer loop & artifact handling** ✅
  - ✅ Scheduler handles `/spec_notes/**` artifacts when produced
  - ✅ `spec.changes_requested` triggers implement/review loop per MASTER-SPEC §14.2
  - ✅ Spec maintainer approval gated on review approval

- **Task C – Resume/idempotency alignment** ✅
  - ✅ `detectMidSpecLoop()` helper in `scheduler.go:186-249` for granular resume
  - ✅ Ledger replay correctly identifies pending commands in spec loops
  - ✅ Idempotency keys + receipts prevent duplicate work
  - ✅ Integration test (`TestCrashAndResumeAfterSpecChangesRequested`) validates crash after `spec.changes_requested`
  - ✅ **Bug fixed**: `spec.changes_requested` was incorrectly marked as terminal event in `ledger.go`

- **Task D – Developer ergonomics & docs** ✅
  - ✅ Clear error messages for builder test failures (includes task_id, summary)
  - ✅ Console output shows test validation results
  - ✅ Implementation summary created: `P1.4-IMPLEMENTATION-SUMMARY.md`

- **Exit criteria** ✅ **MET**
  - ✅ All 7 new tests passing; scheduler blocks builder completions without passing tests with clear diagnostics
  - ✅ Event log + receipts record builder test summaries and spec_notes artifacts
  - ✅ `lorch resume` performs granular continuation from pending command (per P1.4-ANSWERS A5)
  - ✅ Task completion strictly gated on `spec.updated` or `spec.no_changes_needed`

**Deliverables**:
- 📦 1 new helper: `validateBuilderTestResults()`, 1 new helper: `detectMidSpecLoop()`
- 📝 7 new tests, 5 new fixtures
- 🐛 1 critical bug fix (terminal event classification)
- 📊 Implementation summary: `P1.4-IMPLEMENTATION-SUMMARY.md`

### P1.5 Milestone – QA, Packaging & Docs ✅ **COMPLETE**
> **Summary**: Release tooling, lint/test automation, and documentation landed. `lorch release` now builds cross-platform binaries with smoke validation; CI runs lint → unit → smoke pipelines, and docs cover usage through Phase 1.

- ✅ `lorch release` command builds darwin/linux (amd64 + arm64) binaries, writes `dist/manifest.json`, and records smoke outcomes per target (skipping non-native architectures when needed).
- ✅ `internal/release` package encapsulates manifest generation and smoke gating with unit coverage.
- ✅ `pkg/testharness` provides reusable binary builds and smoke scenarios; `TestRunSmokeSimpleSuccess` ensures mock-agent flows stay green.
- ✅ GitHub Actions workflow (`.github/workflows/ci.yml`) runs `golangci-lint`, `go test ./...`, and the smoke harness while archiving logs under `logs/ci/<run-id>/`.
- ✅ `.golangci.yml`, README quick start, `docs/AGENT-SHIMS.md`, and `docs/releases/P1.5.md` document tooling, shims, and release artifacts.

## Phase 2 – Natural Language Task Intake
Add orchestration agent flow and human-in-the-loop approvals.

- **Console interaction loop**
  - Prompt user for NL instruction when `--task` absent; stream agent transcripts to console.
- **Orchestration protocol**
  - Send `intake`/`task_discovery` commands, handle `orchestration.proposed_tasks` and `needs_clarification`, capture `system.user_decision`.
- **Task activation pipeline**
  - Materialize approved tasks into the Phase 1 execution loop with appropriate idempotency metadata.
- **UX polish**
  - Provide clear prompts for clarifications, approval summaries, and conflict messaging.
- **Testing**
  - Mock-agent scenarios covering successful intake, clarification loop, and user-denied plans.

## Phase 3 – Interactive Configuration
Ship `lorch config`, validation enhancements, and flexible agent/tool settings.

- **Config editor**
  - Interactive TUI/CLI flow to edit `lorch.json` with immediate validation feedback.
- **Schema & compatibility checks**
  - Enforce versioning, detect deprecated keys, and surface warnings.
- **Extensibility hooks**
  - Support per-agent environment overrides, credential prompts, and toolchain presets without code changes.
- **Testing**
  - Unit tests for config transformations, compatibility migrations, and validation failure reporting.

## Phase 4 – Advanced Error Handling & Conflict Resolution
Improve diagnostics, recovery, and human control.

- **Enhanced telemetry**
  - Richer error codes, structured conflict reports, resource usage stats in heartbeats.
- **Automated recovery aids**
  - Guided suggestions when agents repeatedly fail, including surfacing conflict artifacts.
- **Spec-maintainer collaboration**
  - Better diff previews for SPEC.md updates, tools to accept/reject spec changes.
- **Testing**
  - Fault-injection tests to validate recovery workflows and human-in-the-loop escalation.

## Cross-Cutting Activities
- **Documentation**
  - Developer on-boarding guide, agent integration cookbook, and user-facing quick start.
- **Build & release tooling**
  - Reproducible builds, packaging for target platforms, and release checklist aligned with each phase.
- **Observation & metrics**
  - Plan for optional logging verbosity, trace IDs, and hooks for future analytics (without violating local-first goals).

## Next Steps
1. Draft Phase 2 intake UX specification (console prompts, approval loop, transcript expectations).
2. Prototype orchestration-agent shim contract updates (inputs/outputs, env vars) ahead of implementation.
3. Extend smoke fixtures to cover change-request iterations in preparation for Phase 2 regression coverage.

### Current Status (2025-10-20)
**Phase 1 Complete**: Milestones P1.1 through P1.5 are delivered with passing tests, release tooling, and documentation. The orchestrator now:
- Captures deterministic snapshots and idempotency keys.
- Schedules builder → reviewer → spec-maintainer loops with enforced test reporting and granular resume.
- Persists receipts, events, and run state for crash-safe restarts.
- Publishes cross-platform binaries (`lorch release`), smoke-validates them, and runs lint/unit/smoke checks in CI.
- Documents agent shims and release artefacts for local operation.

**Ready for Phase 2**: Natural Language Task Intake (orchestration agent + approval workflow).
