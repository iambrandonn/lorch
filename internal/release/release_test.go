package release

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBuildHostTarget(t *testing.T) {
	tempDir := t.TempDir()
	distDir := filepath.Join(tempDir, "dist")

	cacheDir := filepath.Join(tempDir, "gocache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}
	t.Setenv("GOCACHE", cacheDir)

	moduleDir := filepath.Join(tempDir, "module")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("failed to create module dir: %v", err)
	}

	writeFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com/demo\n\ngo 1.22\n")
	writeFile(t, filepath.Join(moduleDir, "main.go"), `package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--help" {
		fmt.Println("stub binary help")
		return
	}
	fmt.Println("stub binary")
}
`)

	ctx := context.Background()

	opts := Options{
		ProjectRoot: moduleDir,
		DistDir:     distDir,
		MainPackage: ".",
		Targets: []Target{
			{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH},
		},
	}

	manifest, err := Build(ctx, opts)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if manifest.GoVersion == "" {
		t.Fatal("expected manifest.GoVersion to be populated")
	}

	if len(manifest.Targets) != 1 {
		t.Fatalf("expected exactly one target, got %d", len(manifest.Targets))
	}

	target := manifest.Targets[0]
	if target.OS != runtime.GOOS {
		t.Fatalf("unexpected OS in manifest: got %s want %s", target.OS, runtime.GOOS)
	}
	if target.Arch != runtime.GOARCH {
		t.Fatalf("unexpected Arch in manifest: got %s want %s", target.Arch, runtime.GOARCH)
	}
	if target.SHA256 == "" {
		t.Fatal("expected SHA256 to be populated")
	}
	if target.Binary == "" {
		t.Fatal("expected Binary path to be populated")
	}

	// Ensure the binary exists.
	binaryPath := filepath.Clean(filepath.Join(moduleDir, target.Binary))
	if _, err := os.Stat(binaryPath); err != nil {
		t.Fatalf("expected binary at %s: %v", binaryPath, err)
	}

	// Verify smoke test ran (passed or skipped depending on environment).
	if target.Smoke.Status != "passed" && target.Smoke.Status != "skipped" {
		t.Fatalf("unexpected smoke status: %s", target.Smoke.Status)
	}

	// Ensure manifest file exists and is valid JSON.
	manifestPath := filepath.Join(distDir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read manifest file: %v", err)
	}
	var decoded Manifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to decode manifest JSON: %v", err)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}
