package main

import (
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
