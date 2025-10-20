package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config represents the lorch.json configuration file
type Config struct {
	Version       string  `json:"version"`
	WorkspaceRoot string  `json:"workspace_root"`
	Policy        Policy  `json:"policy"`
	Agents        Agents  `json:"agents"`
	Tasks         []Task  `json:"tasks"`
}

// Policy contains orchestrator policy settings
type Policy struct {
	Concurrency          int    `json:"concurrency"`
	MessageMaxBytes      int    `json:"message_max_bytes"`
	ArtifactMaxBytes     int    `json:"artifact_max_bytes"`
	Retry                Retry  `json:"retry"`
	StrictVersionPinning bool   `json:"strict_version_pinning"`
	ParallelReviews      bool   `json:"parallel_reviews"`
	RedactSecretsInLogs  bool   `json:"redact_secrets_in_logs"`
}

// Retry contains retry policy configuration
type Retry struct {
	MaxAttempts int     `json:"max_attempts"`
	Backoff     Backoff `json:"backoff"`
}

// Backoff contains exponential backoff configuration
type Backoff struct {
	InitialMs  int     `json:"initial_ms"`
	MaxMs      int     `json:"max_ms"`
	Multiplier float64 `json:"multiplier"`
	Jitter     string  `json:"jitter"`
}

// Agents contains configuration for all agent types
type Agents struct {
	Builder        *AgentConfig `json:"builder"`
	Reviewer       *AgentConfig `json:"reviewer"`
	SpecMaintainer *AgentConfig `json:"spec_maintainer"`
	Orchestration  *AgentConfig `json:"orchestration"`
}

// AgentConfig contains configuration for a single agent
type AgentConfig struct {
	Enabled            bool              `json:"enabled,omitempty"`
	Cmd                []string          `json:"cmd"`
	HeartbeatIntervalS int               `json:"heartbeat_interval_s,omitempty"`
	TimeoutsS          map[string]int    `json:"timeouts_s,omitempty"`
	Env                map[string]string `json:"env,omitempty"`
}

// Task represents a development task
type Task struct {
	ID   string `json:"id"`
	Goal string `json:"goal"`
}

// GenerateDefault creates a new Config with default values matching MASTER-SPEC §8.2
func GenerateDefault() *Config {
	return &Config{
		Version:       "1.0",
		WorkspaceRoot: ".",
		Policy: Policy{
			Concurrency:      1,
			MessageMaxBytes:  262144,
			ArtifactMaxBytes: 1073741824,
			Retry: Retry{
				MaxAttempts: 3,
				Backoff: Backoff{
					InitialMs:  1000,
					MaxMs:      60000,
					Multiplier: 2.0,
					Jitter:     "full",
				},
			},
			StrictVersionPinning: true,
			ParallelReviews:      false,
			RedactSecretsInLogs:  true,
		},
		Agents: Agents{
			Builder: &AgentConfig{
				Cmd:                []string{"claude"},
				HeartbeatIntervalS: 10,
				TimeoutsS: map[string]int{
					"implement":         600,
					"implement_changes": 600,
				},
				Env: map[string]string{
					"CLAUDE_AGENT_ROLE": "builder",
					"LOG_LEVEL":         "info",
				},
			},
			Reviewer: &AgentConfig{
				Cmd:                []string{"claude"},
				HeartbeatIntervalS: 10,
				TimeoutsS: map[string]int{
					"review": 300,
				},
				Env: map[string]string{
					"CLAUDE_AGENT_ROLE": "reviewer",
				},
			},
			SpecMaintainer: &AgentConfig{
				Cmd: []string{"claude"},
				TimeoutsS: map[string]int{
					"update_spec": 180,
				},
				Env: map[string]string{
					"CLAUDE_AGENT_ROLE": "spec_maintainer",
				},
			},
			Orchestration: &AgentConfig{
				Enabled: true,
				Cmd:     []string{"claude"},
				TimeoutsS: map[string]int{
					"intake":          180,
					"task_discovery": 180,
				},
				Env: map[string]string{
					"CLAUDE_AGENT_ROLE": "orchestration",
				},
			},
		},
		Tasks: []Task{},
	}
}

// Validate checks the configuration for errors and returns user-friendly error messages
func (c *Config) Validate() error {
	// Version is required
	if c.Version == "" {
		return fmt.Errorf("configuration error: missing required field 'version'\n\nHint: Add a version field like:\n  \"version\": \"1.0\"")
	}

	// Concurrency must be exactly 1 in Phase 1
	if c.Policy.Concurrency != 1 {
		return fmt.Errorf("configuration error: invalid 'policy.concurrency' value: %d\n\nHint: Concurrency must be 1 (single-agent-at-a-time). Update your config:\n  \"policy\": {\n    \"concurrency\": 1\n  }", c.Policy.Concurrency)
	}

	// Required agents: builder, reviewer, spec_maintainer
	if c.Agents.Builder == nil {
		return fmt.Errorf("configuration error: missing required agent 'builder'\n\nHint: Add a builder agent configuration:\n  \"agents\": {\n    \"builder\": {\n      \"cmd\": [\"claude\"],\n      \"env\": {\"CLAUDE_AGENT_ROLE\": \"builder\"}\n    }\n  }")
	}

	if c.Agents.Reviewer == nil {
		return fmt.Errorf("configuration error: missing required agent 'reviewer'\n\nHint: Add a reviewer agent configuration:\n  \"agents\": {\n    \"reviewer\": {\n      \"cmd\": [\"claude\"],\n      \"env\": {\"CLAUDE_AGENT_ROLE\": \"reviewer\"}\n    }\n  }")
	}

	if c.Agents.SpecMaintainer == nil {
		return fmt.Errorf("configuration error: missing required agent 'spec_maintainer'\n\nHint: Add a spec_maintainer agent configuration:\n  \"agents\": {\n    \"spec_maintainer\": {\n      \"cmd\": [\"claude\"],\n      \"env\": {\"CLAUDE_AGENT_ROLE\": \"spec_maintainer\"}\n    }\n  }")
	}

	// Validate each agent config
	agents := map[string]*AgentConfig{
		"builder":         c.Agents.Builder,
		"reviewer":        c.Agents.Reviewer,
		"spec_maintainer": c.Agents.SpecMaintainer,
	}

	if c.Agents.Orchestration != nil {
		agents["orchestration"] = c.Agents.Orchestration
	}

	for name, agent := range agents {
		if err := agent.Validate(name); err != nil {
			return err
		}
	}

	return nil
}

// Validate checks an agent configuration for errors
func (a *AgentConfig) Validate(agentName string) error {
	if len(a.Cmd) == 0 {
		return fmt.Errorf("configuration error: agent '%s' has empty 'cmd' field\n\nHint: Specify the command to run the agent:\n  \"cmd\": [\"claude\"]", agentName)
	}

	return nil
}

// LoadFromFile loads a configuration from a JSON file
func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	return &cfg, nil
}

// SaveToFile writes the configuration to a JSON file with 0600 permissions
func (c *Config) SaveToFile(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Add newline at end of file
	data = append(data, '\n')

	// Write with 0600 permissions (owner read/write only)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", path, err)
	}

	return nil
}
