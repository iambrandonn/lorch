# Implementation Plan (High-Level)

- **Implementation stack**: ✅ Go (target macOS + Linux equally, statically linked binaries). Assume latest stable Go toolchain (≥1.22) with modules.
- **Agent strategy**: need to build agent shims from scratch; initial implementation wraps Claude Code via CLI (`claude` on `$PATH`). Roles passed via CLI prompt interpolation (e.g., `claude "You are the $ROLE agent"`); design shims so alternative CLIs can be swapped in later.
- **Workspace baseline**: ✅ greenfield repository.
- **Platform priority**: treat macOS and Linux as first-class targets from Phase 1 onward (tooling and packaging should support both).
- **Testing expectations**: aim for high-quality, meaningful coverage (unit + integration); use mocks for deterministic tests and supplement with optional LLM-in-the-loop smoke runs; no artificial 100% threshold.

> Pending answers feed back into the detailed sub-plans for each phase.

## Phase 1 – Core Orchestrator Foundation
Deliver `lorch run` with builder/reviewer/spec-maintainer agents, deterministic persistence, and resumable runs.

### P1.1 Milestone – Workspace & CLI Skeleton
- **Tests first**: define unit specs for config generation/validation and golden snapshot for default `lorch.json`.
- **Task A**: scaffold Go module (`go mod init`), create `cmd/lorch` entrypoint with subcommands (`lorch`, `lorch run`, `lorch resume`).
- **Task B**: implement bootstrap flow to detect/create workspace directories (`state/`, `events/`, `receipts/`, `logs/`, etc.).
- **Task C**: implement config loader/auto-generator matching defaults (Claude agents, policy settings).
- **Task D**: add validation layer (struct tags or JSON schema) with user-friendly errors.
- **Exit criteria**: tests from step one pass, CLI can initialize a fresh workspace, `lorch.json` matches expected golden file.

### P1.2 Milestone – Agent Supervisor & IPC Core
- **Tests first**: craft mock-agent harness specs (echo NDJSON, heartbeat simulation) and scheduler sequencing tests.
- **Task A**: implement agent registry with subprocess lifecycle management (start/stop, restart hooks).
- **Task B**: build NDJSON framing utilities (encoder/decoder, size enforcement, structured logging).
- **Task C**: implement single-agent scheduler enforcing Implement → Review → Spec Maintenance order with back-pressure.
- **Task D**: pipe transcripts to console and persist raw streams to `/events/<run>.ndjson`.
- **Exit criteria**: mock-agent tests pass; transcripts and event logs show correct sequencing.

### P1.3 Milestone – Idempotency & Persistence
- **Tests first**: specify unit tests for idempotency key generation and ledger append; integration scenario covering crash/restart.
- **Task A**: implement IK generator and append-only ledger writer with checksum validation.
- **Task B**: implement artifact receipt pipelines (atomic writes, checksum hashing, directory layout).
- **Task C**: add resume logic loading `/state/run.json`, replaying ledger, resending commands with same IKs.
- **Exit criteria**: crash/restart simulation passes; receipts written to expected paths with verified hashes.

### P1.4 Milestone – Builder/Test Enforcement & Spec Loop Closure
- **Tests first**: author integration specs for builder success/failure scenarios and spec-maintainer change-request loop.
- **Task A**: extend builder command handler to require structured test/lint payloads before success.
- **Task B**: implement spec-maintainer event handling (`spec.updated`, `spec.no_changes_needed`, `spec.changes_requested`) and state transitions.
- **Task C**: persist `/spec_notes/**` artifacts and associated receipts when notes are produced.
- **Exit criteria**: e2e tests covering approval and change-request loops pass; completion hinges on spec maintainer signal.

### P1.5 Milestone – QA, Packaging & Docs
- **Tests first**: finalize smoke-test script using mock agents; ensure CI pipeline executes it.
- **Task A**: configure build scripts for macOS/Linux (amd64/arm64) static binaries; document instructions.
- **Task B**: integrate formatting/linting tools (`go fmt`, `go vet`, `golangci-lint` or equivalent) into CI.
- **Task C**: write onboarding docs (README, agent shim guide, Phase 1 feature list).
- **Exit criteria**: CI green with smoke tests + lint/format; documented release binaries produced.

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
1. Break Phase 1 milestones into discrete issues/tasks (owners, sequencing, estimates) using sections P1.1–P1.5.
2. Define the agent shim interface: CLI contract, prompt templating, stdout/stderr expectations, and local testing scaffold.
3. Set up Go tooling & CI skeleton (module init, lint/test workflows) before starting P1.1 implementation.
