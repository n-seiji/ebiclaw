package archiver

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T, dir string, bare bool) {
	t.Helper()
	args := []string{"init"}
	if bare {
		args = append(args, "--bare")
	}
	cmd := exec.Command("git", append(args, dir)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=tester", "GIT_AUTHOR_EMAIL=t@e",
		"GIT_COMMITTER_NAME=tester", "GIT_COMMITTER_EMAIL=t@e",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestGitPusher_AddCommitPush(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	gitInit(t, bare, true)

	work := filepath.Join(root, "work")
	gitInit(t, work, false)
	git(t, work, "remote", "add", "origin", bare)
	git(t, work, "checkout", "-b", "main")

	git(t, work, "commit", "--allow-empty", "-m", "init")
	git(t, work, "push", "-u", "origin", "main")

	if err := os.WriteFile(filepath.Join(work, "a.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := NewGitPusher(work)
	res, err := g.Run("archive: test commit")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Pushed {
		t.Fatalf("expected pushed: %+v", res)
	}
}

func TestGitPusher_NoChanges(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root, false)
	git(t, root, "checkout", "-b", "main")
	git(t, root, "commit", "--allow-empty", "-m", "init")

	g := NewGitPusher(root)
	res, err := g.Run("archive: noop")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Pushed {
		t.Fatalf("did not expect push when no changes")
	}
}
