# lorch JSON Schemas

This directory contains JSON Schema definitions for all `lorch` data structures. These schemas enable validation, documentation, and tooling integration.

## Schema Version: v1

All schemas follow [JSON Schema Draft 07](https://json-schema.org/draft-07/schema) specification.

---

## Available Schemas

### Protocol Messages

These schemas define the NDJSON protocol messages exchanged between `lorch` and agents over stdio:

| Schema | File | Description |
|--------|------|-------------|
| **Command** | `v1/command.v1.json` | Commands sent from lorch to agents |
| **Event** | `v1/event.v1.json` | Events sent from agents to lorch |
| **Heartbeat** | `v1/heartbeat.v1.json` | Liveness heartbeats from agents |
| **Log** | `v1/log.v1.json` | Diagnostic log messages from agents |

### Persistence Formats

These schemas define file formats used for state persistence:

| Schema | File | Description |
|--------|------|-------------|
| **Snapshot Manifest** | `v1/snapshot-manifest.v1.json` | Workspace state snapshots (`/snapshots/`) |
| **Receipt** | `v1/receipt.v1.json` | Task completion records (`/receipts/`) |
| **Run State** | `v1/run-state.v1.json` | Run tracking state (`/state/run.json`) |

---

## Usage

### Validating Files

Use a JSON Schema validator to check file compliance:

```bash
# Using ajv-cli (npm install -g ajv-cli)
ajv validate -s schemas/v1/run-state.v1.json -d state/run.json

# Using Python jsonschema
python3 -c "
import json, jsonschema
schema = json.load(open('schemas/v1/command.v1.json'))
data = json.load(open('events/run-20251020-140818-4f9f29a1.ndjson'))
jsonschema.validate(data, schema)
"
```

### Editor Integration

Many editors support JSON Schema for autocomplete and validation:

**VSCode** - Add to `.vscode/settings.json`:
```json
{
  "json.schemas": [
    {
      "fileMatch": ["state/run.json"],
      "url": "./schemas/v1/run-state.v1.json"
    },
    {
      "fileMatch": ["snapshots/*.manifest.json"],
      "url": "./schemas/v1/snapshot-manifest.v1.json"
    },
    {
      "fileMatch": ["receipts/**/*.json"],
      "url": "./schemas/v1/receipt.v1.json"
    }
  ]
}
```

**JetBrains IDEs** - Add to project settings under "Languages & Frameworks > Schemas and DTDs > JSON Schema Mappings"

---

## Schema Details

### Command Schema (`command.v1.json`)

Defines commands sent from lorch orchestrator to agents.

**Key Fields**:
- `idempotency_key` - Deterministic key for crash recovery (format: `ik:<64-hex>`)
- `version.snapshot_id` - Pins workspace version (format: `snap-<12-hex>`)
- `action` - Command type: `implement`, `review`, `update_spec`, etc.
- `inputs` - Action-specific parameters
- `expected_outputs` - Artifacts the agent should produce

**Example**:
```json
{
  "kind": "command",
  "message_id": "cmd-a1b2c3d4",
  "correlation_id": "corr-001",
  "task_id": "T-0042",
  "idempotency_key": "ik:7a3f8e9c2d1b5a6e4f7c8d9e0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c",
  "to": {
    "agent_type": "builder"
  },
  "action": "implement",
  "inputs": {
    "goal": "Add user authentication"
  },
  "expected_outputs": [
    {"path": "src/auth.go", "required": true}
  ],
  "version": {
    "snapshot_id": "snap-d0ab7e60b764"
  },
  "deadline": "2025-10-20T15:00:00Z",
  "retry": {
    "attempt": 0,
    "max_attempts": 3
  },
  "priority": 5
}
```

---

### Event Schema (`event.v1.json`)

Defines events sent from agents to lorch orchestrator.

**Key Fields**:
- `event` - Event type (e.g., `builder.completed`, `review.completed`)
- `artifacts` - List of produced files with checksums
- `payload` - Event-specific data
- `correlation_id` - Links to originating command

**Example**:
```json
{
  "kind": "event",
  "message_id": "evt-e5f6g7h8",
  "correlation_id": "corr-001",
  "task_id": "T-0042",
  "from": {
    "agent_type": "builder"
  },
  "event": "builder.completed",
  "payload": {
    "summary": "Implemented authentication"
  },
  "artifacts": [
    {
      "path": "src/auth.go",
      "sha256": "sha256:abc123...",
      "size": 2048
    }
  ],
  "occurred_at": "2025-10-20T14:30:00Z"
}
```

---

### Heartbeat Schema (`heartbeat.v1.json`)

Defines liveness heartbeats from agents.

**Key Fields**:
- `seq` - Monotonic sequence number
- `status` - Health status: `starting`, `ready`, `busy`, `stopping`, `backoff`
- `pid` - Process ID
- `stats` - Optional resource usage (CPU, memory)

**Example**:
```json
{
  "kind": "heartbeat",
  "agent": {
    "agent_type": "builder"
  },
  "seq": 42,
  "status": "busy",
  "pid": 12345,
  "uptime_s": 120.5,
  "last_activity_at": "2025-10-20T14:32:00Z",
  "stats": {
    "cpu_pct": 45.2,
    "rss_bytes": 52428800
  },
  "task_id": "T-0042"
}
```

---

### Snapshot Manifest Schema (`snapshot-manifest.v1.json`)

Defines workspace snapshots that pin file versions.

**Key Fields**:
- `snapshot_id` - Deterministic ID (first 12 hex chars of content hash)
- `files` - List of tracked files with checksums
- Tracks: `specs/`, `src/`, `tests/`, `docs/`
- Excludes: `.git`, `node_modules`, `state/`, etc.

**Example**:
```json
{
  "snapshot_id": "snap-d0ab7e60b764",
  "created_at": "2025-10-20T14:08:18Z",
  "workspace_root": "./",
  "files": [
    {
      "path": "src/main.go",
      "sha256": "sha256:abc123...",
      "size": 1024,
      "mtime": "2025-10-20T14:00:00Z"
    },
    {
      "path": "specs/SPEC.md",
      "sha256": "sha256:def456...",
      "size": 2048,
      "mtime": "2025-10-20T13:00:00Z"
    }
  ]
}
```

---

### Receipt Schema (`receipt.v1.json`)

Defines receipts recording completed work.

**Key Fields**:
- `idempotency_key` - IK of command that produced this work
- `artifacts` - Files produced with checksums
- `events` - Event message IDs associated with this work
- Stored at: `/receipts/<task>/step-<n>.json`

**Example**:
```json
{
  "task_id": "T-0042",
  "step": 1,
  "action": "implement",
  "idempotency_key": "ik:7a3f8e9c2d1b...",
  "snapshot_id": "snap-d0ab7e60b764",
  "command_message_id": "cmd-a1b2c3d4",
  "correlation_id": "corr-001",
  "artifacts": [
    {
      "path": "src/auth.go",
      "sha256": "sha256:abc123...",
      "size": 2048
    }
  ],
  "events": ["evt-e5f6g7h8"],
  "created_at": "2025-10-20T14:30:00Z"
}
```

---

### Run State Schema (`run-state.v1.json`)

Defines persistent run state for crash recovery.

**Key Fields**:
- `status` - Run status: `running`, `completed`, `failed`, `aborted`
- `current_stage` - Execution stage: `implement`, `review`, `spec_maintain`, `complete`
- `snapshot_id` - Pinned workspace version for this run
- `terminal_events` - Map of agent → terminal event type
- Stored at: `/state/run.json`

**Example**:
```json
{
  "run_id": "run-20251020-140818-4f9f29a1",
  "status": "running",
  "task_id": "T-0042",
  "snapshot_id": "snap-d0ab7e60b764",
  "current_stage": "review",
  "started_at": "2025-10-20T14:08:18Z",
  "last_command_id": "cmd-a1b2c3d4",
  "last_event_id": "evt-e5f6g7h8",
  "terminal_events": {
    "builder": "builder.completed"
  }
}
```

---

### Log Schema (`log.v1.json`)

Defines diagnostic log messages from agents.

**Key Fields**:
- `level` - Severity: `info`, `warn`, `error`
- `message` - Human-readable message
- `fields` - Optional structured data

**Example**:
```json
{
  "kind": "log",
  "level": "info",
  "message": "Starting implementation",
  "fields": {
    "task_id": "T-0042",
    "action": "implement"
  },
  "timestamp": "2025-10-20T14:08:20Z"
}
```

---

## Pattern Reference

### Common Patterns

| Pattern | Regex | Example |
|---------|-------|---------|
| **Task ID** | `^T-[0-9]{4}$` | `T-0042` |
| **Snapshot ID** | `^snap-[a-f0-9]{12}$` | `snap-d0ab7e60b764` |
| **Idempotency Key** | `^ik:[a-f0-9]{64}$` | `ik:7a3f8e9c...` |
| **SHA256 Hash** | `^sha256:[a-f0-9]{64}$` | `sha256:abc123...` |
| **Run ID** | `^run-[0-9]{8}-[0-9]{6}-[a-f0-9]{8}$` | `run-20251020-140818-4f9f29a1` |
| **Command Message ID** | `^cmd-[a-f0-9]{8}$` | `cmd-a1b2c3d4` |
| **Event Message ID** | `^evt-[a-f0-9]{8}$` | `evt-e5f6g7h8` |

### Timestamp Format

All timestamps use **RFC3339** format:
```
2025-10-20T14:08:18Z          (UTC)
2025-10-20T14:08:18.123Z      (with milliseconds)
2025-10-20T10:08:18-04:00     (with timezone)
```

---

## Testing Schemas

### Validating Schema Files

Ensure schema files themselves are valid:

```bash
# Using ajv-cli
ajv compile -s schemas/v1/*.json

# Using Python jsonschema
python3 -c "
import json, jsonschema
schema = json.load(open('schemas/v1/command.v1.json'))
jsonschema.Draft7Validator.check_schema(schema)
print('Schema is valid!')
"
```

### Example Test

```go
// Example Go code for schema validation (future enhancement)
package schema_test

import (
    "testing"
    "github.com/xeipuuv/gojsonschema"
)

func TestCommandSchema(t *testing.T) {
    schemaLoader := gojsonschema.NewReferenceLoader("file://schemas/v1/command.v1.json")
    documentLoader := gojsonschema.NewStringLoader(`{
        "kind": "command",
        "message_id": "cmd-a1b2c3d4",
        ...
    }`)

    result, err := gojsonschema.Validate(schemaLoader, documentLoader)
    if err != nil {
        t.Fatal(err)
    }

    if !result.Valid() {
        t.Errorf("Validation errors: %v", result.Errors())
    }
}
```

---

## Future Enhancements

Potential future additions:

- **Automated validation** - CI checks for all persisted files
- **Schema versioning** - Support multiple schema versions (v1, v2, etc.)
- **Code generation** - Generate Go/TypeScript types from schemas
- **Documentation generation** - Auto-generate API docs from schemas
- **Migration tools** - Scripts to upgrade old format files

---

## References

- **JSON Schema**: https://json-schema.org/
- **MASTER-SPEC**: `../docs/MASTER-SPEC.md` - Full protocol specification
- **Protocol Types**: `../internal/protocol/types.go` - Go type definitions
- **Idempotency**: `../docs/IDEMPOTENCY.md` - IK generation details
- **Resume**: `../docs/RESUME.md` - Crash recovery flow

---

## Version History

| Version | Date | Changes |
|---------|------|---------|
| **v1** | 2025-10-20 | Initial schema definitions for Phase 1.3 |
