// Package discovery implements deterministic plan/spec file discovery for the
// orchestration workflow. Given a workspace root, it walks a curated set of
// directories, filters for plan-friendly extensions, and assigns deterministic
// confidence scores based on filename heuristics, directory depth, and heading
// matches. The traversal order, scoring, and output structure are stable – the
// same workspace snapshot always yields the same candidate list. The results
// are returned as `protocol.DiscoveryMetadata` so they can be injected directly
// into orchestration command inputs.
package discovery

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/iambrandonn/lorch/internal/protocol"
)

// DefaultSearchPaths enumerates the directories inspected (relative to workspace root).
var DefaultSearchPaths = []string{".", "docs", "specs", "plans"}

// DefaultIgnoredDirs lists directory names that are skipped during discovery.
var DefaultIgnoredDirs = []string{".git", "node_modules", ".idea", ".cache", "dist", "build"}

// DefaultExtensions enumerates file extensions considered for discovery.
var DefaultExtensions = []string{".md", ".rst", ".txt"}

// Config configures deterministic discovery.
type Config struct {
	Root          string
	SearchPaths   []string
	IgnoreDirs    []string
	Extensions    []string
	MaxCandidates int
	Strategy      string
}

// DefaultConfig returns a Config populated with deterministic defaults.
func DefaultConfig(root string) Config {
	return Config{
		Root:          root,
		SearchPaths:   append([]string{}, DefaultSearchPaths...),
		IgnoreDirs:    append([]string{}, DefaultIgnoredDirs...),
		Extensions:    append([]string{}, DefaultExtensions...),
		MaxCandidates: 20,
		Strategy:      "heuristic:v1",
	}
}

// Discover scans the configured workspace and returns deterministic metadata for orchestration.
func Discover(cfg Config) (*protocol.DiscoveryMetadata, error) {
	if strings.TrimSpace(cfg.Root) == "" {
		return nil, errors.New("discovery: root is required")
	}

	root, err := filepath.Abs(cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("discovery: resolve root: %w", err)
	}

	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("discovery: stat root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("discovery: root is not a directory: %s", root)
	}

	searchPaths := cfg.SearchPaths
	if len(searchPaths) == 0 {
		searchPaths = DefaultSearchPaths
	}
	ignoreDirs := make(map[string]struct{}, len(cfg.IgnoreDirs))
	for _, name := range cfg.IgnoreDirs {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			ignoreDirs[trimmed] = struct{}{}
		}
	}
	extensions := cfg.Extensions
	if len(extensions) == 0 {
		extensions = DefaultExtensions
	}
	extSet := make(map[string]struct{}, len(extensions))
	for _, ext := range extensions {
		extSet[strings.ToLower(ext)] = struct{}{}
	}

	var candidates []discoveryCandidate
	visited := make([]string, 0, len(searchPaths))
	for _, rel := range searchPaths {
		joined := filepath.Join(root, rel)
		if !strings.HasPrefix(joined, root) {
			// Protect against path traversal in configuration.
			continue
		}
		joined = filepath.Clean(joined)

		skip := false
		for _, seen := range visited {
			if joined == seen || strings.HasPrefix(joined, seen+string(os.PathSeparator)) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		if _, err := os.Stat(joined); os.IsNotExist(err) {
			continue
		}

		if err := walk(joined, root, ignoreDirs, extSet, &candidates); err != nil {
			return nil, err
		}
		visited = append(visited, joined)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].RelPath < candidates[j].RelPath
		}
		return candidates[i].Score > candidates[j].Score
	})

	limit := cfg.MaxCandidates
	if limit <= 0 || limit > len(candidates) {
		limit = len(candidates)
	}
	selected := candidates[:limit]

	out := protocol.DiscoveryMetadata{
		Root:        root,
		Strategy:    cfg.Strategy,
		SearchPaths: append([]string{}, searchPaths...),
		GeneratedAt: time.Now().UTC(),
	}
	if len(ignoreDirs) > 0 {
		for name := range ignoreDirs {
			out.IgnoredPaths = append(out.IgnoredPaths, name)
		}
		sort.Strings(out.IgnoredPaths)
	}

	if strings.TrimSpace(out.Strategy) == "" {
		out.Strategy = cfg.Strategy
		if strings.TrimSpace(out.Strategy) == "" {
			out.Strategy = "heuristic:v1"
		}
	}

	for _, cand := range selected {
		out.Candidates = append(out.Candidates, protocol.DiscoveryCandidate{
			Path:     cand.RelPath,
			Score:    clampScore(cand.Score),
			Reason:   cand.Reason,
			Headings: cand.Headings,
		})
	}

	if err := out.Validate(); err != nil {
		return nil, fmt.Errorf("discovery: metadata validation failed: %w", err)
	}

	return &out, nil
}

type discoveryCandidate struct {
	RelPath  string
	Score    float64
	Reason   string
	Headings []string
}

func walk(path, root string, ignoreDirs map[string]struct{}, extSet map[string]struct{}, candidates *[]discoveryCandidate) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("discovery: read dir %s: %w", path, err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			// Ignore hidden files and directories.
			continue
		}
		if entry.IsDir() {
			if _, ignored := ignoreDirs[name]; ignored {
				continue
			}
			child := filepath.Join(path, name)
			if err := walk(child, root, ignoreDirs, extSet, candidates); err != nil {
				return err
			}
			continue
		}

		ext := strings.ToLower(filepath.Ext(name))
		if _, ok := extSet[ext]; !ok {
			continue
		}

		fullPath := filepath.Join(path, name)
		rel, err := filepath.Rel(root, fullPath)
		if err != nil {
			return fmt.Errorf("discovery: relative path error for %s: %w", fullPath, err)
		}

		headings := extractHeadings(fullPath, 3)
		score, reason := scoreCandidate(rel, headings)
		*candidates = append(*candidates, discoveryCandidate{
			RelPath:  filepath.ToSlash(rel),
			Score:    score,
			Reason:   reason,
			Headings: headings,
		})
	}

	return nil
}

func extractHeadings(path string, limit int) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var headings []string
	buf := make([]byte, 4096)
	var builder strings.Builder
	for {
		n, rerr := file.Read(buf)
		if n > 0 {
			builder.Write(buf[:n])
			if builder.Len() > 64*1024 {
				break // limit reads to avoid large files
			}
		}
		if rerr != nil {
			if rerr != io.EOF {
				return headings
			}
			break
		}
	}

	for _, line := range strings.Split(builder.String(), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			headings = append(headings, line)
			if len(headings) >= limit {
				break
			}
		}
	}
	return headings
}

func scoreCandidate(relPath string, headings []string) (float64, string) {
	score := 0.5
	var reasons []string

	base := strings.ToLower(filepath.Base(relPath))
	segments := strings.Split(relPath, string(filepath.Separator))
	lowerSegments := make([]string, 0, len(segments))
	for _, s := range segments {
		lowerSegments = append(lowerSegments, strings.ToLower(s))
	}

	if strings.Contains(base, "plan") {
		score += 0.25
		reasons = append(reasons, "filename contains 'plan'")
	} else if strings.Contains(base, "spec") {
		score += 0.2
		reasons = append(reasons, "filename contains 'spec'")
	} else if strings.Contains(base, "proposal") {
		score += 0.15
		reasons = append(reasons, "filename contains 'proposal'")
	}

	for _, seg := range lowerSegments {
		if seg == "docs" || seg == "plans" || seg == "specs" {
			score += 0.05
			reasons = append(reasons, fmt.Sprintf("located under '%s'", seg))
			break
		}
	}

	depth := len(lowerSegments) - 1
	if depth > 0 {
		penalty := float64(depth) * 0.04
		score -= penalty
		reasons = append(reasons, fmt.Sprintf("depth penalty -%.2f", penalty))
	}

	headingTokens := []string{"plan", "spec", "proposal"}
	for _, heading := range headings {
		lower := strings.ToLower(heading)
		for _, token := range headingTokens {
			if strings.Contains(lower, token) {
				score += 0.05
				reasons = append(reasons, fmt.Sprintf("heading matches '%s'", token))
				break
			}
		}
	}

	score = clampScore(score)
	reasons = append(reasons, fmt.Sprintf("score=%.2f", score))
	return score, strings.Join(reasons, "; ")
}

func clampScore(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}
