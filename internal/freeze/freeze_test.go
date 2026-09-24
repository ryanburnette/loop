package freeze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotAndOK(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a_test.go"), "package a\n")
	mustWrite(t, filepath.Join(root, "pkg", "b_test.go"), "package b\n")
	mustWrite(t, filepath.Join(root, "pkg", "b.go"), "package b\n")

	state := filepath.Join(t.TempDir(), "frozen")
	if err := Snapshot(root, state, []string{"*_test.go"}); err != nil {
		t.Fatal(err)
	}
	if err := Check(root, state); err != nil {
		t.Fatalf("expected ok: %v", err)
	}
}

func TestDrift(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a_test.go")
	mustWrite(t, p, "package a\n")

	state := filepath.Join(t.TempDir(), "frozen")
	if err := Snapshot(root, state, []string{"*_test.go"}); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, p, "package a\n// weakened\n")
	if err := Check(root, state); err == nil {
		t.Fatal("expected drift")
	}
}

func TestIgnoresGitAndState(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a_test.go"), "package a\n")
	mustWrite(t, filepath.Join(root, ".git", "a_test.go"), "nope\n")
	state := filepath.Join(root, "state", "id", "frozen")
	mustWrite(t, filepath.Join(root, "state", "id", "a_test.go"), "nope\n")

	if err := Snapshot(root, state, []string{"*_test.go"}); err != nil {
		t.Fatal(err)
	}
	// Changing .git or state copies must not count as drift.
	mustWrite(t, filepath.Join(root, ".git", "a_test.go"), "changed\n")
	mustWrite(t, filepath.Join(root, "state", "id", "a_test.go"), "changed\n")
	if err := Check(root, state); err != nil {
		t.Fatalf("git/state should be ignored: %v", err)
	}
}

func TestEmptyPatternsOK(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "frozen")
	if err := Snapshot(root, state, nil); err != nil {
		t.Fatal(err)
	}
	if err := Check(root, state); err != nil {
		t.Fatal(err)
	}
	// An empty index means nothing is frozen, even if a matching file appears later.
	mustWrite(t, filepath.Join(root, "a_test.go"), "package a\n")
	if err := Check(root, state); err != nil {
		t.Fatal(err)
	}
}

func TestMissingIndex(t *testing.T) {
	root := t.TempDir()
	if err := Check(root, filepath.Join(t.TempDir(), "frozen")); err == nil || err.Error() != "freeze index missing" {
		t.Fatalf("absent snapshot: %v", err)
	}
	mustWrite(t, filepath.Join(root, "a_test.go"), "package a\n")
	state := filepath.Join(t.TempDir(), "frozen")
	if err := Snapshot(root, state, []string{"*_test.go"}); err != nil {
		t.Fatal(err)
	}
	base, err := Load(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(state, "index")); err != nil {
		t.Fatal(err)
	}
	if err := Check(root, state); err == nil || err.Error() != "freeze index missing" {
		t.Fatalf("Check: %v", err)
	}
	if err := base.Check(root, state); err == nil || err.Error() != "freeze index missing" {
		t.Fatalf("baseline: %v", err)
	}
}

func TestEditedSumFailsAgainstBaseline(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a_test.go")
	mustWrite(t, p, "package a\n")
	state := filepath.Join(t.TempDir(), "frozen")
	if err := Snapshot(root, state, []string{"*_test.go"}); err != nil {
		t.Fatal(err)
	}
	base, err := Load(state)
	if err != nil {
		t.Fatal(err)
	}
	// Rewrite the file and the on-disk sum so a fresh read of *.sum matches.
	// That is the edit a turn can make. The loaded baseline must still fail.
	mustWrite(t, p, "package a\n// weakened\n")
	if err := writeSums(filepath.Join(state, "1.sum"), root, []string{p}); err != nil {
		t.Fatal(err)
	}
	if err := Check(root, state); err != nil {
		t.Fatalf("fresh load trusts the rewritten sum, got %v", err)
	}
	if err := base.Check(root, state); err == nil || err.Error() != "freeze store modified" {
		t.Fatalf("got %v", err)
	}
}

func TestEditedIndexFailsAgainstBaseline(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a_test.go"), "package a\n")
	state := filepath.Join(t.TempDir(), "frozen")
	if err := Snapshot(root, state, []string{"*_test.go"}); err != nil {
		t.Fatal(err)
	}
	base, err := Load(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "index"), []byte("*.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := base.Check(root, state); err == nil || err.Error() != "freeze store modified" {
		t.Fatalf("got %v", err)
	}
}

func TestDeletedSumFailsAgainstBaseline(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a_test.go"), "package a\n")
	state := filepath.Join(t.TempDir(), "frozen")
	if err := Snapshot(root, state, []string{"*_test.go"}); err != nil {
		t.Fatal(err)
	}
	base, err := Load(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(state, "1.sum")); err != nil {
		t.Fatal(err)
	}
	if err := base.Check(root, state); err == nil || err.Error() != "freeze store modified" {
		t.Fatalf("got %v", err)
	}
}

func TestEmptySumThenCreatedFileIsDrift(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "frozen")
	if err := Snapshot(root, state, []string{"*_test.go"}); err != nil {
		t.Fatal(err)
	}
	sumPath := filepath.Join(state, "1.sum")
	before, err := os.ReadFile(sumPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(before)) != "" {
		t.Fatalf("expected empty sum, got %q", before)
	}
	base, err := Load(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := base.Check(root, state); err != nil {
		t.Fatalf("empty sum should pass before any file exists: %v", err)
	}
	mustWrite(t, filepath.Join(root, "new_test.go"), "package n\n")
	err = base.Check(root, state)
	if err == nil {
		t.Fatal("file created after an empty snapshot should be drift")
	}
	if !strings.Contains(err.Error(), "freeze drift:") || !strings.Contains(err.Error(), "new_test.go") {
		t.Fatalf("got %v", err)
	}
	after, err := os.ReadFile(sumPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("check must not rewrite the sum")
	}
}

func TestMissingFileIsDrift(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a_test.go")
	mustWrite(t, p, "package a\n")
	state := filepath.Join(t.TempDir(), "frozen")
	if err := Snapshot(root, state, []string{"*_test.go"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := Check(root, state); err == nil {
		t.Fatal("deleted frozen file should be drift")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
