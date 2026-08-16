package control

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadAndTruncate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "control")
	body := "pause\nset LOOP_MAX_ITER=2\n# comment\n\nstop\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmds, err := Consume(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 3 {
		t.Fatalf("cmds: %+v", cmds)
	}
	if cmds[0].Kind != Pause {
		t.Fatalf("0: %+v", cmds[0])
	}
	if cmds[1].Kind != Set || cmds[1].Key != "LOOP_MAX_ITER" || cmds[1].Value != "2" {
		t.Fatalf("1: %+v", cmds[1])
	}
	if cmds[2].Kind != Stop {
		t.Fatalf("2: %+v", cmds[2])
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 0 {
		t.Fatalf("expected truncated, got %q", b)
	}
}

func TestMissingFile(t *testing.T) {
	cmds, err := Consume(filepath.Join(t.TempDir(), "control"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 0 {
		t.Fatalf("got %+v", cmds)
	}
}

func TestUnknownLineKeptAsUnknown(t *testing.T) {
	p := filepath.Join(t.TempDir(), "control")
	if err := os.WriteFile(p, []byte("flarp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmds, err := Consume(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].Kind != Unknown || cmds[0].Raw != "flarp" {
		t.Fatalf("got %+v", cmds)
	}
}

func TestResume(t *testing.T) {
	p := filepath.Join(t.TempDir(), "control")
	if err := os.WriteFile(p, []byte("resume\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmds, err := Consume(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].Kind != Resume {
		t.Fatalf("got %+v", cmds)
	}
}
