package idempotency

import (
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
)

func TestCanonicalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
		wantErr  bool
	}{
		{
			name:     "empty map",
			input:    map[string]interface{}{},
			expected: "{}",
			wantErr:  false,
		},
		{
			name: "sorted keys",
			input: map[string]interface{}{
				"z": 1,
				"a": 2,
				"m": 3,
			},
			expected: `{"a":2,"m":3,"z":1}`,
			wantErr:  false,
		},
		{
			name: "nested maps",
			input: map[string]interface{}{
				"outer": map[string]interface{}{
					"z": "last",
					"a": "first",
				},
			},
			expected: `{"outer":{"a":"first","z":"last"}}`,
			wantErr:  false,
		},
		{
			name: "arrays preserved",
			input: map[string]interface{}{
				"items": []interface{}{"z", "a", "m"},
			},
			expected: `{"items":["z","a","m"]}`,
			wantErr:  false,
		},
		{
			name: "complex nested structure",
			input: map[string]interface{}{
				"z_field": "value",
				"a_field": map[string]interface{}{
					"nested_z": 1,
					"nested_a": 2,
				},
				"m_field": []interface{}{
					map[string]interface{}{
						"z": 1,
						"a": 2,
					},
				},
			},
			expected: `{"a_field":{"nested_a":2,"nested_z":1},"m_field":[{"a":2,"z":1}],"z_field":"value"}`,
			wantErr:  false,
		},
		{
			name: "different order same content",
			input: map[string]interface{}{
				"b": 2,
				"a": 1,
			},
			expected: `{"a":1,"b":2}`,
			wantErr:  false,
		},
		{
			name:     "string value",
			input:    "simple string",
			expected: `"simple string"`,
			wantErr:  false,
		},
		{
			name:     "number value",
			input:    42,
			expected: `42`,
			wantErr:  false,
		},
		{
			name:     "nil value",
			input:    nil,
			expected: "null",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CanonicalJSON(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("CanonicalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && string(result) != tt.expected {
				t.Errorf("CanonicalJSON() = %s, want %s", string(result), tt.expected)
			}
		})
	}
}

func TestCanonicalJSONDeterministic(t *testing.T) {
	// Same logical content, different construction order
	input1 := map[string]interface{}{
		"a": 1,
		"b": 2,
		"c": 3,
	}

	input2 := map[string]interface{}{
		"c": 3,
		"a": 1,
		"b": 2,
	}

	result1, err1 := CanonicalJSON(input1)
	result2, err2 := CanonicalJSON(input2)

	if err1 != nil || err2 != nil {
		t.Fatalf("CanonicalJSON() errors: %v, %v", err1, err2)
	}

	if string(result1) != string(result2) {
		t.Errorf("CanonicalJSON() not deterministic:\n  %s\n  %s", string(result1), string(result2))
	}
}

func TestGenerateIK(t *testing.T) {
	// Create a sample command
	baseCmd := &protocol.Command{
		Kind:          protocol.MessageKindCommand,
		MessageID:     "msg-001",
		CorrelationID: "corr-001",
		TaskID:        "T-0042",
		To: protocol.AgentRef{
			AgentType: protocol.AgentTypeBuilder,
		},
		Action: protocol.ActionImplement,
		Inputs: map[string]any{
			"goal": "implement feature X",
			"spec_path": "/specs/MASTER-SPEC.md",
		},
		ExpectedOutputs: []protocol.ExpectedOutput{
			{
				Path:     "src/feature.go",
				Required: true,
			},
		},
		Version: protocol.Version{
			SnapshotID: "snap-abc123",
		},
		Deadline: time.Now().Add(10 * time.Minute),
		Retry: protocol.Retry{
			Attempt:     0,
			MaxAttempts: 3,
		},
		Priority: 5,
	}

	// Generate IK
	ik, err := GenerateIK(baseCmd)
	if err != nil {
		t.Fatalf("GenerateIK() error = %v", err)
	}

	// Verify format
	if len(ik) != 67 { // "ik:" (3) + 64 hex chars
		t.Errorf("GenerateIK() length = %d, want 67", len(ik))
	}

	if ik[:3] != "ik:" {
		t.Errorf("GenerateIK() prefix = %s, want 'ik:'", ik[:3])
	}

	// Verify determinism - same inputs = same IK
	ik2, err := GenerateIK(baseCmd)
	if err != nil {
		t.Fatalf("GenerateIK() second call error = %v", err)
	}

	if ik != ik2 {
		t.Errorf("GenerateIK() not deterministic: %s != %s", ik, ik2)
	}
}

func TestGenerateIKChangeDetection(t *testing.T) {
	baseCmd := &protocol.Command{
		TaskID: "T-0042",
		Action: protocol.ActionImplement,
		Inputs: map[string]any{
			"goal": "implement feature X",
		},
		ExpectedOutputs: []protocol.ExpectedOutput{},
		Version: protocol.Version{
			SnapshotID: "snap-abc123",
		},
	}

	baseIK, _ := GenerateIK(baseCmd)

	tests := []struct {
		name   string
		modify func(*protocol.Command)
	}{
		{
			name: "different action",
			modify: func(cmd *protocol.Command) {
				cmd.Action = protocol.ActionReview
			},
		},
		{
			name: "different task_id",
			modify: func(cmd *protocol.Command) {
				cmd.TaskID = "T-0043"
			},
		},
		{
			name: "different snapshot_id",
			modify: func(cmd *protocol.Command) {
				cmd.Version.SnapshotID = "snap-xyz789"
			},
		},
		{
			name: "different inputs",
			modify: func(cmd *protocol.Command) {
				cmd.Inputs = map[string]any{
					"goal": "implement feature Y",
				}
			},
		},
		{
			name: "added input field",
			modify: func(cmd *protocol.Command) {
				cmd.Inputs["new_field"] = "new value"
			},
		},
		{
			name: "different expected_outputs",
			modify: func(cmd *protocol.Command) {
				cmd.ExpectedOutputs = []protocol.ExpectedOutput{
					{Path: "src/new.go", Required: true},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clone command
			modCmd := *baseCmd
			modCmd.Inputs = make(map[string]any)
			for k, v := range baseCmd.Inputs {
				modCmd.Inputs[k] = v
			}

			// Apply modification
			tt.modify(&modCmd)

			// Generate new IK
			newIK, err := GenerateIK(&modCmd)
			if err != nil {
				t.Fatalf("GenerateIK() error = %v", err)
			}

			// Should be different
			if newIK == baseIK {
				t.Errorf("GenerateIK() unchanged after modification: %s", newIK)
			}
		})
	}
}

func TestGenerateIKIgnoresNonIKFields(t *testing.T) {
	// Fields that should NOT affect IK
	cmd1 := &protocol.Command{
		MessageID:     "msg-001",
		CorrelationID: "corr-001",
		TaskID:        "T-0042",
		Action:        protocol.ActionImplement,
		Inputs:        map[string]any{"goal": "test"},
		ExpectedOutputs: []protocol.ExpectedOutput{},
		Version: protocol.Version{
			SnapshotID: "snap-abc123",
		},
		Deadline: time.Now(),
		Priority: 5,
	}

	cmd2 := *cmd1
	cmd2.MessageID = "msg-002"
	cmd2.CorrelationID = "corr-002"
	cmd2.Deadline = time.Now().Add(1 * time.Hour)
	cmd2.Priority = 10

	ik1, _ := GenerateIK(cmd1)
	ik2, _ := GenerateIK(&cmd2)

	if ik1 != ik2 {
		t.Errorf("GenerateIK() differs on non-IK fields:\n  %s\n  %s", ik1, ik2)
	}
}
