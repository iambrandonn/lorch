package main

import (
	"fmt"
	"os"
	"path/filepath"
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

func TestMockFSProviderArtifactSizeCap(t *testing.T) {
	provider := NewMockFSProvider()

	// Test normal size (should pass)
	normalContent := []byte("normal content")
	artifact, err := provider.WriteArtifactAtomic("/workspace", "normal.txt", normalContent)
	require.NoError(t, err)
	assert.Equal(t, "normal.txt", artifact.Path)
	assert.Equal(t, int64(len(normalContent)), artifact.Size)

	// Test size at the limit (1 GiB)
	limitContent := make([]byte, 1024*1024*1024) // 1 GiB
	for i := range limitContent {
		limitContent[i] = byte(i % 256)
	}
	artifact, err = provider.WriteArtifactAtomic("/workspace", "limit.txt", limitContent)
	require.NoError(t, err)
	assert.Equal(t, "limit.txt", artifact.Path)
	assert.Equal(t, int64(len(limitContent)), artifact.Size)

	// Test size exceeding the limit (1 GiB + 1 byte)
	oversizedContent := make([]byte, 1024*1024*1024+1) // 1 GiB + 1 byte
	for i := range oversizedContent {
		oversizedContent[i] = byte(i % 256)
	}
	_, err = provider.WriteArtifactAtomic("/workspace", "oversized.txt", oversizedContent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "artifact exceeds size cap")
	assert.Contains(t, err.Error(), "1073741825 > 1073741824 bytes")
}

func TestMockFSProviderArtifactChecksum(t *testing.T) {
	provider := NewMockFSProvider()

	// Test that checksums are consistent for same content
	content1 := []byte("test content")
	artifact1, err := provider.WriteArtifactAtomic("/workspace", "file1.txt", content1)
	require.NoError(t, err)

	artifact2, err := provider.WriteArtifactAtomic("/workspace", "file2.txt", content1)
	require.NoError(t, err)

	// Same content should produce same checksum
	assert.Equal(t, artifact1.SHA256, artifact2.SHA256)
	assert.Equal(t, artifact1.Size, artifact2.Size)

	// Test that different content produces different checksums
	content2 := []byte("different content")
	artifact3, err := provider.WriteArtifactAtomic("/workspace", "file3.txt", content2)
	require.NoError(t, err)

	assert.NotEqual(t, artifact1.SHA256, artifact3.SHA256)
	assert.NotEqual(t, artifact1.Size, artifact3.Size)

	// Verify checksum format
	assert.Contains(t, artifact1.SHA256, "sha256:")
	assert.Contains(t, artifact2.SHA256, "sha256:")
	assert.Contains(t, artifact3.SHA256, "sha256:")
}

func TestMockFSProviderArtifactAtomicWrite(t *testing.T) {
	provider := NewMockFSProvider()

	// Test writing to nested directory
	content := []byte("nested content")
	artifact, err := provider.WriteArtifactAtomic("/workspace", "deep/nested/path/file.txt", content)
	require.NoError(t, err)
	assert.Equal(t, "deep/nested/path/file.txt", artifact.Path)
	assert.Equal(t, int64(len(content)), artifact.Size)

	// Test writing to root directory
	content2 := []byte("root content")
	artifact2, err := provider.WriteArtifactAtomic("/workspace", "root.txt", content2)
	require.NoError(t, err)
	assert.Equal(t, "root.txt", artifact2.Path)
	assert.Equal(t, int64(len(content2)), artifact2.Size)

	// Verify both files are stored
	assert.Contains(t, provider.files, "/workspace/deep/nested/path/file.txt")
	assert.Contains(t, provider.files, "/workspace/root.txt")
	assert.Equal(t, "nested content", provider.files["/workspace/deep/nested/path/file.txt"])
	assert.Equal(t, "root content", provider.files["/workspace/root.txt"])
}

func TestMockFSProviderArtifactEmptyContent(t *testing.T) {
	provider := NewMockFSProvider()

	// Test writing empty content
	emptyContent := []byte{}
	artifact, err := provider.WriteArtifactAtomic("/workspace", "empty.txt", emptyContent)
	require.NoError(t, err)
	assert.Equal(t, "empty.txt", artifact.Path)
	assert.Equal(t, int64(0), artifact.Size)
	assert.NotEmpty(t, artifact.SHA256) // Empty content still has a checksum
	assert.Contains(t, artifact.SHA256, "sha256:")
}

func TestMockFSProviderArtifactLargeContent(t *testing.T) {
	provider := NewMockFSProvider()

	// Test with moderately large content (10 MB)
	largeContent := make([]byte, 10*1024*1024) // 10 MB
	for i := range largeContent {
		largeContent[i] = byte(i % 256)
	}

	artifact, err := provider.WriteArtifactAtomic("/workspace", "large.txt", largeContent)
	require.NoError(t, err)
	assert.Equal(t, "large.txt", artifact.Path)
	assert.Equal(t, int64(len(largeContent)), artifact.Size)
	assert.NotEmpty(t, artifact.SHA256)
	assert.Contains(t, artifact.SHA256, "sha256:")

	// Verify content is stored correctly
	storedContent := provider.files["/workspace/large.txt"]
	assert.Equal(t, string(largeContent), storedContent)
}

func TestMockFSProviderArtifactWriteLog(t *testing.T) {
	provider := NewMockFSProvider()

	// Test multiple writes and verify logging
	content1 := []byte("content1")
	content2 := []byte("content2")
	content3 := []byte("content3")

	_, err := provider.WriteArtifactAtomic("/workspace", "file1.txt", content1)
	require.NoError(t, err)

	_, err = provider.WriteArtifactAtomic("/workspace", "file2.txt", content2)
	require.NoError(t, err)

	_, err = provider.WriteArtifactAtomic("/workspace", "file3.txt", content3)
	require.NoError(t, err)

	// Verify write log
	writeLog := provider.GetWriteLog()
	assert.Len(t, writeLog, 3)
	assert.Contains(t, writeLog, "WriteArtifactAtomic(/workspace, file1.txt, 8 bytes)")
	assert.Contains(t, writeLog, "WriteArtifactAtomic(/workspace, file2.txt, 8 bytes)")
	assert.Contains(t, writeLog, "WriteArtifactAtomic(/workspace, file3.txt, 8 bytes)")
}

func TestMockFSProviderArtifactSizeCapEdgeCases(t *testing.T) {
	provider := NewMockFSProvider()

	// Test exactly at the limit
	exactLimitContent := make([]byte, 1024*1024*1024) // Exactly 1 GiB
	artifact, err := provider.WriteArtifactAtomic("/workspace", "exact_limit.txt", exactLimitContent)
	require.NoError(t, err)
	assert.Equal(t, int64(1024*1024*1024), artifact.Size)

	// Test just over the limit
	overLimitContent := make([]byte, 1024*1024*1024+1) // 1 GiB + 1 byte
	_, err = provider.WriteArtifactAtomic("/workspace", "over_limit.txt", overLimitContent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "artifact exceeds size cap")

	// Test much larger than limit
	hugeContent := make([]byte, 2*1024*1024*1024) // 2 GiB
	_, err = provider.WriteArtifactAtomic("/workspace", "huge.txt", hugeContent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "artifact exceeds size cap")
}

// Real filesystem tests (using temporary directories)
func TestRealFSProvider(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "lorch-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	provider := NewRealFSProvider(tempDir)

	// Test ResolveWorkspacePath
	path, err := provider.ResolveWorkspacePath(tempDir, "test.txt")
	require.NoError(t, err)
	expectedPath := filepath.Join(tempDir, "test.txt")
	// On macOS, tempDir might be a symlink, so we need to resolve it
	resolvedTempDir, _ := filepath.EvalSymlinks(tempDir)
	expectedPath = filepath.Join(resolvedTempDir, "test.txt")
	assert.Equal(t, expectedPath, path)

	// Test ReadFileSafe with non-existent file
	_, err = provider.ReadFileSafe(filepath.Join(tempDir, "nonexistent.txt"), 1024)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open file")

	// Test WriteArtifactAtomic
	content := []byte("test content")
	artifact, err := provider.WriteArtifactAtomic(tempDir, "test.txt", content)
	require.NoError(t, err)
	assert.Equal(t, "test.txt", artifact.Path)
	assert.Equal(t, int64(len(content)), artifact.Size)
	assert.Contains(t, artifact.SHA256, "sha256:")

	// Verify file was created
	fullPath := filepath.Join(tempDir, "test.txt")
	_, err = os.Stat(fullPath)
	require.NoError(t, err)

	// Test ReadFileSafe with existing file
	readContent, err := provider.ReadFileSafe(fullPath, 1024)
	require.NoError(t, err)
	assert.Equal(t, string(content), readContent)
}

func TestRealFSProviderArtifactSizeCap(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "lorch-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	provider := NewRealFSProvider(tempDir)

	// Test normal size (should pass)
	normalContent := []byte("normal content")
	artifact, err := provider.WriteArtifactAtomic(tempDir, "normal.txt", normalContent)
	require.NoError(t, err)
	assert.Equal(t, "normal.txt", artifact.Path)
	assert.Equal(t, int64(len(normalContent)), artifact.Size)

	// Test size exceeding the limit (1 GiB + 1 byte)
	oversizedContent := make([]byte, 1024*1024*1024+1) // 1 GiB + 1 byte
	for i := range oversizedContent {
		oversizedContent[i] = byte(i % 256)
	}
	_, err = provider.WriteArtifactAtomic(tempDir, "oversized.txt", oversizedContent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "artifact exceeds size cap")
	assert.Contains(t, err.Error(), "1073741825 > 1073741824 bytes")
}

func TestRealFSProviderArtifactChecksum(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "lorch-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	provider := NewRealFSProvider(tempDir)

	// Test that checksums are consistent for same content
	content1 := []byte("test content")
	artifact1, err := provider.WriteArtifactAtomic(tempDir, "file1.txt", content1)
	require.NoError(t, err)

	artifact2, err := provider.WriteArtifactAtomic(tempDir, "file2.txt", content1)
	require.NoError(t, err)

	// Same content should produce same checksum
	assert.Equal(t, artifact1.SHA256, artifact2.SHA256)
	assert.Equal(t, artifact1.Size, artifact2.Size)

	// Test that different content produces different checksums
	content2 := []byte("different content")
	artifact3, err := provider.WriteArtifactAtomic(tempDir, "file3.txt", content2)
	require.NoError(t, err)

	assert.NotEqual(t, artifact1.SHA256, artifact3.SHA256)
	assert.NotEqual(t, artifact1.Size, artifact3.Size)

	// Verify checksum format
	assert.Contains(t, artifact1.SHA256, "sha256:")
	assert.Contains(t, artifact2.SHA256, "sha256:")
	assert.Contains(t, artifact3.SHA256, "sha256:")
}

func TestRealFSProviderArtifactAtomicWrite(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "lorch-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	provider := NewRealFSProvider(tempDir)

	// Test writing to nested directory
	content := []byte("nested content")
	artifact, err := provider.WriteArtifactAtomic(tempDir, "deep/nested/path/file.txt", content)
	require.NoError(t, err)
	assert.Equal(t, "deep/nested/path/file.txt", artifact.Path)
	assert.Equal(t, int64(len(content)), artifact.Size)

	// Verify directory was created
	nestedDir := filepath.Join(tempDir, "deep", "nested", "path")
	_, err = os.Stat(nestedDir)
	require.NoError(t, err)

	// Verify file was created
	nestedFile := filepath.Join(nestedDir, "file.txt")
	_, err = os.Stat(nestedFile)
	require.NoError(t, err)

	// Test writing to root directory
	content2 := []byte("root content")
	artifact2, err := provider.WriteArtifactAtomic(tempDir, "root.txt", content2)
	require.NoError(t, err)
	assert.Equal(t, "root.txt", artifact2.Path)
	assert.Equal(t, int64(len(content2)), artifact2.Size)

	// Verify file was created
	rootFile := filepath.Join(tempDir, "root.txt")
	_, err = os.Stat(rootFile)
	require.NoError(t, err)
}

func TestRealFSProviderArtifactEmptyContent(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "lorch-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	provider := NewRealFSProvider(tempDir)

	// Test writing empty content
	emptyContent := []byte{}
	artifact, err := provider.WriteArtifactAtomic(tempDir, "empty.txt", emptyContent)
	require.NoError(t, err)
	assert.Equal(t, "empty.txt", artifact.Path)
	assert.Equal(t, int64(0), artifact.Size)
	assert.NotEmpty(t, artifact.SHA256) // Empty content still has a checksum
	assert.Contains(t, artifact.SHA256, "sha256:")

	// Verify file was created
	emptyFile := filepath.Join(tempDir, "empty.txt")
	_, err = os.Stat(emptyFile)
	require.NoError(t, err)

	// Verify file is empty
	fileInfo, err := os.Stat(emptyFile)
	require.NoError(t, err)
	assert.Equal(t, int64(0), fileInfo.Size())
}

func TestRealFSProviderPathValidation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "lorch-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	provider := NewRealFSProvider(tempDir)

	// Test valid relative path
	content := []byte("test content")
	artifact, err := provider.WriteArtifactAtomic(tempDir, "valid.txt", content)
	require.NoError(t, err)
	assert.Equal(t, "valid.txt", artifact.Path)

	// Test path traversal attempt (should fail)
	_, err = provider.WriteArtifactAtomic(tempDir, "../escape.txt", content)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path escapes workspace")

	// Test absolute path (should fail)
	_, err = provider.WriteArtifactAtomic(tempDir, "/absolute/path.txt", content)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path escapes workspace")

	// Test path with multiple ../ (should fail)
	_, err = provider.WriteArtifactAtomic(tempDir, "../../escape.txt", content)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path escapes workspace")

	// Test path with ./../ (should fail)
	_, err = provider.WriteArtifactAtomic(tempDir, "./../escape.txt", content)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path escapes workspace")
}

func TestRealFSProviderPathValidationDebug(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "lorch-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	provider := NewRealFSProvider(tempDir)

	// Debug: Test what happens with path resolution
	path, err := provider.ResolveWorkspacePath(tempDir, "../escape.txt")
	t.Logf("Path resolution result: %s, error: %v", path, err)

	// This should fail
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path escapes workspace")

	// Debug: Test absolute path
	path, err = provider.ResolveWorkspacePath(tempDir, "/absolute/path.txt")
	t.Logf("Absolute path resolution result: %s, error: %v", path, err)

	// This should also fail
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path escapes workspace")
}

func TestRealFSProviderArtifactSizeCapEdgeCases(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "lorch-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	provider := NewRealFSProvider(tempDir)

	// Test with a reasonable size that should work (1 MB)
	normalContent := make([]byte, 1024*1024) // 1 MB
	for i := range normalContent {
		normalContent[i] = byte(i % 256)
	}
	artifact, err := provider.WriteArtifactAtomic(tempDir, "normal.txt", normalContent)
	require.NoError(t, err)
	assert.Equal(t, int64(1024*1024), artifact.Size)

	// Test with a size that should work (10 MB)
	largeContent := make([]byte, 10*1024*1024) // 10 MB
	for i := range largeContent {
		largeContent[i] = byte(i % 256)
	}
	artifact, err = provider.WriteArtifactAtomic(tempDir, "large.txt", largeContent)
	require.NoError(t, err)
	assert.Equal(t, int64(10*1024*1024), artifact.Size)

	// Test with a size that should work (100 MB)
	veryLargeContent := make([]byte, 100*1024*1024) // 100 MB
	for i := range veryLargeContent {
		veryLargeContent[i] = byte(i % 256)
	}
	artifact, err = provider.WriteArtifactAtomic(tempDir, "very_large.txt", veryLargeContent)
	require.NoError(t, err)
	assert.Equal(t, int64(100*1024*1024), artifact.Size)

	// Note: We skip the 1 GiB + 1 byte test here to avoid memory issues in CI
	// The size cap enforcement is tested in the mock tests above
}
