package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RealReceiptStore implements ReceiptStore using filesystem
type RealReceiptStore struct {
	workspace string
}

// NewRealReceiptStore creates a new real receipt store
func NewRealReceiptStore(workspace string) *RealReceiptStore {
	return &RealReceiptStore{
		workspace: workspace,
	}
}

// LoadReceipt loads a receipt from the filesystem
func (r *RealReceiptStore) LoadReceipt(path string) (*Receipt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read receipt: %w", err)
	}

	var receipt Receipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return nil, fmt.Errorf("failed to unmarshal receipt: %w", err)
	}

	return &receipt, nil
}

// SaveReceipt saves a receipt to the filesystem
func (r *RealReceiptStore) SaveReceipt(path string, receipt *Receipt) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create receipt directory: %w", err)
	}

	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal receipt: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write receipt: %w", err)
	}

	return nil
}

// FindReceiptByIK checks index first, falls back to directory scan
func (r *RealReceiptStore) FindReceiptByIK(taskID, action, ik string) (*Receipt, string, error) {
	// Try index first (O(1))
	receipt, path, err := r.findReceiptByIKIndex(taskID, action, ik)
	if err == nil && receipt != nil {
		return receipt, path, nil
	}

	// Fall back to directory scan (existing code)
	return r.findReceiptByIKScan(taskID, action, ik)
}

// findReceiptByIKIndex checks the IK index for fast lookup
func (r *RealReceiptStore) findReceiptByIKIndex(taskID, action, ik string) (*Receipt, string, error) {
	ikHash := r.generateIKHash(ik)
	indexPath := filepath.Join(r.workspace, "receipts", taskID, "index", "by-ik", ikHash+".json")

	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, "", err // Index not found or error
	}

	var idx map[string]string
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, "", err
	}

	receiptPath, ok := idx["receipt_path"]
	if !ok {
		return nil, "", fmt.Errorf("index missing receipt_path")
	}

	receipt, err := r.LoadReceipt(receiptPath)
	if err != nil {
		return nil, "", err
	}

	// Verify the receipt actually matches the IK (safety check)
	if receipt.IdempotencyKey != ik {
		return nil, "", fmt.Errorf("index points to receipt with different IK")
	}

	return receipt, receiptPath, nil
}

// findReceiptByIKScan performs directory scan for IK lookup (fallback)
func (r *RealReceiptStore) findReceiptByIKScan(taskID, action, ik string) (*Receipt, string, error) {
	receiptDir := filepath.Join(r.workspace, "receipts", taskID)
	entries, err := os.ReadDir(receiptDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil // No receipts yet
		}
		return nil, "", fmt.Errorf("failed to read receipt directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Only consider receipts for this action
		if !strings.HasPrefix(entry.Name(), string(action)+"-") {
			continue
		}

		receiptPath := filepath.Join(receiptDir, entry.Name())
		receipt, err := r.LoadReceipt(receiptPath)
		if err != nil {
			continue // Skip invalid receipts
		}

		if receipt.IdempotencyKey == ik {
			return receipt, receiptPath, nil
		}
	}

	return nil, "", nil // No matching receipt
}

// generateIKHash creates a hash for the IK index
func (r *RealReceiptStore) generateIKHash(ik string) string {
	hash := sha256.Sum256([]byte(ik))
	return fmt.Sprintf("%x", hash)[:8]
}

// SaveReceiptWithIndex saves a receipt and creates an IK index entry
func (r *RealReceiptStore) SaveReceiptWithIndex(receiptPath string, receipt *Receipt) error {
	// Save main receipt
	if err := r.SaveReceipt(receiptPath, receipt); err != nil {
		return err
	}

	// Best-effort index (failures don't block receipt save)
	indexDir := filepath.Join(r.workspace, "receipts", receipt.TaskID, "index", "by-ik")
	if err := os.MkdirAll(indexDir, 0700); err == nil {
		ikHash := r.generateIKHash(receipt.IdempotencyKey)
		indexPath := filepath.Join(indexDir, ikHash+".json")
		indexData := map[string]string{"receipt_path": receiptPath}
		if data, err := json.Marshal(indexData); err == nil {
			_ = os.WriteFile(indexPath, data, 0600) // Best effort
		}
	}

	return nil
}

// MockReceiptStore implements ReceiptStore for testing
type MockReceiptStore struct {
	receipts map[string]*Receipt
	callLog  []string
}

// NewMockReceiptStore creates a new mock receipt store
func NewMockReceiptStore() *MockReceiptStore {
	return &MockReceiptStore{
		receipts: make(map[string]*Receipt),
		callLog:  make([]string, 0),
	}
}

// LoadReceipt loads a receipt from the mock store
func (m *MockReceiptStore) LoadReceipt(path string) (*Receipt, error) {
	m.callLog = append(m.callLog, fmt.Sprintf("LoadReceipt(%s)", path))

	if receipt, exists := m.receipts[path]; exists {
		return receipt, nil
	}

	return nil, fmt.Errorf("receipt not found: %s", path)
}

// SaveReceipt saves a receipt to the mock store
func (m *MockReceiptStore) SaveReceipt(path string, receipt *Receipt) error {
	m.callLog = append(m.callLog, fmt.Sprintf("SaveReceipt(%s)", path))
	m.receipts[path] = receipt
	return nil
}

// FindReceiptByIK finds a receipt by idempotency key in the mock store
func (m *MockReceiptStore) FindReceiptByIK(taskID, action, ik string) (*Receipt, string, error) {
	m.callLog = append(m.callLog, fmt.Sprintf("FindReceiptByIK(%s, %s, %s)", taskID, action, ik))

	for path, receipt := range m.receipts {
		if receipt.IdempotencyKey == ik {
			return receipt, path, nil
		}
	}

	return nil, "", nil
}

// GetCallLog returns the log of method calls for testing
func (m *MockReceiptStore) GetCallLog() []string {
	return m.callLog
}

// ClearCallLog clears the call log
func (m *MockReceiptStore) ClearCallLog() {
	m.callLog = m.callLog[:0]
}

// SetReceipt sets a receipt in the mock store for testing
func (m *MockReceiptStore) SetReceipt(path string, receipt *Receipt) {
	m.receipts[path] = receipt
}

// SaveReceiptWithIndex is a no-op for mock store (index not needed for testing)
func (m *MockReceiptStore) SaveReceiptWithIndex(receiptPath string, receipt *Receipt) error {
	m.callLog = append(m.callLog, fmt.Sprintf("SaveReceiptWithIndex(%s)", receiptPath))
	return m.SaveReceipt(receiptPath, receipt)
}
