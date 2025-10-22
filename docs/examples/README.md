# Example lorch.json Configurations

This directory contains sample `lorch.json` configurations for different usage scenarios.

## Files

### `lorch-with-real-claude.json`

**Use case**: Production usage with real Claude CLI

**Agents**: All agents (builder, reviewer, spec_maintainer, orchestration) use actual Claude API

**Requirements**:
- `claude` command available on `$PATH`
- Valid Claude authentication (API key or session)
- Internet connection

**Usage**:
```bash
cp docs/examples/lorch-with-real-claude.json lorch.json
lorch run
# or
lorch run --task T-0042
```

**Notes**:
- Costs LLM API credits per run
- Non-deterministic (responses vary)
- Slowest execution (network latency)
- Best for: Real-world usage, production runs

---

### `lorch-with-fixtures.json`

**Use case**: Deterministic testing, CI/CD, offline development

**Agents**: All agents use `mockagent` with pre-scripted fixtures

**Requirements**:
- `mockagent` binary (build with `go build ./cmd/mockagent`)
- Fixture files in `testdata/fixtures/`

**Usage**:
```bash
# Build mockagent first
go build -o mockagent ./cmd/mockagent

# Run with fixtures
lorch run --config docs/examples/lorch-with-fixtures.json
```

**Notes**:
- Zero API costs
- Fully deterministic (same inputs → same outputs)
- Fastest execution (no network calls)
- Works offline
- Best for: CI/CD, unit tests, development without LLM access

**Fixtures used**:
- Builder/Reviewer/Spec-Maintainer: `testdata/fixtures/simple-success.json`
- Orchestration: `testdata/fixtures/orchestration-simple.json`

**Customizing fixtures**:
Edit the fixture files or create new ones:
```bash
cp testdata/fixtures/orchestration-simple.json my-custom-fixture.json
# Edit my-custom-fixture.json with your scenario
# Update lorch.json to reference new fixture
```

See `testdata/fixtures/README.md` for fixture format documentation.

---

### `lorch-hybrid-real-orchestration.json`

**Use case**: Developing/testing natural language intake without slow execution loops

**Agents**:
- **Orchestration**: Real Claude via `claude-agent` shim
- **Builder/Reviewer/Spec-Maintainer**: Fixtures (fast mock responses)

**Requirements**:
- `claude-agent` binary (build with `go build ./cmd/claude-agent`)
- `mockagent` binary
- `claude` command for orchestration agent
- Fixture files

**Usage**:
```bash
# Build binaries
go build -o claude-agent ./cmd/claude-agent
go build -o mockagent ./cmd/mockagent

# Run with hybrid config
lorch run --config docs/examples/lorch-hybrid-real-orchestration.json
```

**Notes**:
- Moderate API costs (only orchestration uses LLM)
- Orchestration responses vary, execution is deterministic
- Faster than full LLM (execution mocked)
- Best for: Iterating on NL intake prompts, testing approval flows

**Use cases**:
- Testing orchestration agent prompt templates
- Validating file discovery integration
- Developing clarification/conflict flows
- Quick iteration without waiting for full task execution

---

## Configuration Patterns

### Switching Between Modes

**Development → Testing**:
```bash
# Start with fixtures for fast iteration
lorch run --config docs/examples/lorch-with-fixtures.json

# Switch to real LLM once logic is solid
cp docs/examples/lorch-with-real-claude.json lorch.json
lorch run
```

**Testing NL Intake**:
```bash
# Use hybrid for quick intake testing
lorch run --config docs/examples/lorch-hybrid-real-orchestration.json
```

### Per-Agent Fixture Customization

To test specific scenarios (e.g., reviewer requests changes):

```json
{
  "agents": {
    "builder": {
      "cmd": ["./mockagent", "-type", "builder", "-script", "testdata/fixtures/simple-success.json"]
    },
    "reviewer": {
      "cmd": ["./mockagent", "-type", "reviewer", "-script", "testdata/fixtures/review-changes-requested.json"]
    }
  }
}
```

Available fixtures:
- `simple-success.json` – Happy path (all agents succeed)
- `review-changes-requested.json` – Reviewer requests changes (iteration loop)
- `spec-changes-requested.json` – Spec-maintainer requests changes
- `build-failure.json` – Builder fails with error
- `progress-tracking.json` – Builder emits progress updates
- `orchestration-simple.json` – Orchestration returns candidates + tasks

### Environment Variable Overrides

Override agent binary at runtime without editing config:

```bash
# Use custom Claude binary
CLAUDE_CLI=/path/to/custom-claude lorch run --config lorch-with-real-claude.json

# Use fixture mode for orchestration
CLAUDE_CLI=./claude-fixture \
CLAUDE_FIXTURE=testdata/fixtures/orchestration-simple.json \
lorch run --config lorch-hybrid-real-orchestration.json
```

---

## Creating Custom Configurations

### 1. Copy Base Template

```bash
cp docs/examples/lorch-with-fixtures.json my-custom-config.json
```

### 2. Edit Agent Commands

```json
{
  "agents": {
    "orchestration": {
      "cmd": ["./my-custom-orchestration-agent", "--verbose"],
      "env": {
        "MY_CUSTOM_VAR": "value"
      }
    }
  }
}
```

### 3. Test Configuration

```bash
lorch run --config my-custom-config.json --task T-test
```

### 4. Validate Against Schema

```bash
# Planned for Phase 3
lorch validate --config my-custom-config.json
```

---

## Troubleshooting

### "command not found: claude"

**Problem**: `claude` command not on `$PATH`

**Solutions**:
- Install Claude CLI: Follow [Claude documentation](https://docs.anthropic.com/claude/docs)
- Use absolute path: `"cmd": ["/usr/local/bin/claude"]`
- Switch to fixture mode for testing

### "command not found: ./mockagent"

**Problem**: Binary not built

**Solution**:
```bash
go build -o mockagent ./cmd/mockagent
```

### "failed to load script: no such file or directory"

**Problem**: Fixture file path is incorrect

**Solution**:
- Use absolute path: `"script": "/full/path/to/fixture.json"`
- Or relative to workspace root: `"script": "testdata/fixtures/simple-success.json"`
- Verify file exists: `ls -la testdata/fixtures/`

### "agent timeout after 180s"

**Problem**: Real LLM agent is slow or unresponsive

**Solutions**:
- Increase timeout: `"timeouts_s": {"intake": 300}`
- Use fixtures for faster execution
- Check network connection

### Orchestration returns empty tasks

**Problem**: Fixture doesn't match expected format

**Solution**:
Verify fixture structure:
```json
{
  "responses": {
    "intake": {
      "events": [{
        "type": "orchestration.proposed_tasks",
        "payload": {
          "plan_candidates": [...],
          "derived_tasks": [...]
        }
      }]
    }
  }
}
```

---

## Additional Resources

- **Main documentation**: `../AGENT-SHIMS.md`
- **Orchestration guide**: `../ORCHESTRATION.md`
- **Fixture format**: `../../testdata/fixtures/README.md`
- **MASTER-SPEC**: `../../MASTER-SPEC.md` (protocol specification)

---

**Last Updated**: 2025-10-22 (Phase 2.5)
