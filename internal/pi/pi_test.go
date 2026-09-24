package pi

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fakePI(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	// internal/pi -> repo root testdata/fake-pi
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	p := filepath.Join(root, "testdata", "fake-pi")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fake-pi missing at %s: %v", p, err)
	}
	return p
}

func TestArgvPrintJSON(t *testing.T) {
	args := Argv(Request{
		PiPath:     "pi",
		Model:      "xai/grok-4.5",
		SessionID:  "abc",
		SessionDir: "/tmp/sess",
		Approve:    true,
		PromptFile: "/abs/prompt.md",
		Handoff:    "/abs/handoff.md",
		Context:    "fix it",
	})
	got := strings.Join(args, " ")
	for _, want := range []string{
		"pi", "-p", "--mode json", "--model xai/grok-4.5",
		"--session-id abc", "--session-dir /tmp/sess",
		"--approve", "@/abs/prompt.md", "@/abs/handoff.md", "fix it",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("argv missing %q\n%s", want, got)
		}
	}
	if strings.Contains(got, "--no-session") {
		t.Fatal("shared session should not pass --no-session")
	}
}

func TestArgvContextAfterEndOfFlags(t *testing.T) {
	args := Argv(Request{
		PiPath:     "pi",
		SessionID:  "abc",
		SessionDir: "/tmp/sess",
		PromptFile: "/abs/prompt.md",
		Handoff:    "/abs/handoff.md",
		Context:    "--no-session",
	})
	dash := -1
	for i, a := range args {
		if a == "--" {
			dash = i
			break
		}
	}
	if dash < 0 {
		t.Fatalf("argv missing --: %q", args)
	}
	sessionBefore := false
	before, after := 0, 0
	for i, a := range args {
		if a == "--session-id" && i < dash {
			sessionBefore = true
		}
		if a == "--no-session" {
			if i < dash {
				before++
			} else if i > dash {
				after++
			}
		}
	}
	if !sessionBefore {
		t.Fatalf("--session-id not before --: %q", args)
	}
	if before != 0 || after != 1 {
		t.Fatalf("--no-session before=%d after=%d, want 0 and 1: %q", before, after, args)
	}
	for _, want := range []string{"@/abs/prompt.md", "@/abs/handoff.md"} {
		at := -1
		for i, a := range args {
			if a == want {
				at = i
				break
			}
		}
		if at <= dash {
			t.Fatalf("%s not after --: %q", want, args)
		}
	}
}

func TestArgvNoSession(t *testing.T) {
	args := Argv(Request{PiPath: "pi", PromptFile: "/p.md"})
	got := strings.Join(args, " ")
	if !strings.Contains(got, "--no-session") {
		t.Fatalf("none policy should pass --no-session: %s", got)
	}
}

func TestRunExtractsTextAndUsage(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "turn.md")
	res, err := Run(Request{
		PiPath:     fakePI(t),
		PromptFile: "/p.md",
		WorkRoot:   dir,
		StdoutFile: out,
		JSONLFile:  out + ".jsonl",
		StderrFile: out + ".err",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hello from fake-pi" {
		t.Fatalf("text: %q", res.Text)
	}
	if len(res.Messages) != 1 || res.Messages[0] != "hello from fake-pi" {
		t.Fatalf("messages: %#v", res.Messages)
	}
	// fake-pi still emits session_status. The json stream must not believe it.
	if res.ContextPercent != 0 {
		t.Fatalf("context percent: %d", res.ContextPercent)
	}
	if res.LastTool != "bash" {
		t.Fatalf("last tool: %q", res.LastTool)
	}
	if res.Compacted {
		t.Fatal("should not be compacted")
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "hello from fake-pi") {
		t.Fatalf("turn file: %s", b)
	}
}

func TestRunDetectsCompaction(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "turn.md")
	t.Setenv("FAKE_PI_COMPACT", "1")
	res, err := Run(Request{
		PiPath:     fakePI(t),
		PromptFile: "/p.md",
		WorkRoot:   dir,
		StdoutFile: out,
		JSONLFile:  out + ".jsonl",
		StderrFile: out + ".err",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Compacted {
		t.Fatal("expected compacted")
	}
}

func TestParseFixture(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	raw, err := os.ReadFile(filepath.Join(root, "testdata", "events", "turn.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := ParseJSONL(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "fixed the loader" {
		t.Fatalf("text: %q", res.Text)
	}
	if len(res.Messages) != 1 || res.Messages[0] != "fixed the loader" {
		t.Fatalf("messages: %#v", res.Messages)
	}
	if res.LastTool != "edit" {
		t.Fatalf("last tool: %q", res.LastTool)
	}
	// The fixture still emits session_status. That event must not set percent.
	if res.ContextPercent != 0 {
		t.Fatalf("percent: %d", res.ContextPercent)
	}
}

func TestParseMultiMessageKeepsEveryAssistantBlock(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	raw, err := os.ReadFile(filepath.Join(root, "testdata", "events", "turn-multimessage.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var deltas []string
	res, err := parseStream(strings.NewReader(string(raw)), func(ev Event) {
		if ev.TextDelta != "" {
			deltas = append(deltas, ev.TextDelta)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(deltas) != 1 || deltas[0] != "VERDICT: PASS" {
		t.Fatalf("onEvent deltas: %#v", deltas)
	}
	first := "VERDICT: PASS\nchecked the loader"
	second := "edited loader.go"
	if len(res.Messages) != 2 || res.Messages[0] != first || res.Messages[1] != second {
		t.Fatalf("messages: %#v", res.Messages)
	}
	want := first + "\n\n---\n\n" + second
	if res.Text != want {
		t.Fatalf("text: %q", res.Text)
	}
	// The delta is a prefix of the first message. Twice means it was concatenated.
	if strings.Count(res.Text, "VERDICT: PASS") != 1 {
		t.Fatalf("verdict count: %q", res.Text)
	}
	if res.ContextPercent != 0 {
		t.Fatalf("percent: %d", res.ContextPercent)
	}
}

func writeSession(t *testing.T, dir, id string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "20260101T000000_"+id+".jsonl")
	body := "{\"type\":\"session\",\"version\":3,\"id\":\"" + id + "\",\"cwd\":\"/work\"}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProbeArgvOmitsSessionIDAndFork(t *testing.T) {
	args := probeArgv("pi", "/abs/sess.jsonl", "/abs/sessions", "xai/grok")
	got := strings.Join(args, "\n")
	for _, want := range []string{
		"pi",
		"--mode",
		"rpc",
		"--offline",
		"--no-tools",
		"--no-extensions",
		"--session",
		"/abs/sess.jsonl",
		"--session-dir",
		"/abs/sessions",
		"--model",
		"xai/grok",
	} {
		if !strings.Contains("\n"+got+"\n", "\n"+want+"\n") {
			t.Fatalf("argv missing %q\n%s", want, got)
		}
	}
	for _, ban := range []string{"--session-id", "--fork", "switch_session", "-p"} {
		if strings.Contains("\n"+got+"\n", "\n"+ban+"\n") {
			t.Fatalf("argv must not contain %q\n%s", ban, got)
		}
	}
	noModel := probeArgv("pi", "/abs/sess.jsonl", "/abs/sessions", "")
	if strings.Contains(strings.Join(noModel, "\n"), "--model") {
		t.Fatalf("empty model should be omitted: %v", noModel)
	}
}

func TestProbeRoundsPercentFromOtherCwd(t *testing.T) {
	work := t.TempDir()
	sess := filepath.Join(work, "sessions")
	id := "turn-1"
	jsonl := writeSession(t, sess, id)
	// Header cwd is the workroot. The process itself starts somewhere else,
	// which is the miss that makes pi create a second file for --session-id.
	cwdLine := "{\"type\":\"session\",\"version\":3,\"id\":\"" + id + "\",\"cwd\":\"" + work + "\"}\n"
	if err := os.WriteFile(jsonl, []byte(cwdLine), 0o644); err != nil {
		t.Fatal(err)
	}
	away := t.TempDir()
	t.Chdir(away)
	t.Setenv("FAKE_PI_PERCENT", "39.9")

	logPath := filepath.Join(t.TempDir(), "argv")
	wrapper := writePi(t, "#!/bin/sh\npwd >> "+shellQuote(logPath)+"\nprintf '%s\\n' \"$@\" >> "+shellQuote(logPath)+"\necho '---' >> "+shellQuote(logPath)+"\nexec "+shellQuote(fakePI(t))+" \"$@\"\n")

	pct, known, err := Probe(ProbeRequest{
		PiPath:     wrapper,
		SessionID:  id,
		SessionDir: sess,
		WorkRoot:   work,
		Model:      "xai/grok",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !known || pct != 40 {
		t.Fatalf("percent=%d known=%v, want 40 known", pct, known)
	}
	files, err := filepath.Glob(filepath.Join(sess, "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("probe created a session file: %v", files)
	}
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("argv log: %q", b)
	}
	if !samePath(t, lines[0], work) {
		t.Fatalf("probe cwd %q, want workroot %q (process started in %s)", lines[0], work, away)
	}
	args := strings.Join(lines[1:], "\n")
	if !strings.Contains(args, "--session\n"+jsonl) {
		t.Fatalf("probe did not open the turn's jsonl:\n%s", args)
	}
	for _, ban := range []string{"--session-id", "--fork", "switch_session"} {
		if strings.Contains("\n"+args+"\n", "\n"+ban+"\n") {
			t.Fatalf("argv contains %q\n%s", ban, args)
		}
	}
}

func TestProbePayload(t *testing.T) {
	cases := []struct {
		name    string
		percent string
		known   bool
		pct     int
	}{
		{name: "integer", percent: "41", known: true, pct: 41},
		{name: "known zero", percent: "0", known: true, pct: 0},
		{name: "unknown omits contextUsage", percent: "unknown"},
		{name: "null percent", percent: "null"},
		{name: "non-numeric", percent: "nope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			id := "turn-1"
			writeSession(t, dir, id)
			t.Setenv("FAKE_PI_PERCENT", tc.percent)
			pct, known, err := Probe(ProbeRequest{
				PiPath:     fakePI(t),
				SessionID:  id,
				SessionDir: dir,
				WorkRoot:   t.TempDir(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if known != tc.known || pct != tc.pct {
				t.Fatalf("percent=%d known=%v, want %d known=%v", pct, known, tc.pct, tc.known)
			}
		})
	}

	t.Run("session id mismatch", func(t *testing.T) {
		dir := t.TempDir()
		id := "turn-1"
		writeSession(t, dir, id)
		t.Setenv("FAKE_PI_PERCENT", "41")
		t.Setenv("FAKE_PI_SESSION_ID", "other")
		_, known, err := Probe(ProbeRequest{
			PiPath: fakePI(t), SessionID: id, SessionDir: dir, WorkRoot: t.TempDir(),
		})
		if err != nil || known {
			t.Fatalf("known=%v err=%v", known, err)
		}
	})
	t.Run("missing session id", func(t *testing.T) {
		dir := t.TempDir()
		id := "turn-1"
		writeSession(t, dir, id)
		t.Setenv("FAKE_PI_PERCENT", "41")
		t.Setenv("FAKE_PI_SESSION_ID", "")
		_, known, err := Probe(ProbeRequest{
			PiPath: fakePI(t), SessionID: id, SessionDir: dir, WorkRoot: t.TempDir(),
		})
		if err != nil || known {
			t.Fatalf("known=%v err=%v", known, err)
		}
	})
}

func TestProbeNonZeroExitIsUnknown(t *testing.T) {
	dir := t.TempDir()
	id := "turn-1"
	writeSession(t, dir, id)
	bin := writePi(t, "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"response\",\"command\":\"get_session_stats\",\"success\":true,\"data\":{\"sessionId\":\"turn-1\",\"contextUsage\":{\"percent\":41}}}'\nexit 1\n")
	pct, known, err := Probe(ProbeRequest{
		PiPath: bin, SessionID: id, SessionDir: dir, WorkRoot: t.TempDir(),
	})
	if err != nil || known || pct != 0 {
		t.Fatalf("percent=%d known=%v err=%v", pct, known, err)
	}
}

func TestProbeTimeoutIsUnknown(t *testing.T) {
	old := probeTimeout
	probeTimeout = 200 * time.Millisecond
	t.Cleanup(func() { probeTimeout = old })

	dir := t.TempDir()
	id := "turn-1"
	writeSession(t, dir, id)
	bin := writePi(t, "#!/bin/sh\nsleep 30\n")
	start := time.Now()
	_, known, err := Probe(ProbeRequest{
		PiPath: bin, SessionID: id, SessionDir: dir, WorkRoot: t.TempDir(),
	})
	if known || err == nil {
		t.Fatalf("known=%v err=%v", known, err)
	}
	if time.Since(start) > 8*time.Second {
		t.Fatalf("probe did not time out, took %s", time.Since(start))
	}
}

func TestProbeScanDoesNotStartPi(t *testing.T) {
	dir := t.TempDir()
	id := "turn-1"
	marker := filepath.Join(dir, "started")
	bin := writePi(t, "#!/bin/sh\ntouch "+shellQuote(marker)+"\nexit 0\n")

	t.Run("no file", func(t *testing.T) {
		pct, known, err := Probe(ProbeRequest{
			PiPath: bin, SessionID: id, SessionDir: dir, WorkRoot: dir,
		})
		if err != nil || known || pct != 0 {
			t.Fatalf("percent=%d known=%v err=%v", pct, known, err)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("pi started with no session file")
		}
	})
	t.Run("two files", func(t *testing.T) {
		writeSession(t, dir, id)
		second := filepath.Join(dir, "other_"+id+".jsonl")
		if err := os.WriteFile(second, []byte("{\"type\":\"session\",\"version\":3,\"id\":\""+id+"\",\"cwd\":\"/work\"}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		pct, known, err := Probe(ProbeRequest{
			PiPath: bin, SessionID: id, SessionDir: dir, WorkRoot: dir,
		})
		if err != nil || known || pct != 0 {
			t.Fatalf("percent=%d known=%v err=%v", pct, known, err)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("pi started with two session files")
		}
	})
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func samePath(t *testing.T, a, b string) bool {
	t.Helper()
	aa, err1 := filepath.EvalSymlinks(a)
	bb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	return aa == bb
}
