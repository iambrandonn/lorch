package fsutil

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
