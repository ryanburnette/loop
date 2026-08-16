package run

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	run("add", ".")
	run("commit", "-qm", "init")
}

func TestUntilGreenSucceeds(t *testing.T) {
	root := t.TempDir()
	// Copy the fixture loop into a throwaway git repo so workroot resolves.
	src := filepath.Join(repoRoot(t), "testdata", "loops", "until-green")
	dst := filepath.Join(root, "myloop")
	if err := copyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)

	code, err := Run(Options{
		Dir:   dst,
		Pi:    filepath.Join(repoRoot(t), "testdata", "fake-pi"),
		Quiet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit %d want 0", code)
	}
}

func TestCompactionFailPolicy(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(repoRoot(t), "testdata", "loops", "until-green")
	dst := filepath.Join(root, "myloop")
	if err := copyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)

	t.Setenv("FAKE_PI_COMPACT", "1")
	code, err := Run(Options{
		Dir:     dst,
		Pi:      filepath.Join(repoRoot(t), "testdata", "fake-pi"),
		Quiet:   true,
		Compact: "fail",
		MaxIter: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("compact=fail should not succeed when fake-pi compacts")
	}
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, info.Mode())
	})
}
