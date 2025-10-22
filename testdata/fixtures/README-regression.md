# Regression Test Fixtures

This document describes the fixtures used for Phase 2.5 Task C regression tests.

## Active Fixtures

### orchestration-simple.json
**Used by:**
- TestRegression_DeclineDuringTaskSelection
- TestRegression_DeclinePreservesIntakeLog
- TestRegression_MultipleDeclineAttempts
- TestRegression_DeclineWithNonTTY
- TestRegression_NonTTY_EndToEndApproval
- TestRegression_NonTTY_Decline
- TestRegression_NonTTY_EOFHandling

**Pattern**: Single intake → proposed_tasks response

### orchestration-discovery-expanded.json
**Used by:**
- TestRegression_DeclineAfterTaskDiscovery
- TestRegression_TaskDiscoveryFollowedByDecline
- TestRegression_NonTTY_WithTaskDiscovery

**Pattern**: Low-confidence intake response + expanded task_discovery response with 5 candidates

### orchestration-error-retriable.json
**Used by:**
- TestRegression_AgentErrorEventHandling

**Pattern**: Intake → error event (for graceful error handling validation)

### orchestration-malformed-response.json
**Used by:**
- TestRegression_MalformedPayloadGracefulDegradation

**Pattern**: proposed_tasks with missing required field (plan_candidates) to test error handling

## Fixture Limitations

**Important**: The fixture agent (`internal/fixtureagent`) looks up responses by command action only (`"intake"`, `"task_discovery"`). It does NOT support sequential retry patterns like `"intake_retry_1"`, `"intake_retry_2"`.

**For multi-step scenarios** (clarifications, conflicts, multiple retries), use **mock supervisors** (`fakeOrchestrationSupervisor` in tests) instead of fixtures. Examples:
- TestRegression_DeclineAfterMultipleClarifications
- TestRegression_AbortDuringConflictResolution
- TestRegression_MultipleConflictResolutions
- TestRegression_ClarificationConflictApprovalFlow
- TestRegression_InvalidInputRetryLimit
- TestRegression_ResumeAfterPartialNegotiation
- TestRegression_NonTTY_WithClarifications
- TestRegression_NonTTY_WithConflictResolution

Mock supervisors allow fine-grained control over event sequences and idempotency key verification, making them ideal for complex interaction testing.
