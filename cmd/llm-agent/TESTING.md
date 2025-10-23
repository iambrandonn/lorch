# LLM Agent Testing Framework

This document describes the comprehensive testing framework for the LLM Agent implementation, covering all aspects of testing as specified in workstream K of the LLM-AGENTS-PLAN.md.

## Overview

The testing framework provides comprehensive coverage across multiple test categories:

- **Test Infrastructure** - Basic test utilities and setup
- **Unit Tests** - Core component testing
- **Schema Validation** - Protocol compliance testing
- **Integration Tests** - Component integration testing
- **End-to-End Tests** - Complete workflow testing
- **Negative Tests** - Error handling and edge cases
- **Logging & Redaction** - Security and logging testing

## Test Files

| File | Purpose | Test Count |
|------|---------|------------|
| `test_utils.go` | Test utilities and helpers | - |
| `unit_tests.go` | Unit tests for core components | 10+ |
| `schema_validation_tests.go` | Protocol schema compliance | 8+ |
| `integration_tests.go` | Integration with mock LLM | 6+ |
| `e2e_test_harness.go` | End-to-end test harness | 7+ |
| `negative_tests.go` | Negative scenarios and edge cases | 12+ |
| `logging_redaction_tests.go` | Logging and secret redaction | 8+ |
| `test_runner.go` | Comprehensive test runner | 1 |

## Test Categories

### 1. Test Infrastructure (`test_utils.go`)

Provides common testing utilities for all test categories:

- **TestUtilities**: Main utility class for test setup
- **Workspace Management**: Test workspace creation and cleanup
- **Agent Creation**: Test agent instantiation with mock dependencies
- **Command/Event Creation**: Test message creation helpers
- **Validation Helpers**: Assertion helpers for message validation
- **Mock Setup**: Mock LLM script creation and configuration

**Key Features:**
- Automatic test workspace creation and cleanup
- Mock dependency injection
- Comprehensive validation helpers
- Path safety testing utilities

### 2. Unit Tests (`unit_tests.go`)

Tests individual components in isolation:

- **Event Builders**: `TestEventBuilders` - Event construction and validation
- **Receipt Storage**: `TestReceiptStorage` - Receipt save/load/idempotency
- **Path Safety**: `TestPathSafety` - Filesystem path validation
- **Artifact Production**: `TestArtifactProduction` - Artifact creation and validation
- **LLM Caller Interface**: `TestLLMCallerInterface` - LLM calling functionality
- **Agent Configuration**: `TestAgentConfiguration` - Agent setup and validation
- **Message Validation**: `TestMessageValidation` - Message structure validation
- **Concurrent Access**: `TestConcurrentAccess` - Thread safety testing
- **Error Handling**: `TestErrorHandling` - Error scenario testing
- **Data Structures**: `TestDataStructures` - Internal data structure testing

**Coverage:**
- All core components tested in isolation
- Mock dependencies for deterministic testing
- Concurrent access patterns validated
- Error handling scenarios covered

### 3. Schema Validation (`schema_validation_tests.go`)

Tests protocol schema compliance:

- **Command Schema**: `TestCommandSchema` - Command message validation
- **Event Schema**: `TestEventSchema` - Event message validation
- **Heartbeat Schema**: `TestHeartbeatSchema` - Heartbeat message validation
- **Log Schema**: `TestLogSchema` - Log message validation
- **Message Size Limits**: `TestMessageSizeLimits` - NDJSON size compliance
- **Enum Validation**: `TestEnumValidation` - Valid enum value testing
- **Field Validation**: `TestFieldValidation` - Individual field validation
- **Negative Validation**: `TestNegativeValidation` - Invalid enum testing
- **Schema Compliance**: `TestSchemaCompliance` - Complete schema compliance
- **Edge Cases**: `TestEdgeCases` - Edge case validation

**Coverage:**
- All protocol schemas validated
- Message size limits enforced
- Enum values validated
- Required fields checked
- Invalid data rejected

### 4. Integration Tests (`integration_tests.go`)

Tests component integration with mock LLM:

- **Full Integration**: `TestLLMAgentFullIntegration` - Complete agent workflow
- **NDJSON Protocol**: `TestNDJSONProtocolIntegration` - Protocol compliance
- **Mock LLM Integration**: `TestMockLLMIntegration` - LLM integration testing
- **Workspace Integration**: `TestWorkspaceIntegration` - File system integration
- **Error Recovery**: `TestErrorRecovery` - Error handling scenarios

**Coverage:**
- Complete intake workflow testing
- Task discovery workflow testing
- Error handling and recovery
- NDJSON protocol compliance
- Workspace file integration

### 5. End-to-End Tests (`e2e_test_harness.go`)

Tests complete workflows with real agent binary:

- **Agent Startup**: `TestE2EAgentStartup` - Agent initialization and startup
- **Intake Flow**: `TestE2EIntakeFlow` - Complete intake workflow
- **Error Handling**: `TestE2EErrorHandling` - Error scenario handling
- **Protocol Compliance**: `TestE2EProtocolCompliance` - End-to-end protocol compliance
- **Workspace Integration**: `TestE2EWorkspaceIntegration` - File system integration
- **Performance**: `TestE2EPerformance` - Performance characteristics
- **Real-World Scenarios**: `TestE2ERealWorldScenarios` - Real usage patterns

**Coverage:**
- Complete agent lifecycle testing
- Real agent binary execution
- Workspace file access validation
- Performance benchmarking
- Real-world usage scenarios

### 6. Negative Tests (`negative_tests.go`)

Tests error handling and edge cases:

- **Oversize Messages**: `TestOversizeMessages` - Message size limit testing
- **Invalid Enums**: `TestInvalidEnums` - Invalid enum value testing
- **Malformed JSON**: `TestMalformedJSON` - Invalid JSON handling
- **Missing Fields**: `TestMissingFields` - Required field validation
- **Invalid Types**: `TestInvalidTypes` - Type validation
- **Invalid Formats**: `TestInvalidFormats` - Format validation
- **Edge Cases**: `TestEdgeCases` - Edge case handling
- **Agent Behavior**: `TestNegativeAgentBehavior` - Agent error handling
- **Protocol Compliance**: `TestNegativeProtocolCompliance` - Protocol error handling

**Coverage:**
- All error scenarios tested
- Edge cases validated
- Invalid data rejected
- Graceful error handling
- Protocol compliance maintained

### 7. Logging & Redaction (`logging_redaction_tests.go`)

Tests logging and secret redaction:

- **Basic Logging**: `TestLoggingAndRedaction` - Basic logging functionality
- **Secret Redaction**: `TestSecretRedactionPatterns` - Secret field redaction
- **Performance**: `TestLoggingPerformance` - Logging performance testing
- **Error Handling**: `TestLoggingErrorHandling` - Logging error scenarios
- **Integration**: `TestLoggingIntegration` - Logging with other components
- **Call Logging**: `TestLoggingCallLogging` - Call logging functionality

**Coverage:**
- All log levels tested
- Secret redaction patterns validated
- Performance characteristics measured
- Error handling scenarios covered
- Integration with other components tested

## Test Runner

The `test_runner.go` provides a comprehensive test runner with the following capabilities:

### Test Categories
- **RunAllTests()** - Runs all test categories
- **RunSpecificTestCategory()** - Runs a specific test category
- **RunPerformanceTests()** - Performance-focused testing
- **RunStressTests()** - Maximum load testing
- **RunRegressionTests()** - Known issue prevention

### Usage Examples

```bash
# Run all tests
go test ./cmd/llm-agent -v

# Run specific test category
go test ./cmd/llm-agent -run TestInfrastructure -v
go test ./cmd/llm-agent -run TestUnitTests -v
go test ./cmd/llm-agent -run TestSchemaValidation -v

# Run performance tests
go test ./cmd/llm-agent -run TestPerformanceTests -v

# Run stress tests
go test ./cmd/llm-agent -run TestStressTests -v

# Run regression tests
go test ./cmd/llm-agent -run TestRegressionTests -v
```

## Test Coverage

### Protocol Compliance
- ✅ Command schema validation
- ✅ Event schema validation
- ✅ Heartbeat schema validation
- ✅ Log schema validation
- ✅ Message size limits (256 KiB)
- ✅ Enum value validation
- ✅ Required field validation
- ✅ Type validation
- ✅ Format validation

### Idempotency & Determinism
- ✅ Idempotency key handling
- ✅ Receipt storage and retrieval
- ✅ Cache hit/miss scenarios
- ✅ Replay functionality
- ✅ Deterministic output ordering
- ✅ Version mismatch detection

### Security & Safety
- ✅ Path traversal prevention
- ✅ Secret field redaction
- ✅ File permission validation
- ✅ Size limit enforcement
- ✅ Input validation
- ✅ Error handling

### Performance & Scalability
- ✅ High volume logging
- ✅ Concurrent operations
- ✅ Memory usage testing
- ✅ Response time validation
- ✅ Stress testing
- ✅ Load testing

### Error Handling
- ✅ Invalid input handling
- ✅ LLM error scenarios
- ✅ File system errors
- ✅ Network timeouts
- ✅ Context cancellation
- ✅ Graceful degradation

## Mock Dependencies

The testing framework uses comprehensive mock dependencies:

### MockLLMCaller
- Configurable responses
- Error simulation
- Call counting
- Timeout testing

### MockReceiptStore
- In-memory storage
- Call logging
- Error simulation
- Idempotency testing

### MockFSProvider
- File system simulation
- Path validation
- Size limits
- Error scenarios

### MockEventEmitter
- Event recording
- Call logging
- Error tracking
- Artifact tracking

## Test Data

The framework provides comprehensive test data:

### Test Commands
- Valid command structures
- Invalid command structures
- Edge case commands
- Performance test commands

### Test Events
- All event types
- Valid event structures
- Invalid event structures
- Edge case events

### Test Artifacts
- Valid artifact structures
- Invalid artifact structures
- Large artifacts
- Checksum validation

### Test Workspaces
- Standard workspace structure
- Test files and directories
- Permission validation
- Path safety testing

## Continuous Integration

The testing framework is designed for CI/CD integration:

### Fast Feedback
- Quick unit tests (< 1 second)
- Fast integration tests (< 5 seconds)
- Comprehensive coverage
- Clear error messages

### Deterministic Results
- Mock dependencies
- Isolated test environments
- No external dependencies
- Reproducible results

### Comprehensive Reporting
- Detailed test output
- Performance metrics
- Coverage reporting
- Error categorization

## Best Practices

### Test Organization
- Clear test categories
- Descriptive test names
- Comprehensive coverage
- Maintainable structure

### Mock Usage
- Isolated dependencies
- Configurable behavior
- Realistic scenarios
- Error simulation

### Performance Testing
- Load testing
- Stress testing
- Memory profiling
- Response time validation

### Security Testing
- Secret redaction
- Path validation
- Input sanitization
- Error handling

## Future Enhancements

### Planned Improvements
- Code coverage reporting
- Performance benchmarking
- Load testing automation
- Security scanning integration

### Extensibility
- Plugin architecture
- Custom test categories
- External test data
- Integration with external tools

## Conclusion

The LLM Agent Testing Framework provides comprehensive coverage across all aspects of the agent implementation, ensuring reliability, security, and performance. The framework is designed for maintainability, extensibility, and continuous integration, providing confidence in the agent's behavior across all scenarios.

For more information, see the individual test files and the LLM-AGENTS-PLAN.md specification.
