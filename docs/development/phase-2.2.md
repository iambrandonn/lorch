# Phase 2.2 Implementation Summary

**Milestone**: CLI Intake Loop  
**Completed**: 2025-10-21  
**Status**: ✅ All tasks complete, all exit criteria met

---

## Overview

Phase 2.2 brings natural-language intake directly into `lorch run`. When no `--task` is provided the CLI now prompts the user, performs deterministic plan discovery, invokes the orchestration agent, and records the entire exchange for later approval. Console output mirrors the live transcript while an intake run log and outcome snapshot are persisted under the workspace.

Key outcomes:
- `lorch run` offers an interactive (or piped) prompt to collect the user instruction.
- Discovery results are bundled into orchestration commands using the new protocol types from Phase 2.1.
- Orchestration transcripts stream to the console with friendly summaries and are archived to `/events/<run>-intake.ndjson`.
- Each intake produces an outcome JSON in `state/intake/` (plus `latest.json`) so future phases can drive approvals.

---

## Task Breakdown

### Tests First – CLI Interaction Coverage
- Added comprehensive tests in `internal/cli/run_test.go`:
  - TTY vs non-TTY prompts (`TestPromptForInstruction*`).
  - EOF fallbacks (`TestPromptForInstructionEOFWithoutNewline`, `TestPromptForInstructionImmediateEOF`).
  - Full integration using `claude-fixture` (`TestRunIntakeFlowSuccess`).
  - Cancellation handling via context (`TestRunIntakeFlowContextCancellation`).
- Ensured every test asserts transcript output and artifacts (event log, state files) exist as expected.

### Task A – Intake Detection & Command Construction
- `internal/cli/run.go`
  - When `--task` is omitted, `runRun` invokes the new `runIntakeFlow` path.
  - Prompts for the instruction, normalises input, and executes deterministic discovery (`internal/discovery`).
  - Builds orchestration commands (`protocol.ActionIntake`) with idempotency keys and timeout metadata.

### Task B – Transcript Streaming
- Reused the supervisor infrastructure to stream orchestration events, heartbeats, and logs to stdout.
- Extended `internal/transcript/formatter.go` to display orchestration-specific summaries (candidate counts, clarification prompts).
- Integration test confirms transcript text includes discovery and candidate information.

### Task C – Transcript Persistence & Outcome Snapshot
- Each intake run creates `/events/<run-id>-intake.ndjson` capturing commands/events/heartbeats/logs.
- Introduced `IntakeOutcome` (instruction, discovery metadata, final event, timestamps, log path).
- Persisted outcomes to `state/intake/<run-id>.json` and `state/intake/latest.json` to seed Phase 2.3 approvals.

---

## Integration Points for Phase 2.3+
- `runIntakeFlow` returns an `IntakeOutcome`; future work can launch the approval loop directly from this data.
- Outcome JSON files provide durable context for resuming approvals and generating `system.user_decision` events.
- Stored discovery metadata avoids re-scanning during approvals.

---

## Deliverables Summary

| Deliverable | Path | Notes |
|-------------|------|-------|
| CLI intake implementation | `internal/cli/run.go` | Prompt, discovery, orchestration orchestration pipeline |
| Intake outcome persistence | `state/intake/<run-id>.json`, `state/intake/latest.json` | Stored for approvals |
| Transcript formatter update | `internal/transcript/formatter.go` | Orchestration event summaries |
| Intake tests | `internal/cli/run_test.go` | TTY/non-TTY, EOF, full flow, cancellation |

---

## Lessons Learned

- The fixture harness from Phase 2.1 enables deterministic end-to-end testing without real LLM calls.
- Persisting intake outcomes up front simplifies later approval logic and testing.
- Context cancellation tests guard against goroutine leaks and ensure a responsive CLI.

---

**Tests**: `go test ./internal/cli` (passes); full `go test ./...` requires an extended timeout (>10s) due to long-running suites.

