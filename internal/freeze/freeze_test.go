package freeze

import (
	"os"
	"path/filepath"
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
