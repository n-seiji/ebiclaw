package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ArchiveReadTool provides read-only access to a single topic file in the
// conversation archive. It is opt-in via the Enabled() gate so the registry
// can decide whether to expose it based on archiver configuration.
type ArchiveReadTool struct {
	repoRoot string
	enabled  bool
}

// NewArchiveReadTool constructs an ArchiveReadTool rooted at repoRoot.
// When enabled is false, Execute returns a no-op silent result.
func NewArchiveReadTool(repoRoot string, enabled bool) *ArchiveReadTool {
	return &ArchiveReadTool{repoRoot: repoRoot, enabled: enabled}
}

// Name returns the tool name.
func (t *ArchiveReadTool) Name() string { return "archive_read" }

// Enabled reports whether this tool is configured to run. The registry uses
// this to skip registration when the archiver feature is disabled.
func (t *ArchiveReadTool) Enabled() bool { return t.enabled }

// Description returns the tool description.
func (t *ArchiveReadTool) Description() string {
	return "Read a single topic file from the conversation archive (read-only). Argument: slug."
}

// Parameters returns the tool parameters schema.
func (t *ArchiveReadTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"slug": map[string]any{
				"type":        "string",
				"description": "Topic slug, e.g., login-flow-bug",
			},
		},
		"required": []string{"slug"},
	}
}

// Execute reads the topic markdown file identified by the slug argument.
// The slug is validated to prevent path traversal: it must be non-empty and
// must not contain path separators or parent-directory references.
func (t *ArchiveReadTool) Execute(_ context.Context, args map[string]any) *ToolResult {
	if !t.enabled {
		return SilentResult("archive_read disabled")
	}

	slug, _ := args["slug"].(string)
	if !isValidSlug(slug) {
		return ErrorResult("invalid slug")
	}

	path := filepath.Join(t.repoRoot, "topics", slug+".md")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ErrorResult(fmt.Sprintf("topic not found: %s", slug))
	}
	if err != nil {
		return ErrorResult(fmt.Sprintf("read topic: %v", err)).WithError(err)
	}
	return SilentResult(string(data))
}

// isValidSlug rejects empty strings, path separators, and parent references
// to keep reads constrained to the topics/ directory.
func isValidSlug(slug string) bool {
	if slug == "" {
		return false
	}
	if strings.ContainsAny(slug, "/\\") {
		return false
	}
	if slug == "." || slug == ".." || strings.Contains(slug, "..") {
		return false
	}
	return true
}
