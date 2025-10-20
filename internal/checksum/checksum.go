package checksum

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// SHA256Bytes computes the SHA256 hash of a byte slice and returns it as "sha256:hexstring"
func SHA256Bytes(data []byte) string {
	hash := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(hash[:])
}

// SHA256File computes the SHA256 hash of a file and returns it as "sha256:hexstring"
// Uses streaming to handle large files efficiently
func SHA256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

// VerifyFile checks if a file's SHA256 hash matches the expected value
// Expected format: "sha256:hexstring"
func VerifyFile(path string, expectedSum string) error {
	// Validate expected sum format
	if !strings.HasPrefix(expectedSum, "sha256:") {
		return fmt.Errorf("invalid checksum format: must start with 'sha256:'")
	}
	if len(expectedSum) != 71 { // "sha256:" (7) + 64 hex chars
		return fmt.Errorf("invalid checksum format: expected 71 characters, got %d", len(expectedSum))
	}

	// Compute actual hash
	actualSum, err := SHA256File(path)
	if err != nil {
		return fmt.Errorf("failed to compute checksum: %w", err)
	}

	// Compare
	if actualSum != expectedSum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedSum, actualSum)
	}

	return nil
}
