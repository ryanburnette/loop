package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestHeaderPlain(t *testing.T) {
	var buf bytes.Buffer
	r := New(Options{Out: &buf, Color: false})
	r.Header(Header{
		ID:        "20260101T000000Z-1",
		Dir:       "/tmp/myloop",
		WorkRoot:  "/tmp/repo",
		Branch:    "loop/20260101T000000Z-1",
		Session:   "none",
		MaxIter:   5,
		Objective: true,
	})
	s := buf.String()
	for _, want := range []string{
		"loop",
		"20260101T000000Z-1",
		"/tmp/myloop",
		"/tmp/repo",
		"none",
		"5",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("header missing %q\n%s", want, s)
		}
	}
}

func TestIterationAndSteps(t *testing.T) {
	var buf bytes.Buffer
	r := New(Options{Out: &buf, Color: false})
	r.Iteration(1, 5)
	r.StepStart("turn", "writer", "xai/grok-4.5")
	r.StepDone(true, "done", 3)
	r.StepStart("gate", "tests", "required")
	r.StepDone(false, "FAIL", 1)
	s := buf.String()
	if !strings.Contains(s, "iteration 1/5") {
		t.Fatalf("iter: %s", s)
	}
	if !strings.Contains(s, "turn writer") || !strings.Contains(s, "gate tests") {
		t.Fatalf("steps: %s", s)
	}
	if !strings.Contains(s, "FAIL") {
		t.Fatalf("fail mark: %s", s)
	}
}

func TestQuietOnlyFinal(t *testing.T) {
	var buf bytes.Buffer
	r := New(Options{Out: &buf, Color: false, Quiet: true})
	r.Header(Header{ID: "x"})
	r.Iteration(1, 2)
	r.Success(1, "loop/x")
	s := buf.String()
	if strings.Contains(s, "iteration") {
		t.Fatalf("quiet leaked progress: %s", s)
	}
	if !strings.Contains(s, "SUCCESS") {
		t.Fatalf("quiet missing result: %s", s)
	}
}

func TestNoColorEscapesWhenDisabled(t *testing.T) {
	var buf bytes.Buffer
	r := New(Options{Out: &buf, Color: false})
	r.Success(1, "loop/x")
	if bytes.Contains(buf.Bytes(), []byte{0x1b}) {
		t.Fatalf("escape in plain output: %q", buf.Bytes())
	}
}

func TestJSONEmitsRunnerEvents(t *testing.T) {
	var buf bytes.Buffer
	r := New(Options{Out: &buf, Color: false, JSON: true})
	r.Header(Header{ID: "abc", Session: "none", MaxIter: 3, Objective: true})
	r.Iteration(1, 3)
	r.StepStart("turn", "writer", "default")
	r.StepDone(true, "done", 2)
	r.Success(1, "state/abc")
	s := buf.String()
	if s == "" {
		t.Fatal("--json must emit runner events, not swallow output")
	}
	for _, want := range []string{`"type"`, "header", "iteration", "success"} {
		if !strings.Contains(s, want) {
			t.Fatalf("json stream missing %q\n%s", want, s)
		}
	}
}
