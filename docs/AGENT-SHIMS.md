# Agent Shims

The orchestrator treats every agent as a thin CLI process that speaks NDJSON over stdin/stdout. This document captures the practical details for wiring those shims locally (Claude, mock agents, or your own tools).

## Executables & Roles

| Role             | Purpose                                               | Default command (lorch.json)                             |
|------------------|-------------------------------------------------------|----------------------------------------------------------|
| builder          | Applies code/test changes and reports artefacts/tests | `./mockagent -type builder` (mock mode)                  |
| reviewer         | Reviews changes and emits `review.completed`          | `./mockagent -type reviewer`                             |
| spec_maintainer  | Verifies SPEC.md coverage / updates allowed sections  | `./mockagent -type spec_maintainer`                      |
| orchestration    | (Phase 2+) task intake / planning                     | `./claude-agent --role orchestration --` (Phase 2 shim)  |

Swap in a different model by replacing the `cmd` array in `lorch.json` (e.g. `claude`, `openai`, `python script.py`). Lorch passes no hidden arguments; everything after `cmd[0]` is under your control.

## Environment Variables

Set per-agent env in the config file:

- `CLAUDE_AGENT_ROLE` – free-form hint for prompt templating. The mock agent ignores it; real shims can embed role-specific behaviour.
- `WORKSPACE_ROOT`, `LOG_LEVEL`, tokens, etc. – add whatever your shim needs. They are exported exactly as listed.

### Claude Agent Shim (`cmd/claude-agent`)

The Phase 2 shim wraps the real Claude CLI (or any compatible binary) and injects environment variables. Example usage:

```bash
./claude-agent \
  --role orchestration \
  --workspace /Users/me/lorch \
  --log-level debug \
  -- --model claude-3-5-sonnet
```

- `CLAUDE_ROLE`, `CLAUDE_WORKSPACE`, and `CLAUDE_LOG_LEVEL` are exported automatically.
- Additional args after `--` are passed directly to the underlying CLI.
- Use `--fixture path/to/script.jsonl` to set `CLAUDE_FIXTURE`, enabling deterministic playback for tests and smoke runs.
- Override the binary with `--bin /custom/path` or set `CLAUDE_CLI` in your environment.

## Orchestration Agent (Natural Language Intake)

The **orchestration** agent is a specialized agent introduced in Phase 2 that translates natural-language instructions into concrete task plans. Unlike other agents, it never edits code or spec files—it only produces planning artifacts.

### Purpose & Scope

When you run `lorch` without a `--task` flag, the orchestration agent:
1. Receives your natural-language instruction (e.g., "Implement authentication from PLAN.md")
2. Analyzes file discovery results provided by lorch
3. Proposes candidate plan files and derived task objects
4. Handles clarifications if your instruction is ambiguous
5. Surfaces conflicts if multiple incompatible plans are found

### Actions

| Action | Purpose | Inputs | Output Events |
|--------|---------|---------|---------------|
| `intake` | Initial NL instruction → task plan conversion | `user_instruction`, `discovery_metadata` | `proposed_tasks`, `needs_clarification`, `plan_conflict` |
| `task_discovery` | Incremental task expansion mid-run | `user_instruction`, `context`, `discovery_metadata` | `proposed_tasks`, `needs_clarification` |

### Event Types

**`orchestration.proposed_tasks`**
- Returns candidate plan files (with confidence scores) and derived task objects
- Payload structure:
  ```json
  {
    "plan_candidates": [
      {"path": "PLAN.md", "confidence": 0.85},
      {"path": "docs/roadmap.md", "confidence": 0.61}
    ],
    "derived_tasks": [
      {
        "id": "T-100",
        "title": "Implement user authentication",
        "files": ["src/auth.go", "tests/auth_test.go"]
      }
    ],
    "notes": "Optional context or rationale"
  }
  ```

**`orchestration.needs_clarification`**
- Emitted when user instruction is ambiguous or incomplete
- Lorch will prompt the user for answers and retry with the same idempotency key
- Payload structure:
  ```json
  {
    "questions": [
      "Which authentication method? (OAuth, JWT, or session-based)",
      "Should this include password reset flows?"
    ],
    "context": "Previous user instruction preserved here"
  }
  ```

**`orchestration.plan_conflict`**
- Emitted when multiple incompatible plan files are detected
- Lorch surfaces the conflict to the user for resolution
- Payload structure:
  ```json
  {
    "conflicts": [
      {
        "paths": ["PLAN.md", "PLAN-v2.md"],
        "reason": "Both define conflicting task T-0042"
      }
    ],
    "suggested_resolution": "Use PLAN.md (most recent)"
  }
  ```

### Integration with File Discovery

Lorch performs deterministic file discovery **before** invoking the orchestration agent. Discovery metadata is injected into orchestration commands via the `discovery_metadata` input field:

```json
{
  "strategy": "heuristic:v1",
  "search_paths": [".", "docs", "specs", "plans"],
  "candidates": [
    {"path": "PLAN.md", "score": 0.85, "reason": "filename match + root location"},
    {"path": "docs/plan_v2.md", "score": 0.61, "reason": "filename match in docs/"}
  ],
  "total_files_scanned": 47
}
```

The agent can use this metadata to:
- Rank candidates by score (already sorted by lorch)
- Explain why certain files were selected
- Request additional discovery via `task_discovery` action

See "File Discovery Behavior" section below for scoring algorithm details.

### Configuration Example

```json
{
  "agents": {
    "orchestration": {
      "enabled": true,
      "cmd": ["./claude-agent", "--role", "orchestration", "--workspace", ".", "--"],
      "timeouts_s": {
        "intake": 180,
        "task_discovery": 180
      },
      "env": {
        "CLAUDE_ROLE": "orchestration",
        "CLAUDE_WORKSPACE": "."
      }
    }
  }
}
```

For real LLM usage, replace `./claude-agent` with your preferred CLI and pass model/auth args after `--`.

### Testing with Fixtures

To test orchestration flows without an LLM, use `claude-fixture`:

```bash
# Create a fixture (see testdata/fixtures/orchestration-simple.json)
$ cat > my-orchestration-fixture.json <<'EOF'
{
  "responses": {
    "intake": {
      "events": [{
        "type": "orchestration.proposed_tasks",
        "payload": {
          "plan_candidates": [{"path": "PLAN.md", "confidence": 0.82}],
          "derived_tasks": [{
            "id": "T-100",
            "title": "Implement feature X",
            "files": ["src/x.go"]
          }]
        }
      }]
    }
  }
}
EOF

# Run claude-agent with fixture mode
$ CLAUDE_FIXTURE=my-orchestration-fixture.json \
  ./claude-agent --role orchestration --workspace . -- --no-heartbeat
```

Or configure in `lorch.json`:

```json
{
  "agents": {
    "orchestration": {
      "cmd": ["./claude-agent", "--role", "orchestration", "--fixture", "testdata/fixtures/orchestration-simple.json", "--"],
      "env": {
        "CLAUDE_CLI": "./claude-fixture"
      }
    }
  }
}
```

## File Discovery Behavior

Lorch performs deterministic file discovery when no `--task` is provided. This happens **before** the orchestration agent is invoked, ensuring consistent results across runs with the same workspace snapshot.

### Search Algorithm

**Search Paths** (in order):
1. `.` (workspace root)
2. `docs/`
3. `specs/`
4. `plans/`

**Exclusions**:
- Hidden files/directories (starting with `.`)
- `node_modules/`, `.git/`, `vendor/`, `dist/`, `build/`

**Scoring Heuristics** (`heuristic:v1`):
- **Base score**: 0.5
- **Filename tokens** (case-insensitive, mutually exclusive):
  - "plan" in filename: +0.25
  - "spec" in filename: +0.20
  - "proposal" in filename: +0.15
- **Directory location**: +0.05 if located under `docs/`, `plans/`, or `specs/` (any level)
- **Depth penalty**: -0.04 per directory level beyond workspace root
- **Heading matches**: +0.05 per heading containing "plan", "spec", or "proposal"

**Determinism Guarantees**:
- Files are traversed in lexicographic order (sorted paths)
- Scores are stable (same file → same score)
- Ranking is stable (score DESC, then path ASC for ties)
- Output includes `strategy: "heuristic:v1"` version tag for future evolution

### Example Discovery Output

```json
{
  "root": ".",
  "strategy": "heuristic:v1",
  "search_paths": [".", "docs", "specs", "plans"],
  "generated_at": "2025-10-22T18:30:00Z",
  "candidates": [
    {
      "path": "PLAN.md",
      "score": 0.80,
      "reason": "filename contains 'plan', heading matches 'plan'"
    },
    {
      "path": "docs/plan_v2.md",
      "score": 0.72,
      "reason": "filename contains 'plan', located under 'docs', depth penalty -0.04"
    },
    {
      "path": "specs/API-SPEC.md",
      "score": 0.71,
      "reason": "filename contains 'spec', located under 'specs', depth penalty -0.04"
    }
  ]
}
```

### Integration Notes

- Discovery is **snapshot-coupled**: results are tied to workspace snapshot ID for determinism
- Discovery metadata is **read-only** for orchestration agents (agents cannot trigger rediscovery)
- Users can request "more options" via `task_discovery` action to expand the candidate set

## Mock Agent (cmd/mockagent)

The mock agent is invaluable for deterministic testing:

```bash
./mockagent -type builder -script testdata/fixtures/simple-success.json
```

Flags of note:

- `-type {builder|reviewer|spec_maintainer|orchestration}` – controls emitted message schema.
- `-script <path>` – JSON fixture describing command → event sequences.
- `-no-heartbeat` – disables periodic heartbeats (useful for tight loop tests).
- `-review-changes-count` / `-spec-changes-count` – force a number of change-request iterations before approving.

Script format is documented in `testdata/fixtures/README.md`; the new smoke harness consumes the same files.

### Orchestration Fixtures

For testing natural language intake flows, use orchestration-specific fixtures:

```bash
./mockagent -type orchestration -script testdata/fixtures/orchestration-simple.json
```

Example fixture structure:

```json
{
  "responses": {
    "intake": {
      "events": [{
        "type": "orchestration.proposed_tasks",
        "payload": {
          "plan_candidates": [
            {"path": "PLAN.md", "confidence": 0.82}
          ],
          "derived_tasks": [
            {"id": "T-100", "title": "Mock task", "files": ["src/example.go"]}
          ]
        }
      }]
    },
    "task_discovery": {
      "events": [{
        "type": "orchestration.proposed_tasks",
        "payload": {
          "plan_candidates": [{"path": "PLAN.md", "confidence": 0.85}],
          "derived_tasks": [
            {"id": "T-100-2", "title": "Follow-up task", "files": ["src/example_followup.go"]}
          ],
          "notes": "Additional context from scripted discovery"
        }
      }]
    }
  }
}
```

See `testdata/fixtures/orchestration-simple.json` for a complete example.

## Building Shims

Use `pkg/testharness.BuildBinaries` (or `go build ./cmd/{lorch,mockagent}`) to bake static binaries for CI or offline environments. The Phase 1.5 release command (`lorch release`) cross-compiles these combinations automatically under `dist/<os>-<arch>/`.

## Smoke Harness Integration

`pkg/testharness.RunSmoke` wires compiled shims, a temporary workspace, and fixture scripts into a one-shot run:

```go
ctx := context.Background()
lorchBin, mockBin, _ := testharness.BuildBinaries(ctx, repoRoot, "./bin")
result, _ := testharness.RunSmoke(ctx, testharness.SmokeOptions{
    Scenario:        testharness.ScenarioSimpleSuccess,
    LorchBinary:     lorchBin,
    MockAgentBinary: mockBin,
})
```

The harness captures stdout/stderr, the generated `runstate`, and key workspace artefacts (events, receipts) for further assertions.

## Testing Without LLMs

For development, CI/CD, and offline testing, lorch supports fully deterministic operation using fixtures instead of real LLM calls.

### Why Fixture-Based Testing?

- **Determinism**: Same inputs → same outputs, every time
- **Speed**: No network calls, no API rate limits
- **Cost**: Zero LLM API costs during development
- **Reproducibility**: Tests pass identically in CI and locally
- **Offline**: Work without internet connection
- **Coverage**: Test error cases (timeouts, conflicts) that are hard to trigger with real LLMs

### Approaches

**1. Mock Agent (Pure Mock)**

Use `mockagent` directly for all roles:

```json
{
  "agents": {
    "builder": {"cmd": ["./mockagent", "-type", "builder", "-script", "testdata/fixtures/simple-success.json"]},
    "reviewer": {"cmd": ["./mockagent", "-type", "reviewer", "-script", "testdata/fixtures/simple-success.json"]},
    "spec_maintainer": {"cmd": ["./mockagent", "-type", "spec_maintainer", "-script", "testdata/fixtures/simple-success.json"]},
    "orchestration": {"cmd": ["./mockagent", "-type", "orchestration", "-script", "testdata/fixtures/orchestration-simple.json"]}
  }
}
```

**2. Claude-Fixture (Mock via Shim)**

Use `claude-agent` + `claude-fixture` to test the shim layer:

```json
{
  "agents": {
    "orchestration": {
      "cmd": ["./claude-agent", "--role", "orchestration", "--fixture", "testdata/fixtures/orchestration-simple.json", "--"],
      "env": {"CLAUDE_CLI": "./claude-fixture"}
    }
  }
}
```

This approach tests:
- Environment variable propagation
- Argument passing through `--`
- Shim subprocess management
- Fixture file loading

**3. Hybrid (Real + Mock)**

Use real LLM for orchestration, mock for execution (faster iteration):

```json
{
  "agents": {
    "orchestration": {"cmd": ["claude"], "env": {"CLAUDE_ROLE": "orchestration"}},
    "builder": {"cmd": ["./mockagent", "-type", "builder", "-script", "testdata/fixtures/simple-success.json"]},
    "reviewer": {"cmd": ["./mockagent", "-type", "reviewer"]},
    "spec_maintainer": {"cmd": ["./mockagent", "-type", "spec_maintainer"]}
  }
}
```

### Writing Custom Fixtures

1. **Identify the scenario** you want to test (happy path, error, conflict, etc.)
2. **Create a JSON fixture** in `testdata/fixtures/`:
   ```json
   {
     "responses": {
       "intake": {
         "events": [{
           "type": "orchestration.proposed_tasks",
           "payload": {
             "plan_candidates": [{"path": "PLAN.md", "confidence": 0.90}],
             "derived_tasks": [{"id": "T-200", "title": "Custom task", "files": ["src/custom.go"]}]
           }
         }]
       }
     }
   }
   ```
3. **Test it manually**:
   ```bash
   echo '{"kind":"command",...}' | ./mockagent -type orchestration -script your-fixture.json
   ```
4. **Wire it into lorch.json** or integration tests

See `testdata/fixtures/README.md` for complete fixture format documentation.

### Integration Test Pattern

```go
func TestIntakeFlowWithFixture(t *testing.T) {
    // Build binaries
    lorchBin, mockBin, fixtureBin, _ := testharness.BuildBinaries(ctx, repoRoot, "./bin")

    // Create fixture-based config
    cfg := createFixtureConfig(mockBin, fixtureBin, "orchestration-simple.json")

    // Run lorch with deterministic inputs
    result := testharness.RunSmoke(ctx, testharness.SmokeOptions{
        Scenario: testharness.ScenarioWithIntake,
        Config: cfg,
        Input: "Implement feature X\n1\nall\n", // Simulated user input
    })

    // Assert expected outcomes
    assert.NoError(t, result.Error)
    assert.Contains(t, result.Stdout, "Executing 1 task")
}
```

### Fixture Development Workflow

1. **Run with real LLM** to observe actual event sequences
2. **Capture events** from `/events/<run>-intake.ndjson`
3. **Extract relevant events** into fixture format
4. **Test fixture** in isolation with mockagent
5. **Iterate** until behavior matches real agent
6. **Commit fixture** to version control

This workflow ensures fixtures stay aligned with actual protocol behavior while providing deterministic testing.

## Troubleshooting Tips

- Heartbeats are required in production, but you can disable them for fixtures to keep logs quieter.
- If you see `operation not permitted` when spawning Go binaries under sandboxed shells, rerun with `GOCACHE`/`GOMODCACHE` pointing to writable locations (e.g. workspace-local directories).
- A missing script entry for a command (`implement_changes`, `update_spec`, …) will leave lorch waiting indefinitely—double-check fixture coverage when simulating change-request loops.
