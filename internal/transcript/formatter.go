package transcript

import (
	"fmt"
	"strings"

	"github.com/iambrandonn/lorch/internal/protocol"
)

// Formatter formats protocol messages for console output
type Formatter struct{}

// NewFormatter creates a new transcript formatter
func NewFormatter() *Formatter {
	return &Formatter{}
}

// FormatEvent formats an event for console display
func (f *Formatter) FormatEvent(evt *protocol.Event) string {
	agentType := string(evt.From.AgentType)

	// Build details based on event type
	var details string

	switch evt.Event {
	case protocol.EventBuilderCompleted:
		if tests, ok := evt.Payload["tests"].(map[string]any); ok {
			if status, ok := tests["status"].(string); ok {
				details = fmt.Sprintf("tests: %s", status)
			}
		}

	case protocol.EventReviewCompleted:
		details = fmt.Sprintf("status: %s", evt.Status)
		if len(evt.Artifacts) > 0 {
			details += fmt.Sprintf(", review: %s", evt.Artifacts[0].Path)
		}

	case protocol.EventSpecUpdated:
		details = "spec updated"

	case protocol.EventSpecNoChangesNeeded:
		details = "no changes needed"

	case protocol.EventSpecChangesRequested:
		details = "changes requested"
		if len(evt.Artifacts) > 0 {
			details += fmt.Sprintf(", notes: %s", evt.Artifacts[0].Path)
		}

	case protocol.EventArtifactProduced:
		if len(evt.Artifacts) > 0 {
			artifact := evt.Artifacts[0]
			size := f.formatSize(artifact.Size)
			details = fmt.Sprintf("%s (%s)", artifact.Path, size)
		}

	default:
		// Generic format
		if evt.Status != "" {
			details = fmt.Sprintf("status: %s", evt.Status)
		}
	}

	if details != "" {
		return fmt.Sprintf("[%s] %s: %s", agentType, evt.Event, details)
	}

	return fmt.Sprintf("[%s] %s", agentType, evt.Event)
}

// FormatHeartbeat formats a heartbeat for console display
func (f *Formatter) FormatHeartbeat(hb *protocol.Heartbeat) string {
	agentType := string(hb.Agent.AgentType)
	return fmt.Sprintf("[%s] heartbeat seq=%d status=%s uptime=%.1fs",
		agentType, hb.Seq, hb.Status, hb.UptimeS)
}

// FormatCommand formats a command for console display
func (f *Formatter) FormatCommand(cmd *protocol.Command) string {
	agentType := string(cmd.To.AgentType)
	return fmt.Sprintf("[lorch→%s] %s (task: %s)",
		agentType, cmd.Action, cmd.TaskID)
}

// FormatLog formats a log message for console display
func (f *Formatter) FormatLog(log *protocol.Log) string {
	level := strings.ToUpper(string(log.Level))
	return fmt.Sprintf("[LOG:%s] %s", level, log.Message)
}

// formatSize formats a byte size in a human-readable format
func (f *Formatter) formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GiB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MiB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KiB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
