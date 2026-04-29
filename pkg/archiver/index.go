package archiver

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RenderIndex builds the contents of index.md. Order: open first (newest first),
// then resolved (newest first), then archived.
func RenderIndex(entries []TopicIndexEntry) string {
	cp := make([]TopicIndexEntry, len(entries))
	copy(cp, entries)
	statusRank := map[string]int{"open": 0, "resolved": 1, "archived": 2}
	sort.SliceStable(cp, func(i, j int) bool {
		ri, rj := statusRank[cp[i].Status], statusRank[cp[j].Status]
		if ri != rj {
			return ri < rj
		}
		return cp[i].Updated > cp[j].Updated
	})
	var b strings.Builder
	b.WriteString("# Topics\n\n")
	if len(cp) == 0 {
		b.WriteString("_No topics yet._\n")
		return b.String()
	}
	currentStatus := ""
	for _, e := range cp {
		if e.Status != currentStatus {
			fmt.Fprintf(&b, "\n## %s\n\n", strings.Title(e.Status))
			currentStatus = e.Status
		}
		channels := strings.Join(e.Channels, ", ")
		fmt.Fprintf(&b, "- [%s](topics/%s.md) — %s _(updated %s)_\n", e.Title, e.Slug, channels, e.Updated)
	}
	return b.String()
}

// AppendLog appends a single line "<RFC3339> — <summary>" to log.md.
func AppendLog(repoRoot string, ts time.Time, summary string) error {
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		return err
	}
	p := filepath.Join(repoRoot, "log.md")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if stat, _ := f.Stat(); stat != nil && stat.Size() == 0 {
		if _, err := f.WriteString("# Activity Log\n\n"); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(f, "- %s — %s\n", ts.UTC().Format(time.RFC3339), summary)
	return err
}
