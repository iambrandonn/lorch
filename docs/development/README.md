# Development Documentation

This directory contains internal development documentation for the lorch project, including implementation summaries, architectural decisions, and lessons learned from each development phase.

## Purpose

Development documentation serves to:
- **Preserve context**: Capture design decisions and their rationale
- **Enable onboarding**: Help new contributors understand how the system evolved
- **Support maintenance**: Provide reference when debugging or extending features
- **Document lessons**: Record what worked well and what to improve

## Organization

### Phase Summaries

Each phase/milestone has a consolidated summary document:

- **[phase-2.1.md](./phase-2.1.md)** - Orchestration Agent Foundations (P2.1)
  - Task A: Orchestration agent contract (protocol types)
  - Task B: Agent shim scaffolding (`claude-agent`)
  - Task C: Mock harness (`claude-fixture`)
  - Task D: Deterministic file discovery service
  - Integration examples for P2.2

More phase summaries will be added as development progresses.

### What's Included in Summaries

Each phase summary contains:
- **Overview**: What was built and why
- **Task breakdowns**: Detailed description of each task's deliverables
- **Key design decisions**: Architectural choices and their rationale
- **Testing approach**: Test coverage and verification methods
- **Integration points**: How the phase connects to future work
- **Lessons learned**: What worked well, improvements made
- **Deliverables**: Packages, binaries, tests, documentation created

### What's NOT Included

Phase summaries are consolidated views, not exhaustive logs:
- Intermediate review iterations (only final outcomes)
- Step-by-step implementation details (code is self-documenting)
- Routine bug fixes (unless architecturally significant)
- Process mechanics (git commits, tool usage, etc.)

## For Contributors

### Reading Documentation

**New to the project?** Start with:
1. Main `README.md` - Project overview and quick start
2. `MASTER-SPEC.md` - System specification
3. `PLAN.md` - Implementation roadmap
4. Phase summaries (this directory) - Understand what's been built

**Working on a feature?** Reference:
- The relevant phase summary for architectural context
- Code comments and package documentation
- `MASTER-SPEC.md` for protocol requirements

### Creating Documentation

When completing a phase/milestone, create a phase summary following this structure:

```markdown
# Phase X.Y Implementation Summary

**Milestone**: Name
**Completed**: Date
**Status**: ✅ Complete

## Overview
[High-level summary of what was delivered]

## Task A: Name
### What Was Built
[Packages, types, features created]

### Key Design Decisions
[Architectural choices and rationale]

### Testing
[Test approach and coverage]

## [Additional tasks...]

## Integration Points for Next Phase
[How future work will use this phase's deliverables]

## Deliverables Summary
[Packages, binaries, tests, docs created]

## Lessons Learned
[What worked, what improved, technical debt]
```

See `phase-2.1.md` as a reference example.

## Archive Policy

**Keep**:
- ✅ Final phase summaries
- ✅ Architectural decision records
- ✅ Integration examples

**Can discard**:
- ❌ Intermediate review drafts (superseded by finals)
- ❌ Planning documents (after milestone completion)
- ❌ Temporary analysis or exploration notes

When in doubt, consolidate multiple documents into a single phase summary rather than keeping everything.

## Index

### Phase 1: Core Orchestrator Foundation
*Summary pending consolidation from P1.3-P1.5 implementation summaries*

### Phase 2: Natural Language Task Intake

- **[Phase 2.1: Orchestration Agent Foundations](./phase-2.1.md)** ✅
  - Orchestration protocol types
  - Generic agent shim (`claude-agent`)
  - Fixture-based mock harness (`claude-fixture`)
  - Deterministic file discovery service
- **[Phase 2.2: CLI Intake Loop](./phase-2.2.md)** ✅
- **[Phase 2.3: Plan Negotiation & Approvals](./phase-2.3.md)** ✅ (Summary; see [final review](./phase-2.3-review-final.md))
- **[Phase 2.5: UX Polish & Documentation](./phase-2.5.md)** ✅ (Tasks A & B complete, Task C pending)
  - **Unit Tests**: 37 new tests (118 sub-tests) validating console output
    - Transcript formatter tests (10 tests - event/heartbeat/command formatting)
    - Console output snapshot tests (12 tests - user-facing prompts and menus)
    - Prompting edge cases tests (15 tests - input validation and retries)
    - Review fixes: Extracted helper functions, fixed clarification retry bug, added retry tests
  - **[Task A: UX Copy Refinement](./phase-2.5-task-a.md)** ✅
    - Added example text to initial instruction prompt (MASTER-SPEC §4.1)
    - Shortened plan/task selection prompts with format examples
    - Improved conflict resolution messaging (more natural phrasing)
    - Made discovery message more specific
    - Updated 9 test assertions to enforce new copy
    - Review fix: Added explicit assertion for example text
  - **[Task B: Documentation Updates](./phase-2.5-task-b.md)** ✅
    - Updated README.md with Natural Language Intake section
    - Expanded docs/AGENT-SHIMS.md (+400 lines): orchestration agent, file discovery, testing without LLMs
    - Created docs/ORCHESTRATION.md (22KB technical reference for orchestration implementers)
    - Created docs/examples/ with 3 sample configs + README
    - Enhanced testdata/fixtures/README.md orchestration section (+100 lines)
    - Total: 1,450+ lines of documentation added/enhanced
  - **Task C**: Regression tests for denied approvals, retry flows, non-TTY intake (pending)
- **[Phase 2.4: Task Activation Pipeline](./phase-2.4.md)** ✅
  - Task A: Activation mapping (intake approvals → concrete tasks)
  - Task B: Scheduler integration (task execution with metadata preservation)
  - Task C: Receipt traceability (intake origin metadata in all receipts)
  - See also: [Task A details](./phase-2.4-task-a.md), [Task C details](./phase-2.4-task-c.md), [Test Plan](./phase-2.4-test-plan.md)

### Phase 3: Interactive Configuration
*(Not yet started)*

### Phase 4: Advanced Error Handling & Conflict Resolution
*(Not yet started)*

---

**Last Updated**: 2025-10-22 (Phase 2.5 Task B complete)
