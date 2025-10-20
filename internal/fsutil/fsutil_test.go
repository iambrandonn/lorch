package fsutil

import (
	"os"
	"path/filepath"
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
