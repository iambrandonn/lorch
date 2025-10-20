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

## Future Extensions
- Add support for alternative LLM CLIs (OpenAI, local models) by swapping the underlying prompt invocation while preserving the shim interface.
- Consider per-agent configuration in `lorch.json` for model selection, temperature, or additional tooling hooks.
