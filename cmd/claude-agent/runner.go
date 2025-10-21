package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/iambrandonn/lorch/internal/protocol"
)

// Config captures runtime options for the Claude CLI shim.
type Config struct {
	Role        string   // logical agent role (builder, reviewer, spec_maintainer, orchestration)
	Workspace   string   // absolute workspace path
	Binary      string   // path to claude CLI (or equivalent)
	LogLevel    string   // normalized log level string (lowercase)
	FixturePath string   // optional fixture path propagated via CLAUDE_FIXTURE
	Args        []string // passthrough arguments for the underlying CLI
	BaseEnv     []string // base environment variables (defaults to os.Environ)

	normalizedRole string
}

// NormalizeAndValidate resolves defaults and validates the configuration.
func (c *Config) NormalizeAndValidate() error {
	if strings.TrimSpace(c.Role) == "" {
		return errors.New("role is required")
	}

	role := normalizeRole(c.Role)
	if role == "" {
		return fmt.Errorf("unsupported role %q (valid: builder, reviewer, spec_maintainer, orchestration)", c.Role)
	}
	c.normalizedRole = role

	if strings.TrimSpace(c.Workspace) == "" {
		return errors.New("workspace is required")
	}

	info, err := os.Stat(c.Workspace)
	if err != nil {
		return fmt.Errorf("workspace check failed: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace must be a directory: %s", c.Workspace)
	}

	if strings.TrimSpace(c.Binary) == "" {
		return errors.New("binary path is required")
	}

	if c.BaseEnv == nil {
		c.BaseEnv = os.Environ()
	}

	if strings.TrimSpace(c.LogLevel) == "" {
		c.LogLevel = "info"
	}

	return nil
}

// BuildCommand constructs the exec.Cmd that will run the underlying CLI.
func (c Config) BuildCommand(ctx context.Context) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, c.Binary, c.Args...)
	cmd.Env = append([]string{}, c.BaseEnv...)
	cmd.Env = setEnv(cmd.Env, "CLAUDE_ROLE", c.normalizedRole)
	cmd.Env = setEnv(cmd.Env, "CLAUDE_WORKSPACE", c.Workspace)
	cmd.Env = setEnv(cmd.Env, "CLAUDE_LOG_LEVEL", c.LogLevel)
	if c.FixturePath != "" {
		if !filepath.IsAbs(c.FixturePath) {
			if abs, err := filepath.Abs(c.FixturePath); err == nil {
				cmd.Env = setEnv(cmd.Env, "CLAUDE_FIXTURE", abs)
			} else {
				return nil, fmt.Errorf("resolve fixture path: %w", err)
			}
		} else {
			cmd.Env = setEnv(cmd.Env, "CLAUDE_FIXTURE", c.FixturePath)
		}
	}
	cmd.Dir = c.Workspace
	return cmd, nil
}

// Run executes the shim with the provided configuration.
func Run(ctx context.Context, cfg Config, logger *slog.Logger, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd, err := cfg.BuildCommand(ctx)
	if err != nil {
		return err
	}

	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	logger.Info("launching claude CLI",
		"binary", cfg.Binary,
		"args", cfg.Args,
		"role", cfg.normalizedRole,
		"workspace", cfg.Workspace)

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		switch {
		case errors.Is(err, context.Canceled):
			return context.Canceled
		case errors.As(err, &exitErr):
			logger.Error("claude CLI exited with error",
				"exit_code", exitErr.ExitCode(),
				"stderr", string(exitErr.Stderr))
			return err
		default:
			return err
		}
	}

	logger.Info("claude CLI exited successfully")
	return nil
}

func normalizeRole(role string) string {
	s := strings.ToLower(strings.TrimSpace(role))
	s = strings.ReplaceAll(s, "-", "_")
	switch protocol.AgentType(s) {
	case protocol.AgentTypeBuilder,
		protocol.AgentTypeReviewer,
		protocol.AgentTypeSpecMaintainer,
		protocol.AgentTypeOrchestration:
		return s
	default:
		return ""
	}
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
