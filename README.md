# lorch – Local ORCHestrator for Multi-Agent Workflows

`lorch` is a command-line orchestrator that keeps AI-assisted development grounded, traceable, and under your direct control. Point it at a repository and it will coordinate specialized agents (builder, reviewer, spec maintainer) through the classic “implement → review → spec update” loop—capturing snapshots, event logs, receipts, and run state along the way. Everything happens locally, with the filesystem as the source of truth.

## Why it matters

- **Deterministic automation** – Every command carries an idempotency key and every run produces receipts, so you can resume after a crash without redoing work or corrupting the repo.
- **Human-in-the-loop** – Reviews, spec updates, and run decisions stay transparent: transcripts stream to the console, state is recorded on disk, and you decide when to resume or abort.
- **Easy to operate** – A single binary (`lorch run --task <ID>`) captures workspace snapshots, launches agents, logs events, and produces a full audit trail (`/events`, `/receipts`, `/state`).
- **Extensible by design** – Agents are simple CLI processes (e.g., Claude wrappers or custom tools). Swap them out, script them for testing, or run them in “mock” mode for quick demos.

The goal is to give you a trustworthy automation layer for spec-driven projects—something you can run on your machine, inspect at any time, and resume safely whenever the workflow gets interrupted.

## Quick Start
- `go test ./...` – exercises the full suite, including crash/restart simulations and the new smoke harness (`pkg/testharness`).
- `golangci-lint run` – static analysis and formatting checks (configured via `.golangci.yml`).
- `go run ./cmd/lorch run --config lorch.json --task <ID>` – executes a task using agents defined in your config (typically mock agents during development).

If you are running in a restricted environment, set `GOCACHE=$(pwd)/.gocache` (and optionally `GOMODCACHE=$(pwd)/.gomodcache`) before invoking Go commands to keep build artefacts inside the workspace.

## Release Builds
- Build everything: `go run ./cmd/lorch release`.
- Artifacts land in `dist/<os>-<arch>/lorch` and are summarized in `dist/manifest.json` (Go/toolchain metadata, checksums, smoke status).
- Pass `--target darwin/arm64` (or any GOOS/GOARCH pair) to limit the build set, and `--skip-smoke` when cross-compiling for non-native platforms.

## QA & Automation
- GitHub Actions (`.github/workflows/ci.yml`) runs lint → unit tests → smoke tests, capturing logs under `logs/ci/<run-id>/` and uploading them as artifacts.
- Local parity: `GOCACHE=$(pwd)/.gocache go test ./pkg/testharness -run TestRunSmokeSimpleSuccess -v` exercises the mock-agent pipeline end-to-end (set `GOMODCACHE=$(pwd)/.gomodcache` if your environment restricts the default Go cache).
- The smoke harness can be reused programmatically via `pkg/testharness.RunSmoke` to script richer scenarios.

## Agent Shims & Mocking
- `cmd/mockagent` provides deterministic responses for builder/reviewer/spec-maintainer roles. Scripts live in `testdata/fixtures/`.
- `docs/AGENT-SHIMS.md` explains required environment variables, CLI switches, and how to plug alternative models into the shims.

## Further Reading
- `PLAN.md` – implementation roadmap across phases.
- `MASTER-SPEC.md` – end-to-end protocol and workspace conventions.
- `docs/releases/` – phase-specific release notes (see `P1.5.md` for this milestone).
