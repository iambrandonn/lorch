package fsutil

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		path    string
		data    []byte
		wantErr bool
	}{
		{
			name:    "write to new file",
			path:    filepath.Join(tmpDir, "new.txt"),
			data:    []byte("hello world"),
			wantErr: false,
		},
		{
			name:    "overwrite existing file",
			path:    filepath.Join(tmpDir, "existing.txt"),
			data:    []byte("updated content"),
			wantErr: false,
		},
		{
			name:    "write empty file",
			path:    filepath.Join(tmpDir, "empty.txt"),
			data:    []byte{},
			wantErr: false,
		},
		{
			name:    "write to nested directory",
			path:    filepath.Join(tmpDir, "nested", "deep", "file.txt"),
			data:    []byte("nested content"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For overwrite test, create initial file
			if tt.name == "overwrite existing file" {
				if err := os.WriteFile(tt.path, []byte("original"), 0600); err != nil {
					t.Fatalf("failed to create initial file: %v", err)
				}
			}

			// Perform atomic write
			err := AtomicWrite(tt.path, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("AtomicWrite() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify file exists and has correct content
				content, err := os.ReadFile(tt.path)
				if err != nil {
					t.Errorf("failed to read written file: %v", err)
					return
				}

				if string(content) != string(tt.data) {
					t.Errorf("file content = %q, want %q", string(content), string(tt.data))
				}

				// Verify file permissions (should be 0600)
				info, err := os.Stat(tt.path)
				if err != nil {
					t.Errorf("failed to stat file: %v", err)
					return
				}

				mode := info.Mode().Perm()
				if mode != 0600 {
					t.Errorf("file permissions = %o, want 0600", mode)
				}
			}
		})
	}
}

func TestAtomicWriteJSON(t *testing.T) {
	tmpDir := t.TempDir()

	type TestStruct struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
		Items []string `json:"items"`
	}

	tests := []struct {
		name    string
		path    string
		data    interface{}
		wantErr bool
	}{
		{
			name: "write simple struct",
			path: filepath.Join(tmpDir, "simple.json"),
			data: TestStruct{
				Name:  "test",
				Count: 42,
				Items: []string{"a", "b", "c"},
			},
			wantErr: false,
		},
		{
			name: "write map",
			path: filepath.Join(tmpDir, "map.json"),
			data: map[string]interface{}{
				"key": "value",
				"number": 123,
			},
			wantErr: false,
		},
		{
			name:    "write nil fails",
			path:    filepath.Join(tmpDir, "nil.json"),
			data:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AtomicWriteJSON(tt.path, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("AtomicWriteJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify file exists and is valid JSON
				content, err := os.ReadFile(tt.path)
				if err != nil {
					t.Errorf("failed to read written file: %v", err)
					return
				}

				if len(content) == 0 {
					t.Error("written file is empty")
				}

				// Should be pretty-printed with newline at end
				if content[len(content)-1] != '\n' {
					t.Error("JSON file should end with newline")
				}
			}
		})
	}
}

func TestAtomicWriteNoTempFilesLeft(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Perform multiple writes
	for i := 0; i < 5; i++ {
		if err := AtomicWrite(testFile, []byte("content")); err != nil {
			t.Fatalf("AtomicWrite() failed: %v", err)
		}
	}

	// Check no temp files left behind
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}

	for _, entry := range entries {
		if entry.Name() != "test.txt" {
			t.Errorf("unexpected file left behind: %s", entry.Name())
		}
	}
}

func TestAtomicWriteConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "concurrent.txt")

	// Run multiple concurrent writes
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			data := []byte("concurrent write")
			done <- AtomicWrite(testFile, data)
		}(i)
	}

	// Wait for all to complete
	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent write failed: %v", err)
		}
	}

	// Verify final file is valid (one of the writes succeeded)
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read final file: %v", err)
	}

	if string(content) != "concurrent write" {
		t.Errorf("unexpected final content: %q", string(content))
	}
}

func TestResolveWorkspacePath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test workspace structure
	workspace := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	// Create some test files
	testFile := filepath.Join(workspace, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create a subdirectory
	subDir := filepath.Join(workspace, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	subFile := filepath.Join(subDir, "subfile.txt")
	if err := os.WriteFile(subFile, []byte("sub content"), 0644); err != nil {
		t.Fatalf("failed to create subfile: %v", err)
	}

	tests := []struct {
		name      string
		workspace string
		relative  string
		wantErr   bool
		checkPath bool
	}{
		{
			name:      "valid file in root",
			workspace: workspace,
			relative:  "test.txt",
			wantErr:   false,
			checkPath: true,
		},
		{
			name:      "valid file in subdirectory",
			workspace: workspace,
			relative:  "subdir/subfile.txt",
			wantErr:   false,
			checkPath: true,
		},
		{
			name:      "directory traversal attempt",
			workspace: workspace,
			relative:  "../test.txt",
			wantErr:   true,
			checkPath: false,
		},
		{
			name:      "multiple directory traversal",
			workspace: workspace,
			relative:  "../../../etc/passwd",
			wantErr:   true,
			checkPath: false,
		},
		{
			name:      "absolute path",
			workspace: workspace,
			relative:  "/etc/passwd",
			wantErr:   true,
			checkPath: false,
		},
		{
			name:      "nonexistent file",
			workspace: workspace,
			relative:  "nonexistent.txt",
			wantErr:   false, // Path resolution should succeed even if file doesn't exist
			checkPath: true,
		},
		{
			name:      "empty relative path",
			workspace: workspace,
			relative:  "",
			wantErr:   false,
			checkPath: true,
		},
		{
			name:      "current directory",
			workspace: workspace,
			relative:  ".",
			wantErr:   false,
			checkPath: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveWorkspacePath(tt.workspace, tt.relative)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveWorkspacePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.checkPath {
				// Verify the resolved path is within workspace by checking relative path
				// First resolve the workspace path to handle symlinks
				workspaceAbs, err := filepath.EvalSymlinks(tt.workspace)
				if err != nil {
					t.Errorf("failed to resolve workspace path: %v", err)
					return
				}

				relPath, err := filepath.Rel(workspaceAbs, got)
				if err != nil || strings.HasPrefix(relPath, "..") {
					t.Errorf("ResolveWorkspacePath() = %v, should be within workspace %v (resolved: %v)", got, tt.workspace, workspaceAbs)
				}
			}
		})
	}
}

func TestResolveWorkspacePathSymlinks(t *testing.T) {
	tmpDir := t.TempDir()

	// Create workspace
	workspace := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	// Create a file outside workspace
	outsideDir := filepath.Join(tmpDir, "outside")
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("failed to create outside dir: %v", err)
	}

	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0644); err != nil {
		t.Fatalf("failed to create outside file: %v", err)
	}

	// Create symlink inside workspace pointing outside
	symlinkPath := filepath.Join(workspace, "link")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	tests := []struct {
		name      string
		relative  string
		wantErr   bool
	}{
		{
			name:     "follow symlink to outside file",
			relative: "link",
			wantErr:  true, // Should reject symlinks that escape workspace
		},
		{
			name:     "valid file in workspace",
			relative: "valid.txt",
			wantErr:  false,
		},
	}

	// Create a valid file for the second test
	validFile := filepath.Join(workspace, "valid.txt")
	if err := os.WriteFile(validFile, []byte("valid"), 0644); err != nil {
		t.Fatalf("failed to create valid file: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveWorkspacePath(workspace, tt.relative)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveWorkspacePath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestReadFileSafe(t *testing.T) {
	tmpDir := t.TempDir()
	workspace := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	// Create test files
	smallFile := filepath.Join(workspace, "small.txt")
	smallContent := []byte("small content")
	if err := os.WriteFile(smallFile, smallContent, 0644); err != nil {
		t.Fatalf("failed to create small file: %v", err)
	}

	largeFile := filepath.Join(workspace, "large.txt")
	largeContent := make([]byte, 1000) // 1KB
	for i := range largeContent {
		largeContent[i] = 'A'
	}
	if err := os.WriteFile(largeFile, largeContent, 0644); err != nil {
		t.Fatalf("failed to create large file: %v", err)
	}

	tests := []struct {
		name     string
		relative string
		maxBytes int64
		wantErr  bool
		wantLen  int
	}{
		{
			name:     "read small file within limit",
			relative: "small.txt",
			maxBytes: 100,
			wantErr:  false,
			wantLen:  len(smallContent),
		},
		{
			name:     "read large file with limit",
			relative: "large.txt",
			maxBytes: 500,
			wantErr:  false,
			wantLen:  500, // Should be truncated
		},
		{
			name:     "read large file without limit",
			relative: "large.txt",
			maxBytes: 2000,
			wantErr:  false,
			wantLen:  len(largeContent),
		},
		{
			name:     "path traversal attempt",
			relative: "../small.txt",
			maxBytes: 100,
			wantErr:  true,
			wantLen:  0,
		},
		{
			name:     "nonexistent file",
			relative: "nonexistent.txt",
			maxBytes: 100,
			wantErr:  true,
			wantLen:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := ReadFileSafe(workspace, tt.relative, tt.maxBytes)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadFileSafe() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(content) != tt.wantLen {
					t.Errorf("ReadFileSafe() content length = %v, want %v", len(content), tt.wantLen)
				}
			}
		})
	}
}

func TestWriteArtifactAtomic(t *testing.T) {
	tmpDir := t.TempDir()
	workspace := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	content := []byte("test artifact content")
	relativePath := "artifacts/test.txt"

	t.Run("write artifact", func(t *testing.T) {
		artifact, err := WriteArtifactAtomic(workspace, relativePath, content)
		if err != nil {
			t.Fatalf("WriteArtifactAtomic() error = %v", err)
		}

		// Verify artifact metadata
		if artifact.Path != relativePath {
			t.Errorf("artifact.Path = %v, want %v", artifact.Path, relativePath)
		}
		if artifact.Size != int64(len(content)) {
			t.Errorf("artifact.Size = %v, want %v", artifact.Size, len(content))
		}
		if !strings.HasPrefix(artifact.SHA256, "sha256:") {
			t.Errorf("artifact.SHA256 = %v, should start with 'sha256:'", artifact.SHA256)
		}

		// Verify file was created with correct content
		fullPath, err := ResolveWorkspacePath(workspace, relativePath)
		if err != nil {
			t.Fatalf("failed to resolve path: %v", err)
		}

		readContent, err := os.ReadFile(fullPath)
		if err != nil {
			t.Fatalf("failed to read created file: %v", err)
		}

		if string(readContent) != string(content) {
			t.Errorf("file content = %q, want %q", string(readContent), string(content))
		}

		// Verify file permissions
		info, err := os.Stat(fullPath)
		if err != nil {
			t.Fatalf("failed to stat file: %v", err)
		}

		if info.Mode().Perm() != 0600 {
			t.Errorf("file permissions = %o, want 0600", info.Mode().Perm())
		}
	})

	t.Run("path traversal attempt", func(t *testing.T) {
		_, err := WriteArtifactAtomic(workspace, "../outside.txt", content)
		if err == nil {
			t.Error("WriteArtifactAtomic() should have failed for path traversal")
		}
	})

	t.Run("verify checksum", func(t *testing.T) {
		artifact, err := WriteArtifactAtomic(workspace, "checksum_test.txt", content)
		if err != nil {
			t.Fatalf("WriteArtifactAtomic() error = %v", err)
		}

		// Verify checksum matches content
		expectedHash := fmt.Sprintf("sha256:%x", sha256.Sum256(content))
		if artifact.SHA256 != expectedHash {
			t.Errorf("artifact.SHA256 = %v, want %v", artifact.SHA256, expectedHash)
		}
	})
}

func TestWriteArtifactAtomicConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	workspace := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	content := []byte("concurrent test content")
	relativePath := "concurrent.txt"

	// Run multiple concurrent writes
	done := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func() {
			_, err := WriteArtifactAtomic(workspace, relativePath, content)
			done <- err
		}()
	}

	// Wait for all to complete
	var errors []error
	for i := 0; i < 5; i++ {
		if err := <-done; err != nil {
			errors = append(errors, err)
		}
	}

	// At least one should succeed
	if len(errors) == 5 {
		t.Error("all concurrent writes failed")
	}

	// Verify final file is valid
	fullPath, err := ResolveWorkspacePath(workspace, relativePath)
	if err != nil {
		t.Fatalf("failed to resolve path: %v", err)
	}

	finalContent, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("failed to read final file: %v", err)
	}

	if string(finalContent) != string(content) {
		t.Errorf("final content = %q, want %q", string(finalContent), string(content))
	}
}
