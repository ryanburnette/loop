package loopdir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDefaultsToDotLoopInCWD(t *testing.T) {
	got, err := Resolve("/some/cwd", "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/some/cwd", ".loop")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveExplicitRelativeJoinsCWD(t *testing.T) {
	got, err := Resolve("/some/cwd", "otherdir")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/some/cwd", "otherdir")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveExplicitAbsolutePassesThrough(t *testing.T) {
	got, err := Resolve("/some/cwd", "/abs/dir")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/abs/dir" {
		t.Fatalf("got %q want /abs/dir", got)
	}
}

func TestMissingTrueForEmptyDir(t *testing.T) {
	dir := t.TempDir()
	if !Missing(dir) {
		t.Fatal("expected Missing=true for an empty/nonexistent dir")
	}
}

func TestMissingTrueForNonexistentDir(t *testing.T) {
	if !Missing(filepath.Join(t.TempDir(), "nope")) {
		t.Fatal("expected Missing=true for a dir that doesn't exist")
	}
}

func TestMissingFalseWhenLoopEnvPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "loop.env"), []byte("LOOP_MAX_ITER=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Missing(dir) {
		t.Fatal("expected Missing=false when loop.env exists")
	}
}

func TestMissingFalseWhenManifestPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest"), []byte("turn w p.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Missing(dir) {
		t.Fatal("expected Missing=false when manifest exists")
	}
}

func TestMissingFalseWhenPromptsHaveFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompts", "01-writer.md"), []byte("go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Missing(dir) {
		t.Fatal("expected Missing=false when prompts/ has files")
	}
}

func TestMissingTrueWhenPromptsDirEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !Missing(dir) {
		t.Fatal("expected Missing=true when prompts/ exists but is empty")
	}
}

func TestMissingMessageMentionsInit(t *testing.T) {
	msg := MissingMessage("/x/.loop")
	if !strings.Contains(msg, "loop init") {
		t.Fatalf("message should mention loop init, got %q", msg)
	}
	if !strings.Contains(msg, "/x/.loop") {
		t.Fatalf("message should name the dir it looked in, got %q", msg)
	}
}
