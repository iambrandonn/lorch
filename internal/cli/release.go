package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/iambrandonn/lorch/internal/release"
	"github.com/spf13/cobra"
)

var releaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Build release binaries and manifest",
	Long:  "Cross-compile lorch binaries for supported targets, run smoke checks, and emit dist/manifest.json.",
	RunE:  runRelease,
}

func init() {
	releaseCmd.Flags().String("dist", "dist", "Output directory for release artifacts (relative or absolute)")
	releaseCmd.Flags().Bool("skip-smoke", false, "Skip running smoke tests against built binaries")
	releaseCmd.Flags().StringSlice("target", nil, "Limit builds to specific GOOS/GOARCH pairs (e.g., darwin/arm64)")
	rootCmd.AddCommand(releaseCmd)
}

func runRelease(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	projectRoot, err := findModuleRoot()
	if err != nil {
		return err
	}
	logger.Info("project root detected", "path", projectRoot)

	distDir, err := cmd.Flags().GetString("dist")
	if err != nil {
		return err
	}
	skipSmoke, err := cmd.Flags().GetBool("skip-smoke")
	if err != nil {
		return err
	}
	targetStrings, err := cmd.Flags().GetStringSlice("target")
	if err != nil {
		return err
	}

	targets, err := parseReleaseTargets(targetStrings)
	if err != nil {
		return err
	}

	opts := release.Options{
		ProjectRoot: projectRoot,
		DistDir:     distDir,
		Targets:     targets,
		SkipSmoke:   skipSmoke,
		Logger:      logger,
	}

	manifest, err := release.Build(ctx, opts)
	if err != nil {
		return err
	}

	manifestPath := filepath.Join(opts.DistDir, "manifest.json")
	logger.Info("manifest written", "path", manifestPath)

	for _, artifact := range manifest.Targets {
		logger.Info("artifact produced",
			"target", fmt.Sprintf("%s/%s", artifact.OS, artifact.Arch),
			"path", artifact.Binary,
			"sha256", artifact.SHA256,
			"smoke", artifact.Smoke.Status)
	}

	return nil
}

func parseReleaseTargets(values []string) ([]release.Target, error) {
	if len(values) == 0 {
		return nil, nil
	}

	targets := make([]release.Target, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parts := strings.Split(value, "/")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid target %q (expected format GOOS/GOARCH)", value)
		}
		targets = append(targets, release.Target{GOOS: parts[0], GOARCH: parts[1]})
	}
	return targets, nil
}

func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found (starting from %s)", dir)
		}
		dir = parent
	}
}
