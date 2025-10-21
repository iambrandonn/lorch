package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLogLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		want     slog.Level
		wantName string
		wantErr  bool
	}{
		{"", slog.LevelInfo, "info", false},
		{"INFO", slog.LevelInfo, "info", false},
		{"debug", slog.LevelDebug, "debug", false},
		{"warn", slog.LevelWarn, "warn", false},
		{"warning", slog.LevelWarn, "warn", false},
		{"error", slog.LevelError, "error", false},
		{"err", slog.LevelError, "error", false},
		{"verbose", slog.LevelInfo, "", true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			level, name, err := parseLogLevel(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, level)
			assert.Equal(t, tc.wantName, name)
		})
	}
}

func TestNormalizeRole(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"builder":         "builder",
		"Builder":         "builder",
		"reviewer":        "reviewer",
		"spec-maintainer": "spec_maintainer",
		"spec_maintainer": "spec_maintainer",
		"Orchestration":   "orchestration",
		"orchestration  ": "orchestration",
		"unknown":         "",
		"":                "",
		" implementer  ":  "",
	}

	for input, want := range tests {
		input := input
		want := want
		t.Run(strings.ReplaceAll(input, " ", "_"), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, normalizeRole(input))
		})
	}
}

func TestConfigNormalizeAndValidate(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfg := Config{
		Role:      "Orchestration",
		Workspace: tmpDir,
		Binary:    dummyBinary(),
		LogLevel:  "debug",
		BaseEnv:   []string{"FOO=bar"},
	}

	require.NoError(t, cfg.NormalizeAndValidate())
	assert.Equal(t, "orchestration", cfg.normalizedRole)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, tmpDir, cfg.Workspace)
}

func TestConfigNormalizeAndValidateErrors(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfg := Config{
		Role:      "unknown",
		Workspace: tmpDir,
		Binary:    dummyBinary(),
	}
	err := cfg.NormalizeAndValidate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported role")

	cfg = Config{
		Role:      "builder",
		Workspace: filepath.Join(tmpDir, "missing"),
		Binary:    dummyBinary(),
	}
	err = cfg.NormalizeAndValidate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workspace check failed")

	cfg = Config{
		Role:     "builder",
		Binary:   dummyBinary(),
		LogLevel: "info",
		BaseEnv:  os.Environ(),
	}
	err = cfg.NormalizeAndValidate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workspace is required")
}

func TestBuildCommandSetsEnvAndDir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfg := Config{
		Role:        "builder",
		Workspace:   tmpDir,
		Binary:      dummyBinary(),
		LogLevel:    "warn",
		FixturePath: "fixtures/sample.json",
		Args:        []string{"--mode", "ndjson"},
		BaseEnv:     []string{"FOO=bar"},
	}

	require.NoError(t, cfg.NormalizeAndValidate())

	cmd, err := cfg.BuildCommand(context.Background())
	require.NoError(t, err)

	require.Equal(t, dummyBinary(), cmd.Path)
	assert.Equal(t, []string{dummyBinary(), "--mode", "ndjson"}, cmd.Args)
	assert.Equal(t, tmpDir, cmd.Dir)

	env := envToMap(cmd.Env)
	assert.Equal(t, "builder", env["CLAUDE_ROLE"])
	assert.Equal(t, "warn", env["CLAUDE_LOG_LEVEL"])
	assert.Equal(t, tmpDir, env["CLAUDE_WORKSPACE"])
	expectedFixture, _ := filepath.Abs("fixtures/sample.json")
	assert.Equal(t, expectedFixture, env["CLAUDE_FIXTURE"])
	assert.Equal(t, "bar", env["FOO"])
}

func envToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if kv == "" {
			continue
		}
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}

func dummyBinary() string {
	exe, err := os.Executable()
	if err != nil {
		return "echo"
	}
	return exe
}
