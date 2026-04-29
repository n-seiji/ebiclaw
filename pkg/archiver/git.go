package archiver

import (
	"bytes"
	"fmt"
	"os/exec"
)

type GitPusher struct {
	repoRoot string
}

type PushResult struct {
	Committed bool
	Pushed    bool
}

func NewGitPusher(repoRoot string) *GitPusher {
	return &GitPusher{repoRoot: repoRoot}
}

func (g *GitPusher) Run(message string) (PushResult, error) {
	dirty, err := g.hasChanges()
	if err != nil {
		return PushResult{}, err
	}
	if !dirty {
		return PushResult{}, nil
	}
	if err := g.exec("add", "."); err != nil {
		return PushResult{}, err
	}
	if err := g.exec("commit", "-m", message); err != nil {
		return PushResult{}, err
	}
	res := PushResult{Committed: true}
	if err := g.exec("push"); err != nil {
		return res, fmt.Errorf("git push: %w", err)
	}
	res.Pushed = true
	return res, nil
}

func (g *GitPusher) hasChanges() (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = g.repoRoot
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("git status: %w: %s", err, out.String())
	}
	return out.Len() > 0, nil
}

func (g *GitPusher) exec(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.repoRoot
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %v: %w: %s", args, err, out.String())
	}
	return nil
}
