package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateDefault(t *testing.T) {
	cfg := GenerateDefault()

	// Basic structure validation
	assert.Equal(t, "1.0", cfg.Version)
	assert.Equal(t, ".", cfg.WorkspaceRoot)

	// Policy defaults
	assert.Equal(t, 1, cfg.Policy.Concurrency)
	assert.Equal(t, 262144, cfg.Policy.MessageMaxBytes)
	assert.Equal(t, 1073741824, cfg.Policy.ArtifactMaxBytes)
	assert.True(t, cfg.Policy.StrictVersionPinning)
	assert.False(t, cfg.Policy.ParallelReviews)
	assert.True(t, cfg.Policy.RedactSecretsInLogs)

	// Retry policy
	assert.Equal(t, 3, cfg.Policy.Retry.MaxAttempts)
	assert.Equal(t, 1000, cfg.Policy.Retry.Backoff.InitialMs)
	assert.Equal(t, 60000, cfg.Policy.Retry.Backoff.MaxMs)
	assert.Equal(t, 2.0, cfg.Policy.Retry.Backoff.Multiplier)
	assert.Equal(t, "full", cfg.Policy.Retry.Backoff.Jitter)

	// Agent configs
	require.NotNil(t, cfg.Agents.Builder)
	assert.Equal(t, []string{"claude"}, cfg.Agents.Builder.Cmd)
	assert.Equal(t, 10, cfg.Agents.Builder.HeartbeatIntervalS)
	assert.Equal(t, "builder", cfg.Agents.Builder.Env["CLAUDE_AGENT_ROLE"])

	require.NotNil(t, cfg.Agents.Reviewer)
	assert.Equal(t, "reviewer", cfg.Agents.Reviewer.Env["CLAUDE_AGENT_ROLE"])

	require.NotNil(t, cfg.Agents.SpecMaintainer)
	assert.Equal(t, "spec_maintainer", cfg.Agents.SpecMaintainer.Env["CLAUDE_AGENT_ROLE"])

	require.NotNil(t, cfg.Agents.Orchestration)
	assert.True(t, cfg.Agents.Orchestration.Enabled)
	assert.Equal(t, "orchestration", cfg.Agents.Orchestration.Env["CLAUDE_AGENT_ROLE"])

	// Tasks should be empty array, not nil
	assert.NotNil(t, cfg.Tasks)
	assert.Empty(t, cfg.Tasks)
}

func TestGenerateDefaultMatchesGoldenFile(t *testing.T) {
	// Load golden file
	goldenPath := filepath.Join("..", "..", "testdata", "golden_config.json")
	goldenBytes, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "Failed to read golden config file")

	var goldenCfg Config
	err = json.Unmarshal(goldenBytes, &goldenCfg)
	require.NoError(t, err, "Failed to parse golden config")

	// Generate default
	generatedCfg := GenerateDefault()

	// Compare as JSON to ignore struct vs map differences
	generatedJSON, err := json.MarshalIndent(generatedCfg, "", "  ")
	require.NoError(t, err)

	goldenJSON, err := json.MarshalIndent(goldenCfg, "", "  ")
	require.NoError(t, err)

	assert.JSONEq(t, string(goldenJSON), string(generatedJSON),
		"Generated config should match golden file")
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := GenerateDefault()
	err := cfg.Validate()
	assert.NoError(t, err, "Default config should be valid")
}

func TestValidate_MissingVersion(t *testing.T) {
	cfg := GenerateDefault()
	cfg.Version = ""
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "version")
}

func TestValidate_MissingBuilder(t *testing.T) {
	cfg := GenerateDefault()
	cfg.Agents.Builder = nil
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "builder")
}

func TestValidate_MissingReviewer(t *testing.T) {
	cfg := GenerateDefault()
	cfg.Agents.Reviewer = nil
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reviewer")
}

func TestValidate_MissingSpecMaintainer(t *testing.T) {
	cfg := GenerateDefault()
	cfg.Agents.SpecMaintainer = nil
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "spec_maintainer")
}

func TestValidate_InvalidConcurrency(t *testing.T) {
	cfg := GenerateDefault()
	cfg.Policy.Concurrency = 0
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "concurrency")
}

func TestValidate_InvalidConcurrencyGreaterThanOne(t *testing.T) {
	cfg := GenerateDefault()
	cfg.Policy.Concurrency = 2
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "concurrency")
	assert.Contains(t, err.Error(), "must be 1")
}

func TestValidate_EmptyAgentCmd(t *testing.T) {
	cfg := GenerateDefault()
	cfg.Agents.Builder.Cmd = []string{}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cmd")
}

func TestLoadFromFile_ValidFile(t *testing.T) {
	goldenPath := filepath.Join("..", "..", "testdata", "golden_config.json")
	cfg, err := LoadFromFile(goldenPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "1.0", cfg.Version)
	assert.NotNil(t, cfg.Agents.Builder)
}

func TestLoadFromFile_NonExistent(t *testing.T) {
	cfg, err := LoadFromFile("/nonexistent/path/config.json")
	assert.Error(t, err)
	assert.Nil(t, cfg)
}

func TestLoadFromFile_InvalidJSON(t *testing.T) {
	// Create temp file with invalid JSON
	tmpDir := t.TempDir()
	invalidFile := filepath.Join(tmpDir, "invalid.json")
	err := os.WriteFile(invalidFile, []byte("{invalid json"), 0600)
	require.NoError(t, err)

	cfg, err := LoadFromFile(invalidFile)
	assert.Error(t, err)
	assert.Nil(t, cfg)
}

func TestSaveToFile(t *testing.T) {
	cfg := GenerateDefault()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "lorch.json")

	err := cfg.SaveToFile(configPath)
	require.NoError(t, err)

	// Verify file exists and can be loaded
	loaded, err := LoadFromFile(configPath)
	require.NoError(t, err)

	// Compare
	assert.Equal(t, cfg.Version, loaded.Version)
	assert.Equal(t, cfg.Policy.Concurrency, loaded.Policy.Concurrency)

	// Verify file permissions (should be 0600)
	info, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}
