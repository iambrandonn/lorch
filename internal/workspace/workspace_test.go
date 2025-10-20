package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitialize_CreatesAllDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	err := Initialize(tmpDir)
	require.NoError(t, err)

	// Verify all required directories exist
	expectedDirs := []string{
		"state",
		"events",
		"receipts",
		"logs",
		"snapshots",
		"reviews",
		"spec_notes",
		"transcripts",
	}

	for _, dir := range expectedDirs {
		path := filepath.Join(tmpDir, dir)
		info, err := os.Stat(path)
		require.NoError(t, err, "Directory %s should exist", dir)
		assert.True(t, info.IsDir(), "%s should be a directory", dir)

		// Verify permissions are 0700 (owner only)
		assert.Equal(t, os.FileMode(0700), info.Mode().Perm(),
			"Directory %s should have 0700 permissions", dir)
	}
}

func TestInitialize_IdempotentCalls(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize once
	err := Initialize(tmpDir)
	require.NoError(t, err)

	// Initialize again - should not error
	err = Initialize(tmpDir)
	assert.NoError(t, err, "Second initialize should be idempotent")
}

func TestInitialize_InvalidPath(t *testing.T) {
	// Try to initialize in a path that can't be created
	err := Initialize("/nonexistent/deeply/nested/path")
	assert.Error(t, err)
}

func TestIsInitialized_True(t *testing.T) {
	tmpDir := t.TempDir()
	err := Initialize(tmpDir)
	require.NoError(t, err)

	initialized, err := IsInitialized(tmpDir)
	require.NoError(t, err)
	assert.True(t, initialized)
}

func TestIsInitialized_False(t *testing.T) {
	tmpDir := t.TempDir()

	initialized, err := IsInitialized(tmpDir)
	require.NoError(t, err)
	assert.False(t, initialized)
}

func TestIsInitialized_PartiallyInitialized(t *testing.T) {
	tmpDir := t.TempDir()

	// Create only some directories
	err := os.Mkdir(filepath.Join(tmpDir, "state"), 0700)
	require.NoError(t, err)

	initialized, err := IsInitialized(tmpDir)
	require.NoError(t, err)
	assert.False(t, initialized, "Should not be considered initialized if missing directories")
}

func TestGetRequiredDirectories(t *testing.T) {
	dirs := GetRequiredDirectories()

	// Should contain all the required directories from MASTER-SPEC §5.2
	expected := []string{
		"state",
		"events",
		"receipts",
		"logs",
		"snapshots",
		"reviews",
		"spec_notes",
		"transcripts",
	}

	assert.ElementsMatch(t, expected, dirs)
}
