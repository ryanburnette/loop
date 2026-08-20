package run

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

func clearLoopEnv(t *testing.T) {
	t.Helper()
	for _, e := range os.Environ() {
		k, _, ok := strings.Cut(e, "=")
		if !ok || !strings.HasPrefix(k, "LOOP_") {
			continue
		}
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func TestUntilGreenSucceeds(t *testing.T) {
	clearLoopEnv(t)
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
	clearLoopEnv(t)
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

func TestHandoffReadsGoalFromLoopDir(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	src := filepath.Join(repoRoot(t), "testdata", "loops", "until-green")
	dst := filepath.Join(root, "myloop")
	if err := copyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Goal and constraints are part of the recipe, so they live in the loop
	// dir alongside loop.env — everything needed to set up a loop is in one
	// directory. Decoys at the workroot root must be ignored.
	if err := os.WriteFile(filepath.Join(dst, "TODO.md"), []byte("fix the csv loader\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "CONSTRAINTS.md"), []byte("- do not edit tests\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "TODO.md"), []byte("stale root goal\n"), 0o644); err != nil {
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
		t.Fatalf("exit %d", code)
	}

	// Find the run's handoff.
	matches, err := filepath.Glob(filepath.Join(dst, "state", "*", "handoff.md"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no handoff.md written: %v %v", matches, err)
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "fix the csv loader") {
		t.Fatalf("handoff missing the loop dir's TODO.md goal:\n%s", s)
	}
	if !strings.Contains(s, "do not edit tests") {
		t.Fatalf("handoff missing the loop dir's CONSTRAINTS.md:\n%s", s)
	}
	if strings.Contains(s, "stale root goal") {
		t.Fatalf("handoff picked up a TODO.md outside the loop dir:\n%s", s)
	}
}

func TestWritesStatusFile(t *testing.T) {
	clearLoopEnv(t)
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

	code, err := Run(Options{
		Dir:   dst,
		Pi:    filepath.Join(repoRoot(t), "testdata", "fake-pi"),
		Quiet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	matches, err := filepath.Glob(filepath.Join(dst, "state", "*", "status"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no status file written: %v %v", matches, err)
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "success") && !strings.Contains(string(b), "iteration") {
		t.Fatalf("status file empty or unhelpful: %q", b)
	}
}

func TestResumeDoesNotRefreeze(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	src := filepath.Join(repoRoot(t), "testdata", "loops", "until-green")
	dst := filepath.Join(root, "myloop")
	if err := copyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A frozen file we will edit between start and resume.
	frozen := filepath.Join(root, "keep_test.go")
	if err := os.WriteFile(frozen, []byte("package keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Enable freeze via the fixture loop.env and require the built-in gate.
	envPath := filepath.Join(dst, "loop.env")
	b, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, append(b, []byte("\nLOOP_FREEZE=*_test.go\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	manPath := filepath.Join(dst, "manifest")
	mb, err := os.ReadFile(manPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manPath, append(mb, []byte("\ngate frozen loop:frozen\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)

	code, err := Run(Options{
		Dir:     dst,
		Pi:      filepath.Join(repoRoot(t), "testdata", "fake-pi"),
		Quiet:   true,
		MaxIter: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("first run exit %d", code)
	}

	idb, err := os.ReadFile(filepath.Join(dst, "state", "CURRENT_ID"))
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(string(idb))

	// Edit the frozen file after the original snapshot.
	if err := os.WriteFile(frozen, []byte("package keep\n// drifted\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Resume must compare against the original snapshot, not re-hash now.
	code, err = Run(Options{
		Dir:      dst,
		Pi:       filepath.Join(repoRoot(t), "testdata", "fake-pi"),
		Quiet:    true,
		MaxIter:  2,
		ResumeID: id,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("resume should fail frozen gate after drift; re-freeze on resume is a bug")
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
