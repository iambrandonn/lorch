package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

func main() {
	var (
		roleFlag      = flag.String("role", "", "Agent role (builder, reviewer, spec_maintainer, orchestration)")
		workspaceFlag = flag.String("workspace", "", "Workspace root (defaults to current directory)")
		binaryFlag    = flag.String("bin", "", "Override path to Claude CLI (defaults to $CLAUDE_CLI or 'claude')")
		logLevelFlag  = flag.String("log-level", "info", "Log level for shim diagnostics (debug, info, warn, error)")
		fixtureFlag   = flag.String("fixture", "", "Optional fixture path forwarded to the underlying CLI")
	)
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [flags] [-- additional CLI args]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logLevel, logLevelName, err := parseLogLevel(*logLevelFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid log level: %v\n", err)
		os.Exit(2)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))

	binaryPath := resolveBinary(*binaryFlag)

	workspace := *workspaceFlag
	if workspace == "" {
		if cwd, err := os.Getwd(); err == nil {
			workspace = cwd
		}
	}

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		logger.Error("failed to resolve workspace path", "workspace", workspace, "error", err)
		os.Exit(1)
	}

	cfg := Config{
		Role:        *roleFlag,
		Workspace:   absWorkspace,
		Binary:      binaryPath,
		LogLevel:    logLevelName,
		FixturePath: strings.TrimSpace(*fixtureFlag),
		Args:        flag.Args(),
		BaseEnv:     os.Environ(),
	}

	if err := cfg.NormalizeAndValidate(); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(2)
	}

	if err := Run(ctx, cfg, logger, os.Stdin, os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Warn("claude agent interrupted", "error", err)
			os.Exit(1)
		}
		logger.Error("claude agent failed", "error", err)
		os.Exit(1)
	}
}

func resolveBinary(binFlag string) string {
	if binFlag != "" {
		return binFlag
	}
	if env := strings.TrimSpace(os.Getenv("CLAUDE_CLI")); env != "" {
		return env
	}
	return "claude"
}
