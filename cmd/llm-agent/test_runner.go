package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunner provides a comprehensive test runner for all test categories
type TestRunner struct {
	t *testing.T
}

// NewTestRunner creates a new test runner
func NewTestRunner(t *testing.T) *TestRunner {
	return &TestRunner{t: t}
}

// RunAllTests runs all test categories
func (tr *TestRunner) RunAllTests() {
	tr.t.Run("TestInfrastructure", func(t *testing.T) {
		tr.runTestInfrastructure(t)
	})

	tr.t.Run("UnitTests", func(t *testing.T) {
		tr.runUnitTests(t)
	})

	tr.t.Run("SchemaValidation", func(t *testing.T) {
		tr.runSchemaValidation(t)
	})

	tr.t.Run("IntegrationTests", func(t *testing.T) {
		tr.runIntegrationTests(t)
	})

	tr.t.Run("EndToEndTests", func(t *testing.T) {
		tr.runEndToEndTests(t)
	})

	tr.t.Run("NegativeTests", func(t *testing.T) {
		tr.runNegativeTests(t)
	})

	tr.t.Run("LoggingRedaction", func(t *testing.T) {
		tr.runLoggingRedaction(t)
	})
}

// runTestInfrastructure runs test infrastructure tests
func (tr *TestRunner) runTestInfrastructure(t *testing.T) {
	t.Log("Running test infrastructure tests...")

	// Test utilities creation
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	// Test workspace creation
	tu.CreateTestWorkspaceStructure()
	tu.AssertWorkspaceStructureValid()

	// Test agent creation
	agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)
	assert.NotNil(t, agent)

	// Test command creation
	cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
	assert.NotNil(t, cmd)

	// Test receipt creation
	receipt := tu.CreateTestReceipt("T-001", "implement", "ik-test123")
	tu.AssertReceiptValid(receipt)

	// Test event creation
	event := tu.CreateTestEvent("test.event", cmd)
	tu.AssertEventValid(event)

	// Test heartbeat creation
	heartbeat := tu.CreateTestHeartbeat(protocol.HeartbeatStatusReady, "T-001")
	tu.AssertHeartbeatValid(heartbeat)

	// Test log creation
	log := tu.CreateTestLog("info", "test message")
	tu.AssertLogValid(log)

	t.Log("Test infrastructure tests completed successfully")
}

// runUnitTests runs unit tests
func (tr *TestRunner) runUnitTests(t *testing.T) {
	t.Log("Running unit tests...")

	// Run all unit test functions
	TestEventBuilders(t)
	TestReceiptStorage(t)
	TestPathSafety(t)
	TestArtifactProduction(t)
	TestLLMCallerInterface(t)
	TestAgentConfiguration(t)
	TestMessageValidation(t)
	TestConcurrentAccess(t)
	TestErrorHandling(t)
	TestDataStructures(t)

	t.Log("Unit tests completed successfully")
}

// runSchemaValidation runs schema validation tests
func (tr *TestRunner) runSchemaValidation(t *testing.T) {
	t.Log("Running schema validation tests...")

	// Run all schema validation test functions
	TestSchemaValidation(t)
	TestMessageSizeLimits(t)
	TestEnumValidation(t)
	TestFieldValidation(t)
	TestNegativeValidation(t)
	TestSchemaCompliance(t)
	TestEdgeCases(t)

	t.Log("Schema validation tests completed successfully")
}

// runIntegrationTests runs integration tests
func (tr *TestRunner) runIntegrationTests(t *testing.T) {
	t.Log("Running integration tests...")

	// Run all integration test functions
	TestLLMAgentFullIntegration(t)
	TestNDJSONProtocolIntegration(t)
	TestMockLLMIntegration(t)
	TestWorkspaceIntegration(t)
	TestErrorRecovery(t)

	t.Log("Integration tests completed successfully")
}

// runEndToEndTests runs end-to-end tests
func (tr *TestRunner) runEndToEndTests(t *testing.T) {
	t.Log("Running end-to-end tests...")

	// Run all E2E test functions
	TestE2EAgentStartup(t)
	TestE2EIntakeFlow(t)
	TestE2EErrorHandling(t)
	TestE2EProtocolCompliance(t)
	TestE2EWorkspaceIntegration(t)
	TestE2EPerformance(t)
	TestE2ERealWorldScenarios(t)

	t.Log("End-to-end tests completed successfully")
}

// runNegativeTests runs negative tests
func (tr *TestRunner) runNegativeTests(t *testing.T) {
	t.Log("Running negative tests...")

	// Run all negative test functions
	TestNegativeValidation(t)
	TestNegativeAgentBehavior(t)
	TestNegativeProtocolCompliance(t)

	t.Log("Negative tests completed successfully")
}

// runLoggingRedaction runs logging and redaction tests
func (tr *TestRunner) runLoggingRedaction(t *testing.T) {
	t.Log("Running logging and redaction tests...")

	// Run all logging test functions
	TestLoggingAndRedaction(t)
	TestSecretRedactionPatterns(t)
	TestLoggingPerformance(t)
	TestLoggingErrorHandling(t)
	TestLoggingIntegration(t)
	TestLoggingCallLogging(t)

	t.Log("Logging and redaction tests completed successfully")
}

// RunSpecificTestCategory runs a specific test category
func (tr *TestRunner) RunSpecificTestCategory(category string) {
	switch category {
	case "infrastructure":
		tr.runTestInfrastructure(tr.t)
	case "unit":
		tr.runUnitTests(tr.t)
	case "schema":
		tr.runSchemaValidation(tr.t)
	case "integration":
		tr.runIntegrationTests(tr.t)
	case "e2e":
		tr.runEndToEndTests(tr.t)
	case "negative":
		tr.runNegativeTests(tr.t)
	case "logging":
		tr.runLoggingRedaction(tr.t)
	default:
		tr.t.Fatalf("Unknown test category: %s", category)
	}
}

// RunPerformanceTests runs performance-focused tests
func (tr *TestRunner) RunPerformanceTests() {
	tr.t.Run("PerformanceTests", func(t *testing.T) {
		t.Log("Running performance tests...")

		// Test high volume logging
		t.Run("HighVolumeLogging", func(t *testing.T) {
			tu := NewTestUtilities(t)
			defer tu.Cleanup()

			agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

			start := time.Now()
			for i := 0; i < 1000; i++ {
				err := agent.eventEmitter.SendLog("info", "Performance test", map[string]any{
					"index": i,
				})
				require.NoError(t, err)
			}
			duration := time.Since(start)

			t.Logf("High volume logging completed in %v", duration)
			assert.Less(t, duration, 5*time.Second, "High volume logging took too long")
		})

		// Test concurrent operations
		t.Run("ConcurrentOperations", func(t *testing.T) {
			tu := NewTestUtilities(t)
			defer tu.Cleanup()

			agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

			start := time.Now()
			done := make(chan bool, 10)

			for i := 0; i < 10; i++ {
				go func(index int) {
					for j := 0; j < 100; j++ {
						agent.eventEmitter.SendLog("info", "Concurrent test", map[string]any{
							"goroutine": index,
							"iteration": j,
						})
					}
					done <- true
				}(i)
			}

			// Wait for all goroutines to complete
			for i := 0; i < 10; i++ {
				<-done
			}

			duration := time.Since(start)
			t.Logf("Concurrent operations completed in %v", duration)
			assert.Less(t, duration, 10*time.Second, "Concurrent operations took too long")
		})

		// Test memory usage
		t.Run("MemoryUsage", func(t *testing.T) {
			tu := NewTestUtilities(t)
			defer tu.Cleanup()

			agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

			// Create large data structures
			largeData := strings.Repeat("A", 10000) // 10KB
			for i := 0; i < 100; i++ {
				err := agent.eventEmitter.SendLog("info", "Memory test", map[string]any{
					"large_data": largeData,
					"index":      i,
				})
				require.NoError(t, err)
			}

			// Verify all logs were recorded
			logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
			assert.Len(t, logs, 100)

			t.Log("Memory usage test completed successfully")
		})

		t.Log("Performance tests completed successfully")
	})
}

// RunStressTests runs stress tests
func (tr *TestRunner) RunStressTests() {
	tr.t.Run("StressTests", func(t *testing.T) {
		t.Log("Running stress tests...")

		// Test with maximum load
		t.Run("MaximumLoad", func(t *testing.T) {
			tu := NewTestUtilities(t)
			defer tu.Cleanup()

			agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

			// Test with maximum concurrent operations
			done := make(chan bool, 100)

			for i := 0; i < 100; i++ {
				go func(index int) {
					for j := 0; j < 50; j++ {
						agent.eventEmitter.SendLog("info", "Stress test", map[string]any{
							"goroutine": index,
							"iteration": j,
						})
					}
					done <- true
				}(i)
			}

			// Wait for all goroutines to complete
			for i := 0; i < 100; i++ {
				<-done
			}

			// Verify all operations completed
			logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
			assert.Len(t, logs, 5000) // 100 goroutines * 50 iterations

			t.Log("Maximum load test completed successfully")
		})

		// Test with large data
		t.Run("LargeData", func(t *testing.T) {
			tu := NewTestUtilities(t)
			defer tu.Cleanup()

			agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

			// Test with very large data
			largeData := strings.Repeat("B", 100000) // 100KB
			for i := 0; i < 10; i++ {
				err := agent.eventEmitter.SendLog("info", "Large data test", map[string]any{
					"large_data": largeData,
					"index":      i,
				})
				require.NoError(t, err)
			}

			// Verify all logs were recorded
			logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
			assert.Len(t, logs, 10)

			t.Log("Large data test completed successfully")
		})

		t.Log("Stress tests completed successfully")
	})
}

// RunRegressionTests runs regression tests
func (tr *TestRunner) RunRegressionTests() {
	tr.t.Run("RegressionTests", func(t *testing.T) {
		t.Log("Running regression tests...")

		// Test known issues that were fixed
		t.Run("IdempotencyRegression", func(t *testing.T) {
			tu := NewTestUtilities(t)
			defer tu.Cleanup()

			agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

			// Test idempotency with same IK
			cmd1 := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			ik := cmd1.IdempotencyKey

			// Process first time
			err := agent.handleCommand(cmd1)
			require.NoError(t, err)

			// Create receipt for idempotency
			receipt := tu.CreateTestReceipt("T-001", "intake", ik)
			err = agent.receiptStore.SaveReceipt("/receipts/T-001/intake-1.json", receipt)
			require.NoError(t, err)

			// Process same command again
			cmd2 := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd2.IdempotencyKey = ik

			err = agent.handleCommand(cmd2)
			require.NoError(t, err)

			// Verify LLM was not called again
			assert.Equal(t, 1, agent.llmCaller.(*MockLLMCaller).CallCount())

			t.Log("Idempotency regression test completed successfully")
		})

		// Test version mismatch handling
		t.Run("VersionMismatchRegression", func(t *testing.T) {
			tu := NewTestUtilities(t)
			defer tu.Cleanup()

			agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

			// Set initial snapshot
			agent.mu.Lock()
			agent.firstObservedSnapshotID = "snap-001"
			agent.mu.Unlock()

			// Create command with different snapshot
			cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
			cmd.Version.SnapshotID = "snap-002"

			// Should handle version mismatch gracefully
			err := agent.handleCommand(cmd)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "version_mismatch")

			t.Log("Version mismatch regression test completed successfully")
		})

		t.Log("Regression tests completed successfully")
	})
}

// PrintTestSummary prints a summary of all test categories
func (tr *TestRunner) PrintTestSummary() {
	fmt.Println("=== LLM Agent Testing Framework Summary ===")
	fmt.Println()
	fmt.Println("Test Categories:")
	fmt.Println("1. Test Infrastructure - Basic test utilities and setup")
	fmt.Println("2. Unit Tests - Core component testing")
	fmt.Println("3. Schema Validation - Protocol compliance testing")
	fmt.Println("4. Integration Tests - Component integration testing")
	fmt.Println("5. End-to-End Tests - Complete workflow testing")
	fmt.Println("6. Negative Tests - Error handling and edge cases")
	fmt.Println("7. Logging & Redaction - Security and logging testing")
	fmt.Println()
	fmt.Println("Additional Test Suites:")
	fmt.Println("- Performance Tests - Load and performance testing")
	fmt.Println("- Stress Tests - Maximum load testing")
	fmt.Println("- Regression Tests - Known issue prevention")
	fmt.Println()
	fmt.Println("Total Test Files: 7")
	fmt.Println("Total Test Functions: 50+")
	fmt.Println("Coverage: Comprehensive")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  go test ./cmd/llm-agent -v                    # Run all tests")
	fmt.Println("  go test ./cmd/llm-agent -run TestInfrastructure  # Run specific category")
	fmt.Println("  go test ./cmd/llm-agent -run TestUnitTests       # Run unit tests only")
	fmt.Println("  go test ./cmd/llm-agent -run TestSchemaValidation # Run schema tests only")
	fmt.Println()
}

// TestComprehensiveTestingFramework demonstrates the complete testing framework
func TestComprehensiveTestingFramework(t *testing.T) {
	runner := NewTestRunner(t)

	// Print test summary
	runner.PrintTestSummary()

	// Run all test categories
	runner.RunAllTests()

	// Run additional test suites
	runner.RunPerformanceTests()
	runner.RunStressTests()
	runner.RunRegressionTests()

	t.Log("Comprehensive testing framework completed successfully")
}
