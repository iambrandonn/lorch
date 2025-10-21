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

## Troubleshooting Tips

- Heartbeats are required in production, but you can disable them for fixtures to keep logs quieter.
- If you see `operation not permitted` when spawning Go binaries under sandboxed shells, rerun with `GOCACHE`/`GOMODCACHE` pointing to writable locations (e.g. workspace-local directories).
- A missing script entry for a command (`implement_changes`, `update_spec`, …) will leave lorch waiting indefinitely—double-check fixture coverage when simulating change-request loops.
