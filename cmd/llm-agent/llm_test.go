package main

import (
	"context"
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
	config := DefaultLLMConfig("claude")
	caller := NewRealLLMCaller(config)

	ctx := context.Background()
	prompt := "Test prompt"

	// This will return a stub response for now
	response, err := caller.Call(ctx, prompt)
	require.NoError(t, err)
	assert.Contains(t, response, "LLM response to:")
	assert.Contains(t, response, prompt)
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
