# Agent Overview

This document summarizes the agents referenced in the spec (`MASTER-SPEC.md`) and captures practical details for implementing local CLI shims.

## Shared Conventions
- **Transport**: Agents communicate with `lorch` over NDJSON via stdin/stdout (one JSON object per line, UTF-8).
- **Lifecycle**: `lorch` spawns each agent as a subprocess and manages heartbeats; agents should exit cleanly on EOF or `SIGTERM`.
- **Commands**: All messages sent to agents are `command` envelopes with an action (`implement`, `review`, `update_spec`, `intake`, `task_discovery`).
- **Events**: Agents respond with `event` envelopes describing progress/completion, plus optional `artifact.produced` entries referencing filesystem paths.
- **Heartbeats**: Agents must emit `heartbeat` messages while busy to report liveness.
- **Environment/Configuration**:
  - Each agent shim should accept a `CLAUDE_ROLE` (or similar) env variable for prompt templating.
  - Additional env vars may specify workspace root, log verbosity, etc.
  - Access tokens/config for the underlying LLM CLI (e.g., Claude Code) should be provided by the user’s environment; `lorch` does not manage secrets.

## Agent Roles

### Builder
- **Purpose**: Implement tasks by editing code/tests and producing artifacts required by the specification.
- **Primary Actions**: `implement`, `implement_changes`.
- **Key Requirements**:
  - Must run relevant tests/linting before signaling success.
  - `builder.completed` payload must include structured test results (e.g., `{"tests":{"status":"pass","summary":"..."}}`).
  - Emits `artifact.produced` with paths and checksums for any files changed.
- **CLI Prompt Template Example**:
  ```bash
  claude "You are the builder agent. Task: ${TASK_CONTEXT}. Remember to run tests and report their results."
  ```

### Reviewer
- **Purpose**: Review builder output for quality and correctness; request changes if needed.
- **Primary Actions**: `review`.
- **Key Requirements**:
  - Emits `review.completed` with status `approved` or `changes_requested`.
  - Should attach review notes artifacts under `/reviews/<task>.json`.
- **CLI Prompt Template Example**:
  ```bash
  claude "You are the reviewer agent. Provide a code review for the following changes, responding with structured findings."
  ```

### Spec Maintainer
- **Purpose**: Ensure the implementation satisfies SPEC.md requirements, update allowed sections, and capture follow-up notes.
- **Primary Actions**: `update_spec`.
- **Key Requirements**:
  - Emits `spec.updated`, `spec.no_changes_needed`, or `spec.changes_requested`.
  - If updates are needed, writes approved modifications to SPEC.md (allowed sections only) and/or `spec_notes/<task>.json`.
- **CLI Prompt Template Example**:
  ```bash
  claude "You are the spec maintainer. Verify the implementation against SPEC.md. Update allowed sections or request changes."
  ```

### Orchestration (NL Intake) — Phase 2+
- **Purpose**: Translate user natural-language instructions into concrete task plans; gather clarifications.
- **Primary Actions**: `intake`, `task_discovery`.
- **Key Requirements**:
  - Emits `orchestration.proposed_tasks` (plan candidates + derived tasks) or `orchestration.needs_clarification`.
  - Never edits code/spec directly; only produces planning artifacts.
- **CLI Prompt Template Example**:
  ```bash
  claude "You are the orchestration agent. Turn the user's request into explicit development tasks."
  ```

## Agent Shim Implementation Hints
- Provide a uniform shim executable (e.g., `cmd/claude-agent`) that accepts `--role`, `--workspace`, and optionally `--fixture` for testing.
- Translate incoming `command` JSON into prompt text for the CLI tool; parse the CLI response back into required `event` payloads.
- For tests, offer a mock mode that replays scripted responses without invoking the real LLM.
- Ensure stderr is used only for diagnostics; stdout must stay reserved for NDJSON protocol messages.
- End-to-end `go test ./...` runs can exceed default CLI timeouts; when executing the full suite, increase the shell command timeout (e.g., ≥60 s) so longer integration tests are not interrupted.

## Development Documentation Practices (For LLM Agents)

When working on this project, AI agents should create consolidated documentation to preserve context for future agents and human contributors.

### Documentation Approach

**Keep in `docs/development/`**:
- Phase summaries: One consolidated document per milestone (e.g., `phase-2.1.md`)
- Key design decisions with rationale
- Integration examples for future phases
- Lessons learned and improvements made

**Can discard after consolidation**:
- Intermediate review drafts (superseded by final reviews)
- Planning documents (after milestone completion)
- Iteration artifacts (keep outcomes, not process details)

### Creating Phase Summaries

**When**: After completing all tasks in a milestone

**Structure**:
- Overview: What was delivered and why
- Per-task breakdown: What was built, key decisions, testing approach
- Integration points: How future work uses these deliverables
- Deliverables summary: Packages, binaries, tests, docs created
- Lessons learned: What worked, improvements made

**Reference example**: See `docs/development/phase-2.1.md`

### Workflow Pattern

1. **During development**: Create detailed task reviews (e.g., `P2.1-TASK-A-REVIEW.md`) for immediate feedback
2. **After milestone**: Consolidate reviews into one phase summary in `docs/development/`
3. **Clean up**: Delete intermediate review files, keep only the consolidated summary

### Benefits

- Cleaner repository (no review clutter in root)
- Easier onboarding (read phase summaries to understand what exists)
- Preserved context (design decisions documented for future reference)
- Maintainable pattern (scales as project grows)

**Index**: Maintain `docs/development/README.md` linking to all phase summaries

## Future Extensions
- Add support for alternative LLM CLIs (OpenAI, local models) by swapping the underlying prompt invocation while preserving the shim interface.
- Consider per-agent configuration in `lorch.json` for model selection, temperature, or additional tooling hooks.
