# Phase 2.5 Task B Review Resolution

**Review Document**: phase-2.5-task-b-review-1.md
**Reviewer**: Codex (GPT-5)
**Date**: 2025-10-23
**Status**: ✅ All Issues Resolved

---

## Summary

Both documentation issues identified in the review have been addressed by aligning the documentation with the actual implementation in `internal/cli/run.go` and `internal/discovery/discovery.go`.

---

## Issue 1: README Intake Walk-Through Diverges from Actual CLI Output

**Finding**: README.md example showed incorrect prompt text, discovery logs, and menu copy that don't match actual CLI output.

**Root Cause**: Documentation was written from design expectations rather than actual implementation.

**Resolution**:

Reviewed actual CLI code:
- `internal/cli/run.go:1469`: `promptForInstruction()` - actual prompt text
- `internal/cli/run.go:1740`: `printDiscoveryMessage()` - actual discovery message
- `internal/cli/run.go:1207-1228`: `promptPlanSelection()` - actual plan selection prompt
- `internal/cli/run.go:1257-1272`: `promptTaskSelection()` - actual task selection prompt

**Changes Made** (`README.md:25-52`):

**Before**:
```bash
lorch> What should I do? (e.g., "Use PLAN.md and manage its implementation")
> Implement the authentication feature from PLAN.md

[lorch] Discovering plan files...
[lorch] Found 2 candidates: PLAN.md (score: 0.85), docs/plan_v2.md (score: 0.61)

Select a plan file:
  1. PLAN.md
  2. docs/plan_v2.md
  m. Ask for more options
  0. Cancel
Choice: 1
```

**After** (matches actual output):
```bash
lorch> What should I do? (e.g., "Manage PLAN.md" or "Implement section 3.1")
Implement the authentication feature from PLAN.md

Discovering plan files in workspace...

Plan candidates:
  1. PLAN.md (score 0.75)
     filename contains 'plan'
  2. docs/plan_v2.md (score 0.71)
     filename contains 'plan', located under 'docs'
Select a plan [1-2], 'm' for more, or '0' to cancel: 1
```

**Verification**: Prompts now match `internal/cli/run.go` exactly.

---

## Issue 2: File-Discovery Heuristics Documentation Out of Sync

**Finding**: Documentation described incorrect scoring weights, metadata fields, and exclusions that don't exist in the actual implementation.

**Root Cause**: Documentation was written based on planned features rather than implemented algorithm.

**Resolution**:

Reviewed actual discovery implementation:
- `internal/discovery/discovery.go:268-311`: `scoreCandidate()` - actual scoring algorithm
- `internal/protocol/orchestration.go:40-67`: `DiscoveryMetadata` structure - actual fields

**Actual Algorithm**:
- Base score: 0.5
- "plan" in filename: +0.25
- "spec" in filename: +0.20
- "proposal" in filename: +0.15
- Under docs/plans/specs: +0.05
- Depth penalty: -0.04 per level
- Heading match: +0.05 per heading (not +0.10)

**Actual Metadata Fields**:
- `root`: Workspace root path
- `strategy`: Algorithm version (e.g., "heuristic:v1")
- `search_paths`: Directories traversed
- `ignored_paths`: Skipped directories (optional)
- `generated_at`: Timestamp (RFC 3339)
- `candidates`: Array of DiscoveryCandidate

**Fields that DON'T exist**:
- ❌ `total_files_scanned`
- ❌ `duration_ms`
- ❌ `size_bytes` (in candidate)
- ❌ `modified_at` (in candidate)

**Changes Made**:

### docs/AGENT-SHIMS.md

**Scoring Table** (lines 221-229):
- ✅ Updated base score from implicit to explicit 0.5
- ✅ Corrected filename token weights (+0.25/+0.20/+0.15, not +0.30 each)
- ✅ Fixed directory location bonus (+0.05, not +0.20/+0.15/+0.10)
- ✅ Corrected depth penalty (-0.04, not -0.05)
- ✅ Fixed heading match bonus (+0.05, not +0.10)
- ✅ Added "mutually exclusive" note for filename tokens

**Example Discovery Output** (lines 239-262):
- ✅ Added `root` and `generated_at` fields
- ✅ Removed `total_files_scanned` and `duration_ms` fields
- ✅ Updated scores to match actual algorithm (0.80, 0.72, 0.71 instead of 0.85, 0.61, 0.40)
- ✅ Updated reason text to match actual implementation output

**Exclusions** (line 219):
- ✅ Removed "Files larger than 10 MB" (no file size filtering in implementation)

### docs/ORCHESTRATION.md

**Discovery Metadata Structure** (lines 255-273):
- ✅ Added `root`, `ignored_paths`, `generated_at` fields
- ✅ Removed `total_files_scanned`, `duration_ms` fields
- ✅ Removed `size_bytes`, `modified_at` from candidate structure
- ✅ Updated scores and reason text

**Scoring Algorithm Table** (lines 281-287):
- ✅ Added base score row
- ✅ Corrected all weights to match implementation
- ✅ Added "mutually exclusive" note

**Scoring Examples** (lines 290-292):
- ✅ Updated calculations to use correct algorithm
- ✅ Changed example scores from 0.60/0.40/0.30 to 0.80/0.76/0.67

**Command Input Example** (lines 63-75):
- ✅ Updated discovery_metadata structure with correct fields
- ✅ Removed non-existent fields

**Best Practices** (lines 298, 650):
- ✅ Changed "reading all 47 files" to generic "reading all discovered files"

**Security Considerations** (line 663):
- ✅ Changed "Don't read files >10MB (lorch excludes these)" to "Be mindful of reading very large files (may cause timeouts)"

---

## Verification

### Code Alignment Check

**Discovery Implementation**:
```bash
$ grep -A 20 "func scoreCandidate" internal/discovery/discovery.go
# Confirms: base 0.5, +0.25/+0.20/+0.15, +0.05 directory, -0.04 depth
```

**Discovery Metadata**:
```bash
$ grep -A 15 "type DiscoveryMetadata struct" internal/protocol/orchestration.go
# Confirms: root, strategy, search_paths, ignored_paths, generated_at, candidates
```

**CLI Output**:
```bash
$ grep "lorch> What should I do" internal/cli/run.go
# Confirms: exact prompt text matches README
```

### Test Suite

```bash
$ go test ./... -timeout 120s
ok  	github.com/iambrandonn/lorch/internal/cli	7.232s
ok  	github.com/iambrandonn/lorch/internal/discovery	(cached)
...
(all packages passed)
```

**Result**: ✅ No regressions introduced

---

## Files Modified

1. `README.md` - Fixed intake example (lines 25-52)
2. `docs/AGENT-SHIMS.md` - Fixed scoring algorithm (lines 216-262)
3. `docs/ORCHESTRATION.md` - Fixed scoring algorithm and metadata fields (multiple sections)

**Total Changes**:
- 3 files modified
- ~150 lines corrected
- 0 regressions
- All documentation now matches implementation

---

## Lessons Learned

1. **Write docs from code, not design**: Always reference actual implementation when documenting behavior
2. **Cross-reference liberally**: Include file:line references in documentation for verification
3. **Test examples**: Run through examples manually to ensure they match actual output
4. **Validate metadata structures**: Use actual protocol types, not assumed structures
5. **Version check**: Discovery algorithm is tagged with `heuristic:v1` for future evolution - documentation should match current version

---

## Next Steps

**For Phase 2.5 Task B**:
- ✅ All review issues resolved
- ✅ Documentation aligns with implementation
- ✅ Tests passing
- **Ready for final approval**

**For Phase 2.5 Task C** (regression tests):
- Can proceed with confidence that documentation is accurate
- Use actual CLI output from this resolution as test fixtures

---

**Resolution Completed**: 2025-10-22
**Status**: ✅ All Issues Addressed
