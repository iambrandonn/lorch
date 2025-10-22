# Phase 2.5 Review – Unit Tests

**Reviewer**: Codex (GPT‑5)  
**Date**: 2025-10-22  
**Scope**: Phase 2.5 “UX Polish & Documentation (Unit Tests)”

---

## Verdict
- Status: ❌ Changes Requested
- Rationale: The submitted tests miss key behaviours that Phase 2.5 explicitly calls out. In their current form they would not detect regressions in the most visible console flows.

---

## Findings

1. **Snapshot tests aren’t exercising production code** – `TestConsoleOutput_ApprovalConfirmation` and `TestConsoleOutput_DiscoveryMessage` construct the expected strings manually instead of invoking the real code paths. Any future copy regressions inside `runIntakeFlow` will therefore pass unnoticed.  
   - Evidence: `internal/cli/console_output_test.go:386-463` writes the hard‑coded strings directly; the implementation these tests are supposed to guard lives in `internal/cli/run.go:311-741`.  
   - Impact: These tests provide a false sense of coverage. If the CLI copy changes (accidentally or intentionally) the tests will still pass, violating the “snapshot tests for console messaging” requirement in PLAN.md Phase 2.5.  
   - Suggested fix: Drive the assertions through the real functions (e.g. extract helpers for “success banner” and “discovery banner” or invoke `recordIntakeDecision` / the discovery branch of `runIntakeFlow`) so the tests observe the actual console output.

2. **Clarification retry flow is untested and currently broken** – The `promptClarifications` loop relies on `i--` inside a `range`, which does not re-run the same question. There is no unit test covering the “blank answer → retry” path, so the defect goes uncaught.  
   - Evidence: Implementation at `internal/cli/run.go:1191-1208`; the current tests (`internal/cli/console_output_test.go:293-310`) explicitly skip exercising the empty-answer branch. The milestone summary acknowledges the problem but defers it (`docs/development/phase-2.5.md:265-285`).  
   - Impact: Users who submit an empty clarification silently advance to the next question, leaving the intended retry UX unverified. This contradicts the Phase 2.5 exit criterion (“regression tests for … retry flows”).  
   - Suggested fix: Add a unit test that feeds an empty answer followed by a valid one, assert that the prompt repeats and the “Please provide an answer.” message appears, then correct the loop (e.g. switch to an index-based `for i := 0; i < len(questions);`).

---

## Recommendation
Address the findings above, re-run `go test ./...` (with an increased timeout if needed), and resubmit for review.

