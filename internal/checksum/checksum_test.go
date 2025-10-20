package checksum

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSHA256Bytes(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "empty",
			input:    []byte{},
			expected: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "hello world",
			input:    []byte("hello world"),
			expected: "sha256:b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
		},
		{
			name:     "json object",
			input:    []byte(`{"key":"value"}`),
			expected: "sha256:e43abcf3375244839c012f9633f95862d232a95b00d5bc7348b3098b9fed7f32",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SHA256Bytes(tt.input)
			if result != tt.expected {
				t.Errorf("SHA256Bytes() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSHA256File(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test file
	testFile := filepath.Join(tmpDir, "test.txt")
	content := []byte("hello world")
	if err := os.WriteFile(testFile, content, 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Test successful hash
	hash, err := SHA256File(testFile)
	if err != nil {
		t.Fatalf("SHA256File() error = %v", err)
	}

	expected := "sha256:b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if hash != expected {
		t.Errorf("SHA256File() = %v, want %v", hash, expected)
	}

	// Test non-existent file
	_, err = SHA256File(filepath.Join(tmpDir, "missing.txt"))
	if err == nil {
		t.Error("SHA256File() expected error for missing file")
	}
}

func TestVerifyFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test file
	testFile := filepath.Join(tmpDir, "test.txt")
	content := []byte("hello world")
	if err := os.WriteFile(testFile, content, 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	tests := []struct {
		name        string
		path        string
		expectedSum string
		wantErr     bool
	}{
		{
			name:        "valid checksum",
			path:        testFile,
			expectedSum: "sha256:b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
			wantErr:     false,
		},
		{
			name:        "invalid checksum",
			path:        testFile,
			expectedSum: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			wantErr:     true,
		},
		{
			name:        "missing file",
			path:        filepath.Join(tmpDir, "missing.txt"),
			expectedSum: "sha256:b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
			wantErr:     true,
		},
		{
			name:        "malformed checksum (no prefix)",
			path:        testFile,
			expectedSum: "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyFile(tt.path, tt.expectedSum)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyFile() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSHA256FileWithLargeFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a larger test file (1MB)
	testFile := filepath.Join(tmpDir, "large.bin")
	content := make([]byte, 1024*1024)
	for i := range content {
		content[i] = byte(i % 256)
	}
	if err := os.WriteFile(testFile, content, 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Should handle large files efficiently
	hash, err := SHA256File(testFile)
	if err != nil {
		t.Fatalf("SHA256File() error = %v", err)
	}

	// Verify it returns a properly formatted hash
	if len(hash) != 71 { // "sha256:" (7) + 64 hex chars
		t.Errorf("SHA256File() hash length = %d, want 71", len(hash))
	}
	if hash[:7] != "sha256:" {
		t.Errorf("SHA256File() hash prefix = %s, want 'sha256:'", hash[:7])
	}

	// Verify consistency
	hash2, err := SHA256File(testFile)
	if err != nil {
		t.Fatalf("SHA256File() second call error = %v", err)
	}
	if hash != hash2 {
		t.Errorf("SHA256File() inconsistent: %s != %s", hash, hash2)
	}
}
