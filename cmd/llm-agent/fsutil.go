package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/iambrandonn/lorch/internal/protocol"
)

// RealFSProvider implements FSProvider using real filesystem operations
type RealFSProvider struct {
	workspace string
}

// NewRealFSProvider creates a new real filesystem provider
func NewRealFSProvider(workspace string) *RealFSProvider {
	return &RealFSProvider{
		workspace: workspace,
	}
}

// ResolveWorkspacePath validates and resolves a relative path within workspace
func (r *RealFSProvider) ResolveWorkspacePath(workspace, relative string) (string, error) {
	// Get canonical workspace root
	rootAbs, err := filepath.EvalSymlinks(filepath.Clean(workspace))
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace: %w", err)
	}

	// Check if relative path is absolute (starts with /)
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("path escapes workspace: %s", relative)
	}

	// Join paths
	joined := filepath.Join(rootAbs, relative)
	cleanJoined := filepath.Clean(joined)

	// Try to resolve symlinks, but don't fail if the target doesn't exist yet
	fullAbs, err := filepath.EvalSymlinks(cleanJoined)
	if err != nil {
		// If the path doesn't exist, use the cleaned path
		// This is safe because we'll validate it's within the workspace
		fullAbs = cleanJoined
	}

	// Ensure fullAbs is within rootAbs (with path separator guard)
	rootWithSep := rootAbs
	if !strings.HasSuffix(rootWithSep, string(os.PathSeparator)) {
		rootWithSep += string(os.PathSeparator)
	}

	if fullAbs != rootAbs && !strings.HasPrefix(fullAbs, rootWithSep) {
		return "", fmt.Errorf("path escapes workspace: %s", relative)
	}

	return fullAbs, nil
}

// ReadFileSafe reads a file with size limits
func (r *RealFSProvider) ReadFileSafe(path string, maxSize int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	limited := io.LimitReader(file, maxSize)
	content, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	return string(content), nil
}

// WriteArtifactAtomic writes an artifact using the atomic write pattern
func (r *RealFSProvider) WriteArtifactAtomic(workspace, relativePath string, content []byte) (protocol.Artifact, error) {
	// Check artifact size cap BEFORE writing (per spec §12)
	maxSize := int64(1 * 1024 * 1024 * 1024) // 1 GiB default
	if int64(len(content)) > maxSize {
		return protocol.Artifact{}, fmt.Errorf("artifact exceeds size cap: %d > %d bytes",
			len(content), maxSize)
	}

	// Security: validate path with symlink-safe resolution
	fullPath, err := r.ResolveWorkspacePath(workspace, relativePath)
	if err != nil {
		return protocol.Artifact{}, fmt.Errorf("invalid artifact path: %w", err)
	}

	// Atomic write pattern per spec §14.4
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0700); err != nil { // 0700 per spec §13
		return protocol.Artifact{}, err
	}

	tmpFile := fmt.Sprintf("%s/.%s.tmp.%d.%s",
		dir, filepath.Base(fullPath), os.Getpid(), uuid.New().String()[:8])

	if err := os.WriteFile(tmpFile, content, 0600); err != nil { // 0600 per spec §13
		return protocol.Artifact{}, err
	}

	// Sync temp file
	f, err := os.Open(tmpFile)
	if err != nil {
		os.Remove(tmpFile)
		return protocol.Artifact{}, err
	}
	f.Sync()
	f.Close()

	// Atomic rename
	if err := os.Rename(tmpFile, fullPath); err != nil {
		os.Remove(tmpFile)
		return protocol.Artifact{}, err
	}

	// Sync directory
	d, err := os.Open(dir)
	if err != nil {
		return protocol.Artifact{}, err
	}
	d.Sync()
	d.Close()

	// Compute checksum
	fileData, err := os.ReadFile(fullPath)
	if err != nil {
		return protocol.Artifact{}, err
	}

	// Verify final size after write (double-check size cap)
	if int64(len(fileData)) > maxSize {
		os.Remove(fullPath) // Cleanup oversized artifact
		return protocol.Artifact{}, fmt.Errorf("artifact exceeds size cap after write: %d > %d",
			len(fileData), maxSize)
	}

	hash := sha256.Sum256(fileData)

	return protocol.Artifact{
		Path:   relativePath, // Return relative path in artifact
		SHA256: fmt.Sprintf("sha256:%x", hash),
		Size:   int64(len(fileData)),
	}, nil
}

// MockFSProvider implements FSProvider for testing
type MockFSProvider struct {
	files      map[string]string
	writeLog   []string
	readLog    []string
	resolveLog []string
}

// NewMockFSProvider creates a new mock filesystem provider
func NewMockFSProvider() *MockFSProvider {
	return &MockFSProvider{
		files:      make(map[string]string),
		writeLog:   make([]string, 0),
		readLog:    make([]string, 0),
		resolveLog: make([]string, 0),
	}
}

// ResolveWorkspacePath mocks path resolution
func (m *MockFSProvider) ResolveWorkspacePath(workspace, relative string) (string, error) {
	m.resolveLog = append(m.resolveLog, fmt.Sprintf("ResolveWorkspacePath(%s, %s)", workspace, relative))

	// Simple mock: just join the paths
	return filepath.Join(workspace, relative), nil
}

// ReadFileSafe mocks file reading
func (m *MockFSProvider) ReadFileSafe(path string, maxSize int64) (string, error) {
	m.readLog = append(m.readLog, fmt.Sprintf("ReadFileSafe(%s, %d)", path, maxSize))

	if content, exists := m.files[path]; exists {
		// Apply size limit
		if int64(len(content)) > maxSize {
			return content[:maxSize], nil
		}
		return content, nil
	}

	return "", fmt.Errorf("file not found: %s", path)
}

// WriteArtifactAtomic mocks atomic artifact writing
func (m *MockFSProvider) WriteArtifactAtomic(workspace, relativePath string, content []byte) (protocol.Artifact, error) {
	m.writeLog = append(m.writeLog, fmt.Sprintf("WriteArtifactAtomic(%s, %s, %d bytes)", workspace, relativePath, len(content)))

	// Check artifact size cap (same as real implementation)
	maxSize := int64(1 * 1024 * 1024 * 1024) // 1 GiB default
	if int64(len(content)) > maxSize {
		return protocol.Artifact{}, fmt.Errorf("artifact exceeds size cap: %d > %d bytes",
			len(content), maxSize)
	}

	// Store the content
	fullPath := filepath.Join(workspace, relativePath)
	m.files[fullPath] = string(content)

	// Compute mock checksum
	hash := sha256.Sum256(content)

	return protocol.Artifact{
		Path:   relativePath,
		SHA256: fmt.Sprintf("sha256:%x", hash),
		Size:   int64(len(content)),
	}, nil
}

// SetFile sets a file content for testing
func (m *MockFSProvider) SetFile(path, content string) {
	m.files[path] = content
}

// GetWriteLog returns the write operation log
func (m *MockFSProvider) GetWriteLog() []string {
	return m.writeLog
}

// GetReadLog returns the read operation log
func (m *MockFSProvider) GetReadLog() []string {
	return m.readLog
}

// GetResolveLog returns the path resolution log
func (m *MockFSProvider) GetResolveLog() []string {
	return m.resolveLog
}

// ClearLogs clears all operation logs
func (m *MockFSProvider) ClearLogs() {
	m.writeLog = m.writeLog[:0]
	m.readLog = m.readLog[:0]
	m.resolveLog = m.resolveLog[:0]
}
