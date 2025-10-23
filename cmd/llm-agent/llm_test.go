package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMConfig(t *testing.T) {
	config := DefaultLLMConfig("claude")

	assert.Equal(t, "claude", config.CLIPath)
	assert.Equal(t, 180*time.Second, config.Timeout)
	assert.Equal(t, int64(1024*1024), config.MaxOutputBytes)
}

func TestRealLLMCaller(t *testing.T) {
	// Create a mock LLM CLI script for testing
	mockScript := createMockLLMScript(t)
	defer os.Remove(mockScript)

	config := DefaultLLMConfig(mockScript)
	caller := NewRealLLMCaller(config)

	ctx := context.Background()
	prompt := "Test prompt"

	response, err := caller.Call(ctx, prompt)
	require.NoError(t, err)
	// Trim newlines from the response
	response = strings.TrimSpace(response)
	assert.Equal(t, "Mock LLM response to: Test prompt", response)
}

func TestRealLLMCallerTimeout(t *testing.T) {
	// Create a slow mock LLM CLI script for testing
	slowScript := createSlowMockLLMScript(t)
	defer os.Remove(slowScript)

	config := LLMConfig{
		CLIPath:        slowScript,
		Timeout:        100 * time.Millisecond, // Very short timeout
		MaxOutputBytes: 1024,
	}
	caller := NewRealLLMCaller(config)

	ctx := context.Background()
	prompt := "Test prompt"

	_, err := caller.Call(ctx, prompt)
	require.Error(t, err)
	// The error could be either context deadline exceeded or signal killed
	assert.True(t, strings.Contains(err.Error(), "context deadline exceeded") ||
		strings.Contains(err.Error(), "signal: killed"))
}

func TestRealLLMCallerSizeLimit(t *testing.T) {
	// Create a mock LLM CLI script that outputs large content
	largeScript := createLargeOutputMockLLMScript(t)
	defer os.Remove(largeScript)

	config := LLMConfig{
		CLIPath:        largeScript,
		Timeout:        5 * time.Second,
		MaxOutputBytes: 100, // Very small limit
	}
	caller := NewRealLLMCaller(config)

	ctx := context.Background()
	prompt := "Test prompt"

	_, err := caller.Call(ctx, prompt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds size limit")
}

func TestRealLLMCallerStderrPassthrough(t *testing.T) {
	// Create a mock LLM CLI script that writes to stderr
	stderrScript := createStderrMockLLMScript(t)
	defer os.Remove(stderrScript)

	config := DefaultLLMConfig(stderrScript)
	caller := NewRealLLMCaller(config)

	ctx := context.Background()
	prompt := "Test prompt"

	response, err := caller.Call(ctx, prompt)
	require.NoError(t, err)
	// Trim newlines from the response
	response = strings.TrimSpace(response)
	assert.Equal(t, "Mock LLM response", response)
	// Note: stderr output is logged but doesn't affect the response
}

func TestMockLLMCaller(t *testing.T) {
	caller := NewMockLLMCaller()

	// Test initial state
	assert.Equal(t, 0, caller.CallCount())

	// Test with no responses set
	ctx := context.Background()
	response, err := caller.Call(ctx, "test prompt")
	require.NoError(t, err)
	assert.Contains(t, response, "plan_file")
	assert.Contains(t, response, "Mock task")
	assert.Equal(t, 1, caller.CallCount())

	// Test with custom response
	caller.SetResponse("custom prompt", "custom response")
	response, err = caller.Call(ctx, "custom prompt")
	require.NoError(t, err)
	assert.Equal(t, "custom response", response)
	assert.Equal(t, 2, caller.CallCount())

	// Test with different prompt (should get default)
	response, err = caller.Call(ctx, "different prompt")
	require.NoError(t, err)
	assert.Contains(t, response, "plan_file")
	assert.Equal(t, 3, caller.CallCount())
}

func TestMockLLMCallerCallCount(t *testing.T) {
	caller := NewMockLLMCaller()

	ctx := context.Background()

	// Make multiple calls
	caller.Call(ctx, "prompt1")
	caller.Call(ctx, "prompt2")
	caller.Call(ctx, "prompt3")

	assert.Equal(t, 3, caller.CallCount())
}

func TestMockLLMCallerMultipleResponses(t *testing.T) {
	caller := NewMockLLMCaller()

	ctx := context.Background()

	// Set multiple responses
	caller.SetResponse("prompt1", "response1")
	caller.SetResponse("prompt2", "response2")

	// Test each response
	response1, err := caller.Call(ctx, "prompt1")
	require.NoError(t, err)
	assert.Equal(t, "response1", response1)

	response2, err := caller.Call(ctx, "prompt2")
	require.NoError(t, err)
	assert.Equal(t, "response2", response2)

	// Test unknown prompt
	response3, err := caller.Call(ctx, "unknown")
	require.NoError(t, err)
	assert.Contains(t, response3, "plan_file") // Default response
}

// Helper functions to create mock LLM CLI scripts for testing

func createMockLLMScript(t *testing.T) string {
	script := `#!/bin/bash
# Read input from stdin
input=$(cat)
echo "Mock LLM response to: $input"
`
	return createTempScript(t, script)
}

func createSlowMockLLMScript(t *testing.T) string {
	script := `#!/bin/bash
# Read input from stdin
input=$(cat)
sleep 1  # Sleep for 1 second (longer than test timeout)
echo "Mock LLM response to: $input"
`
	return createTempScript(t, script)
}

func createLargeOutputMockLLMScript(t *testing.T) string {
	script := `#!/bin/bash
# Read input from stdin
input=$(cat)
# Generate large output (200 characters)
printf "A%.0s" {1..200}
echo ""
`
	return createTempScript(t, script)
}

func createStderrMockLLMScript(t *testing.T) string {
	script := `#!/bin/bash
# Read input from stdin
input=$(cat)
# Write to stderr
echo "Debug info: processing $input" >&2
echo "Mock LLM response"
`
	return createTempScript(t, script)
}

func createTempScript(t *testing.T, content string) string {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "mock-llm-*.sh")
	require.NoError(t, err)
	defer tmpFile.Close()

	// Write the script content
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)

	// Make it executable
	err = os.Chmod(tmpFile.Name(), 0755)
	require.NoError(t, err)

	return tmpFile.Name()
}
