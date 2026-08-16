package gitinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestCollectCleanRepo(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-qm", "init")

	info := Collect(dir)
	if !info.Repo {
		t.Fatal("expected Repo=true")
	}
	if info.Branch != "main" {
		t.Fatalf("Branch=%q want main", info.Branch)
	}
	if info.ShortSHA == "" {
		t.Fatal("expected a non-empty ShortSHA")
	}
	if info.Dirty || info.DirtyN != 0 {
		t.Fatalf("expected clean, got Dirty=%v DirtyN=%d", info.Dirty, info.DirtyN)
	}
}

func TestCollectDirtyRepo(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-qm", "init")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	info := Collect(dir)
	if !info.Dirty {
		t.Fatal("expected Dirty=true")
	}
	if info.DirtyN < 1 {
		t.Fatalf("expected DirtyN >= 1, got %d", info.DirtyN)
	}
}

func TestCollectOutsideGitRepoDegradesGracefully(t *testing.T) {
	dir := t.TempDir() // not a git repo
	info := Collect(dir)
	if info.Repo {
		t.Fatal("expected Repo=false outside a git repo")
	}
	if info.Branch != "" || info.ShortSHA != "" || info.Dirty || info.DirtyN != 0 {
		t.Fatalf("expected zero value outside a repo, got %+v", info)
	}
}

func TestCollectNeverPanicsOnMissingDir(t *testing.T) {
	// Must degrade, not panic or block, even for a path that doesn't exist.
	info := Collect(filepath.Join(t.TempDir(), "does-not-exist"))
	if info.Repo {
		t.Fatal("expected Repo=false for a missing dir")
	}
}
