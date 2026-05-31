package archiver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/n-seiji/ebiclaw/pkg/logger"
)

// LLMClient is the abstraction over pkg/providers used by the distiller.
type LLMClient interface {
	Distill(ctx context.Context, prompt string) (string, error)
}

// Distiller runs one batch.
type Distiller struct {
	repoRoot string
	llm      LLMClient
}

func NewDistiller(repoRoot string, llm LLMClient) *Distiller {
	return &Distiller{repoRoot: repoRoot, llm: llm}
}

type DistillResult struct {
	Created int
	Updated int
	Merged  int
	Skipped bool // true if no new raw to process
	// CutoffAt is the timestamp captured at the start of the distill run.
	// All raw records with ts <= CutoffAt are guaranteed to have been
	// considered by this run; the caller can use this as a safe upper bound
	// when pruning raw history. Zero when Skipped is true.
	CutoffAt time.Time
}

// distillAction matches the LLM JSON output.
type distillAction struct {
	Action         string      `json:"action"` // "update" | "create" | "merge"
	Slug           string      `json:"slug,omitempty"`
	Title          string      `json:"title,omitempty"`
	Channels       []string    `json:"channels,omitempty"`
	Body           string      `json:"body,omitempty"`
	Patch          patchSpec   `json:"patch,omitempty"`
	SourceRefs     []sourceRef `json:"source_refs,omitempty"`
	Confidence     string      `json:"confidence,omitempty"`
	PrimarilyHuman bool        `json:"primarily_human,omitempty"`
	Slugs          []string    `json:"slugs,omitempty"` // for merge
	Into           string      `json:"into,omitempty"`  // for merge
}

type patchSpec struct {
	TLDR      string   `json:"tldr,omitempty"`
	Timeline  []string `json:"timeline,omitempty"`
	Decisions []string `json:"decisions,omitempty"`
	Open      []string `json:"open,omitempty"`
}

type sourceRef struct {
	File  string `json:"file"`
	Lines string `json:"lines"`
}

type promptRecord struct {
	Timestamp time.Time
	Role      string
	Chat      string
	Thread    string
	Sender    string
	Text      string
}

func (d *Distiller) Run(ctx context.Context, since time.Time) (DistillResult, error) {
	state, err := ReadState(d.repoRoot)
	if err != nil {
		logger.ErrorCF("archiver", "Failed to read archiver state", map[string]any{
			"error":           err.Error(),
			"repository_path": d.repoRoot,
		})
		return DistillResult{}, err
	}
	if since.IsZero() {
		since = state.LastDistilledAt
	}
	// Capture the cutoff *before* reading raw so messages arriving during the
	// distill (including the LLM call) are not at risk of being pruned by a
	// downstream cleanup step keyed off this value.
	cutoff := time.Now().UTC()
	rawRecords, err := d.collectRaw(since)
	if err != nil {
		logger.ErrorCF("archiver", "Failed to collect raw messages", map[string]any{
			"error":           err.Error(),
			"repository_path": d.repoRoot,
		})
		return DistillResult{}, err
	}
	if len(rawRecords) == 0 {
		logger.InfoCF("archiver", "No raw messages to distill", map[string]any{
			"repository_path": d.repoRoot,
			"since":           since,
		})
		return DistillResult{Skipped: true}, nil
	}
	logger.InfoCF("archiver", "Collected raw messages for distill", map[string]any{
		"repository_path": d.repoRoot,
		"raw_count":       len(rawRecords),
		"since":           since,
	})

	prompt := buildPrompt(state.TopicIndex, rawRecords)
	out, err := d.llm.Distill(ctx, prompt)
	if err != nil {
		logger.ErrorCF("archiver", "Distill LLM call failed", map[string]any{
			"error":           err.Error(),
			"repository_path": d.repoRoot,
			"raw_count":       len(rawRecords),
		})
		return DistillResult{}, fmt.Errorf("llm: %w", err)
	}
	out = stripCodeFence(out)

	var actions []distillAction
	if err := json.Unmarshal([]byte(out), &actions); err != nil {
		logger.ErrorCF("archiver", "Failed to parse distill output", map[string]any{
			"error":           err.Error(),
			"repository_path": d.repoRoot,
			"raw_count":       len(rawRecords),
			"output_preview":  truncateForLog(out, 500),
		})
		return DistillResult{}, fmt.Errorf("parse llm output: %w", err)
	}
	logger.InfoCF("archiver", "Parsed distill actions", map[string]any{
		"repository_path": d.repoRoot,
		"raw_count":       len(rawRecords),
		"action_count":    len(actions),
	})

	res := DistillResult{CutoffAt: cutoff}
	now := cutoff
	indexBySlug := make(map[string]TopicIndexEntry)
	for _, e := range state.TopicIndex {
		indexBySlug[e.Slug] = e
	}

	for _, a := range actions {
		switch a.Action {
		case "create":
			t := actionToTopic(a, now)
			if err := writeTopic(d.repoRoot, t); err != nil {
				return res, err
			}
			indexBySlug[t.Slug] = TopicIndexEntry{
				Slug: t.Slug, Title: t.Title, Channels: t.Channels,
				Status: t.Status, Updated: t.Updated,
			}
			res.Created++
		case "update":
			t, err := readTopic(d.repoRoot, a.Slug)
			if err != nil {
				return res, err
			}
			t = applyPatch(t, a, now)
			if err := writeTopic(d.repoRoot, t); err != nil {
				return res, err
			}
			indexBySlug[t.Slug] = TopicIndexEntry{
				Slug: t.Slug, Title: t.Title, Channels: t.Channels,
				Status: t.Status, Updated: t.Updated,
			}
			res.Updated++
		case "merge":
			if err := mergeTopics(d.repoRoot, a.Slugs, a.Into, now); err != nil {
				return res, err
			}
			for _, s := range a.Slugs {
				delete(indexBySlug, s)
			}
			res.Merged++
		}
	}

	state.TopicIndex = state.TopicIndex[:0]
	for _, v := range indexBySlug {
		state.TopicIndex = append(state.TopicIndex, v)
	}
	sort.Slice(state.TopicIndex, func(i, j int) bool { return state.TopicIndex[i].Slug < state.TopicIndex[j].Slug })

	if err := os.WriteFile(filepath.Join(d.repoRoot, "index.md"), []byte(RenderIndex(state.TopicIndex)), 0o644); err != nil {
		logger.ErrorCF("archiver", "Failed to write archive index", map[string]any{
			"error":           err.Error(),
			"repository_path": d.repoRoot,
		})
		return res, err
	}
	summary := fmt.Sprintf("distilled: %d created, %d updated, %d merged", res.Created, res.Updated, res.Merged)
	if err := AppendLog(d.repoRoot, now, summary); err != nil {
		logger.ErrorCF("archiver", "Failed to append archive log", map[string]any{
			"error":           err.Error(),
			"repository_path": d.repoRoot,
		})
		return res, err
	}

	state.LastDistilledAt = now
	if err := WriteState(d.repoRoot, state); err != nil {
		logger.ErrorCF("archiver", "Failed to write archive state", map[string]any{
			"error":           err.Error(),
			"repository_path": d.repoRoot,
		})
		return res, err
	}
	return res, nil
}

func (d *Distiller) collectRaw(since time.Time) ([]promptRecord, error) {
	rawDir := filepath.Join(d.repoRoot, "raw")
	var records []promptRecord
	err := filepath.Walk(rawDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			var rec RawRecord
			if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
				continue
			}
			if since.IsZero() || rec.Timestamp.After(since) {
				chat, thread := normalizePromptChat(rec.Platform, rec.ChatID, rec.ThreadID)
				records = append(records, promptRecord{
					Timestamp: rec.Timestamp,
					Role:      rec.Role,
					Chat:      chat,
					Thread:    thread,
					Sender:    formatPromptSender(rec.Sender),
					Text:      rec.Text,
				})
			}
		}
		return sc.Err()
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.SliceStable(records, func(i, j int) bool {
		if !records[i].Timestamp.Equal(records[j].Timestamp) {
			return records[i].Timestamp.Before(records[j].Timestamp)
		}
		if records[i].Chat != records[j].Chat {
			return records[i].Chat < records[j].Chat
		}
		if records[i].Thread != records[j].Thread {
			return records[i].Thread < records[j].Thread
		}
		if records[i].Role != records[j].Role {
			return records[i].Role < records[j].Role
		}
		return records[i].Text < records[j].Text
	})
	return records, nil
}

func buildPrompt(topics []TopicIndexEntry, rawRecords []promptRecord) string {
	var b strings.Builder
	b.WriteString("You maintain a topic-based knowledge base. Group human messages into topics.\n")
	b.WriteString("Output a JSON array of actions ('create' | 'update' | 'merge'). No prose.\n\n")
	b.WriteString("# Existing topics\n")
	for _, t := range topics {
		fmt.Fprintf(&b, "- %s (%s): %s\n", t.Slug, t.Status, t.Title)
	}
	b.WriteString("\n# Raw messages (toon)\n")
	b.WriteString(renderPromptRecordsTOON(rawRecords))
	b.WriteString("\n")
	return b.String()
}

func formatPromptSender(sender Sender) string {
	switch {
	case sender.DisplayName != "":
		return sender.DisplayName
	case sender.Username != "":
		return sender.Username
	default:
		return sender.PlatformID
	}
}

func normalizePromptChat(platform, chatID, threadID string) (chat string, thread string) {
	thread = threadID
	if baseChat, suffix, ok := strings.Cut(chatID, "/"); ok {
		chatID = baseChat
		if thread == "" {
			thread = suffix
		}
	}
	return ChannelKey(platform, chatID), thread
}

func truncateForLog(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}

func actionToTopic(a distillAction, now time.Time) Topic {
	day := now.Format("2006-01-02")
	srcs := make([]string, 0, len(a.SourceRefs))
	for _, r := range a.SourceRefs {
		srcs = append(srcs, r.File+"#"+r.Lines)
	}
	return Topic{
		Title: a.Title, Slug: a.Slug, Status: "open",
		Channels: a.Channels, Sources: srcs,
		Created: day, Updated: day,
		Confidence: a.Confidence, PrimarilyHuman: a.PrimarilyHuman,
		Body: a.Body,
	}
}

func applyPatch(t Topic, a distillAction, now time.Time) Topic {
	day := now.Format("2006-01-02")
	for _, r := range a.SourceRefs {
		t.Sources = append(t.Sources, r.File+"#"+r.Lines)
	}
	for _, c := range a.Channels {
		if !contains(t.Channels, c) {
			t.Channels = append(t.Channels, c)
		}
	}
	if a.Confidence != "" {
		t.Confidence = a.Confidence
	}
	t.Updated = day
	if a.Patch.TLDR != "" || len(a.Patch.Timeline) > 0 || len(a.Patch.Decisions) > 0 || len(a.Patch.Open) > 0 {
		t.Body = renderBody(a.Patch, t.Body)
	}
	return t
}

func renderBody(p patchSpec, prev string) string {
	var b strings.Builder
	if p.TLDR != "" {
		fmt.Fprintf(&b, "## TL;DR\n\n%s\n\n", p.TLDR)
	}
	if len(p.Timeline) > 0 {
		b.WriteString("## 経緯\n\n")
		for _, t := range p.Timeline {
			fmt.Fprintf(&b, "- %s\n", t)
		}
		b.WriteString("\n")
	}
	if len(p.Decisions) > 0 {
		b.WriteString("## 決定事項\n\n")
		for _, d := range p.Decisions {
			fmt.Fprintf(&b, "- %s\n", d)
		}
		b.WriteString("\n")
	}
	if len(p.Open) > 0 {
		b.WriteString("## 未解決事項\n\n")
		for _, o := range p.Open {
			fmt.Fprintf(&b, "- %s\n", o)
		}
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return prev
	}
	return b.String()
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func writeTopic(repoRoot string, t Topic) error {
	dir := filepath.Join(repoRoot, "topics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, t.Slug+".md"), []byte(t.Render()), 0o644)
}

func readTopic(repoRoot, slug string) (Topic, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "topics", slug+".md"))
	if errors.Is(err, os.ErrNotExist) {
		return Topic{Slug: slug, Status: "open"}, nil
	}
	if err != nil {
		return Topic{}, err
	}
	return ParseTopic(string(data))
}

func mergeTopics(repoRoot string, slugs []string, into string, now time.Time) error {
	if into == "" || len(slugs) == 0 {
		return fmt.Errorf("merge requires into and slugs")
	}
	mergedTopic, err := readTopic(repoRoot, into)
	if err != nil {
		return err
	}
	if mergedTopic.Slug == "" {
		mergedTopic = Topic{Slug: into, Status: "open"}
	}
	for _, s := range slugs {
		if s == into {
			continue
		}
		t, err := readTopic(repoRoot, s)
		if err != nil {
			return err
		}
		for _, src := range t.Sources {
			if !contains(mergedTopic.Sources, src) {
				mergedTopic.Sources = append(mergedTopic.Sources, src)
			}
		}
		for _, c := range t.Channels {
			if !contains(mergedTopic.Channels, c) {
				mergedTopic.Channels = append(mergedTopic.Channels, c)
			}
		}
		_ = os.Remove(filepath.Join(repoRoot, "topics", s+".md"))
	}
	mergedTopic.Updated = now.Format("2006-01-02")
	return writeTopic(repoRoot, mergedTopic)
}
