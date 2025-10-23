package main

import (
	"strings"
	"testing"

	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoggingAndRedaction tests logging and secret redaction functionality
func TestLoggingAndRedaction(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

	t.Run("BasicLogging", func(t *testing.T) {
		// Test basic log emission
		err := agent.eventEmitter.SendLog("info", "Test log message", map[string]any{
			"key1": "value1",
			"key2": "value2",
		})
		require.NoError(t, err)

		// Verify log was recorded
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)
		assert.Equal(t, "info", logs[0].Level)
		assert.Equal(t, "Test log message", logs[0].Message)
		assert.Equal(t, "value1", logs[0].Fields["key1"])
		assert.Equal(t, "value2", logs[0].Fields["key2"])
	})

	t.Run("SecretRedaction", func(t *testing.T) {
		// Test secret redaction
		fields := map[string]any{
			"api_key":        "secret123",
			"auth_token":     "token456",
			"database_secret": "secret789",
			"user_name":      "john_doe", // Should not be redacted
			"normal_field":   "normal_value",
		}

		err := agent.eventEmitter.SendLog("info", "Test log with secrets", fields)
		require.NoError(t, err)

		// Verify secrets were redacted
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)

		redactedFields := logs[0].Fields
		assert.Equal(t, "[REDACTED]", redactedFields["api_key"])
		assert.Equal(t, "[REDACTED]", redactedFields["auth_token"])
		assert.Equal(t, "[REDACTED]", redactedFields["database_secret"])
		assert.Equal(t, "john_doe", redactedFields["user_name"]) // Should not be redacted
		assert.Equal(t, "normal_value", redactedFields["normal_field"])
	})

	t.Run("CaseInsensitiveRedaction", func(t *testing.T) {
		// Test case-insensitive redaction
		fields := map[string]any{
			"API_KEY":        "secret123",
			"Auth_Token":     "token456",
			"database_SECRET": "secret789",
			"user_name":      "john_doe",
		}

		err := agent.eventEmitter.SendLog("info", "Test log with mixed case", fields)
		require.NoError(t, err)

		// Verify secrets were redacted regardless of case
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)

		redactedFields := logs[0].Fields
		assert.Equal(t, "[REDACTED]", redactedFields["API_KEY"])
		assert.Equal(t, "[REDACTED]", redactedFields["Auth_Token"])
		assert.Equal(t, "[REDACTED]", redactedFields["database_SECRET"])
		assert.Equal(t, "john_doe", redactedFields["user_name"])
	})

	t.Run("NestedObjectRedaction", func(t *testing.T) {
		// Test redaction in nested objects
		fields := map[string]any{
			"config": map[string]any{
				"api_key": "secret123",
				"normal":  "value",
			},
			"secrets": map[string]any{
				"auth_token": "token456",
				"database": map[string]any{
					"password": "pass123",
					"host":     "localhost",
				},
			},
		}

		err := agent.eventEmitter.SendLog("info", "Test log with nested secrets", fields)
		require.NoError(t, err)

		// Verify nested secrets were redacted
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)

		redactedFields := logs[0].Fields
		config := redactedFields["config"].(map[string]any)
		assert.Equal(t, "[REDACTED]", config["api_key"])
		assert.Equal(t, "value", config["normal"])

		secrets := redactedFields["secrets"].(map[string]any)
		assert.Equal(t, "[REDACTED]", secrets["auth_token"])

		database := secrets["database"].(map[string]any)
		assert.Equal(t, "[REDACTED]", database["password"])
		assert.Equal(t, "localhost", database["host"])
	})

	t.Run("LogLevels", func(t *testing.T) {
		// Test different log levels
		levels := []string{"info", "warn", "error"}

		for _, level := range levels {
			err := agent.eventEmitter.SendLog(level, "Test "+level+" message", map[string]any{
				"level": level,
			})
			require.NoError(t, err)
		}

		// Verify all logs were recorded
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 3)

		for i, log := range logs {
			assert.Equal(t, levels[i], log.Level)
			assert.Equal(t, "Test "+levels[i]+" message", log.Message)
		}
	})

	t.Run("LogMessageSize", func(t *testing.T) {
		// Test log message size limits
		largeMessage := strings.Repeat("A", 1000) // Large message
		fields := map[string]any{
			"large_data": strings.Repeat("B", 1000), // Large field
		}

		err := agent.eventEmitter.SendLog("info", largeMessage, fields)
		require.NoError(t, err)

		// Verify log was recorded
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)
		assert.Equal(t, largeMessage, logs[0].Message)
		assert.Equal(t, strings.Repeat("B", 1000), logs[0].Fields["large_data"])
	})

	t.Run("EmptyFields", func(t *testing.T) {
		// Test logging with empty fields
		err := agent.eventEmitter.SendLog("info", "Test message", nil)
		require.NoError(t, err)

		// Verify log was recorded
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)
		assert.Equal(t, "Test message", logs[0].Message)
		assert.Nil(t, logs[0].Fields)
	})

	t.Run("EmptyMessage", func(t *testing.T) {
		// Test logging with empty message
		err := agent.eventEmitter.SendLog("info", "", map[string]any{
			"key": "value",
		})
		require.NoError(t, err)

		// Verify log was recorded
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)
		assert.Equal(t, "", logs[0].Message)
		assert.Equal(t, "value", logs[0].Fields["key"])
	})
}

// TestSecretRedactionPatterns tests various secret redaction patterns
func TestSecretRedactionPatterns(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

	t.Run("TokenRedaction", func(t *testing.T) {
		// Test various token patterns
		fields := map[string]any{
			"access_token":    "token123",
			"refresh_token":   "token456",
			"bearer_token":     "token789",
			"oauth_token":      "token000",
			"jwt_token":        "token111",
			"session_token":   "token222",
			"api_token":       "token333",
			"auth_token":      "token444",
			"user_token":      "token555",
			"service_token":   "token666",
		}

		err := agent.eventEmitter.SendLog("info", "Test token redaction", fields)
		require.NoError(t, err)

		// Verify all tokens were redacted
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)

		redactedFields := logs[0].Fields
		for key := range fields {
			assert.Equal(t, "[REDACTED]", redactedFields[key], "Field %s should be redacted", key)
		}
	})

	t.Run("KeyRedaction", func(t *testing.T) {
		// Test various key patterns
		fields := map[string]any{
			"api_key":         "key123",
			"secret_key":      "key456",
			"private_key":     "key789",
			"public_key":      "key000",
			"encryption_key":  "key111",
			"decryption_key":  "key222",
			"master_key":      "key333",
			"session_key":     "key444",
			"auth_key":        "key555",
			"access_key":      "key666",
		}

		err := agent.eventEmitter.SendLog("info", "Test key redaction", fields)
		require.NoError(t, err)

		// Verify all keys were redacted
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)

		redactedFields := logs[0].Fields
		for key := range fields {
			assert.Equal(t, "[REDACTED]", redactedFields[key], "Field %s should be redacted", key)
		}
	})

	t.Run("SecretRedaction", func(t *testing.T) {
		// Test various secret patterns
		fields := map[string]any{
			"api_secret":      "secret123",
			"client_secret":   "secret456",
			"shared_secret":   "secret789",
			"master_secret":   "secret000",
			"session_secret":  "secret111",
			"auth_secret":     "secret222",
			"database_secret": "secret333",
			"encryption_secret": "secret444",
			"decryption_secret": "secret555",
			"service_secret":  "secret666",
		}

		err := agent.eventEmitter.SendLog("info", "Test secret redaction", fields)
		require.NoError(t, err)

		// Verify all secrets were redacted
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)

		redactedFields := logs[0].Fields
		for key := range fields {
			assert.Equal(t, "[REDACTED]", redactedFields[key], "Field %s should be redacted", key)
		}
	})

	t.Run("NonSecretFields", func(t *testing.T) {
		// Test fields that should NOT be redacted
		fields := map[string]any{
			"user_name":       "john_doe",
			"user_id":         "12345",
			"email":           "john@example.com",
			"phone":           "555-1234",
			"address":         "123 Main St",
			"city":            "New York",
			"state":           "NY",
			"zip":             "10001",
			"country":         "USA",
			"normal_field":    "normal_value",
		}

		err := agent.eventEmitter.SendLog("info", "Test non-secret fields", fields)
		require.NoError(t, err)

		// Verify non-secret fields were NOT redacted
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)

		redactedFields := logs[0].Fields
		for key, expectedValue := range fields {
			assert.Equal(t, expectedValue, redactedFields[key], "Field %s should not be redacted", key)
		}
	})

	t.Run("MixedCaseRedaction", func(t *testing.T) {
		// Test mixed case field names
		fields := map[string]any{
			"API_KEY":         "key123",
			"Auth_Token":      "token456",
			"database_SECRET": "secret789",
			"user_name":       "john_doe",
			"normal_field":    "normal_value",
		}

		err := agent.eventEmitter.SendLog("info", "Test mixed case redaction", fields)
		require.NoError(t, err)

		// Verify case-insensitive redaction
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)

		redactedFields := logs[0].Fields
		assert.Equal(t, "[REDACTED]", redactedFields["API_KEY"])
		assert.Equal(t, "[REDACTED]", redactedFields["Auth_Token"])
		assert.Equal(t, "[REDACTED]", redactedFields["database_SECRET"])
		assert.Equal(t, "john_doe", redactedFields["user_name"])
		assert.Equal(t, "normal_value", redactedFields["normal_field"])
	})

	t.Run("EdgeCaseFieldNames", func(t *testing.T) {
		// Test edge case field names
		fields := map[string]any{
			"key":             "value1", // Should not be redacted (too short)
			"token":           "value2", // Should not be redacted (too short)
			"secret":          "value3", // Should not be redacted (too short)
			"api_key_token":   "value4", // Should be redacted (contains _key)
			"auth_secret_key": "value5", // Should be redacted (contains _secret)
			"user_token_id":   "value6", // Should be redacted (contains _token)
		}

		err := agent.eventEmitter.SendLog("info", "Test edge case field names", fields)
		require.NoError(t, err)

		// Verify edge case redaction
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)

		redactedFields := logs[0].Fields
		assert.Equal(t, "value1", redactedFields["key"])           // Should not be redacted
		assert.Equal(t, "value2", redactedFields["token"])         // Should not be redacted
		assert.Equal(t, "value3", redactedFields["secret"])       // Should not be redacted
		assert.Equal(t, "[REDACTED]", redactedFields["api_key_token"])   // Should be redacted
		assert.Equal(t, "[REDACTED]", redactedFields["auth_secret_key"]) // Should be redacted
		assert.Equal(t, "[REDACTED]", redactedFields["user_token_id"])   // Should be redacted
	})
}

// TestLoggingPerformance tests logging performance characteristics
func TestLoggingPerformance(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

	t.Run("HighVolumeLogging", func(t *testing.T) {
		// Test high volume logging
		for i := 0; i < 1000; i++ {
			err := agent.eventEmitter.SendLog("info", "Test message", map[string]any{
				"index": i,
				"data":  strings.Repeat("A", 100),
			})
			require.NoError(t, err)
		}

		// Verify all logs were recorded
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1000)

		// Verify log content
		for i, log := range logs {
			assert.Equal(t, "info", log.Level)
			assert.Equal(t, "Test message", log.Message)
			assert.Equal(t, i, log.Fields["index"])
			assert.Equal(t, strings.Repeat("A", 100), log.Fields["data"])
		}
	})

	t.Run("LargeFieldLogging", func(t *testing.T) {
		// Test logging with large fields
		largeData := strings.Repeat("B", 10000) // 10KB field
		fields := map[string]any{
			"large_data": largeData,
			"api_key":    "secret123",
			"normal":     "value",
		}

		err := agent.eventEmitter.SendLog("info", "Test large field", fields)
		require.NoError(t, err)

		// Verify log was recorded with redaction
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)

		redactedFields := logs[0].Fields
		assert.Equal(t, largeData, redactedFields["large_data"]) // Should not be redacted
		assert.Equal(t, "[REDACTED]", redactedFields["api_key"]) // Should be redacted
		assert.Equal(t, "value", redactedFields["normal"])       // Should not be redacted
	})

	t.Run("ConcurrentLogging", func(t *testing.T) {
		// Test concurrent logging
		done := make(chan bool, 10)

		for i := 0; i < 10; i++ {
			go func(index int) {
				for j := 0; j < 100; j++ {
					err := agent.eventEmitter.SendLog("info", "Concurrent message", map[string]any{
						"goroutine": index,
						"iteration": j,
					})
					require.NoError(t, err)
				}
				done <- true
			}(i)
		}

		// Wait for all goroutines to complete
		for i := 0; i < 10; i++ {
			<-done
		}

		// Verify all logs were recorded
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1000) // 10 goroutines * 100 iterations
	})
}

// TestLoggingErrorHandling tests logging error handling
func TestLoggingErrorHandling(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

	t.Run("InvalidLogLevel", func(t *testing.T) {
		// Test with invalid log level
		err := agent.eventEmitter.SendLog("invalid_level", "Test message", map[string]any{
			"key": "value",
		})
		require.NoError(t, err) // Mock handles gracefully

		// Verify log was recorded
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)
		assert.Equal(t, "invalid_level", logs[0].Level)
	})

	t.Run("NilFields", func(t *testing.T) {
		// Test with nil fields
		err := agent.eventEmitter.SendLog("info", "Test message", nil)
		require.NoError(t, err)

		// Verify log was recorded
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)
		assert.Equal(t, "Test message", logs[0].Message)
		assert.Nil(t, logs[0].Fields)
	})

	t.Run("EmptyFields", func(t *testing.T) {
		// Test with empty fields
		err := agent.eventEmitter.SendLog("info", "Test message", map[string]any{})
		require.NoError(t, err)

		// Verify log was recorded
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)
		assert.Equal(t, "Test message", logs[0].Message)
		assert.Empty(t, logs[0].Fields)
	})

	t.Run("ComplexFields", func(t *testing.T) {
		// Test with complex field types
		fields := map[string]any{
			"string":     "value",
			"int":        123,
			"float":      45.67,
			"bool":       true,
			"array":      []string{"a", "b", "c"},
			"map":        map[string]any{"nested": "value"},
			"api_key":    "secret123",
			"normal":     "value",
		}

		err := agent.eventEmitter.SendLog("info", "Test complex fields", fields)
		require.NoError(t, err)

		// Verify log was recorded with redaction
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)

		redactedFields := logs[0].Fields
		assert.Equal(t, "value", redactedFields["string"])
		assert.Equal(t, 123, redactedFields["int"])
		assert.Equal(t, 45.67, redactedFields["float"])
		assert.Equal(t, true, redactedFields["bool"])
		assert.Equal(t, []string{"a", "b", "c"}, redactedFields["array"])
		assert.Equal(t, "[REDACTED]", redactedFields["api_key"])
		assert.Equal(t, "value", redactedFields["normal"])
	})
}

// TestLoggingIntegration tests logging integration with other components
func TestLoggingIntegration(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

	t.Run("LoggingWithEvents", func(t *testing.T) {
		// Test logging alongside event emission
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")

		// Send log
		err := agent.eventEmitter.SendLog("info", "Processing command", map[string]any{
			"task_id": cmd.TaskID,
			"action":  cmd.Action,
		})
		require.NoError(t, err)

		// Send event
		err = agent.eventEmitter.SendErrorEvent(cmd, "test_error", "Test error message")
		require.NoError(t, err)

		// Verify both were recorded
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)
		assert.Equal(t, "Processing command", logs[0].Message)

		events := agent.eventEmitter.(*MockEventEmitter).GetEvents()
		assert.Len(t, events, 1)
		assert.Equal(t, "error", events[0].Event)
	})

	t.Run("LoggingWithArtifacts", func(t *testing.T) {
		// Test logging alongside artifact production
		cmd := tu.CreateTestCommand(protocol.ActionIntake, "T-001")
		artifact := protocol.Artifact{
			Path:   "test.txt",
			SHA256: "sha256:test123",
			Size:   100,
		}

		// Send log
		err := agent.eventEmitter.SendLog("info", "Producing artifact", map[string]any{
			"artifact_path": artifact.Path,
			"artifact_size": artifact.Size,
		})
		require.NoError(t, err)

		// Send artifact event
		err = agent.eventEmitter.SendArtifactProducedEvent(cmd, artifact)
		require.NoError(t, err)

		// Verify both were recorded
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)
		assert.Equal(t, "Producing artifact", logs[0].Message)

		events := agent.eventEmitter.(*MockEventEmitter).GetEvents()
		assert.Len(t, events, 1)
		assert.Equal(t, "artifact.produced", events[0].Event)
	})

	t.Run("LoggingWithHeartbeats", func(t *testing.T) {
		// Test logging alongside heartbeats
		// Send log
		err := agent.eventEmitter.SendLog("info", "Agent starting", map[string]any{
			"status": "starting",
		})
		require.NoError(t, err)

		// Send another log
		err = agent.eventEmitter.SendLog("info", "Agent ready", map[string]any{
			"status": "ready",
		})
		require.NoError(t, err)

		// Verify logs were recorded
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 2)
		assert.Equal(t, "Agent starting", logs[0].Message)
		assert.Equal(t, "Agent ready", logs[1].Message)
	})

	t.Run("LoggingWithReceipts", func(t *testing.T) {
		// Test logging alongside receipt operations
		receipt := tu.CreateTestReceipt("T-001", "implement", "ik-test123")

		// Send log
		err := agent.eventEmitter.SendLog("info", "Saving receipt", map[string]any{
			"task_id": receipt.TaskID,
			"step":    receipt.Step,
			"api_key": "secret123", // Should be redacted
		})
		require.NoError(t, err)

		// Save receipt
		err = agent.receiptStore.SaveReceipt("/receipts/T-001/implement-1.json", receipt)
		require.NoError(t, err)

		// Verify log was recorded with redaction
		logs := agent.eventEmitter.(*MockEventEmitter).GetLogs()
		assert.Len(t, logs, 1)
		assert.Equal(t, "Saving receipt", logs[0].Message)
		assert.Equal(t, "T-001", logs[0].Fields["task_id"])
		assert.Equal(t, 1, logs[0].Fields["step"])
		assert.Equal(t, "[REDACTED]", logs[0].Fields["api_key"])
	})
}

// TestLoggingCallLogging tests call logging functionality
func TestLoggingCallLogging(t *testing.T) {
	tu := NewTestUtilities(t)
	defer tu.Cleanup()

	agent := tu.CreateTestAgent(protocol.AgentTypeOrchestration)

	t.Run("CallLogContent", func(t *testing.T) {
		// Send multiple logs
		agent.eventEmitter.SendLog("info", "Message 1", map[string]any{"key1": "value1"})
		agent.eventEmitter.SendLog("warn", "Message 2", map[string]any{"key2": "value2"})
		agent.eventEmitter.SendLog("error", "Message 3", map[string]any{"key3": "value3"})

		// Get call log
		callLog := agent.eventEmitter.(*MockEventEmitter).GetCallLog()

		// Verify call log contains all operations
		assert.Contains(t, callLog, "SendLog(info, Message 1)")
		assert.Contains(t, callLog, "SendLog(warn, Message 2)")
		assert.Contains(t, callLog, "SendLog(error, Message 3)")
	})

	t.Run("CallLogOrder", func(t *testing.T) {
		// Send logs in specific order
		agent.eventEmitter.SendLog("info", "First", nil)
		agent.eventEmitter.SendLog("info", "Second", nil)
		agent.eventEmitter.SendLog("info", "Third", nil)

		// Get call log
		callLog := agent.eventEmitter.(*MockEventEmitter).GetCallLog()

		// Verify order is preserved
		callLogStr := strings.Join(callLog, "\n")
		firstIndex := strings.Index(callLogStr, "SendLog(info, First)")
		secondIndex := strings.Index(callLogStr, "SendLog(info, Second)")
		thirdIndex := strings.Index(callLogStr, "SendLog(info, Third)")

		assert.True(t, firstIndex < secondIndex, "First should come before Second")
		assert.True(t, secondIndex < thirdIndex, "Second should come before Third")
	})

	t.Run("CallLogWithSecrets", func(t *testing.T) {
		// Send log with secrets
		agent.eventEmitter.SendLog("info", "Test with secrets", map[string]any{
			"api_key": "secret123",
			"normal":  "value",
		})

		// Get call log
		callLog := agent.eventEmitter.(*MockEventEmitter).GetCallLog()

		// Verify call log does not contain secrets
		assert.Contains(t, callLog, "SendLog(info, Test with secrets)")
		assert.NotContains(t, callLog, "secret123")
		assert.Contains(t, callLog, "value")
	})
}
