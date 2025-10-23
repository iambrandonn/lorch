package main

import (
	"context"
	"testing"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationWorkstreams demonstrates how all interfaces work together
func TestIntegrationWorkstreams(t *testing.T) {
	// This test shows how the interfaces enable parallel development
	// by allowing each workstream to be tested independently

	t.Run("LLM_Caller_Integration", func(t *testing.T) {
		// Workstream G: LLM CLI Caller
		caller := NewMockLLMCaller()
		caller.SetResponse("test prompt", `{
			"plan_file": "PLAN.md",
			"confidence": 0.95,
			"tasks": [
				{"id": "T-001", "title": "Test task", "files": ["test.go"]}
			]
		}`)

		ctx := context.Background()
		response, err := caller.Call(ctx, "test prompt")
		require.NoError(t, err)
		assert.Contains(t, response, "PLAN.md")
		assert.Equal(t, 1, caller.CallCount())
	})

	t.Run("Receipt_Store_Integration", func(t *testing.T) {
		// Workstream E: Idempotency Receipts
		store := NewMockReceiptStore()

		// Simulate saving a receipt
		receipt := &Receipt{
			TaskID:         "T-001",
			Step:           1,
			IdempotencyKey: "ik-123",
			Artifacts: []protocol.Artifact{
				{Path: "test.go", SHA256: "sha256:abc", Size: 100},
			},
			Events:    []string{"event-1"},
			CreatedAt: time.Now(),
		}

		err := store.SaveReceipt("/receipts/T-001/step-1.json", receipt)
		require.NoError(t, err)

		// Simulate idempotency check
		found, path, err := store.FindReceiptByIK("T-001", "implement", "ik-123")
		require.NoError(t, err)
		assert.NotNil(t, found)
		assert.Equal(t, "/receipts/T-001/step-1.json", path)
	})

	t.Run("FS_Provider_Integration", func(t *testing.T) {
		// Workstream D: Security & Filesystem Utilities
		provider := NewMockFSProvider()

		// Simulate reading workspace files
		provider.SetFile("/workspace/PLAN.md", "# Plan\n\n## Task 1\nImplement feature")
		content, err := provider.ReadFileSafe("/workspace/PLAN.md", 1024)
		require.NoError(t, err)
		assert.Contains(t, content, "Plan")

		// Simulate writing artifacts
		artifact, err := provider.WriteArtifactAtomic("/workspace", "tasks/T-001.plan.json", []byte(`{"task": "T-001"}`))
		require.NoError(t, err)
		assert.Equal(t, "tasks/T-001.plan.json", artifact.Path)
		assert.NotEmpty(t, artifact.SHA256)
	})

	t.Run("Event_Emitter_Integration", func(t *testing.T) {
		// Workstream C: Event Builders & Error Helpers
		emitter := NewMockEventEmitter()

		cmd := &protocol.Command{
			CorrelationID: "corr-1",
			TaskID:        "T-001",
			Version: protocol.Version{
				SnapshotID: "snap-1",
			},
		}

		// Simulate error handling
		err := emitter.SendErrorEvent(cmd, "llm_call_failed", "LLM timeout")
		require.NoError(t, err)

		// Simulate artifact production
		artifact := protocol.Artifact{
			Path:   "tasks/T-001.plan.json",
			SHA256: "sha256:abc123",
			Size:   100,
		}
		err = emitter.SendArtifactProducedEvent(cmd, artifact)
		require.NoError(t, err)

		// Verify events
		events := emitter.GetEvents()
		assert.Len(t, events, 2)
		assert.Equal(t, "error", events[0].Event)
		assert.Equal(t, "artifact.produced", events[1].Event)
	})

	t.Run("Complete_Orchestration_Flow", func(t *testing.T) {
		// This demonstrates how all interfaces work together
		// for a complete orchestration flow

		// Setup all interfaces
		llmCaller := NewMockLLMCaller()
		receiptStore := NewMockReceiptStore()
		fsProvider := NewMockFSProvider()
		eventEmitter := NewMockEventEmitter()

		// Mock LLM response
		llmCaller.SetResponse("orchestration prompt", `{
			"plan_file": "PLAN.md",
			"confidence": 0.95,
			"tasks": [
				{"id": "T-001", "title": "Implement feature", "files": ["src/feature.go"]}
			],
			"needs_clarification": false
		}`)

		// Mock workspace files
		fsProvider.SetFile("/workspace/PLAN.md", "# Plan\n\n## Feature Implementation")

		// Simulate orchestration flow
		cmd := &protocol.Command{
			CorrelationID: "corr-1",
			TaskID:        "T-001",
			Version: protocol.Version{
				SnapshotID: "snap-1",
			},
		}

		// 1. Check idempotency
		found, _, err := receiptStore.FindReceiptByIK("T-001", "intake", "ik-123")
		require.NoError(t, err)
		assert.Nil(t, found) // No existing receipt

		// 2. Read workspace files
		content, err := fsProvider.ReadFileSafe("/workspace/PLAN.md", 1024)
		require.NoError(t, err)
		assert.Contains(t, content, "Plan")

		// 3. Call LLM
		ctx := context.Background()
		response, err := llmCaller.Call(ctx, "orchestration prompt")
		require.NoError(t, err)
		assert.Contains(t, response, "PLAN.md")

		// 4. Write artifacts
		artifact, err := fsProvider.WriteArtifactAtomic("/workspace", "tasks/T-001.plan.json", []byte(response))
		require.NoError(t, err)

		// 5. Emit events
		err = eventEmitter.SendArtifactProducedEvent(cmd, artifact)
		require.NoError(t, err)

		// 6. Save receipt for idempotency
		receipt := &Receipt{
			TaskID:         "T-001",
			Step:           1,
			IdempotencyKey: "ik-123",
			Artifacts:     []protocol.Artifact{artifact},
			Events:        []string{"event-1"},
			CreatedAt:     time.Now(),
		}
		err = receiptStore.SaveReceipt("/receipts/T-001/step-1.json", receipt)
		require.NoError(t, err)

		// Verify all interfaces were used
		assert.Equal(t, 1, llmCaller.CallCount())
		assert.Len(t, eventEmitter.GetEvents(), 1)
		assert.Len(t, receiptStore.GetCallLog(), 2) // FindReceiptByIK + SaveReceipt
		assert.Len(t, fsProvider.GetReadLog(), 1)
		assert.Len(t, fsProvider.GetWriteLog(), 1)
	})
}

// TestInterfaceCompatibility ensures all interfaces are compatible
func TestInterfaceCompatibility(t *testing.T) {
	// This test ensures that all interfaces can be used together
	// without type conflicts or missing methods

	t.Run("Interface_Assignment", func(t *testing.T) {
		// Test that all interfaces can be assigned to their interface types
		var llmCaller LLMCaller = NewMockLLMCaller()
		var receiptStore ReceiptStore = NewMockReceiptStore()
		var fsProvider FSProvider = NewMockFSProvider()
		var eventEmitter EventEmitter = NewMockEventEmitter()

		// Test that they can be used
		ctx := context.Background()
		_, err := llmCaller.Call(ctx, "test")
		require.NoError(t, err)

		_, err = receiptStore.LoadReceipt("/test.json")
		require.Error(t, err) // Expected - file doesn't exist

		_, err = fsProvider.ReadFileSafe("/test.txt", 1024)
		require.Error(t, err) // Expected - file doesn't exist

		cmd := &protocol.Command{
			CorrelationID: "corr-1",
			TaskID:        "T-001",
			Version: protocol.Version{
				SnapshotID: "snap-1",
			},
		}

		err = eventEmitter.SendErrorEvent(cmd, "test", "message")
		require.NoError(t, err)
	})
}

// TestWorkstreamDependencies demonstrates how workstreams depend on each other
func TestWorkstreamDependencies(t *testing.T) {
	t.Run("Dependency_Chain", func(t *testing.T) {
		// Workstream A (NDJSON Core) -> Workstream C (Event Builders)
		// Workstream C -> Workstream F (Orchestration Logic)
		// Workstream D (FS Utils) -> Workstream H (Artifact Production)
		// Workstream E (Receipts) -> Workstream F (Orchestration Logic)

		// This test shows the dependency chain is properly designed
		// Each workstream can be developed independently with mocks

		// A -> C dependency
		eventEmitter := NewMockEventEmitter()
		cmd := &protocol.Command{
			CorrelationID: "corr-1",
			TaskID:        "T-001",
			Version: protocol.Version{
				SnapshotID: "snap-1",
			},
		}

		// C can be tested independently
		err := eventEmitter.SendErrorEvent(cmd, "test_error", "test message")
		require.NoError(t, err)
		assert.Len(t, eventEmitter.GetEvents(), 1)

		// D -> H dependency
		fsProvider := NewMockFSProvider()
		artifact, err := fsProvider.WriteArtifactAtomic("/workspace", "test.txt", []byte("content"))
		require.NoError(t, err)
		assert.Equal(t, "test.txt", artifact.Path)

		// E -> F dependency
		receiptStore := NewMockReceiptStore()
		receipt := &Receipt{
			TaskID:         "T-001",
			Step:           1,
			IdempotencyKey: "ik-123",
			Artifacts:     []protocol.Artifact{artifact},
			Events:        []string{"event-1"},
			CreatedAt:     time.Now(),
		}

		err = receiptStore.SaveReceipt("/receipts/T-001/step-1.json", receipt)
		require.NoError(t, err)

		// F can use all dependencies
		found, _, err := receiptStore.FindReceiptByIK("T-001", "implement", "ik-123")
		require.NoError(t, err)
		assert.NotNil(t, found)
	})
}
