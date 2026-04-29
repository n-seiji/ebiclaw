package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ArchiveSearchTool provides read-only search over the conversation archive.
// It is opt-in via the Enabled() gate so the registry can decide whether to
// expose it based on archiver configuration.
type ArchiveSearchTool struct {
	repoRoot string
	enabled  bool
}

// NewArchiveSearchTool constructs an ArchiveSearchTool rooted at repoRoot.
// When enabled is false, Execute returns a no-op silent result.
func NewArchiveSearchTool(repoRoot string, enabled bool) *ArchiveSearchTool {
	return &ArchiveSearchTool{repoRoot: repoRoot, enabled: enabled}
}

// Name returns the tool name.
func (t *ArchiveSearchTool) Name() string { return "archive_search" }

// Enabled reports whether this tool is configured to run. The registry uses
// this to skip registration when the archiver feature is disabled.
func (t *ArchiveSearchTool) Enabled() bool { return t.enabled }

// Description returns the tool description.
func (t *ArchiveSearchTool) Description() string {
	return "Search the conversation archive (read-only). Returns a list of matching topic slugs and titles."
}

// Parameters returns the tool parameters schema.
func (t *ArchiveSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Substring to match in title/body.",
			},
			"status": map[string]any{
				"type":        "string",
				"description": "Optional filter: open|resolved|archived.",
			},
		},
		"required": []string{"query"},
	}
}

type archiveSearchHit struct {
	Slug   string
	Title  string
	Status string
}

// Execute runs the tool with the given arguments.
func (t *ArchiveSearchTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	if !t.enabled {
		return SilentResult("archive_search disabled")
	}

	q, _ := args["query"].(string)
	statusFilter, _ := args["status"].(string)
	if q == "" {
		return ErrorResult("query is required")
	}

	dir := filepath.Join(t.repoRoot, "topics")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return SilentResult("no topics yet")
		}
		return ErrorResult(fmt.Sprintf("read topics dir: %v", err)).WithError(err)
	}

	var hits []archiveSearchHit
	qLower := strings.ToLower(q)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		hit, matched := t.scanTopic(filepath.Join(dir, e.Name()), qLower)
		if !matched {
			continue
		}
		if statusFilter != "" && hit.Status != statusFilter {
			continue
		}
		hits = append(hits, hit)
	}

	if len(hits) == 0 {
		return SilentResult("no matches")
	}

	var b strings.Builder
	for _, h := range hits {
		fmt.Fprintf(&b, "- %s: %s (%s)\n", h.Slug, h.Title, h.Status)
	}
	return SilentResult(b.String())
}

// scanTopic reads a single topic markdown file, extracts frontmatter fields,
// and reports whether the query substring appeared anywhere in the file.
func (t *ArchiveSearchTool) scanTopic(path, qLower string) (archiveSearchHit, bool) {
	f, err := os.Open(path)
	if err != nil {
		return archiveSearchHit{}, false
	}
	defer f.Close()

	var hit archiveSearchHit
	var matched bool
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "slug:"):
			hit.Slug = strings.TrimSpace(strings.TrimPrefix(line, "slug:"))
		case strings.HasPrefix(line, "title:"):
			hit.Title = strings.TrimSpace(strings.TrimPrefix(line, "title:"))
		case strings.HasPrefix(line, "status:"):
			hit.Status = strings.TrimSpace(strings.TrimPrefix(line, "status:"))
		}
		if strings.Contains(strings.ToLower(line), qLower) {
			matched = true
		}
	}
	return hit, matched
}
