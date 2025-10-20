package workspace

import (
	"fmt"
	"os"
	"path/filepath"
)

// GetRequiredDirectories returns the list of directories that must exist in a lorch workspace
// Based on MASTER-SPEC §5.2
func GetRequiredDirectories() []string {
	return []string{
		"state",       // /state/run.json, /state/index.json
		"events",      // /events/run-<id>.ndjson (append-only ledger)
		"receipts",    // /receipts/<task>/<step>.json (artifact manifests)
		"logs",        // /logs/<agent>/<run_id>.ndjson
		"snapshots",   // /snapshots/snap-XXXX.manifest.json
		"reviews",     // /reviews/<task>.json
		"spec_notes",  // /spec_notes/<task>.json
		"transcripts", // /transcripts/<run_id>.txt (optional human-readable)
	}
}

// Initialize creates all required workspace directories with proper permissions (0700)
// This function is idempotent - safe to call multiple times
func Initialize(workspaceRoot string) error {
	dirs := GetRequiredDirectories()

	for _, dir := range dirs {
		path := filepath.Join(workspaceRoot, dir)

		// Create directory with 0700 permissions (owner read/write/execute only)
		// MkdirAll is idempotent - won't error if directory exists
		if err := os.MkdirAll(path, 0700); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", path, err)
		}
	}

	return nil
}

// IsInitialized checks if a workspace has all required directories
func IsInitialized(workspaceRoot string) (bool, error) {
	dirs := GetRequiredDirectories()

	for _, dir := range dirs {
		path := filepath.Join(workspaceRoot, dir)

		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("failed to check directory %s: %w", path, err)
		}

		if !info.IsDir() {
			return false, nil
		}
	}

	return true, nil
}
