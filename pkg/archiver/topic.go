package archiver

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Topic models one topics/<slug>.md file.
type Topic struct {
	Title          string   `yaml:"title"`
	Slug           string   `yaml:"slug"`
	Status         string   `yaml:"status"`
	Tags           []string `yaml:"tags,omitempty"`
	Channels       []string `yaml:"channels,omitempty"`
	Sources        []string `yaml:"sources,omitempty"`
	Created        string   `yaml:"created,omitempty"`
	Updated        string   `yaml:"updated,omitempty"`
	Confidence     string   `yaml:"confidence,omitempty"`
	PrimarilyHuman bool     `yaml:"primarily_human"`
	Related        []string `yaml:"related,omitempty"`
	Body           string   `yaml:"-"`
}

// Render returns the markdown representation with YAML frontmatter.
func (t Topic) Render() string {
	fm, err := yaml.Marshal(t)
	if err != nil {
		fm = []byte(fmt.Sprintf("title: %q\nslug: %q\n", t.Title, t.Slug))
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fm)
	b.WriteString("---\n\n")
	b.WriteString(t.Body)
	return b.String()
}

// ParseTopic decodes a markdown file with YAML frontmatter.
func ParseTopic(s string) (Topic, error) {
	if !strings.HasPrefix(s, "---\n") {
		return Topic{}, fmt.Errorf("missing frontmatter")
	}
	rest := s[4:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return Topic{}, fmt.Errorf("unterminated frontmatter")
	}
	fm := rest[:idx]
	body := rest[idx+len("\n---"):]
	body = strings.TrimLeft(body, "\n")

	var t Topic
	if err := yaml.Unmarshal([]byte(fm), &t); err != nil {
		return Topic{}, err
	}
	t.Body = body
	return t, nil
}
