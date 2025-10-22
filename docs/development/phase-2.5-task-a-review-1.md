# Phase 2.5 Task A Review – Intake Copy Refresh

- **Reviewer**: Codex (GPT-5)
- **Date**: 2025-10-23
- **Status**: ❌ Changes Requested

## Findings
1. **Missing assertion for the new instruction prompt example**  
   `internal/cli/console_output_test.go:23`  
   The copy change adds the `(e.g., "Manage PLAN.md" …)` hint in `promptForInstruction`, but the snapshot test still only asserts the legacy prefix (`"lorch> What should I do?"`). With the current coverage, future regressions could drop the new example without failing a test, undercutting the goal of locking UX copy via tests. Please extend the TTY prompt test to assert the example text (and optionally ensure the non-TTY test verifies it is absent) so the new wording is enforced.

## Tests Observed
- `go test ./...` (passes with 300s timeout)
