package discovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverRanksCandidatesDeterministically(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	mustWrite := func(relPath, contents string) {
		full := filepath.Join(tmpDir, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}

	mustWrite("PLAN.md", "# Implementation Plan\n\nContent")
	mustWrite("docs/spec_overview.md", "# Spec Overview\n\nDetails")
	mustWrite("notes.txt", "Random notes")
	mustWrite("plans/archive/draft_proposal.md", "# Proposal\n\n...")
	mustWrite(".hidden/secret.md", "Should be ignored")
	mustWrite("node_modules/ignore.md", "Ignore directory")

	cfg := DefaultConfig(tmpDir)
	meta, err := Discover(cfg)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if meta.Root != filepath.Clean(tmpDir) {
		t.Fatalf("unexpected root: %s", meta.Root)
	}
	if meta.Strategy != cfg.Strategy {
		t.Fatalf("unexpected strategy: %s", meta.Strategy)
	}

	if len(meta.Candidates) == 0 {
		t.Fatalf("expected candidates to be discovered")
	}

	// Ensure deterministic ordering: PLAN.md should be first due to filename token.
	if meta.Candidates[0].Path != "PLAN.md" {
		t.Fatalf("expected PLAN.md first, got %s", meta.Candidates[0].Path)
	}

	if meta.Candidates[0].Score <= meta.Candidates[len(meta.Candidates)-1].Score {
		t.Fatalf("expected first candidate to have highest score")
	}

	// Hidden directories should be ignored.
	for _, cand := range meta.Candidates {
		if strings.HasPrefix(cand.Path, ".hidden") {
			t.Fatalf("hidden directory candidate found: %s", cand.Path)
		}
		if strings.Contains(cand.Path, "node_modules") {
			t.Fatalf("ignored directory candidate found: %s", cand.Path)
		}
	}
}

func TestDiscoverAppliesDepthPenalty(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	mustWrite := func(relPath, contents string) {
		full := filepath.Join(tmpDir, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}

	mustWrite("PLAN.md", "# Plan\n")
	mustWrite("docs/nested/deep_plan.md", "# Plan\n")

	meta, err := Discover(DefaultConfig(tmpDir))
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(meta.Candidates) < 2 {
		t.Fatalf("expected at least two candidates")
	}

	var top, deep *float64
	for i := range meta.Candidates {
		path := meta.Candidates[i].Path
		switch path {
		case "PLAN.md":
			score := meta.Candidates[i].Score
			top = &score
		case "docs/nested/deep_plan.md":
			score := meta.Candidates[i].Score
			deep = &score
		}
	}
	if top == nil || deep == nil {
		t.Fatalf("expected both candidates to be discovered")
	}
	if *top <= *deep {
		t.Fatalf("expected shallow plan to score higher: top=%.2f deep=%.2f", *top, *deep)
	}
}

func TestDiscoverValidatesRoot(t *testing.T) {
	t.Parallel()

	_, err := Discover(DefaultConfig(filepath.Join("missing", "dir")))
	if err == nil {
		t.Fatalf("expected error for missing root")
	}
}

// Ensure Discover normalizes path separators to '/'.
func TestDiscoverNormalizesPaths(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "plans"), 0o755); err != nil {
		t.Fatalf("mkdir plans: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "plans", "proposal.md"), []byte("# Proposal"), 0o644); err != nil {
		t.Fatalf("write proposal: %v", err)
	}

	meta, err := Discover(DefaultConfig(tmpDir))
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	for _, cand := range meta.Candidates {
		if strings.Contains(cand.Path, "\\") {
			t.Fatalf("expected normalized path with '/', got %s", cand.Path)
		}
	}
}
