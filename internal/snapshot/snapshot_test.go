package snapshot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCaptureSnapshot(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test workspace structure
	createTestWorkspace(t, tmpDir)

	// Capture snapshot
	snapshot, err := CaptureSnapshot(tmpDir)
	if err != nil {
		t.Fatalf("CaptureSnapshot() error = %v", err)
	}

	// Verify snapshot structure
	if snapshot.SnapshotID == "" {
		t.Error("SnapshotID is empty")
	}

	if !snapshot.CreatedAt.After(time.Now().Add(-1 * time.Minute)) {
		t.Error("CreatedAt is not recent")
	}

	if snapshot.WorkspaceRoot != "./" {
		t.Errorf("WorkspaceRoot = %s, want ./", snapshot.WorkspaceRoot)
	}

	// Verify included files
	expectedFiles := map[string]bool{
		"src/main.go":           true,
		"src/subdir/helper.go":  true,
		"tests/main_test.go":    true,
		"specs/SPEC.md":         true,
		"docs/README.md":        true,
	}

	// Verify excluded files are not present
	excludedFiles := map[string]bool{
		"lorch.json":           true,
		".git/config":          true,
		"node_modules/pkg.js":  true,
		"state/run.json":       true,
		"events/run.ndjson":    true,
		"receipts/task.json":   true,
		"logs/builder.ndjson":  true,
		"snapshots/snap.json":  true,
		"transcripts/run.txt":  true,
		".hidden":              true,
	}

	fileMap := make(map[string]FileInfo)
	for _, f := range snapshot.Files {
		fileMap[f.Path] = f

		// Check if this is an excluded file
		if excludedFiles[f.Path] {
			t.Errorf("snapshot includes excluded file: %s", f.Path)
		}
	}

	// Verify all expected files are present
	for expected := range expectedFiles {
		if _, found := fileMap[expected]; !found {
			t.Errorf("snapshot missing expected file: %s", expected)
		}
	}

	// Verify file info correctness
	srcFile := fileMap["src/main.go"]
	if srcFile.SHA256 == "" {
		t.Error("file SHA256 is empty")
	}
	if srcFile.Size == 0 {
		t.Error("file size is 0")
	}
	if srcFile.Mtime.IsZero() {
		t.Error("file mtime is zero")
	}
}

func TestSnapshotIDDeterminism(t *testing.T) {
	tmpDir := t.TempDir()
	createTestWorkspace(t, tmpDir)

	// Capture snapshot
	snap1, err := CaptureSnapshot(tmpDir)
	if err != nil {
		t.Fatalf("CaptureSnapshot() error = %v", err)
	}

	// Verify the format: "snap-" (5 chars) + 12 hex chars = 17 total
	if len(snap1.SnapshotID) != 17 {
		t.Errorf("SnapshotID length = %d, expected 17 (snap- + 12 hex chars)", len(snap1.SnapshotID))
	}

	if snap1.SnapshotID[:5] != "snap-" {
		t.Errorf("SnapshotID prefix = %s, want 'snap-'", snap1.SnapshotID[:5])
	}
}

func TestSnapshotIDChangesWithContent(t *testing.T) {
	tmpDir := t.TempDir()
	createTestWorkspace(t, tmpDir)

	// Capture initial snapshot
	snap1, err := CaptureSnapshot(tmpDir)
	if err != nil {
		t.Fatalf("CaptureSnapshot() error = %v", err)
	}

	// Modify a file
	srcFile := filepath.Join(tmpDir, "src", "main.go")
	if err := os.WriteFile(srcFile, []byte("package main\n\n// Modified\n"), 0600); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	// Capture again
	snap2, err := CaptureSnapshot(tmpDir)
	if err != nil {
		t.Fatalf("CaptureSnapshot() second error = %v", err)
	}

	// Snapshot IDs should differ
	if snap1.SnapshotID == snap2.SnapshotID {
		t.Error("SnapshotID unchanged after file modification")
	}
}

func TestSaveAndLoadSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	createTestWorkspace(t, tmpDir)

	// Capture snapshot
	original, err := CaptureSnapshot(tmpDir)
	if err != nil {
		t.Fatalf("CaptureSnapshot() error = %v", err)
	}

	// Save to disk
	snapshotPath := filepath.Join(tmpDir, "snapshots", original.SnapshotID+".manifest.json")
	if err := SaveSnapshot(original, snapshotPath); err != nil {
		t.Fatalf("SaveSnapshot() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(snapshotPath); os.IsNotExist(err) {
		t.Fatal("snapshot file not created")
	}

	// Load back
	loaded, err := LoadSnapshot(snapshotPath)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	// Verify match
	if loaded.SnapshotID != original.SnapshotID {
		t.Errorf("SnapshotID mismatch: %s != %s", loaded.SnapshotID, original.SnapshotID)
	}

	if len(loaded.Files) != len(original.Files) {
		t.Errorf("file count mismatch: %d != %d", len(loaded.Files), len(original.Files))
	}
}

func TestEmptyWorkspace(t *testing.T) {
	tmpDir := t.TempDir()

	// Create only excluded directories
	os.MkdirAll(filepath.Join(tmpDir, "state"), 0700)
	os.MkdirAll(filepath.Join(tmpDir, "events"), 0700)

	// Capture snapshot
	snapshot, err := CaptureSnapshot(tmpDir)
	if err != nil {
		t.Fatalf("CaptureSnapshot() error = %v", err)
	}

	// Should have zero files
	if len(snapshot.Files) != 0 {
		t.Errorf("expected 0 files, got %d", len(snapshot.Files))
	}

	// Should still have valid snapshot ID
	if snapshot.SnapshotID == "" {
		t.Error("SnapshotID should not be empty for empty workspace")
	}
}

// Helper to create test workspace
func createTestWorkspace(t *testing.T, root string) {
	t.Helper()

	files := map[string]string{
		// Included files
		"src/main.go":          "package main\n",
		"src/subdir/helper.go": "package subdir\n",
		"tests/main_test.go":   "package main_test\n",
		"specs/SPEC.md":        "# Specification\n",
		"docs/README.md":       "# Documentation\n",

		// Excluded files
		"lorch.json":          `{"version":"1.0"}`,
		".git/config":         "[core]\n",
		"node_modules/pkg.js": "module.exports = {}\n",
		"state/run.json":      "{}",
		"events/run.ndjson":   "{}\n",
		"receipts/task.json":  "{}",
		"logs/builder.ndjson": "{}\n",
		"snapshots/snap.json": "{}",
		"transcripts/run.txt": "transcript",
		".hidden":             "hidden file",
	}

	for path, content := range files {
		fullPath := filepath.Join(root, path)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatalf("failed to create directory %s: %v", dir, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0600); err != nil {
			t.Fatalf("failed to write file %s: %v", path, err)
		}
	}
}
