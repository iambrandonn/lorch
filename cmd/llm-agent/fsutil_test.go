package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockFSProvider(t *testing.T) {
	provider := NewMockFSProvider()

	// Test initial state
	assert.Empty(t, provider.GetWriteLog())
	assert.Empty(t, provider.GetReadLog())
	assert.Empty(t, provider.GetResolveLog())

	// Test SetFile
	provider.SetFile("/workspace/test.txt", "test content")

	// Test ReadFileSafe
	content, err := provider.ReadFileSafe("/workspace/test.txt", 1024)
	require.NoError(t, err)
	assert.Equal(t, "test content", content)

	// Test ReadFileSafe with size limit
	content, err = provider.ReadFileSafe("/workspace/test.txt", 5)
	require.NoError(t, err)
	assert.Equal(t, "test ", content) // Truncated to 5 bytes

	// Test ReadFileSafe with file not found
	_, err = provider.ReadFileSafe("/workspace/nonexistent.txt", 1024)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file not found")

	// Test WriteArtifactAtomic
	contentBytes := []byte("new content")
	artifact, err := provider.WriteArtifactAtomic("/workspace", "test.txt", contentBytes)
	require.NoError(t, err)
	assert.Equal(t, "test.txt", artifact.Path)
	assert.NotEmpty(t, artifact.SHA256)
	assert.Equal(t, int64(len(contentBytes)), artifact.Size)
	assert.Contains(t, artifact.SHA256, "sha256:")

	// Test ResolveWorkspacePath
	path, err := provider.ResolveWorkspacePath("/workspace", "test.txt")
	require.NoError(t, err)
	assert.Equal(t, "/workspace/test.txt", path)

	// Test logs
	writeLog := provider.GetWriteLog()
	assert.Contains(t, writeLog, fmt.Sprintf("WriteArtifactAtomic(/workspace, test.txt, %d bytes)", len(contentBytes)))

	readLog := provider.GetReadLog()
	assert.Contains(t, readLog, "ReadFileSafe(/workspace/test.txt, 1024)")
	assert.Contains(t, readLog, "ReadFileSafe(/workspace/test.txt, 5)")
	assert.Contains(t, readLog, "ReadFileSafe(/workspace/nonexistent.txt, 1024)")

	resolveLog := provider.GetResolveLog()
	assert.Contains(t, resolveLog, "ResolveWorkspacePath(/workspace, test.txt)")
}

func TestMockFSProviderClearLogs(t *testing.T) {
	provider := NewMockFSProvider()

	// Make some operations
	provider.SetFile("/test.txt", "content")
	provider.ReadFileSafe("/test.txt", 1024)
	provider.WriteArtifactAtomic("/workspace", "test.txt", []byte("content"))
	provider.ResolveWorkspacePath("/workspace", "test.txt")

	// Verify logs have entries
	assert.Len(t, provider.GetWriteLog(), 1)
	assert.Len(t, provider.GetReadLog(), 1)
	assert.Len(t, provider.GetResolveLog(), 1)

	// Clear logs
	provider.ClearLogs()

	// Verify logs are empty
	assert.Empty(t, provider.GetWriteLog())
	assert.Empty(t, provider.GetReadLog())
	assert.Empty(t, provider.GetResolveLog())
}

func TestMockFSProviderMultipleFiles(t *testing.T) {
	provider := NewMockFSProvider()

	// Set multiple files
	provider.SetFile("/workspace/file1.txt", "content1")
	provider.SetFile("/workspace/file2.txt", "content2")
	provider.SetFile("/workspace/file3.txt", "content3")

	// Test reading each file
	content1, err := provider.ReadFileSafe("/workspace/file1.txt", 1024)
	require.NoError(t, err)
	assert.Equal(t, "content1", content1)

	content2, err := provider.ReadFileSafe("/workspace/file2.txt", 1024)
	require.NoError(t, err)
	assert.Equal(t, "content2", content2)

	content3, err := provider.ReadFileSafe("/workspace/file3.txt", 1024)
	require.NoError(t, err)
	assert.Equal(t, "content3", content3)
}

func TestMockFSProviderWriteArtifactAtomic(t *testing.T) {
	provider := NewMockFSProvider()

	// Test writing different content
	content1 := []byte("short")
	artifact1, err := provider.WriteArtifactAtomic("/workspace", "short.txt", content1)
	require.NoError(t, err)
	assert.Equal(t, "short.txt", artifact1.Path)
	assert.Equal(t, int64(len(content1)), artifact1.Size)

	content2 := []byte("this is a much longer content that should have a different checksum")
	artifact2, err := provider.WriteArtifactAtomic("/workspace", "long.txt", content2)
	require.NoError(t, err)
	assert.Equal(t, "long.txt", artifact2.Path)
	assert.Equal(t, int64(len(content2)), artifact2.Size)

	// Checksums should be different
	assert.NotEqual(t, artifact1.SHA256, artifact2.SHA256)

	// Both should start with "sha256:"
	assert.Contains(t, artifact1.SHA256, "sha256:")
	assert.Contains(t, artifact2.SHA256, "sha256:")
}

func TestMockFSProviderResolveWorkspacePath(t *testing.T) {
	provider := NewMockFSProvider()

	// Test different workspace and relative path combinations
	path1, err := provider.ResolveWorkspacePath("/workspace", "file.txt")
	require.NoError(t, err)
	assert.Equal(t, "/workspace/file.txt", path1)

	path2, err := provider.ResolveWorkspacePath("/workspace", "subdir/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "/workspace/subdir/file.txt", path2)

	path3, err := provider.ResolveWorkspacePath("/workspace", "deep/nested/path/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "/workspace/deep/nested/path/file.txt", path3)

	// Test resolve log
	resolveLog := provider.GetResolveLog()
	assert.Contains(t, resolveLog, "ResolveWorkspacePath(/workspace, file.txt)")
	assert.Contains(t, resolveLog, "ResolveWorkspacePath(/workspace, subdir/file.txt)")
	assert.Contains(t, resolveLog, "ResolveWorkspacePath(/workspace, deep/nested/path/file.txt)")
}
