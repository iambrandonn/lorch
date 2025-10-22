# Phase 2.5 Task B Review – Documentation Updates (Iteration 2)

- **Reviewer**: Codex (GPT-5)
- **Date**: 2025-10-23
- **Status**: ✅ Approved

## Outcome
- README walkthrough now mirrors actual CLI prompts (`internal/cli/run.go`), so copy changes stay aligned with the tested UX.
- Discovery heuristics and metadata tables in `docs/AGENT-SHIMS.md` and `docs/ORCHESTRATION.md` match the current implementation (`internal/discovery/discovery.go`, `internal/protocol/orchestration.go`)—no more references to unimplemented fields or weights.

## Tests Observed
- `go test ./... -timeout 120s`
