package fixtureagent

import (
	"fmt"
	"log/slog"
	"strings"
)

// ParseLogLevel normalizes a textual log level to slog.Level.
func ParseLogLevel(input string) (slog.Level, string, error) {
	level := strings.ToLower(strings.TrimSpace(input))
	switch level {
	case "", "info":
		return slog.LevelInfo, "info", nil
	case "debug":
		return slog.LevelDebug, "debug", nil
	case "warn", "warning":
		return slog.LevelWarn, "warn", nil
	case "error", "err":
		return slog.LevelError, "error", nil
	default:
		return slog.LevelInfo, "", fmt.Errorf("unsupported log level %q", input)
	}
}
