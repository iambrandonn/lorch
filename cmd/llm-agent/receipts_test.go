package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReceipt(t *testing.T) {
	receipt := &Receipt{
		TaskID:         "T-001",
		Step:           1,
		IdempotencyKey: "ik-123",
		Artifacts: []protocol.Artifact{
			{
				Path:   "test.txt",
				SHA256: "sha256:abc123",
				Size:   100,
			},
		},
		Events:    []string{"event-1", "event-2"},
		CreatedAt: time.Now(),
	}

	assert.Equal(t, "T-001", receipt.TaskID)
	assert.Equal(t, 1, receipt.Step)
	assert.Equal(t, "ik-123", receipt.IdempotencyKey)
	assert.Len(t, receipt.Artifacts, 1)
	assert.Len(t, receipt.Events, 2)
}

func TestMockReceiptStore(t *testing.T) {
	store := NewMockReceiptStore()

	// Test initial state
	assert.Empty(t, store.GetCallLog())

	// Test SaveReceipt
	receipt := &Receipt{
		TaskID:         "T-001",
		Step:           1,
		IdempotencyKey: "ik-123",
		Artifacts:     []protocol.Artifact{},
		Events:        []string{"event-1"},
		CreatedAt:     time.Now(),
	}

	err := store.SaveReceipt("/receipts/T-001/step-1.json", receipt)
	require.NoError(t, err)

	// Test LoadReceipt
	loaded, err := store.LoadReceipt("/receipts/T-001/step-1.json")
	require.NoError(t, err)
	assert.Equal(t, receipt.TaskID, loaded.TaskID)
	assert.Equal(t, receipt.Step, loaded.Step)
	assert.Equal(t, receipt.IdempotencyKey, loaded.IdempotencyKey)

	// Test FindReceiptByIK
	found, path, err := store.FindReceiptByIK("T-001", "implement", "ik-123")
	require.NoError(t, err)
	assert.NotNil(t, found)
	assert.Equal(t, "/receipts/T-001/step-1.json", path)
	assert.Equal(t, "ik-123", found.IdempotencyKey)

	// Test FindReceiptByIK with non-existent IK
	found, path, err = store.FindReceiptByIK("T-001", "implement", "ik-nonexistent")
	require.NoError(t, err)
	assert.Nil(t, found)
	assert.Empty(t, path)

	// Test call log
	log := store.GetCallLog()
	assert.Contains(t, log, "SaveReceipt(/receipts/T-001/step-1.json)")
	assert.Contains(t, log, "LoadReceipt(/receipts/T-001/step-1.json)")
	assert.Contains(t, log, "FindReceiptByIK(T-001, implement, ik-123)")
	assert.Contains(t, log, "FindReceiptByIK(T-001, implement, ik-nonexistent)")
}

func TestMockReceiptStoreLoadReceiptNotFound(t *testing.T) {
	store := NewMockReceiptStore()

	_, err := store.LoadReceipt("/nonexistent/receipt.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "receipt not found")
}

func TestMockReceiptStoreClearCallLog(t *testing.T) {
	store := NewMockReceiptStore()

	// Make some calls
	store.SaveReceipt("/test.json", &Receipt{})
	store.LoadReceipt("/test.json")

	// Verify log has entries
	log := store.GetCallLog()
	assert.Len(t, log, 2)

	// Clear log
	store.ClearCallLog()

	// Verify log is empty
	log = store.GetCallLog()
	assert.Empty(t, log)
}

func TestMockReceiptStoreSetReceipt(t *testing.T) {
	store := NewMockReceiptStore()

	receipt := &Receipt{
		TaskID:         "T-002",
		Step:           2,
		IdempotencyKey: "ik-456",
		Artifacts:     []protocol.Artifact{},
		Events:        []string{"event-3"},
		CreatedAt:     time.Now(),
	}

	// Set receipt directly
	store.SetReceipt("/receipts/T-002/step-2.json", receipt)

	// Load it back
	loaded, err := store.LoadReceipt("/receipts/T-002/step-2.json")
	require.NoError(t, err)
	assert.Equal(t, "T-002", loaded.TaskID)
	assert.Equal(t, "ik-456", loaded.IdempotencyKey)
}

func TestMockReceiptStoreMultipleReceipts(t *testing.T) {
	store := NewMockReceiptStore()

	// Save multiple receipts
	receipt1 := &Receipt{
		TaskID:         "T-001",
		Step:           1,
		IdempotencyKey: "ik-111",
		Artifacts:     []protocol.Artifact{},
		Events:        []string{"event-1"},
		CreatedAt:     time.Now(),
	}

	receipt2 := &Receipt{
		TaskID:         "T-001",
		Step:           2,
		IdempotencyKey: "ik-222",
		Artifacts:     []protocol.Artifact{},
		Events:        []string{"event-2"},
		CreatedAt:     time.Now(),
	}

	store.SaveReceipt("/receipts/T-001/step-1.json", receipt1)
	store.SaveReceipt("/receipts/T-001/step-2.json", receipt2)

	// Test finding by different IKs
	found1, path1, err := store.FindReceiptByIK("T-001", "implement", "ik-111")
	require.NoError(t, err)
	assert.NotNil(t, found1)
	assert.Equal(t, "ik-111", found1.IdempotencyKey)

	found2, path2, err := store.FindReceiptByIK("T-001", "implement", "ik-222")
	require.NoError(t, err)
	assert.NotNil(t, found2)
	assert.Equal(t, "ik-222", found2.IdempotencyKey)

	// Paths should be different
	assert.NotEqual(t, path1, path2)
}

func TestRealReceiptStoreIKIndex(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()
	store := NewRealReceiptStore(tempDir)

	receipt := &Receipt{
		TaskID:         "T-001",
		Step:           1,
		IdempotencyKey: "ik-test-123",
		Artifacts:     []protocol.Artifact{},
		Events:        []string{"event-1"},
		CreatedAt:     time.Now(),
	}

	receiptPath := filepath.Join(tempDir, "receipts", "T-001", "implement-1.json")

	// Test SaveReceiptWithIndex
	err := store.SaveReceiptWithIndex(receiptPath, receipt)
	require.NoError(t, err)

	// Verify receipt was saved
	loaded, err := store.LoadReceipt(receiptPath)
	require.NoError(t, err)
	assert.Equal(t, receipt.IdempotencyKey, loaded.IdempotencyKey)

	// Verify index was created
	ikHash := store.generateIKHash("ik-test-123")
	indexPath := filepath.Join(tempDir, "receipts", "T-001", "index", "by-ik", ikHash+".json")
	indexData, err := os.ReadFile(indexPath)
	require.NoError(t, err)

	var index map[string]string
	err = json.Unmarshal(indexData, &index)
	require.NoError(t, err)
	assert.Equal(t, receiptPath, index["receipt_path"])

	// Test FindReceiptByIK uses index
	found, path, err := store.FindReceiptByIK("T-001", "implement", "ik-test-123")
	require.NoError(t, err)
	assert.NotNil(t, found)
	assert.Equal(t, receiptPath, path)
	assert.Equal(t, "ik-test-123", found.IdempotencyKey)
}

func TestRealReceiptStoreIKIndexFallback(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()
	store := NewRealReceiptStore(tempDir)

	receipt := &Receipt{
		TaskID:         "T-001",
		Step:           1,
		IdempotencyKey: "ik-test-456",
		Artifacts:     []protocol.Artifact{},
		Events:        []string{"event-1"},
		CreatedAt:     time.Now(),
	}

	receiptPath := filepath.Join(tempDir, "receipts", "T-001", "implement-1.json")

	// Save receipt without index
	err := store.SaveReceipt(receiptPath, receipt)
	require.NoError(t, err)

	// Test FindReceiptByIK falls back to scan when index is missing
	found, path, err := store.FindReceiptByIK("T-001", "implement", "ik-test-456")
	require.NoError(t, err)
	assert.NotNil(t, found)
	assert.Equal(t, receiptPath, path)
	assert.Equal(t, "ik-test-456", found.IdempotencyKey)
}

func TestRealReceiptStoreIKIndexCorrupted(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()
	store := NewRealReceiptStore(tempDir)

	receipt := &Receipt{
		TaskID:         "T-001",
		Step:           1,
		IdempotencyKey: "ik-test-789",
		Artifacts:     []protocol.Artifact{},
		Events:        []string{"event-1"},
		CreatedAt:     time.Now(),
	}

	receiptPath := filepath.Join(tempDir, "receipts", "T-001", "implement-1.json")

	// Save receipt with index
	err := store.SaveReceiptWithIndex(receiptPath, receipt)
	require.NoError(t, err)

	// Corrupt the index file
	ikHash := store.generateIKHash("ik-test-789")
	indexPath := filepath.Join(tempDir, "receipts", "T-001", "index", "by-ik", ikHash+".json")
	err = os.WriteFile(indexPath, []byte("invalid json"), 0600)
	require.NoError(t, err)

	// Test FindReceiptByIK falls back to scan when index is corrupted
	found, path, err := store.FindReceiptByIK("T-001", "implement", "ik-test-789")
	require.NoError(t, err)
	assert.NotNil(t, found)
	assert.Equal(t, receiptPath, path)
	assert.Equal(t, "ik-test-789", found.IdempotencyKey)
}

func TestRealReceiptStoreIKIndexMismatch(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()
	store := NewRealReceiptStore(tempDir)

	receipt := &Receipt{
		TaskID:         "T-001",
		Step:           1,
		IdempotencyKey: "ik-test-999",
		Artifacts:     []protocol.Artifact{},
		Events:        []string{"event-1"},
		CreatedAt:     time.Now(),
	}

	receiptPath := filepath.Join(tempDir, "receipts", "T-001", "implement-1.json")

	// Save receipt with index
	err := store.SaveReceiptWithIndex(receiptPath, receipt)
	require.NoError(t, err)

	// Create a corrupted index that points to a different IK
	ikHash := store.generateIKHash("ik-test-999")
	indexPath := filepath.Join(tempDir, "receipts", "T-001", "index", "by-ik", ikHash+".json")

	// Point index to a receipt with different IK
	otherReceipt := &Receipt{
		TaskID:         "T-001",
		Step:           2,
		IdempotencyKey: "ik-different-999",
		Artifacts:     []protocol.Artifact{},
		Events:        []string{"event-2"},
		CreatedAt:     time.Now(),
	}
	otherReceiptPath := filepath.Join(tempDir, "receipts", "T-001", "implement-2.json")
	err = store.SaveReceipt(otherReceiptPath, otherReceipt)
	require.NoError(t, err)

	// Corrupt the index to point to wrong receipt
	indexData := map[string]string{"receipt_path": otherReceiptPath}
	data, _ := json.Marshal(indexData)
	err = os.WriteFile(indexPath, data, 0600)
	require.NoError(t, err)

	// Test FindReceiptByIK falls back to scan when index points to wrong receipt
	found, path, err := store.FindReceiptByIK("T-001", "implement", "ik-test-999")
	require.NoError(t, err)
	assert.NotNil(t, found)
	assert.Equal(t, receiptPath, path)
	assert.Equal(t, "ik-test-999", found.IdempotencyKey)
}

func TestMockReceiptStoreSaveReceiptWithIndex(t *testing.T) {
	store := NewMockReceiptStore()

	receipt := &Receipt{
		TaskID:         "T-001",
		Step:           1,
		IdempotencyKey: "ik-test-mock",
		Artifacts:     []protocol.Artifact{},
		Events:        []string{"event-1"},
		CreatedAt:     time.Now(),
	}

	receiptPath := "/receipts/T-001/implement-1.json"

	// Test SaveReceiptWithIndex
	err := store.SaveReceiptWithIndex(receiptPath, receipt)
	require.NoError(t, err)

	// Verify receipt was saved
	loaded, err := store.LoadReceipt(receiptPath)
	require.NoError(t, err)
	assert.Equal(t, receipt.IdempotencyKey, loaded.IdempotencyKey)

	// Verify call log includes the new method
	log := store.GetCallLog()
	assert.Contains(t, log, "SaveReceiptWithIndex(/receipts/T-001/implement-1.json)")
	assert.Contains(t, log, "SaveReceipt(/receipts/T-001/implement-1.json)")
}
