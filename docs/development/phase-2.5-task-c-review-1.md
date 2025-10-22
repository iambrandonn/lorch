# Phase 2.5 Task C Review – Regression Safeguards

- **Reviewer**: Codex (GPT-5)
- **Date**: 2025-10-23
- **Status**: ❌ Changes Requested

## Findings
1. **Regression suite currently fails (non-TTY decline test hangs)**  
   `internal/cli/intake_regression_test.go:420`–`internal/cli/intake_regression_test.go:449`  
   The newly added `TestRegression_DeclineWithNonTTY` spins up `runIntakeFlow` in a goroutine and a 5 s context timeout to detect hangs, but in practice the test always trips the timeout. Running `go test ./internal/cli` or even `go test ./internal/cli -run TestRegression_DeclineWithNonTTY` exits with `non-TTY decline test timed out (possible hang)`. We need this test (and the overall suite) green before shipping Task C.

2. **New orchestration fixtures won’t replay retries as written**  
   `testdata/fixtures/orchestration-clarification-then-conflict.json`, `testdata/fixtures/orchestration-multiple-conflicts.json`  
   The fixture scripts introduce keys such as `intake_retry_1` / `intake_retry_2`, but the fixture agent (`internal/agent/script` + `internal/fixtureagent/agent.go`) looks up responses strictly by the command action (`"intake"`, `"task_discovery"`, …). Subsequent intake commands will therefore keep replaying the initial `"intake"` template instead of advancing through the retry entries. If these fixtures are meant to back future regression tests they need to use a supported pattern (e.g. a single `"intake"` response emitting multiple events, or metadata-driven scripting); otherwise they provide a false sense of coverage.

## Tests Observed
- `go test ./internal/cli` (fails: `TestRegression_DeclineWithNonTTY` timeout)
