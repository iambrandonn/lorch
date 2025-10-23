package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/iambrandonn/lorch/internal/protocol"
)

func main() {
	var (
		role      = flag.String("role", "", "Agent role (builder, reviewer, spec_maintainer, orchestration)")
		llmCLI    = flag.String("llm-cli", "claude", "LLM CLI command (claude, codex, etc.)")
		workspace = flag.String("workspace", ".", "Workspace root")
		logLevel  = flag.String("log-level", "info", "Log level")
	)
	flag.Parse()

	if *role == "" {
		fmt.Fprintf(os.Stderr, "error: --role is required\n")
		os.Exit(1)
	}

	// Set up logger
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: parseLogLevel(*logLevel),
	}))

	// Create context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Info("received shutdown signal")
		cancel()
	}()

	// Create agent configuration
	cfg := &AgentConfig{
		Role:      protocol.AgentType(*role),
		LLMCLI:    *llmCLI,
		Workspace: *workspace,
		Logger:    logger,
		MaxMessageBytes: 256 * 1024, // 256 KiB default (Spec §12)
	}

	// Create agent
	agent, err := NewLLMAgent(cfg)
	if err != nil {
		logger.Error("failed to create agent", "error", err)
		os.Exit(1)
	}

	// Run NDJSON I/O loop
	if err := agent.Run(ctx, os.Stdin, os.Stdout, os.Stderr); err != nil {
		logger.Error("agent failed", "error", err)
		os.Exit(1)
	}
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
