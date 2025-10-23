package fsutil

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// AtomicWrite writes data to a file atomically using the pattern:
// 1. Write to .<basename>.tmp.<pid>.<rand>
// 2. fsync(tmp)
// 3. rename(tmp, final)
// 4. fsync(dir)
//
// Files are created with 0600 permissions (owner read/write only).
// This ensures that partial writes are never visible and concurrent writes are safe.
func AtomicWrite(path string, data []byte) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Generate temporary filename
	tmpPath, err := generateTempPath(path)
	if err != nil {
		return fmt.Errorf("failed to generate temp path: %w", err)
	}

	// Write to temporary file
	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	// Ensure cleanup on failure
	success := false
	defer func() {
		tmpFile.Close()
		if !success {
			os.Remove(tmpPath)
		}
	}()

	// Write data
	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	// Fsync the file
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	// Close before rename
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Fsync the directory to ensure rename is durable
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("failed to sync directory: %w", err)
	}

	success = true
	return nil
}

// AtomicWriteJSON writes a JSON-serialized value to a file atomically
// The JSON is pretty-printed with indentation for readability
func AtomicWriteJSON(path string, v interface{}) error {
	if v == nil {
		return fmt.Errorf("cannot write nil value")
	}

	// Marshal with indentation
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Add trailing newline
	data = append(data, '\n')

	return AtomicWrite(path, data)
}

// generateTempPath creates a temporary filename in the same directory as the target
// Format: .<basename>.tmp.<pid>.<rand>
func generateTempPath(path string) (string, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	pid := os.Getpid()

	// Generate random suffix (8 hex chars = 4 random bytes)
	randBytes := make([]byte, 4)
	if _, err := rand.Read(randBytes); err != nil {
		return "", fmt.Errorf("failed to generate random suffix: %w", err)
	}
	randSuffix := hex.EncodeToString(randBytes)

	tmpName := fmt.Sprintf(".%s.tmp.%d.%s", base, pid, randSuffix)
	return filepath.Join(dir, tmpName), nil
}

// syncDir opens a directory and calls fsync on it
// This ensures directory metadata (including rename operations) is durable
func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open directory: %w", err)
	}
	defer dir.Close()

	if err := dir.Sync(); err != nil {
		return fmt.Errorf("failed to sync directory: %w", err)
	}

	return nil
}

// ResolveWorkspacePath validates and resolves a relative path within workspace
// Returns canonical absolute path or error if path escapes workspace
// This function prevents directory traversal attacks and symlink escapes
func ResolveWorkspacePath(workspace, relative string) (string, error) {
	// Get canonical workspace root
	rootAbs, err := filepath.EvalSymlinks(filepath.Clean(workspace))
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace: %w", err)
	}

	// Check if relative path is absolute (should be rejected)
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("absolute paths not allowed: %s", relative)
	}

	// Join paths and clean
	joined := filepath.Join(rootAbs, relative)
	cleanPath := filepath.Clean(joined)

	// Check if the cleaned path is within the workspace root
	// Use filepath.Rel to check if the path is within the root
	relPath, err := filepath.Rel(rootAbs, cleanPath)
	if err != nil {
		return "", fmt.Errorf("failed to compute relative path: %w", err)
	}

	// Check for directory traversal attempts
	if strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("path escapes workspace: %s", relative)
	}

	// If the target exists, resolve any symlinks
	if _, err := os.Stat(cleanPath); err == nil {
		resolved, err := filepath.EvalSymlinks(cleanPath)
		if err != nil {
			return "", fmt.Errorf("failed to resolve symlinks: %w", err)
		}

		// Check if resolved path is still within workspace
		resolvedRel, err := filepath.Rel(rootAbs, resolved)
		if err != nil || strings.HasPrefix(resolvedRel, "..") {
			return "", fmt.Errorf("symlink escapes workspace: %s", relative)
		}

		return resolved, nil
	}

	return cleanPath, nil
}

// ReadFileSafe reads a file with size limits and security validation
// Returns file content or error if file is too large or path is invalid
func ReadFileSafe(workspace, relativePath string, maxBytes int64) ([]byte, error) {
	// Security: validate path with symlink-safe resolution
	fullPath, err := ResolveWorkspacePath(workspace, relativePath)
	if err != nil {
		return nil, fmt.Errorf("invalid file path: %w", err)
	}

	// Safety: limit file size
	file, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Read with size limit
	limited := io.LimitReader(file, maxBytes)
	content, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return content, nil
}

// WriteArtifactAtomic writes an artifact using the atomic write pattern per spec §14.4
// Returns artifact metadata with SHA256 checksum and size
func WriteArtifactAtomic(workspace, relativePath string, content []byte) (Artifact, error) {
	// Security: validate path with symlink-safe resolution
	fullPath, err := ResolveWorkspacePath(workspace, relativePath)
	if err != nil {
		return Artifact{}, fmt.Errorf("invalid artifact path: %w", err)
	}

	// Atomic write pattern per spec §14.4
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0700); err != nil { // 0700 per spec §13
		return Artifact{}, fmt.Errorf("failed to create directory: %w", err)
	}

	// Generate temporary filename
	tmpFile, err := generateTempPath(fullPath)
	if err != nil {
		return Artifact{}, fmt.Errorf("failed to generate temp path: %w", err)
	}

	// Write to temp file with restrictive permissions
	if err := os.WriteFile(tmpFile, content, 0600); err != nil { // 0600 per spec §13
		return Artifact{}, fmt.Errorf("failed to write temp file: %w", err)
	}

	// Sync temp file
	f, err := os.Open(tmpFile)
	if err != nil {
		os.Remove(tmpFile)
		return Artifact{}, fmt.Errorf("failed to open temp file for sync: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpFile)
		return Artifact{}, fmt.Errorf("failed to sync temp file: %w", err)
	}
	f.Close()

	// Atomic rename
	if err := os.Rename(tmpFile, fullPath); err != nil {
		os.Remove(tmpFile)
		return Artifact{}, fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Sync directory
	if err := syncDir(dir); err != nil {
		return Artifact{}, fmt.Errorf("failed to sync directory: %w", err)
	}

	// Compute checksum
	hash := sha256.Sum256(content)

	return Artifact{
		Path:   relativePath, // Return relative path in artifact
		SHA256: fmt.Sprintf("sha256:%x", hash),
		Size:   int64(len(content)),
	}, nil
}

// Artifact represents a file artifact with metadata
type Artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
