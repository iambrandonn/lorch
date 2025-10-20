# lorch – Local ORCHestrator for Multi-Agent Workflows

`lorch` is a command-line orchestrator that keeps AI-assisted development grounded, traceable, and under your direct control. Point it at a repository and it will coordinate specialized agents (builder, reviewer, spec maintainer) through the classic “implement → review → spec update” loop—capturing snapshots, event logs, receipts, and run state along the way. Everything happens locally, with the filesystem as the source of truth.

## Why it matters

- **Deterministic automation** – Every command carries an idempotency key and every run produces receipts, so you can resume after a crash without redoing work or corrupting the repo.
- **Human-in-the-loop** – Reviews, spec updates, and run decisions stay transparent: transcripts stream to the console, state is recorded on disk, and you decide when to resume or abort.
- **Easy to operate** – A single binary (`lorch run --task <ID>`) captures workspace snapshots, launches agents, logs events, and produces a full audit trail (`/events`, `/receipts`, `/state`).
- **Extensible by design** – Agents are simple CLI processes (e.g., Claude wrappers or custom tools). Swap them out, script them for testing, or run them in “mock” mode for quick demos.

The goal is to give you a trustworthy automation layer for spec-driven projects—something you can run on your machine, inspect at any time, and resume safely whenever the workflow gets interrupted.

---
Ready to dive deeper? Check `PLAN.md` for the implementation roadmap and `AGENTS.md` for agent roles and protocols.
