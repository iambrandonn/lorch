package script

import (
	"encoding/json"
	"fmt"
	"os"
)

// Script represents a scripted set of responses for mock agents.
type Script struct {
	Responses map[string]ResponseTemplate `json:"responses"`
}

// ResponseTemplate describes how to respond to a specific command action.
type ResponseTemplate struct {
	Events   []EventTemplate `json:"events,omitempty"`
	DelayMs  int             `json:"delay_ms,omitempty"`
	Error    string          `json:"error,omitempty"`
	Metadata map[string]any  `json:"metadata,omitempty"`
}

// EventTemplate describes an event emitted by a scripted agent.
type EventTemplate struct {
	Type      string             `json:"type"`
	Status    string             `json:"status,omitempty"`
	Payload   map[string]any     `json:"payload,omitempty"`
	Artifacts []ArtifactTemplate `json:"artifacts,omitempty"`
}

// ArtifactTemplate describes an artifact to include in a scripted event.
type ArtifactTemplate struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Load reads a script from the provided path.
func Load(path string) (*Script, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read script: %w", err)
	}

	var script Script
	if err := json.Unmarshal(data, &script); err != nil {
		return nil, fmt.Errorf("parse script JSON: %w", err)
	}

	if len(script.Responses) == 0 {
		return nil, fmt.Errorf("script has no responses defined")
	}

	return &script, nil
}
