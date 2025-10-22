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

## Development Documentation Practices (For LLM Agents Developing Lorch)

**Important**: This section is for AI agents working on lorch development itself, not for using lorch to develop other projects.

When implementing features for lorch, follow this documentation workflow to preserve context for future agents and human contributors.

---

### File Organization

All development documentation lives in **`docs/development/`**:

```
docs/development/
├── README.md                      # Index of all phase documentation (keep updated!)
├── phase-2.1.md                   # Phase 2.1 milestone summary
├── phase-2.4.md                   # Phase 2.4 milestone summary
├── phase-2.4-task-a.md            # Detailed Task A documentation (optional)
├── phase-2.4-task-c.md            # Detailed Task C documentation (optional)
├── phase-2.4-task-c-review-1.md   # Review feedback (keep for reference)
└── phase-2.4-test-plan.md         # Test plan (keep for reference)
```

**Naming Conventions**:
- Phase summaries: `phase-X.Y.md` (e.g., `phase-2.4.md`)
- Detailed task docs: `phase-X.Y-task-Z.md` (e.g., `phase-2.4-task-a.md`)
- Reviews: `phase-X.Y-task-Z-review-N.md` (e.g., `phase-2.4-task-c-review-1.md`)
- Test plans: `phase-X.Y-test-plan.md`

---

### When to Document

**During Development** (temporary files in root OK):
- Create review files for feedback (e.g., `P2.4-TASK-C-REVIEW.md`)
- Draft implementation summaries as you work
- Keep planning notes for your own reference

**After Task/Milestone Completion** (before asking user to commit):
1. **Create or update phase summary** (`docs/development/phase-X.Y.md`)
2. **Move detailed docs** from root → `docs/development/` with proper naming
3. **Update the index** (`docs/development/README.md`)
4. **Update PLAN.md** to mark tasks complete
5. **Delete temporary files** from root (consolidate into organized docs)

---

### Phase Summary Structure

Each `phase-X.Y.md` file should contain:

```markdown
# Phase X.Y Summary – Milestone Name

**Milestone**: Phase X.Y
**Completed**: YYYY-MM-DD
**Status**: ✅ Delivered

---

## Overview
[What was built and why - 2-3 paragraphs]

---

## Key Deliverables

### Task A – Name ✅
- **What**: [Bullet points of what was created]
- **Why**: [Design decisions and rationale]
- **Testing**: [Test coverage summary]

### Task B – Name ✅
[Same structure]

---

## Architecture
[Data flow diagrams, component descriptions]

---

## Design Decisions

### 1. Decision Title
**Decision**: [What was chosen]
**Alternative Considered**: [What was rejected]
**Rationale**: [Why this was better]

---

## Testing
- Unit tests: [Summary with counts]
- Integration tests: [Summary]
- Regression check: [go test ./... results]

---

## Integration Notes for Future Phases
[How Phase X+1 will use these deliverables]

---

## Follow-Up / Technical Debt
1. ✅ Item completed
2. Item pending for future phase

---

## Spec Alignment
[References to MASTER-SPEC sections satisfied]

---

**Next Phase**: Phase X.Y+1 – Name
```

**Reference Examples**:
- `docs/development/phase-2.1.md` - Full phase with multiple tasks
- `docs/development/phase-2.4.md` - Recent complete example

---

### Updating the Index

**Critical**: Always update `docs/development/README.md` when adding new documentation.

**Location**: The "Index" section near the bottom of the file.

**Pattern**:
```markdown
### Phase 2: Natural Language Task Intake

- **[Phase 2.4: Task Activation Pipeline](./phase-2.4.md)** ✅
  - Task A: Activation mapping (intake approvals → concrete tasks)
  - Task B: Scheduler integration (task execution with metadata preservation)
  - Task C: Receipt traceability (intake origin metadata in all receipts)
  - See also: [Task A details](./phase-2.4-task-a.md), [Task C details](./phase-2.4-task-c.md)
```

**Also update**:
- The "Last Updated" date at the bottom of README.md
- Add links to detailed task docs if they provide significant additional context

---

### What to Keep vs. Discard

**Always Keep**:
- ✅ Phase summary files (`phase-X.Y.md`)
- ✅ Final review feedback (e.g., `phase-2.4-task-c-review-1.md`)
- ✅ Test plans if they're comprehensive
- ✅ Detailed task docs if they contain significant architectural context

**Can Discard After Consolidation**:
- ❌ Temporary review drafts in root (e.g., `P2.4-TASK-C-REVIEW.md` after moving to docs/development/)
- ❌ Planning documents after milestone completion
- ❌ Iteration notes that are now reflected in code/tests
- ❌ Summary files in root (e.g., `P2.4-IMPLEMENTATION-SUMMARY.md` after moving)

**Rule of Thumb**: If it's in the root directory and the milestone is complete, it probably should be moved to `docs/development/` or consolidated into the phase summary.

---

### Complete Documentation Workflow Example

This is the exact process to follow when finishing a task/milestone:

#### 1. Check Current State
```bash
ls -la *.md              # Files in root
ls -la docs/development/ # Existing docs
git status               # What's changed
```

#### 2. Create/Update Phase Summary
- If first task in phase: Create `docs/development/phase-X.Y.md`
- If completing later task: Update existing `docs/development/phase-X.Y.md`
- Follow the structure template above

#### 3. Move Detailed Documentation
```bash
# Move summary from root to development docs with proper naming
mv P2.4-TASK-C-SUMMARY.md docs/development/phase-2.4-task-c.md

# Review files stay in docs/development/ (if they exist)
# mv P2.4-TASK-C-REVIEW.md docs/development/phase-2.4-task-c-review-1.md
```

#### 4. Update Documentation Index
Edit `docs/development/README.md`:
- Add new phase summary to the Index section
- Add links to detailed task docs if they exist
- Update "Last Updated" date

#### 5. Update PLAN.md
- Mark task/milestone complete with ✅
- Add brief summary of deliverables
- Update exit criteria status

#### 6. Verify Commit-Ready State
Before telling the user "ready to commit", verify:
- ✅ No temporary docs in root directory
- ✅ `docs/development/README.md` index updated
- ✅ `PLAN.md` reflects current completion status
- ✅ All tests passing (including regressions)
- ✅ Phase summary exists in `docs/development/`

#### 7. Check Git Status
```bash
git status --short
```

Expected output pattern:
```
 M PLAN.md
 M docs/development/README.md
 M internal/...
?? docs/development/phase-X.Y.md
?? docs/development/phase-X.Y-task-Z.md
```

Should NOT see:
```
?? P2.4-SOME-DOC.md        # Temporary files should be moved
?? TASK-REVIEW.md          # Should be in docs/development/
```

---

### Example: Phase 2.4 Task C Documentation Workflow

**What I just did** (follow this pattern):

1. **Checked existing documentation**:
   ```bash
   ls docs/development/phase-2.4*.md
   # Found: phase-2.4-task-a.md, phase-2.4-task-b-review.md, phase-2.4-test-plan.md
   # Missing: phase-2.4.md (main summary), phase-2.4-task-c.md (detailed doc)
   ```

2. **Created phase summary**:
   - Created `docs/development/phase-2.4.md` consolidating all three tasks (A, B, C)
   - Included overview, architecture, testing, design decisions
   - Added integration notes for future phases

3. **Moved detailed documentation**:
   ```bash
   mv P2.4-TASK-C-SUMMARY.md docs/development/phase-2.4-task-c.md
   # Review already in correct location: docs/development/phase-2.4-task-c-review-1.md
   ```

4. **Updated index**:
   - Edited `docs/development/README.md`
   - Updated Phase 2.4 entry with all three tasks
   - Added links to detailed docs
   - Changed "Last Updated" to 2025-10-22

5. **Updated PLAN.md**:
   - Marked Task C complete with ✅
   - Added deliverables list (6 traceability fields, helpers, tests)
   - Updated exit criteria status

6. **Verified commit-ready state**:
   ```bash
   git status --short
   # Output showed:
   #  M PLAN.md
   #  M docs/development/README.md
   #  M internal/activation/input.go
   #  M internal/receipt/receipt.go
   #  M internal/receipt/receipt_test.go
   #  M internal/scheduler/scheduler.go
   # ?? docs/development/phase-2.4.md
   # ?? docs/development/phase-2.4-task-c.md
   # ?? docs/development/phase-2.4-task-c-review-1.md
   ```
   - ✅ No temporary files in root
   - ✅ All docs properly organized
   - ✅ Tests passing
   - **Ready for commit!**

---

### Documentation Quality Standards

**Phase summaries should be**:
- **Comprehensive**: Cover all tasks in the milestone
- **Structured**: Follow the template consistently
- **Decision-focused**: Explain *why* choices were made, not just *what* was built
- **Future-oriented**: Help next phase understand integration points
- **Concise**: 8-15 pages max; extract detailed content to separate task docs

**Detailed task docs should include**:
- Deep technical details (algorithms, data structures)
- Extensive code examples
- Rationale for complex design decisions
- Migration notes if behavior changed
- Reference only when content is substantial (not for simple tasks)

**Index entries should be**:
- One-line descriptions of what was delivered
- Links to main summary + optional detailed docs
- Status indicators (✅ for complete, "planned" for future)

---

### Benefits of This Workflow

- **Cleaner repository**: No documentation clutter in root
- **Easier onboarding**: New contributors read phase summaries to understand system
- **Preserved context**: Design decisions documented for debugging and extension
- **Maintainable**: Pattern scales as project grows
- **Commit-ready**: User can immediately commit without cleanup
- **Reviewable**: Future agents can quickly assess what was built

---

### Quick Reference

**Before asking user to commit**, run this checklist:

```bash
# 1. Check root for temporary docs
ls *.md | grep -v README | grep -v PLAN | grep -v MASTER-SPEC | grep -v AGENTS | grep -v CLAUDE

# 2. Verify phase summary exists
ls docs/development/phase-X.Y.md

# 3. Check index is updated
grep "Phase X.Y" docs/development/README.md

# 4. Verify PLAN.md updated
grep "Task X" PLAN.md | grep ✅

# 5. Run tests
go test ./... -timeout 120s

# 6. Check git status
git status --short
```

If all checks pass → Tell user "Ready to commit!" with summary of changes.

---

**Remember**: Documentation is for future you (and other agents). Make it easy to understand what was built and why. When in doubt, include more context rather than less.

## Future Extensions
- Add support for alternative LLM CLIs (OpenAI, local models) by swapping the underlying prompt invocation while preserving the shim interface.
- Consider per-agent configuration in `lorch.json` for model selection, temperature, or additional tooling hooks.
