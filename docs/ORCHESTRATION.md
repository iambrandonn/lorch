# Orchestration Agent Technical Reference

**Audience**: Developers implementing orchestration agents for lorch
**Status**: Phase 2 (complete)
**Spec Reference**: MASTER-SPEC.md §2.2, §10.2, §10.4

---

## Overview

The **orchestration agent** is a specialized agent type that translates natural-language instructions into concrete task plans. It participates in lorch's Natural Language Task Intake workflow (Phase 2+) but never directly modifies code or spec files.

### Role in System Architecture

```
User Instruction
      ↓
   lorch (runs file discovery)
      ↓
Orchestration Agent (analyzes instruction + discovered files)
      ↓
   lorch (presents candidates/tasks to user for approval)
      ↓
   lorch (activates approved tasks into standard pipeline)
      ↓
Builder → Reviewer → Spec-Maintainer
```

**Key Principles**:
- **Read-only**: Never writes to workspace (no code, no specs, no config)
- **Stateless**: All context provided in each command; no memory between commands
- **Deterministic**: Same inputs → same outputs (when possible)
- **Human-gated**: User approves all proposals before execution

---

## Protocol Contract

### Transport

- **Format**: NDJSON (newline-delimited JSON) over stdin/stdout
- **Envelope**: `command` messages from lorch → agent, `event` messages from agent → lorch
- **Heartbeats**: Required every 10s (configurable) to prove liveness
- **Timeout**: Default 180s per action (configurable in `lorch.json`)

See MASTER-SPEC.md §3 for full protocol schema.

### Commands (lorch → agent)

Orchestration agents receive two action types:

#### 1. `intake` – Initial Task Planning

**When**: User runs `lorch` without `--task` flag
**Purpose**: Convert NL instruction into task plan

**Input fields**:
```json
{
  "action": "intake",
  "inputs": {
    "user_instruction": "Implement authentication feature from PLAN.md",
    "discovery_metadata": {
      "root": ".",
      "strategy": "heuristic:v1",
      "search_paths": [".", "docs", "specs", "plans"],
      "ignored_paths": [".git", "node_modules"],
      "generated_at": "2025-10-22T18:30:00Z",
      "candidates": [
        {"path": "PLAN.md", "score": 0.75, "reason": "filename contains 'plan'"},
        {"path": "docs/plan_v2.md", "score": 0.71, "reason": "filename contains 'plan', located under 'docs', depth penalty -0.04"}
      ]
    }
  }
}
```

**Expected behavior**:
1. Parse `user_instruction` to understand intent
2. Analyze `discovery_metadata.candidates` to identify relevant plan files
3. (Optionally) Read candidate files to extract tasks
4. Emit `orchestration.proposed_tasks` with plan selection and derived task list

**When to emit**:
- `orchestration.proposed_tasks` – Normal case
- `orchestration.needs_clarification` – Instruction is ambiguous
- `orchestration.plan_conflict` – Multiple conflicting plans detected

#### 2. `task_discovery` – Incremental Task Expansion

**When**: User requests "more options" during approval, or mid-run expansion
**Purpose**: Expand candidate set or derive additional tasks

**Input fields**:
```json
{
  "action": "task_discovery",
  "inputs": {
    "user_instruction": "Original instruction preserved",
    "context": {
      "previous_candidates": ["PLAN.md"],
      "reason": "User requested more options"
    },
    "discovery_metadata": {
      // Refreshed discovery results (may include more files)
    }
  }
}
```

**Expected behavior**:
1. Consider broader criteria (lower score threshold, alternative file types)
2. Derive additional tasks not covered in first proposal
3. Emit `orchestration.proposed_tasks` with expanded candidates/tasks

### Events (agent → lorch)

#### `orchestration.proposed_tasks` (terminal)

**Purpose**: Return plan candidates and derived task objects for user approval

**Payload structure**:
```json
{
  "event": "orchestration.proposed_tasks",
  "payload": {
    "plan_candidates": [
      {"path": "PLAN.md", "confidence": 0.85},
      {"path": "docs/roadmap.md", "confidence": 0.61}
    ],
    "derived_tasks": [
      {
        "id": "T-100",
        "title": "Implement user authentication module",
        "description": "Add login/logout endpoints and session management",
        "files": ["src/auth.go", "src/session.go", "tests/auth_test.go"],
        "estimated_complexity": "medium"
      },
      {
        "id": "T-101",
        "title": "Add password reset flow",
        "files": ["src/password_reset.go", "tests/password_reset_test.go"]
      }
    ],
    "notes": "Derived 2 tasks from PLAN.md sections 3.1-3.2"
  }
}
```

**Field requirements**:
- `plan_candidates`: Array of `{"path": string, "confidence": 0.0-1.0}`
  - At least 1 candidate required
  - `confidence` is relative ranking, not absolute probability
  - Sorted by confidence DESC (lorch will re-sort if needed)
- `derived_tasks`: Array of task objects (may be empty if only discovery)
  - `id`: Unique identifier (convention: `T-<number>` or `T-<parent>-<sub>`)
  - `title`: Human-readable task summary (required)
  - `files`: Array of file paths likely affected (optional but recommended)
  - Additional fields are ignored by lorch but preserved in receipts
- `notes`: Optional rationale or context

**Validation errors lorch will reject**:
- Missing `plan_candidates` or empty array
- `confidence` outside 0.0-1.0 range
- Missing `id` or `title` in any `derived_tasks` entry
- Duplicate task IDs within same proposal

#### `orchestration.needs_clarification` (terminal)

**Purpose**: Request additional information from user when instruction is ambiguous

**Payload structure**:
```json
{
  "event": "orchestration.needs_clarification",
  "payload": {
    "questions": [
      "Which authentication method should be used? (OAuth, JWT, session cookies)",
      "Should this include password reset flows?"
    ],
    "context": {
      "original_instruction": "Implement authentication feature",
      "ambiguity_reason": "Multiple authentication approaches possible"
    }
  }
}
```

**Behavior**:
1. Lorch displays questions to user (numbered menu)
2. User provides answers (free-form text)
3. Lorch re-invokes `intake` with **same idempotency key** but updated inputs:
   ```json
   {
     "user_instruction": "Original instruction + user answers",
     "clarifications": ["JWT authentication", "Yes, include password reset"],
     "discovery_metadata": {...}
   }
   ```
4. Agent must handle updated inputs and provide `proposed_tasks`

**Best practices**:
- Limit to 3-5 questions per clarification round
- Make questions specific and actionable
- Provide context in `ambiguity_reason` for debugging
- Preserve original instruction in `context` for continuity

#### `orchestration.plan_conflict` (terminal)

**Purpose**: Surface conflicts when multiple incompatible plans are detected

**Payload structure**:
```json
{
  "event": "orchestration.plan_conflict",
  "payload": {
    "conflicts": [
      {
        "paths": ["PLAN.md", "PLAN-v2.md"],
        "reason": "Both define conflicting task T-0042 with different acceptance criteria"
      },
      {
        "paths": ["specs/AUTH-SPEC.md", "specs/AUTH-SPEC-DRAFT.md"],
        "reason": "Duplicate spec files with divergent requirements"
      }
    ],
    "suggested_resolution": "Use PLAN.md and specs/AUTH-SPEC.md (most recent)",
    "notes": "Consider archiving deprecated files"
  }
}
```

**Behavior**:
1. Lorch displays conflicts to user
2. User selects resolution strategy:
   - Choose one plan (lorch filters discovery)
   - Abort (user will manually resolve outside lorch)
   - Retry with guidance (re-invoke `intake` with hint)
3. If user provides guidance, lorch re-invokes with updated context

**When to emit**:
- Multiple plans define overlapping task IDs
- Contradictory requirements across candidate files
- Ambiguous hierarchy (which plan is canonical?)

---

## File Discovery Integration

Lorch performs deterministic file discovery **before** invoking the orchestration agent. The agent receives discovery results as read-only metadata.

### Discovery Metadata Structure

```json
{
  "root": ".",
  "strategy": "heuristic:v1",
  "search_paths": [".", "docs", "specs", "plans"],
  "ignored_paths": [".git", "node_modules", "vendor"],
  "generated_at": "2025-10-22T18:30:00Z",
  "candidates": [
    {
      "path": "PLAN.md",
      "score": 0.80,
      "reason": "filename contains 'plan', heading matches 'plan'"
    },
    {
      "path": "docs/plan_v2.md",
      "score": 0.76,
      "reason": "filename contains 'plan', located under 'docs', depth penalty -0.04"
    }
  ]
}
```

### Scoring Algorithm (`heuristic:v1`)

Lorch uses these heuristics to rank candidate files:

| Factor | Weight | Notes |
|--------|--------|-------|
| Base score | 0.5 | Starting point for all files |
| Filename tokens | +0.25 ("plan"), +0.20 ("spec"), +0.15 ("proposal") | Mutually exclusive; case-insensitive |
| Directory location | +0.05 | If located under `docs/`, `plans/`, or `specs/` at any depth |
| Depth penalty | -0.04 per level | Nested files rank lower |
| Heading match | +0.05 per heading | Headings containing "plan", "spec", or "proposal" |

**Example**:
- `PLAN.md` at root with heading "Authentication Plan": 0.5 + 0.25 + 0.05 = **0.80**
- `docs/auth-plan.md` at depth 1: 0.5 + 0.25 + 0.05 - 0.04 = **0.76**
- `specs/nested/auth-spec.md` at depth 2: 0.5 + 0.20 + 0.05 - 0.08 = **0.67**

### Using Discovery Results

**Best practices**:
1. **Trust the ranking**: Candidates are pre-sorted by score; top candidate is usually correct
2. **Use scores as confidence**: Map score ranges to confidence levels (e.g., >0.75 = high)
3. **Read top N files**: Focus on top 2-3 candidates (reading all discovered files may cause timeouts)
4. **Explain selections**: Reference score/reason in `notes` field for transparency
5. **Handle ties**: If scores are close (<0.10 difference), ask user to choose

**Don't**:
- ❌ Trigger your own discovery (agents are read-only)
- ❌ Ignore provided candidates and search from scratch
- ❌ Treat score as absolute probability (it's relative ranking)

### Requesting Additional Discovery

If initial candidates are insufficient, use `task_discovery` action:

**In `proposed_tasks` payload**:
```json
{
  "plan_candidates": [...],
  "derived_tasks": [],
  "notes": "Suggest running task_discovery with broader criteria (include .txt files)"
}
```

Lorch will offer "m. Ask for more options" in approval menu. If user selects it, lorch invokes `task_discovery` with refreshed discovery metadata.

---

## Implementation Guide

### Minimal Implementation (Echo Agent)

A barebones orchestration agent that echoes back the top discovery candidate:

```python
#!/usr/bin/env python3
import sys, json, time

def main():
    for line in sys.stdin:
        msg = json.loads(line)
        if msg.get('kind') == 'command' and msg.get('action') == 'intake':
            inputs = msg['inputs']
            candidates = inputs.get('discovery_metadata', {}).get('candidates', [])

            # Return top candidate and dummy task
            response = {
                'kind': 'event',
                'message_id': f"evt-{int(time.time())}",
                'correlation_id': msg['correlation_id'],
                'task_id': msg['task_id'],
                'from': {'agent_type': 'orchestration'},
                'event': 'orchestration.proposed_tasks',
                'payload': {
                    'plan_candidates': [
                        {'path': candidates[0]['path'], 'confidence': candidates[0]['score']}
                    ] if candidates else [],
                    'derived_tasks': [
                        {'id': 'T-999', 'title': 'Placeholder task', 'files': []}
                    ]
                },
                'occurred_at': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
            }
            print(json.dumps(response), flush=True)
            break

if __name__ == '__main__':
    main()
```

Run: `./orchestration-echo.py < command.json`

### Full Implementation Checklist

#### 1. Parse NDJSON Commands
- [x] Read stdin line-by-line
- [x] Parse each line as JSON
- [x] Filter for `kind == "command"`
- [x] Extract `action`, `inputs`, `correlation_id`, `task_id`

#### 2. Handle `intake` Action
- [x] Extract `user_instruction` and `discovery_metadata`
- [x] Analyze top N candidates (read files if needed)
- [x] Parse plan structure (sections, tasks, acceptance criteria)
- [x] Derive task objects with IDs, titles, file lists
- [x] Decide: emit `proposed_tasks`, `needs_clarification`, or `plan_conflict`

#### 3. Handle `task_discovery` Action
- [x] Extract `context` (previous candidates, reason for discovery)
- [x] Apply broader criteria (lower score threshold, alternative patterns)
- [x] Derive additional tasks not in first proposal
- [x] Emit `proposed_tasks` with expanded candidates/tasks

#### 4. Emit Events
- [x] Construct valid `event` envelope (see schema)
- [x] Include required fields: `message_id`, `correlation_id`, `task_id`, `from`, `event`, `payload`, `occurred_at`
- [x] Use RFC 3339 UTC timestamps
- [x] Print to stdout with newline + flush

#### 5. Heartbeats
- [x] Emit `heartbeat` messages every 10s while processing
- [x] Include `agent`, `seq`, `status` (starting → busy → ready), `pid`, `uptime_s`, `last_activity_at`

#### 6. Error Handling
- [x] Catch exceptions and emit `error` event with machine-readable `code`
- [x] Timeouts: Respect deadline from command (or default 180s)
- [x] Graceful shutdown on SIGTERM

### LLM Integration Pattern

If using an LLM (Claude, GPT, local model) for orchestration:

**Prompt template**:
```
You are an orchestration agent for lorch, a local task orchestrator.

# Your Role
- Convert natural language instructions into structured task plans
- Analyze file discovery results to identify relevant plan/spec files
- Never edit code or specs directly (read-only role)

# Current Task
User instruction: "{user_instruction}"

# Discovered Plan/Spec Files
{for each candidate in discovery_metadata.candidates:
  - {candidate.path} (score: {candidate.score}, reason: {candidate.reason})
}

# Expected Output
Respond with JSON matching this schema:
{
  "plan_candidates": [{"path": "...", "confidence": 0.0-1.0}],
  "derived_tasks": [{"id": "T-XXX", "title": "...", "files": [...]}],
  "notes": "..."
}

If the instruction is ambiguous, respond with:
{
  "needs_clarification": true,
  "questions": ["...", "..."]
}

If multiple conflicting plans exist, respond with:
{
  "plan_conflict": true,
  "conflicts": [{"paths": [...], "reason": "..."}]
}
```

**Parsing LLM response**:
1. Extract JSON from response (handle markdown fences: ```json...```)
2. Validate structure (required fields present)
3. Map to lorch event format
4. Emit event to stdout

---

## Prompt Template Examples

### Example 1: Simple Intake (Feature Implementation)

**User Instruction**: "Implement password reset feature from PLAN.md"

**Discovery Candidates**:
- `PLAN.md` (score: 0.85)
- `docs/security-plan.md` (score: 0.55)

**Prompt**:
```
Analyze this request and the discovered files to create a task plan.

USER REQUEST:
"Implement password reset feature from PLAN.md"

DISCOVERED FILES:
1. PLAN.md (score: 0.85, reason: filename:plan + root location)
2. docs/security-plan.md (score: 0.55, reason: filename:plan + docs location)

INSTRUCTIONS:
1. Select the most relevant plan file (usually highest score)
2. Read the selected file to extract tasks related to password reset
3. Create task objects with:
   - Unique IDs (T-<number>)
   - Clear titles
   - List of files likely to be affected
4. Return structured JSON response

RESPONSE FORMAT:
{
  "plan_candidates": [{"path": "PLAN.md", "confidence": 0.85}],
  "derived_tasks": [
    {
      "id": "T-200",
      "title": "Implement password reset endpoint",
      "files": ["src/auth/password_reset.go", "tests/auth/password_reset_test.go"]
    }
  ],
  "notes": "Derived from PLAN.md section 4.2"
}
```

### Example 2: Clarification Needed (Ambiguous Instruction)

**User Instruction**: "Add authentication"

**Prompt Response**:
```json
{
  "needs_clarification": true,
  "questions": [
    "What type of authentication? (Options: OAuth 2.0, JWT tokens, session cookies, API keys)",
    "Should this include: a) Login/logout only, b) Login + registration, c) Full auth with password reset and email verification?",
    "Are there existing auth components to integrate with, or start from scratch?"
  ],
  "context": {
    "original_instruction": "Add authentication",
    "ambiguity_reason": "Instruction lacks specificity on auth method and scope"
  }
}
```

### Example 3: Conflict Detection (Multiple Plans)

**User Instruction**: "Implement API endpoints"

**Discovery Candidates**:
- `PLAN.md` (score: 0.80, defines REST API tasks)
- `PLAN-v2.md` (score: 0.78, defines GraphQL API tasks)

**Prompt Response**:
```json
{
  "plan_conflict": true,
  "conflicts": [
    {
      "paths": ["PLAN.md", "PLAN-v2.md"],
      "reason": "PLAN.md specifies REST endpoints (sections 2.1-2.5) while PLAN-v2.md specifies GraphQL schema (sections 3.x). Cannot implement both simultaneously."
    }
  ],
  "suggested_resolution": "Use PLAN.md (REST approach) if maintaining backward compatibility. Use PLAN-v2.md (GraphQL) if greenfield project.",
  "notes": "User should archive one plan file or merge into single canonical plan."
}
```

---

## Testing

### Unit Testing (Isolated Agent)

Test agent logic without lorch:

```bash
# Create test command
$ cat > test-intake-command.json <<'EOF'
{
  "kind": "command",
  "message_id": "cmd-test-1",
  "correlation_id": "corr-test",
  "task_id": "T-0",
  "idempotency_key": "ik:test:0000",
  "to": {"agent_type": "orchestration"},
  "action": "intake",
  "inputs": {
    "user_instruction": "Implement authentication",
    "discovery_metadata": {
      "strategy": "heuristic:v1",
      "candidates": [
        {"path": "PLAN.md", "score": 0.85, "reason": "..."}
      ]
    }
  },
  "expected_outputs": [],
  "version": {"snapshot_id": "snap-test"},
  "deadline": "2099-12-31T23:59:59Z",
  "retry": {"attempt": 0, "max_attempts": 1},
  "priority": 5
}
EOF

# Run agent, capture output
$ ./your-orchestration-agent < test-intake-command.json > output.ndjson

# Validate output
$ cat output.ndjson | jq '.event == "orchestration.proposed_tasks"'
true
```

### Integration Testing (With Lorch)

Use fixtures to test full intake flow:

```bash
# Configure lorch with your agent
$ cat > lorch-test.json <<'EOF'
{
  "agents": {
    "orchestration": {
      "cmd": ["./your-orchestration-agent"],
      "timeouts_s": {"intake": 60}
    }
  }
}
EOF

# Run lorch with test input
$ echo "Implement feature X" | lorch run --config lorch-test.json

# Check outcomes
$ cat state/intake/latest.json | jq '.decision.approved_plan'
"PLAN.md"
```

### Fixture-Based Testing (Deterministic)

For CI/CD, create fixtures that bypass agent logic:

```json
{
  "responses": {
    "intake": {
      "events": [{
        "type": "orchestration.proposed_tasks",
        "payload": {
          "plan_candidates": [{"path": "PLAN.md", "confidence": 0.90}],
          "derived_tasks": [
            {"id": "T-500", "title": "Test task", "files": ["src/test.go"]}
          ]
        }
      }]
    }
  }
}
```

Use with `mockagent -type orchestration -script your-fixture.json`.

---

## Best Practices

### Do
- ✅ **Trust discovery ranking**: Top candidate is usually correct
- ✅ **Read selectively**: Only read top 2-3 files to avoid timeout
- ✅ **Emit heartbeats**: Every 10s while processing
- ✅ **Use stable task IDs**: Convention is `T-<number>` or scoped IDs like `T-parent-sub`
- ✅ **Provide rationale**: Use `notes` field to explain task derivation
- ✅ **Handle clarifications gracefully**: Preserve context across retries
- ✅ **Surface conflicts explicitly**: Don't guess—ask user to resolve

### Don't
- ❌ **Don't edit files**: Orchestration is read-only
- ❌ **Don't ignore discovery**: Candidates are pre-ranked for a reason
- ❌ **Don't be greedy**: Reading all discovered files may cause timeouts
- ❌ **Don't hallucinate tasks**: Derive from actual plan content, not assumptions
- ❌ **Don't skip heartbeats**: Lorch will kill unresponsive agents
- ❌ **Don't auto-resolve conflicts**: Surface to user for decision

### Performance Tips
- Keep intake processing under 60s (default timeout: 180s, but users are impatient)
- Cache file reads if processing multiple candidates
- Use streaming JSON parsing for large plan files (>1MB)
- Parallelize file reads if safe (but stay stateless)

### Security Considerations
- **Path traversal**: Reject discovery candidates with `..` or absolute paths (lorch filters these, but double-check)
- **File size limits**: Be mindful of reading very large files (may cause timeouts)
- **Secrets**: Never log full file contents (may contain tokens/passwords)
- **User input**: Sanitize `user_instruction` if passing to shell commands

---

## Troubleshooting

### Common Issues

**Q: Agent times out after 180s**
A: Processing is too slow. Profile to find bottleneck (likely file I/O). Consider:
- Reading fewer files (top 2-3 only)
- Streaming parsing for large files
- Caching discovery metadata

**Q: Lorch says "agent stopped responding"**
A: Missing heartbeats. Ensure you emit `heartbeat` every 10s while busy.

**Q: User always selects "Ask for more options"**
A: Initial candidates are too narrow. Increase confidence threshold or include more candidates in first proposal.

**Q: Tasks don't match user intent**
A: Instruction parsing is too literal. Use LLM or semantic matching to understand synonyms/context.

**Q: Duplicate task IDs across runs**
A: Task ID generation is not unique. Use timestamp or hash of task content in ID.

**Q: Clarifications loop forever**
A: Too many clarification rounds. Limit to 2-3 rounds, then emit `plan_conflict` or provide best-guess proposal.

---

## References

- **MASTER-SPEC.md**: Full protocol specification (§3, §10.2, §10.4)
- **docs/AGENT-SHIMS.md**: Agent implementation guide and fixture usage
- **testdata/fixtures/orchestration-simple.json**: Example fixture
- **internal/protocol/orchestration.go**: Go types for orchestration protocol
- **internal/discovery/discovery.go**: File discovery implementation

---

**Last Updated**: 2025-10-22 (Phase 2.5)
