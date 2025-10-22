# Phase 2.5 Task A – UX Copy Refinement

**Milestone**: Phase 2.5 Task A
**Completed**: 2025-10-22
**Status**: ✅ Delivered

---

## Overview

Task A refined all user-facing console prompts, messages, and menus to be clearer, more helpful, and aligned with MASTER-SPEC §4.1 guidelines. The work focused on making the natural language intake flow more intuitive by adding examples, shortening prompts, and using consistent phrasing throughout.

**Key Principle**: All copy changes were validated by the 37 snapshot tests from Phase 2.5 unit tests, ensuring no regressions and enforcing the new UX through automated testing.

---

## Copy Changes Made

### 1. Initial Instruction Prompt ⭐ HIGH PRIORITY

**Location**: `internal/cli/run.go:1469`

**Before**:
```go
fmt.Fprint(w, "lorch> What should I do? ")
```

**After**:
```go
fmt.Fprint(w, "lorch> What should I do? (e.g., \"Manage PLAN.md\" or \"Implement section 3.1\") ")
```

**Rationale**:
- MASTER-SPEC §4.1 explicitly shows an example in the prompt
- Helps first-time users understand expected input format
- Reduces confusion about what "instruction" means

**Test Coverage**: `TestConsoleOutput_IntakePrompt_TTY` now asserts example text is present

---

### 2. Plan Selection Prompt

**Location**: `internal/cli/run.go:1228`

**Before**:
```go
fmt.Fprintf(w, "Select plan candidate [1-%d], 'm' for more options, or 0 to cancel: ", len(candidates))
```

**After**:
```go
fmt.Fprintf(w, "Select a plan [1-%d], 'm' for more, or '0' to cancel: ", len(candidates))
```

**Changes**:
- "plan candidate" → "a plan" (more natural)
- "more options" → "more" (shorter, less verbose)
- `0` → `'0'` (consistent quoting)

**Rationale**: Shorter prompts are easier to scan; consistent quoting improves readability

**Test Coverage**: `TestConsoleOutput_PlanCandidateMenu`

---

### 3. Task Selection Prompt

**Location**: `internal/cli/run.go:1272`

**Before**:
```go
fmt.Fprint(w, "Select tasks to approve (comma separated numbers, blank for all, 0 to cancel): ")
```

**After**:
```go
fmt.Fprint(w, "Select tasks [1,2,3 or blank for all, '0' to cancel]: ")
```

**Changes**:
- Added format example `[1,2,3 or ...]` showing how to input
- "comma separated numbers" → implicit from example
- Removed "to approve" (context is clear)
- `0` → `'0'` (consistent quoting)

**Rationale**: Example is clearer than description; users see format immediately

**Test Coverage**: `TestConsoleOutput_TaskSelectionMenu`

---

### 4. Conflict Resolution Messages

**Location**: `internal/cli/run.go:1350, 1354`

**Before**:
```go
fmt.Fprintln(w, "Plan conflict reported by orchestration:")
// ...
fmt.Fprint(w, "Provide guidance (text), 'm' for more options, or type 'abort' to cancel: ")
```

**After**:
```go
fmt.Fprintln(w, "The orchestration agent detected a plan conflict:")
// ...
fmt.Fprint(w, "How should this be resolved? ('m' for more options, 'abort' to cancel): ")
```

**Changes**:
- "reported by orchestration" → "The orchestration agent detected" (less technical)
- "Provide guidance (text)" → "How should this be resolved?" (more direct question)
- Consistent quoting for options

**Rationale**:
- "orchestration" alone is jargon; "orchestration agent" is clearer
- Questions are more natural than instructions
- Spec philosophy: human-in-control, so ask questions rather than demand input

**Test Coverage**: `TestConsoleOutput_ConflictSummary`

---

### 5. Discovery Message

**Location**: `internal/cli/run.go:1742` (helper: `printDiscoveryMessage`)

**Before**:
```go
fmt.Fprintln(w, "Running workspace discovery...")
```

**After**:
```go
fmt.Fprintln(w, "Discovering plan files in workspace...")
```

**Changes**:
- "Running workspace discovery" → "Discovering plan files in workspace"
- More specific about what's being discovered

**Rationale**: Users don't need to know it's "running"; they care what it's looking for

**Test Coverage**: `TestConsoleOutput_DiscoveryMessage`

---

## Design Decisions

### 1. Prioritize Examples Over Descriptions

**Decision**: Show format in prompts rather than describing it
**Example**: `[1,2,3 or blank for all]` instead of `(comma separated numbers, blank for all)`

**Alternative Considered**: Keep descriptive text
**Why Example is Better**:
- Faster to understand (visual pattern recognition)
- Less ambiguous (is it "1, 2, 3" or "1,2,3"?)
- Matches how users think ("show me an example")

---

### 2. Consistent Quoting for Options

**Decision**: Quote all special options (`'0'`, `'m'`, `'abort'`)
**Example**: `'m' for more` not `m for more`

**Rationale**:
- Visual consistency across all prompts
- Distinguishes option keywords from surrounding text
- Matches conventions in CLI tools (e.g., git, npm)

---

### 3. Shorter Over Complete

**Decision**: Prefer brevity when meaning is clear from context
**Example**: "Select a plan" not "Select a plan candidate to use"

**Rationale**:
- Console output is ephemeral; users scan it quickly
- Verbosity creates cognitive load
- Context (menu just above) makes meaning obvious

---

### 4. Questions Over Instructions

**Decision**: Ask questions ("How should this be resolved?") rather than give commands ("Provide guidance")

**Rationale**:
- Aligns with spec philosophy: human-in-control, collaborative
- Questions feel less demanding, more conversational
- Natural for an AI assistant workflow

---

## Test Updates

### Files Modified

1. **`internal/cli/console_output_test.go`**:
   - Line 24: Added assertion for example text in TTY prompt
   - Line 41: Added assertion example text is absent in non-TTY
   - Lines 123-125: Updated plan selection assertions
   - Lines 203-206: Updated task selection assertions
   - Lines 387, 401-403: Updated conflict message assertions
   - Lines 494-495: Updated discovery message assertions

2. **`internal/cli/run_test.go`**:
   - Line 106: Updated plan selection assertion
   - Line 120: Updated task selection assertion
   - Line 161: Updated discovery message assertion

### Test Strategy

All changes followed this workflow:
1. Update copy in `run.go`
2. Run tests to see failures showing old vs new text
3. Review diffs to confirm changes are intentional
4. Update test assertions to match new copy
5. Verify all tests pass

This ensures tests act as **regression guards** for UX copy.

---

## Review & Fixes

### Initial Review (2025-10-22)

**Reviewer**: Codex (GPT-5)
**Status**: ❌ Changes Requested
**Finding**: Missing assertion for new instruction prompt example text

**Issue**: `TestConsoleOutput_IntakePrompt_TTY` only checked for `"lorch> What should I do?"` but didn't verify the new example text was present. This meant the example could be removed without tests failing.

**Fix Applied**:
1. Added explicit assertion in TTY test:
   ```go
   require.Contains(t, result, "(e.g., \"Manage PLAN.md\" or \"Implement section 3.1\")")
   ```

2. Added negative assertion in non-TTY test:
   ```go
   require.NotContains(t, result, "(e.g., \"Manage PLAN.md\" or \"Implement section 3.1\")")
   ```

**Result**: ✅ Tests now enforce the complete prompt including example text

---

## Spec Alignment

### MASTER-SPEC Requirements Met

**§4.1 Normal Execution - Phase 2 NL Intake**:
> "If user starts with no explicit task: lorch prompts:
> `lorch> What should I do? (e.g., "Use PLAN.md and manage its implementation")`"

✅ **Implemented**: Prompt now includes example text as specified

**§9 Routing & Console Transcript**:
> "lorch prints all agent messages (commands and events) to console in human-readable lines"

✅ **Maintained**: All changes keep output human-readable and clear

**§7.4 Conflict Handling Philosophy**:
> "lorch prints the issue, requests a human decision, records system.user_decision"

✅ **Improved**: Conflict messages now more clearly request human guidance

---

## Integration Notes for Phase 2.5 Task B

### Documentation Tasks

Task B should reference these copy changes when documenting:
- User guide examples should use new prompt text
- CLI reference should show actual prompts with examples
- Troubleshooting guides should reference new error messages

### Files to Update (Task B)

1. **README.md** - Quick start section should show new prompts
2. **docs/AGENT-SHIMS.md** - May need updated examples if it shows intake flows
3. Any tutorial/walkthrough documents showing console output

### Example Documentation Pattern

When documenting intake flow in Task B, use actual console output:

```
$ lorch
lorch> What should I do? (e.g., "Manage PLAN.md" or "Implement section 3.1")
> Implement the authentication system from PLAN.md

Discovering plan files in workspace...
[lorch→orchestration] intake (task: intake-20251022-...)
...
```

---

## Lessons Learned

### What Worked Well

1. **Test-Driven Copy Changes**
   - Snapshot tests caught every change
   - Reviewing test diffs confirmed changes were intentional
   - No manual QA needed; tests enforce UX

2. **Examples in Prompts**
   - Clear, immediate understanding
   - Reduces support burden (fewer "what do I type?" questions)

3. **Consistent Conventions**
   - Quoting, phrasing, question format all uniform
   - Easy to maintain; new prompts follow established patterns

### What to Improve

1. **Initial Test Coverage Gap**
   - Forgot to assert example text in first implementation
   - Fixed after review, but should have been in original tests
   - **Lesson**: When adding new text, explicitly test for it

2. **No User Testing**
   - Changes based on spec and best practices, not actual user feedback
   - **Future**: Consider lightweight user testing for major UX changes

---

## Metrics

| Metric | Value |
|--------|-------|
| **Prompts Updated** | 5 |
| **Lines Changed (run.go)** | 5 |
| **Tests Updated** | 9 assertions |
| **Test Files Modified** | 2 |
| **Regressions Introduced** | 0 |
| **Full Suite Status** | ✅ All passing |

---

## Deliverables

### Code Changes
- ✅ `internal/cli/run.go` - 5 UX copy improvements
- ✅ `internal/cli/console_output_test.go` - 7 test assertion updates
- ✅ `internal/cli/run_test.go` - 3 test assertion updates

### Documentation
- ✅ This document (`docs/development/phase-2.5-task-a.md`)
- ✅ Review document (`docs/development/phase-2.5-task-a-review-1.md`)

### Test Results
- ✅ Console output tests: 12/12 passing
- ✅ Full regression suite: All tests passing
- ✅ Execution time: ~2s (unit tests), ~40s (full suite)

---

## Next Steps

**Immediate** (Task B - Documentation):
1. Update README.md with new prompt examples
2. Review docs/AGENT-SHIMS.md for any intake flow examples
3. Update any walkthrough/tutorial documents

**Future** (Phase 3):
1. Consider adding more examples to other prompts
2. Evaluate if success messages need enhancement (considered adding ✓, deferred for now)
3. Get user feedback on new prompts

---

**Task A Status**: ✅ Complete
**Next Task**: Phase 2.5 Task B - Documentation Updates
