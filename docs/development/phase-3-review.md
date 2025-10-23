# Phase 3 Plan Review: Pragmatic Feedback & Recommendations

**Date:** 2025-10-22
**Reviewer:** Technical Review
**Target:** PLAN.md Phase 3 steps vs MASTER-SPEC.md requirements

## Executive Summary

**Assessment:** Phase 3 is well-structured and maintains testing discipline, but includes **~40% scope creep** beyond MASTER-SPEC requirements. The core deliverable (interactive config editor) is appropriate, but the plan adds significant complexity in preset systems, credential management, and premature migration frameworks.

**Recommendation:** Simplify to focus on interactive editor + documentation, defer advanced features to Phase 4.

---

## What MASTER-SPEC Actually Requires

From MASTER-SPEC.md §1.2 (line 42-43):
> "**Phase 3 – Interactive Configuration**: Ship auto-initialized `lorch.json`, the `lorch config` interactive editor with two-tier validation, and generic LLM/tool configuration hooks to keep the orchestrator flexible."

### Requirements breakdown:
1. ✅ **Auto-initialized `lorch.json`** — Already delivered in Phase 1 (P1.1 Task C)
2. ❓ **Interactive config editor with two-tier validation** — Core Phase 3 requirement
3. ❓ **Generic LLM/tool configuration hooks** — Already supported via `agents.*.cmd` and `agents.*.env`

**Key observation:** 2 of 3 requirements are already done. Phase 3 is primarily about the editor experience.

---

## Milestone-by-Milestone Analysis

### P3.1 – Config Schema & Validation Framework ⚠️ **OVER-ENGINEERED**

#### What the plan proposes:
- Full schema versioning with semver parsing
- Migration framework with intermediate version support (`MigratorRegistry` interface)
- Deprecation warnings system
- Two-tier validation (startup strict vs edit permissive)
- Enhanced config metadata (`SchemaVersion`, `LastModified`, `Comment`)
- Compatibility checks with version mismatch detection

#### Issues:

**1. YAGNI Violation (You Aren't Gonna Need It)**
- Currently at v1.0 with no migrations needed
- Building `MigratorRegistry`, intermediate version support, and deprecation system before any migrations exist
- Premature abstraction solving future problems

**2. Two-Tier Validation Confusion**
- MASTER-SPEC says validation happens "on `lorch config` changes and at startup"
- This likely means: same validation, different contexts (interactive feedback vs fail-fast)
- Plan interprets as different strictness levels (edit allows incomplete, startup requires complete)
- Risk: User saves incomplete config during edit, lorch crashes on next startup

**3. Schema Version Duplication**
- Config already has `"version": "1.0"` field (config.go:11)
- Plan adds separate `SchemaVersion` field
- Creates confusion: which version is authoritative?

**4. Metadata Bloat**
- `LastModified` timestamp: Git already tracks this
- `Comment` field as `_comment`: Not in MASTER-SPEC, adds surface area

#### Pragmatic recommendation:

**Simplify to:**
- Version checking: Warn if config.version != expected, error if incompatible
- Basic struct validation with clear error messages
- Defer migration framework until v1.1 actually exists
- Same validation everywhere, but interactive feedback during editing

**Rationale:** Build migration tools when you need them (when v1.1 changes schema), not speculatively.

---

### P3.2 – Interactive Config Editor (`lorch config`) ✅ **APPROPRIATE**

#### What the plan proposes:
- Section-based prompts (Policy → Agents → Tasks)
- Inline validation feedback after each input
- Diff preview with confirmation before save
- Field-specific editors (strings, arrays, maps, nested objects)
- JSON path navigation for nested keys
- Support for quit/help commands

#### Assessment:
- **This IS the core Phase 3 deliverable** ✅
- Complexity is justified for good UX
- Well-aligned with spec's "git config style" approach
- Good balance of features without over-engineering

#### One minor concern:
- JSON path navigation (`agents.builder.cmd` → nested editing) might be complex
- Consider: Start with top-level section editing, add deep nesting if users request it
- Acceptable to ship "edit entire agent block" vs "edit single nested field" initially

#### Recommendation:
**Keep as-is**, with option to simplify nested editing if implementation proves complex.

---

### P3.3 – Extensibility & Presets ⚠️ **MAJOR SCOPE CREEP**

#### What the plan proposes:
- Built-in preset system (`internal/config/presets/` directory)
- Preset files: `claude.json`, `openai.json` embedded in binary
- `lorch config init --preset <name>` command
- `lorch config list-presets` command
- Credential management system with prompting
- Keychain integration (optional)
- Credential placeholder syntax: `"${ANTHROPIC_API_KEY}"`
- Template expansion with fallbacks: `"${CLAUDE_BIN:-claude}"`
- Per-agent environment override system with merge priority (agent > global > system)
- `EnvDefaults` section in config for global env vars
- Runtime overrides: `lorch run --env KEY=value`

#### Issues:

**1. Preset System: Solving a Non-Problem**
- Current config already supports swapping agents (change `cmd` and `env`)
- Example configs in `docs/examples/` achieve same goal without complexity
- No need for embedded presets, loading logic, or list-presets command
- Adds: preset directory structure, loading/validation code, new commands

**2. Credential Management: Not in Spec**
- MASTER-SPEC mentions "redact env ending in `_TOKEN|_KEY|_SECRET`" (§13) but not credential management
- Current approach works: set env vars, agents read them
- Adds significant complexity:
  - Placeholder detection/parsing
  - Interactive prompting during first run
  - Keychain integration
  - Credential storage decisions (where? how?)

**3. Template Expansion: Requires Parser**
- `"${VAR:-default}"` syntax needs bash-like template parser
- Current approach simpler: agents read env directly at runtime
- Unix philosophy: use environment variables as-is
- Adds: template parsing logic, error handling for malformed templates

**4. Environment Override Layers: Already Works**
- Current: `agents.builder.env` overrides system env (simple, clear)
- Plan adds: `EnvDefaults` + agent-specific + runtime `--env` flags with 3-level merge
- Increases cognitive load: "Which env var won? Where did this value come from?"

#### Pragmatic recommendation:

**Replace entire P3.3 with:**
- Create `docs/examples/` directory with sample configs:
  - `lorch-claude.json` (claude-agent configuration)
  - `lorch-openai.json` (hypothetical OpenAI agent)
  - `lorch-local-mock.json` (mockagent for testing)
  - `README.md` explaining how to use them
- Document in `docs/CONFIGURATION.md`: "Copy example config: `cp docs/examples/lorch-claude.json lorch.json`"
- Document environment variable usage (already supported via `agents.*.env`)
- **No new code required** — just documentation and example files

**Defer to Phase 4 or never:**
- Preset loading system
- Credential management
- Template expansion
- Complex environment merge logic

**Estimated scope reduction:** ~60% of P3.3 eliminated

---

### P3.4 – Additional Commands, Documentation & Polish ⚠️ **MIXED BAG**

#### What the plan proposes:
- `lorch validate [--config path]` standalone command
- `lorch doctor` health check command (env checks, binary checks, permissions)
- `docs/CONFIGURATION.md` comprehensive guide
- `docs/examples/` with 3 example configs
- Migration regression tests (`testdata/configs/{v1.0,v1.1,...}`)
- Config editor idempotency tests

#### Assessment:

**✅ Keep (aligned with spec or high value):**
- `lorch validate` command: Useful for CI, reasonable addition
- `docs/CONFIGURATION.md`: Essential documentation
- `docs/examples/`: Moved from P3.3, high value
- Config editor idempotency tests: Core functionality validation

**❓ Questionable:**
- `lorch doctor` command: MASTER-SPEC §8.3 lists it as "planned, low priority"
  - Adds ~100-200 LOC for checks (binaries exist, dirs writable, git repo, env vars set)
  - Nice-to-have but not blocking

**⚠️ Premature:**
- Migration regression tests: No migrations exist yet (still v1.0)
- `testdata/configs/{v1.0,v1.1,...}` directory structure assumes future versions
- Should be added when v1.1 actually ships

#### Pragmatic recommendation:

**Keep:**
- `lorch validate` command with `--strict` flag for CI
- Complete `docs/CONFIGURATION.md` with examples
- Config editor round-trip tests (edit → save → load → verify)

**Simplify or defer:**
- `lorch doctor`: Implement as `lorch validate --verbose` instead (reuse code)
- Migration tests: Add when v1.1 changes schema format

---

## Alignment with MASTER-SPEC Principles

MASTER-SPEC §1.2 Goals:
- ✅ **"Local-first"** — No concerns
- ⚠️ **"Clarity & Extensibility: minimal deps"** — Phase 3 plan adds complexity beyond spec
- ✅ **"generic LLM CLI wrappers; swappable agents"** — Already achieved, presets not needed for this
- ✅ **"Human-in-control"** — Interactive editor supports this well

MASTER-SPEC §8 Configuration Management:
- ✅ Auto-creation (done in Phase 1)
- ❓ Two-tier validation (interpretation differs)
- ✅ Generic agent configuration (already works via `cmd`/`env`)

---

## Specific Technical Concerns

### 1. Schema Version vs Config Version
**P3.1 Task C** adds `SchemaVersion` field separate from `Config.Version`.
- Current: `config.Version = "1.0"` (line 11 of internal/config/config.go)
- Plan: Add separate `config.SchemaVersion` field
- **Issue:** Two version fields create confusion. Which is authoritative? How do they differ?
- **Recommendation:** Use single `version` field, track schema changes in docs

### 2. ValidateForEdit() Allows Incomplete Config
**P3.1 Task B** says `ValidateForEdit()` "MAY have incomplete fields, return warnings not errors."
- **Risk:** User saves incomplete config → lorch crashes on next startup
- **Better approach:** Always validate strictly, provide helpful guidance during interactive editing
- Editor can show "This field is required" without allowing save

### 3. Credential Placeholder Parsing
**P3.3 Task B** requires parsing `"${ANTHROPIC_API_KEY}"` syntax.
- Requires implementing template parser (lexer, variable extraction, fallback handling)
- Current approach simpler: agents read from env directly via OS
- **Alternative:** Document that agents should read env vars themselves (push complexity to agent shims)

### 4. Duplicate Doctor Commands
- **P3.3 Task D:** "Add `lorch config doctor` to diagnose common misconfigurations"
- **P3.4 Task B:** "Implement `lorch doctor` health check command"
- **Issue:** Same command specified twice in different milestones
- **Recommendation:** Consolidate into single implementation in P3.4 (or defer)

### 5. Migration Test Prematureness
**P3.4 Task D** includes regression tests for `v1.0 → v1.1` migrations.
- No v1.1 exists yet
- No schema changes planned yet
- Tests would be empty stubs or fictional scenarios
- **Recommendation:** Add migration tests in same PR that introduces v1.1 schema changes

---

## Recommended Revision: Pragmatic Phase 3

### P3.1 – Basic Validation & Version Checking (Simplified)

**Scope:**
- Version checking: Warn if config.version != expected, error if incompatible
- Struct validation with clear error messages (required fields, valid enums, ranges)
- Field-level validation helpers for editor integration
- Defer migration framework until v1.1 or v2.0 actually exists

**Deliverables:**
- `internal/config/validation.go` with `Validate(cfg) []error` function
- `internal/config/version.go` with `CheckVersion(cfg)` function
- Unit tests for validation (valid/invalid configs)
- Unit tests for version checking (match, newer, older, incompatible)

**Exit criteria:**
- Validation rejects invalid configs with helpful, field-specific error messages
- Version check warns on mismatch, errors on incompatibility
- ~15-20 unit tests passing

**Estimated effort:** 2-3 days (vs 5-7 days in original plan)

---

### P3.2 – Interactive Config Editor (Keep As-Is)

**Scope:**
- Section-based prompts (Policy → Agents → Tasks)
- Inline validation feedback after each input
- Diff preview with before/after comparison
- Confirmation before save
- Backup old config to `.lorch.json.backup.<timestamp>`
- Field-specific editors for common types

**Note on nesting:**
- Start with section-level editing (edit entire agent block as JSON or key-value pairs)
- Deep nested field editing (`agents.builder.cmd[0]`) can be added later if users request it

**Deliverables:**
- `internal/config/editor.go` with interactive prompt logic
- `internal/config/diff.go` for change preview
- `lorch config edit/get/set/show` commands
- Integration tests with mocked stdin/stdout
- Golden transcript tests for prompt flows

**Exit criteria:**
- Full edit flow works (prompt → validate → diff → save)
- `lorch config get/set` work for nested keys like `agents.builder.cmd`
- Validation errors prevent save with clear inline feedback
- Non-TTY tests pass for CI/automation

**Estimated effort:** 7-10 days (unchanged)

---

### P3.3 – Documentation & Examples (Replaces Presets Milestone)

**Scope:**
- Create `docs/CONFIGURATION.md` with structure reference, validation guide, troubleshooting
- Create `docs/examples/` directory with sample configs:
  - `lorch-claude.json`: claude-agent with typical Anthropic settings
  - `lorch-openai.json`: generic OpenAI-compatible agent config
  - `lorch-local-mock.json`: mockagent configuration for testing
  - `README.md`: explains how to use examples, copy-paste workflow
- Update `README.md` with configuration section linking to docs
- Document environment variable usage (agents read from `agents.*.env`)

**No new code required** — just documentation and example files.

**Deliverables:**
- `docs/CONFIGURATION.md` (~2000 lines with examples, troubleshooting, best practices)
- `docs/examples/lorch-*.json` (3 files + README)
- Updated `README.md` "Configuration" section

**Exit criteria:**
- Documentation covers all config fields with examples
- Example configs are valid and tested (load successfully)
- Users can configure different agents by copying examples

**Estimated effort:** 3-4 days (vs 8-10 days for preset system)

---

### P3.4 – Validate Command & Polish (Simplified)

**Scope:**
- Implement `lorch validate [--config path]` command
  - Validate schema (required fields, types, ranges)
  - Check compatibility (version matches binary)
  - Verify agent binaries exist and are executable (basic doctor checks)
  - Exit codes: 0 = valid, 1 = errors, 2 = warnings
- Add `--strict` flag to treat warnings as errors (CI mode)
- Add `--verbose` flag for detailed diagnostics (includes binary checks, env var checks)
- Config editor round-trip tests (edit → save → load → verify no changes)
- Update `lorch validate --help` with clear usage examples

**Defer to Phase 4:**
- Standalone `lorch doctor` command (covered by `validate --verbose`)
- Migration regression tests (add when migrations exist)

**Deliverables:**
- `internal/cli/validate.go` with command implementation
- `internal/config/validate.go` enhancements (binary existence checks)
- ~10 unit tests for validate command scenarios
- Integration test for editor round-trip
- Updated CLI help text

**Exit criteria:**
- `lorch validate` reports clear errors for invalid configs
- `--strict` flag works for CI usage
- `--verbose` flag provides agent binary diagnostics
- Editor round-trip produces no spurious diffs

**Estimated effort:** 3-4 days (vs 5-7 days with doctor + migrations)

---

## Summary Comparison

| Aspect | Original Plan | Pragmatic Plan | Change |
|--------|---------------|----------------|--------|
| **New packages** | 8 (version, migration, validation, editor, diff, presets, credentials, overrides) | 4 (validation, version, editor, diff) | -50% |
| **New commands** | 6 (config edit/get/set/show/init/list-presets, validate, doctor) | 4 (config edit/get/set/show, validate) | -33% |
| **New docs** | 3 (CONFIGURATION.md, MIGRATION-GUIDE.md, examples) | 2 (CONFIGURATION.md, examples) | -33% |
| **Test count** | ~75 tests (unit + integration + regression) | ~40 tests (unit + integration) | -47% |
| **Estimated effort** | 20-25 days | 13-17 days | -35% |
| **Scope creep** | ~40% beyond spec | Aligned with spec | Eliminated |

---

## Three Options Forward

### Option A: Conservative (Recommended)
**Implement pragmatic Phase 3** (simplified milestones above)
- **Pros:** Tight alignment with spec, reduced risk, faster delivery, solves actual user needs
- **Cons:** Defers some nice-to-haves (presets, doctor, credentials)
- **Timeline:** ~2-3 weeks
- **Deliverable:** Interactive editor + great docs

### Option B: Phased Approach
**Split into Phase 3a (P3.1-P3.2) and Phase 3b (P3.3-P3.4)**
- **Phase 3a:** Editor only (~10 days)
- **Phase 3b:** Presets & advanced features (~10 days)
- **Pros:** Early delivery of core value, optionality on advanced features
- **Cons:** More overhead, two deploy cycles
- **Timeline:** 3-4 weeks total

### Option C: As-Planned
**Proceed with current plan** (all milestones as written)
- **Pros:** Comprehensive feature set, future-proof
- **Cons:** Significant scope creep, longer timeline, solving problems you don't have yet
- **Timeline:** 4-5 weeks
- **Risk:** Phase 3 becomes larger than Phase 2.1-2.5 combined

---

## Recommendation: Option A (Conservative)

### Rationale:

**1. Spec Compliance**
- MASTER-SPEC Phase 3 is primarily about the interactive editor
- Auto-init already done (Phase 1)
- Generic hooks already work (agents.*.cmd/env)
- Pragmatic plan delivers exactly what's specified

**2. YAGNI Principle**
- No migrations exist → don't build migration framework yet
- No user requests for presets → don't build preset system yet
- Credentials work via env vars → don't build credential manager yet

**3. Faster Time to Value**
- Interactive editor is the core UX win
- Great docs enable users to configure any agent
- ~35% faster delivery means Phase 4 starts sooner

**4. Reduced Complexity**
- Fewer packages = easier to maintain
- Fewer commands = simpler CLI surface
- Fewer abstractions = easier to understand

**5. Extensibility Preserved**
- Can add presets later if users request them
- Can add credential management if env vars prove insufficient
- Can add migrations when v1.1 actually changes schema

### What You Still Get:
- ✅ Interactive config editor with great UX
- ✅ Validation with clear error messages
- ✅ Diff preview before saves
- ✅ Example configs for common scenarios
- ✅ Comprehensive documentation
- ✅ CI-friendly validate command
- ✅ All spec requirements met

### What You Defer:
- ⏸️ Preset loading system (use example configs instead)
- ⏸️ Credential prompting (use env vars for now)
- ⏸️ Template expansion (agents handle env vars)
- ⏸️ Standalone doctor command (use validate --verbose)
- ⏸️ Migration tests (add when migrations exist)

---

## Quick Wins (If You Want Some P3.3 Ideas Without Full Scope)

If you want to keep some advanced features without full implementation:

**1. Environment Templating (Lite)**
- Document that agent shims can read `${VAR}` from config if they want
- No template parsing in lorch — push to agent shims
- Example: claude-agent could expand `${CLAUDE_BIN:-claude}` itself

**2. Preset (Lite)**
- Ship example configs in `docs/examples/`
- Document: `cp docs/examples/lorch-claude.json lorch.json`
- No `lorch config init --preset` command
- No embedded preset loading logic

**3. Doctor (Lite)**
- Add checks to `lorch validate --verbose` instead of separate command
- Reuse validation code
- Output: "✓ Config valid, ✓ Binaries found, ✗ /state/ missing (will be created)"

---

## Questions for Planner

1. **Scope appetite:** Are you willing to reduce Phase 3 scope by ~35% to align with spec, or do you want the full feature set regardless of spec alignment?

2. **Migration timing:** Do you anticipate schema changes in v1.1? If not, migration framework can wait.

3. **Preset value:** Have users requested preset functionality, or is this speculative? Example configs might suffice.

4. **Credential pain:** Is environment variable approach causing friction, or is credential management solving a future problem?

5. **Timeline priority:** Is Phase 3 delivery speed important (to unblock Phase 4), or is comprehensive feature set more important?

---

## Conclusion

Phase 3 as planned is **well-designed but over-scoped** relative to MASTER-SPEC requirements. The interactive editor (P3.2) is the core deliverable and is appropriately scoped. The validation framework (P3.1) and extensibility features (P3.3) add significant complexity to solve problems that may not exist yet.

**Recommendation:** Implement pragmatic Phase 3 (Option A) focusing on editor + docs, defer advanced features until user needs become clear. This maintains spec compliance, reduces risk, and delivers faster.

The planner should evaluate whether the additional features in P3.1/P3.3 solve real user problems or are speculative future-proofing. If speculative, defer to Phase 4. If solving known problems, keep but consider phased delivery (Option B).
