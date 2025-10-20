package idempotency

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/iambrandonn/lorch/internal/protocol"
)

// CanonicalJSON converts a value to deterministic JSON by recursively sorting map keys
// This ensures that logically equivalent data structures always produce the same JSON
func CanonicalJSON(v interface{}) ([]byte, error) {
	// Normalize the value first to ensure all maps are sorted
	normalized, err := normalizeValue(v)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize value: %w", err)
	}

	// Marshal without extra whitespace
	data, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return data, nil
}

// normalizeValue recursively converts maps to sorted representations
func normalizeValue(v interface{}) (interface{}, error) {
	switch val := v.(type) {
	case map[string]interface{}:
		return normalizeSortedMap(val)

	case []interface{}:
		// Process array elements but preserve order
		normalized := make([]interface{}, len(val))
		for i, item := range val {
			n, err := normalizeValue(item)
			if err != nil {
				return nil, err
			}
			normalized[i] = n
		}
		return normalized, nil

	default:
		// Primitives and other types pass through
		return v, nil
	}
}

// sortedMap is a JSON-marshalable type that maintains key ordering
type sortedMap struct {
	keys   []string
	values map[string]interface{}
}

func (sm *sortedMap) MarshalJSON() ([]byte, error) {
	// Build JSON manually with sorted keys
	if len(sm.keys) == 0 {
		return []byte("{}"), nil
	}

	result := "{"
	for i, key := range sm.keys {
		if i > 0 {
			result += ","
		}

		// Marshal key
		keyJSON, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}

		// Marshal value
		valJSON, err := json.Marshal(sm.values[key])
		if err != nil {
			return nil, err
		}

		result += string(keyJSON) + ":" + string(valJSON)
	}
	result += "}"

	return []byte(result), nil
}

func normalizeSortedMap(m map[string]interface{}) (*sortedMap, error) {
	// Extract and sort keys
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Normalize values
	normalized := make(map[string]interface{}, len(m))
	for _, k := range keys {
		n, err := normalizeValue(m[k])
		if err != nil {
			return nil, err
		}
		normalized[k] = n
	}

	return &sortedMap{
		keys:   keys,
		values: normalized,
	}, nil
}

// GenerateIK creates an idempotency key for a command
// Format: ik = SHA256(action + '\n' + task_id + '\n' + snapshot_id + '\n' +
//                      canonical_json(inputs) + '\n' + canonical_json(expected_outputs))
// Returns: "ik:" + hex-encoded SHA256
func GenerateIK(cmd *protocol.Command) (string, error) {
	// Serialize inputs
	inputsJSON, err := CanonicalJSON(cmd.Inputs)
	if err != nil {
		return "", fmt.Errorf("failed to canonicalize inputs: %w", err)
	}

	// Serialize expected_outputs
	outputsJSON, err := CanonicalJSON(cmd.ExpectedOutputs)
	if err != nil {
		return "", fmt.Errorf("failed to canonicalize expected_outputs: %w", err)
	}

	// Build hash input
	hashInput := string(cmd.Action) + "\n" +
		cmd.TaskID + "\n" +
		cmd.Version.SnapshotID + "\n" +
		string(inputsJSON) + "\n" +
		string(outputsJSON)

	// Compute SHA256
	hash := sha256.Sum256([]byte(hashInput))
	hexHash := hex.EncodeToString(hash[:])

	return "ik:" + hexHash, nil
}
