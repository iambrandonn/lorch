package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/iambrandonn/lorch/internal/checksum"
	"github.com/iambrandonn/lorch/internal/fsutil"
	"github.com/iambrandonn/lorch/internal/idempotency"
)

// FileInfo represents a single file in the snapshot
type FileInfo struct {
	Path   string    `json:"path"`
	SHA256 string    `json:"sha256"`
	Size   int64     `json:"size"`
	Mtime  time.Time `json:"mtime"`
}

// Manifest represents a workspace snapshot
type Manifest struct {
	SnapshotID    string     `json:"snapshot_id"`
	CreatedAt     time.Time  `json:"created_at"`
	WorkspaceRoot string     `json:"workspace_root"`
	Files         []FileInfo `json:"files"`
}

// includedDirs are the directories that should be tracked
var includedDirs = []string{"specs", "src", "tests", "docs"}

// excludedDirs are directories that should never be tracked
var excludedDirs = map[string]bool{
	".git":        true,
	"node_modules": true,
	".cache":      true,
	"state":       true,
	"events":      true,
	"receipts":    true,
	"logs":        true,
	"snapshots":   true,
	"transcripts": true,
}

// excludedFiles are specific files that should never be tracked
var excludedFiles = map[string]bool{
	"lorch.json": true,
}

// CaptureSnapshot walks the workspace and creates a snapshot manifest
func CaptureSnapshot(workspaceRoot string) (*Manifest, error) {
	var files []FileInfo

	// Scan included directories
	for _, dir := range includedDirs {
		dirPath := filepath.Join(workspaceRoot, dir)

		// Skip if directory doesn't exist
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			continue
		}

		// Walk directory
		err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Skip directories
			if info.IsDir() {
				// Check if this directory should be excluded
				dirName := filepath.Base(path)
				if excludedDirs[dirName] {
					return filepath.SkipDir
				}
				return nil
			}

			// Skip hidden files (starting with .)
			if strings.HasPrefix(filepath.Base(path), ".") {
				return nil
			}

			// Compute relative path
			relPath, err := filepath.Rel(workspaceRoot, path)
			if err != nil {
				return fmt.Errorf("failed to compute relative path: %w", err)
			}

			// Normalize path separators for cross-platform consistency
			relPath = filepath.ToSlash(relPath)

			// Check if file is excluded
			if excludedFiles[relPath] {
				return nil
			}

			// Compute checksum
			hash, err := checksum.SHA256File(path)
			if err != nil {
				return fmt.Errorf("failed to compute checksum for %s: %w", relPath, err)
			}

			// Add to file list
			files = append(files, FileInfo{
				Path:   relPath,
				SHA256: hash,
				Size:   info.Size(),
				Mtime:  info.ModTime().UTC(),
			})

			return nil
		})

		if err != nil {
			return nil, fmt.Errorf("failed to walk directory %s: %w", dir, err)
		}
	}

	// Sort files by path for deterministic snapshot IDs
	// This ensures consistent hashing regardless of filesystem iteration order
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	// Create manifest (without snapshot_id yet)
	manifest := &Manifest{
		SnapshotID:    "", // Will be computed below
		CreatedAt:     time.Now().UTC(),
		WorkspaceRoot: "./",
		Files:         files,
	}

	// Compute snapshot ID
	snapshotID, err := computeSnapshotID(manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to compute snapshot ID: %w", err)
	}

	manifest.SnapshotID = snapshotID

	return manifest, nil
}

// computeSnapshotID generates the snapshot ID from the manifest content
// Format: "snap-" + first 12 hex chars of SHA256(canonical_json(manifest))
func computeSnapshotID(manifest *Manifest) (string, error) {
	// Temporarily clear snapshot_id for hashing
	originalID := manifest.SnapshotID
	manifest.SnapshotID = ""
	defer func() {
		manifest.SnapshotID = originalID
	}()

	// Serialize manifest canonically
	manifestJSON, err := idempotency.CanonicalJSON(manifest)
	if err != nil {
		return "", fmt.Errorf("failed to canonicalize manifest: %w", err)
	}

	// Compute hash
	hash := checksum.SHA256Bytes(manifestJSON)

	// Extract first 12 hex chars (after "sha256:" prefix)
	if len(hash) < 19 { // "sha256:" (7) + 12 chars
		return "", fmt.Errorf("hash too short: %s", hash)
	}

	hexPart := hash[7:19] // Skip "sha256:" prefix, take next 12 chars

	return "snap-" + hexPart, nil
}

// SaveSnapshot writes a snapshot manifest to disk atomically
func SaveSnapshot(manifest *Manifest, path string) error {
	return fsutil.AtomicWriteJSON(path, manifest)
}

// LoadSnapshot reads a snapshot manifest from disk
func LoadSnapshot(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot file: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	return &manifest, nil
}
