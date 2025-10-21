package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

const (
	// InputKeyUserInstruction stores the user-provided natural language instruction.
	InputKeyUserInstruction = "user_instruction"
	// InputKeyDiscovery stores deterministic file discovery metadata produced by lorch.
	InputKeyDiscovery = "discovery"
	// InputKeyContext stores optional structured context shared with the orchestration agent.
	InputKeyContext = "context"
)

var (
	// ErrMissingUserInstruction indicates a command did not provide the required user instruction text.
	ErrMissingUserInstruction = errors.New("protocol: user_instruction is required")
	// ErrInvalidDiscoveryCandidate indicates a discovery candidate failed validation (missing path or score out of range).
	ErrInvalidDiscoveryCandidate = errors.New("protocol: discovery candidate invalid")
)

// OrchestrationInputs captures structured inputs expected by orchestration actions (`intake`, `task_discovery`).
type OrchestrationInputs struct {
	// UserInstruction contains the raw natural language directive supplied by the human operator.
	UserInstruction string `json:"user_instruction"`
	// Discovery contains deterministic file discovery metadata calculated by lorch prior to the command.
	Discovery *DiscoveryMetadata `json:"discovery,omitempty"`
	// Context allows lorch to pass additional structured context when re-issuing commands
	// (e.g., user's clarification answers, selected plan IDs, prior task_discovery results).
	// This helps keep clarification loops idempotent while still giving the orchestration agent fresh details.
	Context map[string]any `json:"context,omitempty"`
}

// DiscoveryMetadata captures deterministic plan/spec discovery details provided by lorch.
type DiscoveryMetadata struct {
	// Root is the absolute or workspace-relative root used for discovery (typically the workspace root).
	Root string `json:"root"`
	// Strategy identifies the discovery algorithm configuration (e.g., "heuristic:v1").
	Strategy string `json:"strategy"`
	// SearchPaths lists the directories traversed (relative to Root) in the order they were evaluated.
	SearchPaths []string `json:"search_paths"`
	// IgnoredPaths documents directories that were intentionally skipped (hidden dirs, node_modules, etc.).
	IgnoredPaths []string `json:"ignored_paths,omitempty"`
	// GeneratedAt records when discovery results were produced; helps with traceability and snapshot coupling.
	GeneratedAt time.Time `json:"generated_at"`
	// Candidates contains ranked plan/spec candidates surfaced to the orchestration agent.
	Candidates []DiscoveryCandidate `json:"candidates"`
}

// DiscoveryCandidate describes a single candidate plan/spec file produced by deterministic discovery.
type DiscoveryCandidate struct {
	// Path is the workspace-relative path to the candidate (e.g., "PLAN.md").
	Path string `json:"path"`
	// Score conveys the relative confidence assigned by deterministic heuristics (0.0–1.0).
	// Higher scores indicate stronger matches but are only comparable within a single discovery run.
	Score float64 `json:"score"`
	// Reason provides a human-readable explanation for logging or debugging.
	Reason string `json:"reason,omitempty"`
	// Headings captures the most relevant headings extracted from the document (optional).
	Headings []string `json:"headings,omitempty"`
}

// Validate ensures orchestration inputs satisfy minimum requirements for downstream processing.
func (oi OrchestrationInputs) Validate() error {
	if strings.TrimSpace(oi.UserInstruction) == "" {
		return ErrMissingUserInstruction
	}
	if oi.Discovery != nil {
		if err := oi.Discovery.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate ensures discovery metadata is self-consistent and deterministic assumptions hold.
func (dm DiscoveryMetadata) Validate() error {
	if strings.TrimSpace(dm.Root) == "" {
		return fmt.Errorf("protocol: discovery root required")
	}
	if strings.TrimSpace(dm.Strategy) == "" {
		return fmt.Errorf("protocol: discovery strategy required")
	}
	if len(dm.Candidates) == 0 {
		return fmt.Errorf("protocol: discovery candidates required")
	}
	for idx, candidate := range dm.Candidates {
		if err := candidate.Validate(); err != nil {
			return fmt.Errorf("protocol: candidate[%d]: %w", idx, err)
		}
	}
	return nil
}

// Validate ensures the candidate has a path and a score within the expected range.
func (dc DiscoveryCandidate) Validate() error {
	if strings.TrimSpace(dc.Path) == "" {
		return fmt.Errorf("%w: missing path", ErrInvalidDiscoveryCandidate)
	}
	if dc.Score < 0.0 || dc.Score > 1.0 {
		return fmt.Errorf("%w: score %.3f out of range [0.0,1.0]", ErrInvalidDiscoveryCandidate, dc.Score)
	}
	return nil
}

// ToInputsMap converts the orchestration inputs struct into the generic command inputs map.
func (oi OrchestrationInputs) ToInputsMap() (map[string]any, error) {
	if err := oi.Validate(); err != nil {
		return nil, err
	}

	raw := map[string]any{
		InputKeyUserInstruction: oi.UserInstruction,
	}
	if oi.Discovery != nil {
		m, err := marshalToMap(oi.Discovery)
		if err != nil {
			return nil, err
		}
		raw[InputKeyDiscovery] = m
	}
	if len(oi.Context) > 0 {
		raw[InputKeyContext] = oi.Context
	}
	return raw, nil
}

// ParseOrchestrationInputs decodes a generic command inputs map into structured orchestration inputs.
func ParseOrchestrationInputs(raw map[string]any) (OrchestrationInputs, error) {
	if raw == nil {
		return OrchestrationInputs{}, ErrMissingUserInstruction
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return OrchestrationInputs{}, fmt.Errorf("protocol: marshal inputs: %w", err)
	}

	var parsed OrchestrationInputs
	if err := json.Unmarshal(data, &parsed); err != nil {
		return OrchestrationInputs{}, fmt.Errorf("protocol: unmarshal orchestration inputs: %w", err)
	}

	if err := parsed.Validate(); err != nil {
		return OrchestrationInputs{}, err
	}

	return parsed, nil
}

func marshalToMap(v any) (map[string]any, error) {
	if v == nil {
		return nil, nil
	}
	// Fast-path: already a map[string]any
	if m, ok := v.(map[string]any); ok {
		return m, nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return nil, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("protocol: marshal to map: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("protocol: unmarshal to map: %w", err)
	}
	return out, nil
}
