# Phase 2.5 Task B Review – Documentation Updates

- **Reviewer**: Codex (GPT-5)
- **Date**: 2025-10-23
- **Status**: ❌ Changes Requested

## Findings
1. **README intake walk-through diverges from actual CLI output**  
   `README.md:25`–`README.md:47`  
   The new Natural Language Intake example shows prompt text (`"Use PLAN.md and manage its implementation"`), discovery logs (`"[lorch] Discovering plan files..."`), menu copy (`"Select a plan file:"`, `"all. Select all tasks"`), and choice labels (`"m. Ask for more options"`) that the CLI never prints. The real strings come from `promptForInstruction`, `printDiscoveryMessage`, and `promptPlanSelection`/`promptTaskSelection` (`internal/cli/run.go:1236`, `internal/cli/run.go:1272`, `internal/cli/run.go:1469`), so the README should mirror those exact phrases (e.g., `Select a plan [1-…], 'm' for more, or '0' to cancel:`). As written, the documentation will mislead users and drift from the copy locked by tests.

2. **File-discovery heuristics documentation out of sync with implementation**  
   `docs/AGENT-SHIMS.md:204`–`docs/AGENT-SHIMS.md:256`, `docs/ORCHESTRATION.md:60`–`docs/ORCHESTRATION.md:142`  
   The new sections describe scoring weights (+0.30 filename token, +0.20 root, -0.05 depth, +0.10 heading), extra exclusions (`vendor/`, files >10 MB), and metadata fields (`total_files_scanned`, `duration_ms`) that do not exist in the current implementation. The actual algorithm starts at 0.5, adds +0.25/0.20/0.15 depending on the token, +0.05 for docs/specs/plans directories, subtracts 0.04 per depth level, and adds +0.05 for heading matches (`internal/discovery/discovery.go:268`–`internal/discovery/discovery.go:311`). Discovery metadata also omits file-size filtering and does not emit `total_files_scanned` or timing fields (`internal/protocol/orchestration.go:41`–`internal/protocol/orchestration.go:88`). Please realign the documentation (including the example score breakdowns and tables) with the live code to avoid misleading implementers.

## Tests Observed
- _No automated tests run for documentation-only changes_
