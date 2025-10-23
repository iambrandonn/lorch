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
	SaveReceiptWithIndex(receiptPath string, receipt *Receipt) error
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
	// Orchestration-specific events
	SendOrchestrationProposedTasksEvent(cmd *protocol.Command, planCandidates []map[string]any, derivedTasks []map[string]any, notes string) error
	SendOrchestrationNeedsClarificationEvent(cmd *protocol.Command, questions []string, notes string) error
	SendOrchestrationPlanConflictEvent(cmd *protocol.Command, candidates []map[string]any, reason string) error
}

// AgentConfig holds configuration for the LLM agent
type AgentConfig struct {
	Role      protocol.AgentType
	LLMCLI    string
	Workspace string
	Logger    *slog.Logger
	MaxMessageBytes int
}

// LLMAgent interface is defined in agent.go
