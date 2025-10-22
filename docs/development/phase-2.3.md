# Phase 2.3 Summary – Plan Negotiation & Approvals

**Milestone**: Phase 2.3  
**Completed**: 2025-10-21  
**Status**: ✅ Delivered

---

## Overview

Phase 2.3 adds a resilient plan-negotiation loop to `lorch`, closing the gap between NL intake and concrete task activation. The CLI now mediates clarifications, plan conflicts, and user approvals while persisting every decision for resumability. The orchestration agent remains stateless; all context is tracked by lorch and replayed across retries or resumes.

---

## Key Deliverables

- **Intake negotiation loop** (`internal/cli/run.go`)
  - Resume-aware command reconstruction with idempotent retries
  - Multi-step clarifications and “more options” discovery flow
  - Plan-conflict prompting with user guidance or abort paths
  - `system.user_decision` events recorded to ledger and state
  - Heartbeat timeout enforcement (3× interval)
- **State persistence upgrades** (`internal/runstate/runstate.go`)
  - `IntakeState` captures base inputs, pending commands, clarifications, and conflict resolutions
  - Helper methods to snapshot/restore negotiation progress
- **Resume integration** (`internal/cli/resume.go`)
  - Detects in-progress intake sessions and resumes negotiation before returning to task execution
- **Tests**
  - 10 CLI integration/unit tests covering approvals, clarifications, conflicts, discovery, decline flows, non-TTY input, and resumability

---

## Testing

- `go test ./internal/cli` – integration + unit coverage for the negotiation loop
- `go test ./internal/runstate` – persistence helpers and state cloning
- Full suite: `go test ./...` (known issue: existing `internal/supervisor` stop-timeout flake, unchanged this phase)

---

## Integration Notes

- Approved plan + tasks are now available via `IntakeOutcome.Decision`
- Clarifications, conflict resolutions, and discovery metadata are persisted in `/state/run.json` for downstream phases
- Task activation (Phase 2.4) should consume the approved task IDs and enqueue them into the scheduler

---

## Follow-Up / Technical Debt

1. ~~Extract prompt option strings into shared constants~~ ✅ Complete (optionMore, optionNone, etc. defined)
2. ~~Remove unused code~~ ✅ Complete (printIntakeSummary removed)
3. Document task activation wiring once Phase 2.4 lands

---

## Contributors

- Builder Agent (implementation & tests)
- Code Review Agent (final review)

---

**Next Phase**: Phase 2.4 – Task Activation Pipeline
