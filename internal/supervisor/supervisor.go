package supervisor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/iambrandonn/lorch/internal/ndjson"
	"github.com/iambrandonn/lorch/internal/protocol"
)

// AgentSupervisor manages a single agent subprocess
type AgentSupervisor struct {
	agentType protocol.AgentType
	cmd       []string
	env       map[string]string
	logger    *slog.Logger

	mu            sync.Mutex
	process       *exec.Cmd
	encoder       *ndjson.Encoder
	decoder       *ndjson.Decoder
	stdin         io.WriteCloser
	stdout        io.ReadCloser
	stderr        io.ReadCloser
	running       bool
	lastHeartbeat time.Time
	exitChan      chan error // Receives the result of proc.Wait() from waitForExit

	// Channels for messages
	events     chan *protocol.Event
	heartbeats chan *protocol.Heartbeat
	logs       chan *protocol.Log
	stderrLines chan string
}

// NewAgentSupervisor creates a new agent supervisor
func NewAgentSupervisor(
	agentType protocol.AgentType,
	cmd []string,
	env map[string]string,
	logger *slog.Logger,
) *AgentSupervisor {
	return &AgentSupervisor{
		agentType:   agentType,
		cmd:         cmd,
		env:         env,
		logger:      logger,
		events:      make(chan *protocol.Event, 100),
		heartbeats:  make(chan *protocol.Heartbeat, 10),
		logs:        make(chan *protocol.Log, 50),
		stderrLines: make(chan string, 100),
	}
}

// Start launches the agent subprocess
func (s *AgentSupervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("agent already running")
	}
	s.mu.Unlock()

	s.logger.Info("starting agent", "type", s.agentType, "cmd", s.cmd)

	// Create command
	proc := exec.CommandContext(ctx, s.cmd[0], s.cmd[1:]...)

	// Set environment - inherit parent environment first, then add custom vars
	proc.Env = os.Environ()
	proc.Env = append(proc.Env, fmt.Sprintf("AGENT_TYPE=%s", s.agentType))
	for k, v := range s.env {
		proc.Env = append(proc.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Setup pipes
	stdin, err := proc.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := proc.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := proc.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start process
	if err := proc.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return fmt.Errorf("failed to start process: %w", err)
	}

	s.mu.Lock()
	s.process = proc
	s.stdin = stdin
	s.stdout = stdout
	s.stderr = stderr
	s.encoder = ndjson.NewEncoder(stdin, s.logger)
	s.decoder = ndjson.NewDecoder(stdout, s.logger)
	s.running = true
	s.lastHeartbeat = time.Now()
	s.exitChan = make(chan error, 1) // Buffered to prevent goroutine leak
	s.mu.Unlock()

	s.logger.Info("agent started", "type", s.agentType, "pid", proc.Process.Pid)

	// Start IO goroutines
	go s.readStdout(ctx)
	go s.readStderr(ctx)
	go s.waitForExit(ctx)

	return nil
}

// Stop gracefully stops the agent
func (s *AgentSupervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}

	proc := s.process
	stdin := s.stdin
	exitChan := s.exitChan
	s.mu.Unlock()

	s.logger.Info("stopping agent", "type", s.agentType)

	// Close stdin to signal EOF
	if stdin != nil {
		stdin.Close()
	}

	// Wait for process to exit via the waitForExit goroutine (with timeout)
	// Don't call proc.Wait() here - waitForExit already does that
	select {
	case <-ctx.Done():
		// Context cancelled, force kill
		if proc.Process != nil {
			proc.Process.Kill()
		}
		return ctx.Err()
	case err := <-exitChan:
		// Process exited - waitForExit already set running=false
		if err != nil {
			s.logger.Warn("agent exited with error", "type", s.agentType, "error", err)
		} else {
			s.logger.Info("agent stopped", "type", s.agentType)
		}
		return err
	case <-time.After(5 * time.Second):
		// Timeout, force kill
		s.logger.Warn("agent did not stop gracefully, killing", "type", s.agentType)
		if proc.Process != nil {
			proc.Process.Kill()
		}
		return fmt.Errorf("agent stop timeout")
	}
}

// SendCommand sends a command to the agent
func (s *AgentSupervisor) SendCommand(cmd *protocol.Command) error {
	s.mu.Lock()
	encoder := s.encoder
	running := s.running
	s.mu.Unlock()

	if !running {
		return fmt.Errorf("agent not running")
	}

	if encoder == nil {
		return fmt.Errorf("encoder not initialized")
	}

	s.logger.Debug("sending command", "type", s.agentType, "action", cmd.Action, "task_id", cmd.TaskID)

	return encoder.Encode(cmd)
}

// Events returns the channel for receiving events from the agent
func (s *AgentSupervisor) Events() <-chan *protocol.Event {
	return s.events
}

// Heartbeats returns the channel for receiving heartbeats from the agent
func (s *AgentSupervisor) Heartbeats() <-chan *protocol.Heartbeat {
	return s.heartbeats
}

// Logs returns the channel for receiving log messages from the agent
func (s *AgentSupervisor) Logs() <-chan *protocol.Log {
	return s.logs
}

// StderrLines returns the channel for receiving stderr output from the agent
func (s *AgentSupervisor) StderrLines() <-chan string {
	return s.stderrLines
}

// IsRunning returns true if the agent is running
func (s *AgentSupervisor) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// LastHeartbeat returns the time of the last received heartbeat
func (s *AgentSupervisor) LastHeartbeat() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastHeartbeat
}

func (s *AgentSupervisor) readStdout(ctx context.Context) {
	defer close(s.events)
	defer close(s.heartbeats)
	defer close(s.logs)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := s.decoder.DecodeEnvelope()
		if err == io.EOF {
			s.logger.Info("agent stdout closed", "type", s.agentType)
			return
		}
		if err != nil {
			s.logger.Error("failed to decode message from agent",
				"type", s.agentType,
				"error", err)
			continue
		}

		// Route message to appropriate channel
		switch v := msg.(type) {
		case *protocol.Event:
			select {
			case s.events <- v:
			case <-ctx.Done():
				return
			}

		case *protocol.Heartbeat:
			s.mu.Lock()
			s.lastHeartbeat = time.Now()
			s.mu.Unlock()

			s.logger.Debug("received heartbeat",
				"type", s.agentType,
				"seq", v.Seq,
				"status", v.Status)

			select {
			case s.heartbeats <- v:
			case <-ctx.Done():
				return
			}

		case *protocol.Log:
			select {
			case s.logs <- v:
			case <-ctx.Done():
				return
			}

		default:
			s.logger.Warn("unexpected message type from agent",
				"type", s.agentType,
				"msg_type", fmt.Sprintf("%T", msg))
		}
	}
}

func (s *AgentSupervisor) readStderr(ctx context.Context) {
	s.mu.Lock()
	stderr := s.stderr
	s.mu.Unlock()

	if stderr == nil {
		return
	}

	defer close(s.stderrLines)

	// Use a scanner to read line-by-line
	scanner := bufio.NewScanner(stderr)
	// Set a larger buffer size to handle long lines
	scanner.Buffer(make([]byte, 4096), 1024*1024) // 1MB max line length

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()

		// Still log for debugging purposes
		s.logger.Debug("agent stderr",
			"type", s.agentType,
			"line", line)

		// Send line to channel for CLI consumption
		select {
		case s.stderrLines <- line:
		case <-ctx.Done():
			return
		default:
			// If channel is full, skip (shouldn't happen with buffered channel)
			s.logger.Warn("stderr channel full, dropping line", "type", s.agentType)
		}
	}

	if err := scanner.Err(); err != nil {
		if err != io.EOF {
			s.logger.Error("error reading stderr", "type", s.agentType, "error", err)
		}
	}
}

func (s *AgentSupervisor) waitForExit(ctx context.Context) {
	s.mu.Lock()
	proc := s.process
	exitChan := s.exitChan
	s.mu.Unlock()

	if proc == nil {
		return
	}

	err := proc.Wait()

	s.mu.Lock()
	s.running = false
	s.mu.Unlock()

	// Send the exit result to the channel for Stop() to receive
	if exitChan != nil {
		exitChan <- err
	}

	if err != nil {
		s.logger.Warn("agent process exited",
			"type", s.agentType,
			"error", err)
	} else {
		s.logger.Info("agent process exited cleanly",
			"type", s.agentType)
	}
}
