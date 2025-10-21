package release

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/iambrandonn/lorch/internal/checksum"
	"github.com/iambrandonn/lorch/internal/fsutil"
)

// Target represents a GOOS/GOARCH pair to build for.
type Target struct {
	GOOS   string
	GOARCH string
}

// DefaultTargets enumerates the release targets we ship.
var DefaultTargets = []Target{
	{GOOS: "darwin", GOARCH: "amd64"},
	{GOOS: "darwin", GOARCH: "arm64"},
	{GOOS: "linux", GOARCH: "amd64"},
	{GOOS: "linux", GOARCH: "arm64"},
}

// Options controls the release build behaviour.
type Options struct {
	// ProjectRoot is the directory containing go.mod. Defaults to CWD.
	ProjectRoot string
	// DistDir is where artifacts are written. Defaults to <ProjectRoot>/dist.
	DistDir string
	// MainPackage is the package to build (e.g., ./cmd/lorch). Defaults to ./cmd/lorch.
	MainPackage string
	// Targets limits the build to specific pairs. Defaults to DefaultTargets.
	Targets []Target
	// SkipSmoke skips executing the smoke command.
	SkipSmoke bool
	// Logger receives informational logs. Optional.
	Logger *slog.Logger
}

// Manifest captures release metadata.
type Manifest struct {
	BuiltAt   time.Time        `json:"built_at"`
	GoVersion string           `json:"go_version"`
	GitCommit string           `json:"git_commit"`
	Targets   []TargetArtifact `json:"targets"`
}

// TargetArtifact describes a built binary.
type TargetArtifact struct {
	OS     string       `json:"os"`
	Arch   string       `json:"arch"`
	Binary string       `json:"binary"`
	Size   int64        `json:"size"`
	SHA256 string       `json:"sha256"`
	Smoke  SmokeOutcome `json:"smoke"`
}

// SmokeOutcome records the result of running the smoke command.
type SmokeOutcome struct {
	Status        string   `json:"status"` // passed, failed, skipped
	Command       []string `json:"command"`
	Output        string   `json:"output,omitempty"`
	Error         string   `json:"error,omitempty"`
	SkippedReason string   `json:"skipped_reason,omitempty"`
}

// Build assembles release binaries and writes a manifest.
func Build(ctx context.Context, opts Options) (*Manifest, error) {
	if err := opts.applyDefaults(); err != nil {
		return nil, err
	}

	logger := opts.logger()
	logger.Info("starting release build", "dist", opts.DistDir)

	if err := os.MkdirAll(opts.DistDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create dist directory: %w", err)
	}

	goVersion, err := detectGoVersion(ctx, opts.ProjectRoot)
	if err != nil {
		return nil, err
	}

	gitCommit, err := detectGitCommit(ctx, opts.ProjectRoot)
	if err != nil {
		return nil, err
	}
	if gitCommit == "unknown" {
		logger.Warn("git commit could not be determined", "project_root", opts.ProjectRoot)
	}

	manifest := &Manifest{
		BuiltAt:   time.Now().UTC(),
		GoVersion: goVersion,
		GitCommit: gitCommit,
	}

	for _, target := range opts.Targets {
		artifact, err := buildTarget(ctx, opts, target, logger)
		if err != nil {
			return nil, err
		}
		manifest.Targets = append(manifest.Targets, *artifact)
	}

	manifestPath := filepath.Join(opts.DistDir, "manifest.json")
	if err := fsutil.AtomicWriteJSON(manifestPath, manifest); err != nil {
		return nil, fmt.Errorf("failed to write manifest: %w", err)
	}

	logger.Info("release build complete", "targets", len(manifest.Targets))
	return manifest, nil
}

func (opts *Options) applyDefaults() error {
	if opts.ProjectRoot == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to determine working directory: %w", err)
		}
		opts.ProjectRoot = wd
	}

	if opts.DistDir == "" {
		opts.DistDir = filepath.Join(opts.ProjectRoot, "dist")
	} else if !filepath.IsAbs(opts.DistDir) {
		opts.DistDir = filepath.Join(opts.ProjectRoot, opts.DistDir)
	}

	if len(opts.Targets) == 0 {
		opts.Targets = append([]Target(nil), DefaultTargets...)
	}

	if opts.MainPackage == "" {
		opts.MainPackage = "./cmd/lorch"
	}

	return nil
}

func (opts *Options) logger() *slog.Logger {
	if opts.Logger != nil {
		return opts.Logger
	}
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func buildTarget(ctx context.Context, opts Options, target Target, logger *slog.Logger) (*TargetArtifact, error) {
	targetLabel := fmt.Sprintf("%s/%s", target.GOOS, target.GOARCH)
	logger.Info("building target", "target", targetLabel)

	targetDir := filepath.Join(opts.DistDir, fmt.Sprintf("%s-%s", target.GOOS, target.GOARCH))
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create target directory %s: %w", targetDir, err)
	}

	cacheDir := filepath.Join(targetDir, ".gocache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create build cache directory: %w", err)
	}
	defer os.RemoveAll(cacheDir)

	binaryName := "lorch"
	if target.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(targetDir, binaryName)

	// Remove existing binary to avoid stale artifacts.
	_ = os.Remove(binaryPath)

	buildCmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", binaryPath, opts.MainPackage)
	buildCmd.Dir = opts.ProjectRoot
	env := os.Environ()
	env = setEnv(env, "CGO_ENABLED", "0")
	env = setEnv(env, "GOOS", target.GOOS)
	env = setEnv(env, "GOARCH", target.GOARCH)
	env = setEnv(env, "GOFLAGS", "-trimpath")
	env = setEnv(env, "GOCACHE", cacheDir)
	buildCmd.Env = env

	var stderr bytes.Buffer
	buildCmd.Stderr = &stderr

	if err := buildCmd.Run(); err != nil {
		return nil, fmt.Errorf("go build failed for %s: %w\n%s", targetLabel, err, stderr.String())
	}

	info, err := os.Stat(binaryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat binary: %w", err)
	}

	sum, err := checksum.SHA256File(binaryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to compute checksum: %w", err)
	}

	relBinary := binaryPath
	if rel, err := filepath.Rel(opts.ProjectRoot, binaryPath); err == nil {
		relBinary = rel
	}

	smoke := SmokeOutcome{
		Command: []string{relBinary, "--help"},
	}

	if opts.SkipSmoke {
		smoke.Status = "skipped"
		smoke.SkippedReason = "smoke tests disabled"
	} else {
		smoke = runSmoke(ctx, opts.ProjectRoot, binaryPath, target)
		smoke.Command = []string{relBinary, "--help"}
	}

	return &TargetArtifact{
		OS:     target.GOOS,
		Arch:   target.GOARCH,
		Binary: relBinary,
		Size:   info.Size(),
		SHA256: sum,
		Smoke:  smoke,
	}, nil
}

func detectGoVersion(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", "env", "GOVERSION")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to determine Go version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func detectGitCommit(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "unknown", nil
	}
	return strings.TrimSpace(string(out)), nil
}

func runSmoke(ctx context.Context, projectRoot, binaryPath string, target Target) SmokeOutcome {
	result := SmokeOutcome{
		Command: []string{binaryPath, "--help"},
	}

	if target.GOOS != runtime.GOOS {
		result.Status = "skipped"
		result.SkippedReason = "non-native operating system"
		return result
	}
	if target.GOARCH != runtime.GOARCH {
		result.Status = "skipped"
		result.SkippedReason = "non-native architecture"
		return result
	}

	cmd := exec.CommandContext(ctx, binaryPath, "--help")
	cmd.Dir = projectRoot
	cmd.Env = os.Environ()

	output, err := cmd.CombinedOutput()
	result.Output = string(output)

	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
	} else {
		result.Status = "passed"
	}

	return result
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
