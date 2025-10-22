# Mock Agent Script Fixtures

This directory contains JSON script fixtures for the `mockagent` tool. Script fixtures allow you to define deterministic, repeatable agent behaviors for testing.

## Usage

```bash
# Run mockagent with a script fixture
./mockagent -type builder -script testdata/fixtures/simple-success.json

# Use with lorch (configure in lorch.json)
{
  "agents": {
    "builder": {
      "cmd": ["./mockagent", "-type", "builder", "-script", "testdata/fixtures/simple-success.json"]
    }
  }
}
```

---

## Script Format

Scripts are JSON files with the following structure:

```json
{
  "responses": {
    "<action>": {
      "delay_ms": <milliseconds>,
      "error": "<error message>",
      "events": [
        {
          "type": "<event_type>",
          "status": "<status>",
          "payload": { ... },
          "artifacts": [ ... ]
        }
      ]
    }
  }
}
```

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `responses` | object | ✅ | Map of action names to response templates |
| `delay_ms` | integer | ❌ | Milliseconds to wait before responding |
| `error` | string | ❌ | Return this error instead of sending events |
| `events` | array | ❌ | List of events to send (in order) |

### Event Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | ✅ | Event type (e.g., `builder.completed`) |
| `status` | string | ❌ | Event status (e.g., `approved`, `success`) |
| `payload` | object | ❌ | Arbitrary event data |
| `artifacts` | array | ❌ | List of artifact objects |

### Artifact Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `path` | string | ✅ | Artifact file path |
| `sha256` | string | ✅ | SHA256 checksum (format: `sha256:<64-hex>`) |
| `size` | integer | ✅ | File size in bytes |

---

## Available Actions

Scripts can define responses for these actions:

- `implement` - Builder implementing a feature
- `implement_changes` - Builder applying requested changes
- `review` - Reviewer assessing changes
- `update_spec` - Spec maintainer updating documentation
- `intake` - Orchestration agent processing natural language
- `task_discovery` - Orchestration agent discovering tasks

---

## Example Fixtures

### 1. Simple Success (`simple-success.json`)

All actions succeed with minimal delays.

**Use case**: Quick smoke tests, happy path validation

```bash
./mockagent -type builder -script testdata/fixtures/simple-success.json
```

**Responses**:
- `implement` → `builder.completed` (100ms delay, 1 artifact)
- `review` → `review.completed` with `approved` status (50ms delay)
- `update_spec` → `spec.updated` (75ms delay, 1 artifact)

---

### 2. Review Changes Requested (`review-changes-requested.json`)

Builder succeeds, but reviewer requests changes.

**Use case**: Testing iteration loops, change request handling

```bash
./mockagent -type reviewer -script testdata/fixtures/review-changes-requested.json
```

**Responses**:
- `implement` → `builder.completed`
- `review` → `review.completed` with `changes_requested` status and issue list

---

### 3. Build Failure (`build-failure.json`)

Builder sends progress update, then fails with error event.

**Use case**: Testing error handling, failure recovery

```bash
./mockagent -type builder -script testdata/fixtures/build-failure.json
```

**Responses**:
- `implement` → `builder.progress` → `error` (200ms delay total)

---

### 4. Progress Tracking (`progress-tracking.json`)

Builder sends multiple progress updates before completion.

**Use case**: Testing progress UI, long-running tasks, multiple artifacts

```bash
./mockagent -type builder -script testdata/fixtures/progress-tracking.json
```

**Responses**:
- `implement` → 3x `builder.progress` → `builder.completed` (500ms delay, 3 artifacts)

---

## Creating Custom Fixtures

### Step 1: Define Your Scenario

Decide what behavior you want to test:
- Success flow?
- Error cases?
- Multiple iterations?
- Progress tracking?

### Step 2: Write the Script

Create a JSON file in `testdata/fixtures/`:

```json
{
  "responses": {
    "implement": {
      "delay_ms": 300,
      "events": [
        {
          "type": "builder.progress",
          "payload": {
            "stage": "setup",
            "progress": 0.3
          }
        },
        {
          "type": "builder.completed",
          "status": "success",
          "payload": {
            "summary": "Task completed"
          },
          "artifacts": [
            {
              "path": "output.txt",
              "sha256": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
              "size": 1024
            }
          ]
        }
      ]
    }
  }
}
```

### Step 3: Test Your Script

```bash
# Test with a simple command
echo '{"kind":"command","message_id":"cmd-test","correlation_id":"test","task_id":"T-0001","idempotency_key":"ik:0000000000000000000000000000000000000000000000000000000000000000","to":{"agent_type":"builder"},"action":"implement","inputs":{},"expected_outputs":[],"version":{"snapshot_id":"snap-000000000000"},"deadline":"2025-12-31T23:59:59Z","retry":{"attempt":0,"max_attempts":1},"priority":5}' | \
  ./mockagent -type builder -script your-fixture.json -no-heartbeat
```

### Step 4: Validate Output

Check that events match your expectations:
```bash
# Should output NDJSON events to stdout
{"kind":"event","message_id":"evt-...","event":"builder.completed", ...}
```

---

## Event Type Reference

### Builder Events

| Event Type | Description | Typical Status | Terminal? |
|------------|-------------|----------------|-----------|
| `builder.progress` | Implementation progress update | - | ❌ |
| `builder.completed` | Implementation finished | `success` | ✅ |
| `error` | Build/implementation failed | - | ✅ |

### Reviewer Events

| Event Type | Description | Typical Status | Terminal? |
|------------|-------------|----------------|-----------|
| `review.completed` | Review finished | `approved`, `changes_requested` | ✅ |

### Spec Maintainer Events

| Event Type | Description | Typical Status | Terminal? |
|------------|-------------|----------------|-----------|
| `spec.updated` | Spec successfully updated | `success` | ✅ |
| `spec.no_changes_needed` | Spec is already accurate | - | ✅ |
| `spec.changes_requested` | Spec needs clarification | - | ✅ |

### Orchestration Events

| Event Type | Description | Typical Status | Terminal? |
|------------|-------------|----------------|-----------|
| `orchestration.proposed_tasks` | Tasks derived from NL input | - | ✅ |
| `orchestration.needs_clarification` | User input unclear | - | ✅ |
| `orchestration.plan_conflict` | Conflicting task plans | - | ✅ |

---

## Advanced Patterns

### Multiple Responses for Same Action

Scripts define **one response per action**. For testing multiple iterations, use:

1. **Flag-based iteration** (existing mockagent feature):
   ```bash
   # Request changes 2 times before approving
   ./mockagent -type reviewer -review-changes-count 2
   ```

2. **Multiple agents** with different scripts:
   ```json
   // First reviewer (changes requested)
   {"agents": {"reviewer1": {"cmd": ["./mockagent", "-script", "changes.json"]}}}

   // Second reviewer (approved)
   {"agents": {"reviewer2": {"cmd": ["./mockagent", "-script", "approved.json"]}}}
   ```

### Simulating Delays

Use `delay_ms` to simulate realistic agent processing time:

```json
{
  "responses": {
    "implement": {
      "delay_ms": 5000,  // 5 second delay
      "events": [ ... ]
    }
  }
}
```

**Useful for**:
- Testing timeout behavior
- Simulating slow agents
- Progress update testing

### Error Injection

Return an error instead of events:

```json
{
  "responses": {
    "implement": {
      "error": "simulated agent crash"
    }
  }
}
```

**Useful for**:
- Testing error handling
- Crash recovery scenarios
- Retry logic

---

### 4. Orchestration Intake (`orchestration-simple.json`)

Provides canned responses for orchestration `intake` and `task_discovery` actions. Useful when exercising the Phase 2 NL intake shim and fixture-based smoke tests.

```bash
CLAUDE_ROLE=orchestration \
CLAUDE_FIXTURE=testdata/fixtures/orchestration-simple.json \
./claude-fixture --no-heartbeat
```

**Responses**:
- `intake` → `orchestration.proposed_tasks` with two candidate plans and derived tasks
- `task_discovery` → `orchestration.proposed_tasks` follow-up with notes

Combine with the `claude-agent` shim by setting `CLAUDE_CLI=./claude-fixture` and passing the fixture path via `CLAUDE_FIXTURE` (or `--fixture`).

#### Creating Custom Orchestration Fixtures

**1. Needs Clarification Flow**

Test ambiguous instructions that require user clarification:

```json
{
  "responses": {
    "intake": {
      "events": [{
        "type": "orchestration.needs_clarification",
        "payload": {
          "questions": [
            "Which authentication method? (OAuth, JWT, session cookies)",
            "Should this include password reset flows?"
          ],
          "context": {
            "original_instruction": "Add authentication",
            "ambiguity_reason": "Instruction lacks specificity"
          }
        }
      }]
    }
  }
}
```

Lorch will prompt user for answers and re-invoke `intake` with updated inputs (same idempotency key).

**2. Plan Conflict Flow**

Test detection of conflicting plan files:

```json
{
  "responses": {
    "intake": {
      "events": [{
        "type": "orchestration.plan_conflict",
        "payload": {
          "conflicts": [
            {
              "paths": ["PLAN.md", "PLAN-v2.md"],
              "reason": "Both define conflicting task T-0042"
            }
          ],
          "suggested_resolution": "Use PLAN.md (most recent)"
        }
      }]
    }
  }
}
```

Lorch surfaces conflict to user for resolution (choose plan, abort, or provide guidance).

**3. Task Discovery (More Options)**

Test "Ask for more options" flow with expanded candidates:

```json
{
  "responses": {
    "intake": {
      "events": [{
        "type": "orchestration.proposed_tasks",
        "payload": {
          "plan_candidates": [{"path": "PLAN.md", "confidence": 0.82}],
          "derived_tasks": [{"id": "T-100", "title": "Initial task", "files": []}]
        }
      }]
    },
    "task_discovery": {
      "events": [{
        "type": "orchestration.proposed_tasks",
        "payload": {
          "plan_candidates": [
            {"path": "PLAN.md", "confidence": 0.82},
            {"path": "docs/design.md", "confidence": 0.65}
          ],
          "derived_tasks": [
            {"id": "T-100", "title": "Initial task", "files": []},
            {"id": "T-101", "title": "Additional task", "files": []}
          ],
          "notes": "Expanded search with broader criteria"
        }
      }]
    }
  }
}
```

User selects "m" in approval menu → lorch invokes `task_discovery` → fixture returns expanded set.

#### Orchestration Event Requirements

Fixtures must match expected schemas:

**`orchestration.proposed_tasks`**:
- `plan_candidates`: Array with ≥1 entry, each has `path` (string) and `confidence` (0.0-1.0)
- `derived_tasks`: Array (may be empty), each has `id` (string) and `title` (string)
- `files` field in tasks is optional but recommended

**`orchestration.needs_clarification`**:
- `questions`: Non-empty string array
- `context`: Object (may be empty)

**`orchestration.plan_conflict`**:
- `conflicts`: Array with ≥1 entry, each has `paths` (array) and `reason` (string)

See `docs/ORCHESTRATION.md` for complete protocol documentation.

---

## Integration with Tests

### Go Integration Tests

```go
func TestScriptedAgent(t *testing.T) {
    // Build mockagent
    mockAgentPath := buildMockAgent(t)

    // Create supervisor with script
    supervisor := supervisor.NewAgentSupervisor(
        protocol.AgentTypeBuilder,
        []string{
            mockAgentPath,
            "-type", "builder",
            "-script", "testdata/fixtures/simple-success.json",
            "-no-heartbeat",
        },
        map[string]string{},
        logger,
    )

    supervisor.Start(ctx)
    defer supervisor.Stop(ctx)

    // Send command, verify response...
}
```

### Shell Script Testing

```bash
#!/bin/bash
# test-fixture.sh

echo "Testing simple-success fixture..."
echo '{"kind":"command",...}' | \
  ./mockagent -type builder -script testdata/fixtures/simple-success.json -no-heartbeat | \
  jq -e '.event == "builder.completed"'

if [ $? -eq 0 ]; then
    echo "✅ Test passed"
else
    echo "❌ Test failed"
    exit 1
fi
```

---

## Troubleshooting

### Script Not Loading

**Error**: `failed to load script: no such file or directory`

**Solution**: Use absolute path or path relative to CWD:
```bash
./mockagent -script $(pwd)/testdata/fixtures/simple-success.json
```

### Invalid JSON

**Error**: `failed to parse script JSON: ...`

**Solution**: Validate JSON syntax:
```bash
jq . testdata/fixtures/your-fixture.json
```

### Events Not Sending

**Check**:
1. Is action name correct? (must match exactly: `implement`, not `Implement`)
2. Is event type valid? (see Event Type Reference)
3. Are artifacts formatted correctly? (sha256 must start with `sha256:`)

**Debug**:
```bash
# Add verbose logging (stderr)
./mockagent -script fixture.json 2>&1 | grep "scripted"
```

---

## Best Practices

1. **Name fixtures descriptively** - `review-changes-requested.json` is better than `fixture1.json`
2. **Keep fixtures focused** - One scenario per fixture
3. **Use realistic checksums** - Generate valid SHA256 hashes or use placeholder pattern
4. **Document fixture purpose** - Add README entry for each fixture
5. **Version control fixtures** - Commit fixtures to repo for reproducible tests
6. **Test fixtures independently** - Validate each fixture works before integration

---

## Future Enhancements

Potential improvements:

- **Conditional responses** - Different events based on input parameters
- **Sequence tracking** - Different response on 2nd call to same action
- **Template variables** - Interpolate command fields into responses
- **External data** - Reference files for large payloads
- **Validation** - JSON schema validation for fixture format

---

## References

- **mockagent source**: `cmd/mockagent/main.go`
- **Protocol types**: `internal/protocol/types.go`
- **Event schemas**: `schemas/v1/event.v1.json`
- **Integration tests**: `internal/scheduler/integration_test.go`
