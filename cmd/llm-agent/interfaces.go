package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
)

// LLMCaller defines the interface for calling external LLM CLI tools
type LLMCaller interface {
	Call(ctx context.Context, prompt string) (string, error)
}

// LLMCallerFunc is a function type that implements LLMCaller
type LLMCallerFunc func(ctx context.Context, prompt string) (string, error)

func (f LLMCallerFunc) Call(ctx context.Context, prompt string) (string, error) {
	return f(ctx, prompt)
}

// ReceiptStore defines the interface for managing idempotency receipts
type ReceiptStore interface {
	LoadReceipt(path string) (*Receipt, error)
	SaveReceipt(path string, receipt *Receipt) error
	FindReceiptByIK(taskID, action, ik string) (*Receipt, string, error)
}

// Receipt represents a stored receipt for idempotency
type Receipt struct {
	TaskID         string              `json:"task_id"`
	Step           int                 `json:"step"`
	IdempotencyKey string              `json:"idempotency_key"`
	Artifacts      []protocol.Artifact `json:"artifacts"`
	Events         []string            `json:"events"`
	CreatedAt      time.Time           `json:"created_at"`
}

// FSProvider defines the interface for filesystem operations
type FSProvider interface {
	ResolveWorkspacePath(workspace, relative string) (string, error)
	ReadFileSafe(path string, maxSize int64) (string, error)
	WriteArtifactAtomic(workspace, relativePath string, content []byte) (protocol.Artifact, error)
}

// EventEmitter defines the interface for emitting protocol events
type EventEmitter interface {
	NewEvent(cmd *protocol.Command, eventName string) protocol.Event
	EncodeEventCapped(evt protocol.Event) error
	SendErrorEvent(cmd *protocol.Command, code, message string) error
	SendArtifactProducedEvent(cmd *protocol.Command, artifact protocol.Artifact) error
	SendLog(level, message string, fields map[string]any) error
}

// AgentConfig holds configuration for the LLM agent
type AgentConfig struct {
	Role      protocol.AgentType
	LLMCLI    string
	Workspace string
	Logger    *slog.Logger
}

// LLMAgent represents the main agent implementation
type LLMAgent struct {
	config AgentConfig
	// Interfaces will be injected
	llmCaller    LLMCaller
	receiptStore ReceiptStore
	fsProvider   FSProvider
	eventEmitter EventEmitter
}

// NewLLMAgent creates a new LLM agent with the given configuration
func NewLLMAgent(cfg *AgentConfig) (*LLMAgent, error) {
	// TODO: Initialize interfaces with real implementations
	// For now, return a stub that can be used for testing
	return &LLMAgent{
		config: *cfg,
		// Interfaces will be injected by workstreams
	}, nil
}

// Run starts the agent's NDJSON I/O loop
func (a *LLMAgent) Run(ctx context.Context, stdin, stdout, stderr interface{}) error {
	// TODO: Implement NDJSON I/O loop
	// This is a stub for parallel development
	return nil
}
