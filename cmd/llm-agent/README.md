# LLM Agent Interfaces and Stubs

This directory contains the interfaces and stubs for the LLM agent implementation, designed to enable parallel development across multiple workstreams.

## Overview

The LLM agent is designed as a collection of interfaces that can be implemented independently. This allows multiple developers to work on different aspects of the system simultaneously.

## Interfaces

### 1. LLMCaller Interface

**Purpose**: Abstracts calls to external LLM CLI tools (claude, codex, etc.)

**Interface**: `LLMCaller`
- `Call(ctx context.Context, prompt string) (string, error)`

**Implementations**:
- `RealLLMCaller`: Uses actual CLI subprocess calls
- `MockLLMCaller`: For testing with configurable responses

**Usage**:
```go
// Real implementation
config := DefaultLLMConfig("claude")
caller := NewRealLLMCaller(config)
response, err := caller.Call(ctx, "Your prompt here")

// Mock for testing
mockCaller := NewMockLLMCaller()
mockCaller.SetResponse("test prompt", "mock response")
response, err := mockCaller.Call(ctx, "test prompt")
```

### 2. ReceiptStore Interface

**Purpose**: Manages idempotency receipts for deterministic replays

**Interface**: `ReceiptStore`
- `LoadReceipt(path string) (*Receipt, error)`
- `SaveReceipt(path string, receipt *Receipt) error`
- `FindReceiptByIK(taskID, action, ik string) (*Receipt, string, error)`

**Implementations**:
- `RealReceiptStore`: Uses filesystem storage
- `MockReceiptStore`: In-memory storage for testing

**Usage**:
```go
// Real implementation
store := NewRealReceiptStore("/workspace")

// Mock for testing
mockStore := NewMockReceiptStore()
receipt := &Receipt{
    TaskID: "T-001",
    IdempotencyKey: "ik-123",
    // ... other fields
}
err := mockStore.SaveReceipt("/receipts/T-001.json", receipt)
```

### 3. FSProvider Interface

**Purpose**: Provides secure filesystem operations with path validation

**Interface**: `FSProvider`
- `ResolveWorkspacePath(workspace, relative string) (string, error)`
- `ReadFileSafe(path string, maxSize int64) (string, error)`
- `WriteArtifactAtomic(workspace, relativePath string, content []byte) (protocol.Artifact, error)`

**Implementations**:
- `RealFSProvider`: Uses actual filesystem operations
- `MockFSProvider`: In-memory filesystem for testing

**Usage**:
```go
// Real implementation
provider := NewRealFSProvider("/workspace")
content, err := provider.ReadFileSafe("/workspace/PLAN.md", 1024)

// Mock for testing
mockProvider := NewMockFSProvider()
mockProvider.SetFile("/workspace/PLAN.md", "content")
content, err := mockProvider.ReadFileSafe("/workspace/PLAN.md", 1024)
```

### 4. EventEmitter Interface

**Purpose**: Emits protocol events with proper formatting and size limits

**Interface**: `EventEmitter`
- `NewEvent(cmd *protocol.Command, eventName string) protocol.Event`
- `EncodeEventCapped(evt protocol.Event) error`
- `SendErrorEvent(cmd *protocol.Command, code, message string) error`
- `SendArtifactProducedEvent(cmd *protocol.Command, artifact protocol.Artifact) error`
- `SendLog(level, message string, fields map[string]any) error`

**Implementations**:
- `RealEventEmitter`: Uses NDJSON encoding
- `MockEventEmitter`: Captures events for testing

**Usage**:
```go
// Real implementation
encoder := ndjson.NewEncoder(os.Stdout)
emitter := NewRealEventEmitter(encoder, logger)

// Mock for testing
mockEmitter := NewMockEventEmitter()
err := mockEmitter.SendErrorEvent(cmd, "test_error", "message")
events := mockEmitter.GetEvents()
```

## Workstream Dependencies

The interfaces are designed to minimize dependencies between workstreams:

### Independent Workstreams
- **A**: NDJSON Core & CLI Scaffold
- **B**: Heartbeats & Liveness
- **D**: Security & Filesystem Utilities
- **G**: LLM CLI Caller
- **J**: Version Snapshot Tracking
- **L**: Logging & Redaction

### Dependent Workstreams
- **C**: Event Builders & Error Helpers (depends on A)
- **E**: Idempotency Receipts (depends on C, D)
- **H**: Artifact Production (depends on C, D)
- **I**: Event Size Guard (depends on C, H)
- **F**: Orchestration Logic (depends on C, D, E, G)

## Testing Strategy

Each interface includes comprehensive test coverage:

### Unit Tests
- Interface compliance
- Mock behavior verification
- Error handling
- Edge cases

### Integration Tests
- Interface compatibility
- Dependency chain validation
- End-to-end workflow simulation

### Example Test Structure
```go
func TestLLMCallerInterface(t *testing.T) {
    t.Run("RealLLMCaller", func(t *testing.T) {
        // Test real implementation
    })

    t.Run("MockLLMCaller", func(t *testing.T) {
        // Test mock implementation
    })
}
```

## Development Workflow

### For Each Workstream

1. **Start with Interface**: Define the interface contract
2. **Implement Mock**: Create mock implementation for testing
3. **Write Tests**: Comprehensive test coverage
4. **Implement Real**: Create production implementation
5. **Integration**: Test with other workstreams

### Parallel Development

1. **Workstream A** can start immediately (no dependencies)
2. **Workstream C** can start once A is ready
3. **Workstreams D, G, J, L** can start immediately
4. **Workstream E** can start once C and D are ready
5. **Workstream F** can start once C, D, E, and G are ready

### Integration Points

- All workstreams use the same protocol types
- Interfaces are designed to be composable
- Mock implementations enable independent testing
- Real implementations can be swapped in when ready

## Configuration

The agent configuration is designed to be flexible:

```go
type AgentConfig struct {
    Role      protocol.AgentType
    LLMCLI    string
    Workspace string
    Logger    *slog.Logger
}
```

## Error Handling

All interfaces include proper error handling:

- **LLMCaller**: Timeout, subprocess failures, size limits
- **ReceiptStore**: File I/O errors, JSON marshaling
- **FSProvider**: Path validation, permission errors, size limits
- **EventEmitter**: Message size limits, encoding errors

## Security Considerations

- **Path Validation**: All filesystem operations use symlink-safe resolution
- **Size Limits**: All file operations have configurable size limits
- **Secret Redaction**: Log fields ending in `_TOKEN`, `_KEY`, `_SECRET` are redacted
- **Atomic Writes**: All file writes use the atomic pattern per spec §14.4

## Performance Considerations

- **Mock Implementations**: Fast, in-memory operations for testing
- **Real Implementations**: Optimized for production use
- **Interface Overhead**: Minimal - interfaces are designed for efficiency
- **Memory Usage**: Configurable limits prevent runaway memory usage

## Future Extensions

The interface design supports future extensions:

- **Additional LLM Providers**: New LLMCaller implementations
- **Alternative Storage**: Different ReceiptStore backends
- **Enhanced Security**: Additional FSProvider security features
- **Rich Events**: Extended EventEmitter capabilities

## Getting Started

1. **Choose a Workstream**: Start with an independent workstream (A, B, D, G, J, L)
2. **Study the Interface**: Understand the contract and requirements
3. **Write Tests**: Create comprehensive test coverage
4. **Implement Mock**: Create mock implementation for testing
5. **Implement Real**: Create production implementation
6. **Integrate**: Test with other workstreams

## Examples

See the test files for comprehensive examples:
- `interfaces_test.go`: Interface compliance tests
- `llm_test.go`: LLM caller tests
- `receipts_test.go`: Receipt store tests
- `fsutil_test.go`: Filesystem provider tests
- `events_test.go`: Event emitter tests
- `integration_test.go`: End-to-end workflow tests
