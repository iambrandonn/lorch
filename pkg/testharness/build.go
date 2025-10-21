package testharness

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// BuildBinaries compiles the lorch and mockagent binaries into outputDir.
// Returns the absolute paths to the compiled binaries.
func BuildBinaries(ctx context.Context, projectRoot, outputDir string) (string, string, error) {
	if projectRoot == "" {
		return "", "", fmt.Errorf("project root is required")
	}
	if outputDir == "" {
		return "", "", fmt.Errorf("output directory is required")
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", "", fmt.Errorf("failed to create output directory: %w", err)
	}

	lorchPath := filepath.Join(outputDir, "lorch")
	mockagentPath := filepath.Join(outputDir, "mockagent")

	if err := runGoBuild(ctx, projectRoot, lorchPath, "./cmd/lorch"); err != nil {
		return "", "", err
	}
	if err := runGoBuild(ctx, projectRoot, mockagentPath, "./cmd/mockagent"); err != nil {
		return "", "", err
	}

	return lorchPath, mockagentPath, nil
}

func runGoBuild(ctx context.Context, projectRoot, outputPath, pkg string) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", outputPath, pkg)
	cmd.Dir = projectRoot

	env := os.Environ()
	env = setEnv(env, "CGO_ENABLED", "0")
	env = setEnv(env, "GOFLAGS", "-trimpath")
	cmd.Env = env

	var combined []byte
	var err error
	if combined, err = cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build %s failed: %w\n%s", pkg, err, string(combined))
	}
	return nil
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, kv := range env {
		if len(kv) >= len(prefix) && kv[:len(prefix)] == prefix {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
