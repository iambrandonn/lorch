package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/iambrandonn/lorch/internal/agent/script"
	"github.com/iambrandonn/lorch/internal/fixtureagent"
)

func main() {
	var (
		roleFlag      = flag.String("role", "", "Agent role (defaults to $CLAUDE_ROLE)")
		fixtureFlag   = flag.String("fixture", "", "Fixture script path (defaults to $CLAUDE_FIXTURE)")
		logLevelFlag  = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
		noHeartbeat   = flag.Bool("no-heartbeat", false, "Disable heartbeat emission")
		heartbeatFlag = flag.Duration("heartbeat-interval", 10*time.Second, "Heartbeat interval")
	)
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	role := strings.TrimSpace(*roleFlag)
	if role == "" {
		role = strings.TrimSpace(os.Getenv("CLAUDE_ROLE"))
	}
	if role == "" {
		fmt.Fprintln(os.Stderr, "role must be provided via --role or CLAUDE_ROLE environment variable")
		os.Exit(2)
	}

	fixturePath := strings.TrimSpace(*fixtureFlag)
	if fixturePath == "" {
		fixturePath = strings.TrimSpace(os.Getenv("CLAUDE_FIXTURE"))
	}
	if fixturePath == "" {
		fmt.Fprintln(os.Stderr, "fixture path must be provided via --fixture or CLAUDE_FIXTURE environment variable")
		os.Exit(2)
	}

	s, err := script.Load(fixturePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load fixture: %v\n", err)
		os.Exit(1)
	}

	level, _, err := fixtureagent.ParseLogLevel(*logLevelFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid log level: %v\n", err)
		os.Exit(2)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	}))

	agent, err := fixtureagent.New(role, fixtureagent.Options{
		Logger:            logger,
		HeartbeatInterval: *heartbeatFlag,
		DisableHeartbeat:  *noHeartbeat,
		Script:            s,
	})
	if err != nil {
		logger.Error("failed to create fixture agent", "error", err)
		os.Exit(1)
	}

	if err := agent.Run(ctx, os.Stdin, os.Stdout); err != nil {
		logger.Error("fixture agent failed", "error", err)
		os.Exit(1)
	}
}
