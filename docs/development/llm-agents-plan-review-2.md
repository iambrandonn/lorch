# LLM Agents Plan — Spec Compliance Review (Review 2)

**Documents reviewed**: `docs/development/LLM-AGENTS-PLAN.md`, `docs/development/llm-agents-plan-review-self-corrections.md`
**Primary reference**: `MASTER-SPEC.md`
**Status**: Changes requested (minor but important)
**Date**: 2025-10-23

---

## Executive Summary

The plan is much closer to spec compliance after Review 1 and self‑corrections. A few issues remain that affect determinism, safety, and strict adherence to `MASTER-SPEC.md`. Below are focused changes that will make the orchestration agent fully spec‑tight.

---

## Required Changes

1) Idempotency lookup must not rely on retry attempt
- Problem: Receipt lookup uses a path including `Retry.Attempt`, so a retried command (same IK, new attempt) won’t find the prior receipt.
- Spec basis: §5.4 Idempotency Keys — treat repeated commands with the same IK as already handled; do not rewrite artifacts.
- Change:
  - On handling a command, scan `/receipts/<task_id>/` for any receipt whose `idempotency_key` equals the incoming IK. If found, replay and return without calling the LLM.
  - Optionally add a canonical IK‑keyed receipt alongside step files: `/receipts/<task_id>/<action>-ik-<first8(sha256(ik))>.json`.

```go
func (a *LLMAgent) findReceiptByIK(taskID, action, ik string) (*Receipt, string, error) {
    dir := filepath.Join(a.workspace, "receipts", taskID)
    entries, err := os.ReadDir(dir)
    if err != nil {
        if errors.Is(err, os.ErrNotExist) { return nil, "", nil }
        return nil, "", err
    }
    for _, e := range entries {
        if e.IsDir() { continue }
        // Only consider receipts for this action
        if !strings.HasPrefix(e.Name(), string(action)+"-") && !strings.Contains(e.Name(), "ik-") { continue }
        rp := filepath.Join(dir, e.Name())
        r, err := a.loadReceipt(rp)
        if err == nil && r.IdempotencyKey == ik {
            return r, rp, nil
        }
    }
    return nil, "", nil
}
```

2) Version mismatch detection during a single agent run
- Problem: The plan defers mismatch detection entirely to `lorch`. Spec says agents must error on version mismatch (§6.2), but does not prescribe the detection source.
- Change: Record the first `version.snapshot_id` seen by the agent in‑process as `firstObservedSnapshotID`. For each subsequent command, if `cmd.Version.SnapshotID != firstObservedSnapshotID`, emit `error` with `payload.code = "version_mismatch"`, echo both expected/observed in payload, and do not proceed.

```go
// During first command
if a.firstObservedSnapshotID == "" {
    a.firstObservedSnapshotID = cmd.Version.SnapshotID
} else if cmd.Version.SnapshotID != a.firstObservedSnapshotID {
    return a.sendErrorEvent(cmd, "version_mismatch", fmt.Sprintf("expected %s, observed %s", a.firstObservedSnapshotID, cmd.Version.SnapshotID))
}
```

3) Secure path validation with symlink resolution
- Problem: Current examples use `strings.HasPrefix` which is not safe against `..` traversal or symlink escapes.
- Spec basis: §13 Security & Safety — path safety; reject escaping symlinks.
- Change: Use `filepath.Abs`, `filepath.EvalSymlinks`, compare against canonical workspace root with path separator guard.

```go
func resolveWorkspacePath(workspace, relative string) (string, error) {
    rootAbs, err := filepath.EvalSymlinks(filepath.Clean(workspace))
    if err != nil { return "", err }

    joined := filepath.Join(rootAbs, relative)
    fullAbs, err := filepath.EvalSymlinks(filepath.Clean(joined))
    if err != nil { return "", err }

    // Ensure fullAbs is within rootAbs (with path separator guard)
    rootWithSep := rootAbs
    if !strings.HasSuffix(rootWithSep, string(os.PathSeparator)) {
        rootWithSep += string(os.PathSeparator)
    }
    if fullAbs != rootAbs && !strings.HasPrefix(fullAbs, rootWithSep) {
        return "", fmt.Errorf("path escapes workspace: %s", relative)
    }
    return fullAbs, nil
}
```

4) File and directory permissions
- Problem: Examples use `0644` for files and `0755` for directories.
- Spec basis: §13 Security & Safety — create files with `0600`, dirs `0700` (configurable via umask).
- Change: Use `0600` for files and `0700` for directories in artifact writes.

```go
if err := os.MkdirAll(dir, 0700); err != nil { return protocol.Artifact{}, err }
if err := os.WriteFile(tmpFile, content, 0600); err != nil { return protocol.Artifact{}, err }
```

5) Respect `expected_outputs[*].required`
- Problem: Plan doesn’t specify behavior for optional outputs.
- Spec basis: Command schema defines `required` (default true).
- Change: If an `expected_output.required` is false and write fails, emit a `log` with `level=warn` and continue; if true, emit `event:error artifact_write_failed` and stop.

```go
for _, out := range cmd.ExpectedOutputs {
    art, err := a.writeArtifactAtomic(out.Path, bytes)
    if err != nil {
        if out.Required != nil && *out.Required == false {
            a.sendLog("warn", "optional artifact write failed", map[string]any{"path": out.Path, "error": err.Error()})
            continue
        }
        return a.sendErrorEvent(cmd, "artifact_write_failed", err.Error())
    }
    a.sendArtifactProducedEvent(cmd, art)
}
```

6) Cover `orchestration.plan_conflict` explicitly
- Problem: Plan mentions `needs_clarification` but not `plan_conflict`.
- Spec basis: §3.2 Event types — `orchestration.plan_conflict`.
- Change: Add criteria and emission example (e.g., multiple high‑confidence candidates with contradictory sections).

```json
{"kind":"event","event":"orchestration.plan_conflict","payload":{
  "candidates":[{"path":"PLAN.md","confidence":0.81},{"path":"docs/plan_v2.md","confidence":0.80}],
  "reason":"Two high-confidence plans diverge in scope; human selection required."
}}
```

7) Prompt size caps and summarization policy
- Problem: Prompt construction may grow unbounded by concatenating file contents.
- Spec basis: §12 Limits; while the 256 KiB cap applies to NDJSON, agents should still bound LLM inputs.
- Change: Introduce a total prompt size budget (e.g., 256 KiB default, configurable), per‑file caps, and deterministic summarization when exceeded.

```go
const (
    maxPromptBytesDefault = 256 * 1024
    maxPerFileBytes       = 32 * 1024
)

func (a *LLMAgent) buildPrompt(...) string {
    budget := a.cfg.MaxPromptBytes
    if budget == 0 { budget = maxPromptBytesDefault }
    // enforce per-file and total budgets; summarize when exceeding limits
}
```

---

## Nice-to-Have Improvements

- Emit compact `log` events for major steps (inputs parsed, IK hit/miss, LLM invoked, JSON parsed). Keep logs short to respect NDJSON caps.
- Add schema validation in tests against `/schemas/v1/*.json` for events and heartbeats.
- Document redaction policy for env variables ending with `_TOKEN|_KEY|_SECRET` in logs (Spec §13). Optional but recommended.

---

## Verdict

Direction is correct; with the above edits, the orchestration agent will adhere strictly to `MASTER-SPEC.md` while being safer and more deterministic. Please update the plan to incorporate these changes before implementation.
