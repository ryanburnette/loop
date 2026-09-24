package run

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanburnette/loop/internal/mend"
)

func stateText(t *testing.T, loopDir, name string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(loopDir, "state", "*", name))
	if err != nil || len(matches) != 1 {
		t.Fatalf("%s matches: %v %v", name, matches, err)
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func argvInvocations(t *testing.T, log string) [][]string {
	t.Helper()
	var out [][]string
	var cur []string
	for _, line := range strings.Split(log, "\n") {
		if line == "----" {
			if len(cur) > 0 {
				out = append(out, cur)
			}
			cur = nil
			continue
		}
		if line == "" && len(cur) == 0 {
			continue
		}
		cur = append(cur, line)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// turnInvocations drops the stats probe. It is not a turn.
func turnInvocations(t *testing.T, inv [][]string) [][]string {
	t.Helper()
	var out [][]string
	for _, args := range inv {
		probe := false
		for i, a := range args {
			if a == "--mode" && i+1 < len(args) && args[i+1] == "rpc" {
				probe = true
				break
			}
		}
		if !probe {
			out = append(out, args)
		}
	}
	return out
}

func hasArgSuffix(args []string, suffix string) bool {
	for _, a := range args {
		if strings.HasSuffix(a, suffix) {
			return true
		}
	}
	return false
}

func sessionIDs(t *testing.T, args []string) string {
	t.Helper()
	for i, a := range args {
		if a == "--session-id" && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("no --session-id in %q", args)
	return ""
}

func TestMendFactsUseGateExit(t *testing.T) {
	_, loopDir := scratchLoop(t,
		"turn writer prompts/w.md\ngate tests gates/tests.sh\n",
		map[string]string{
			"prompts/w.md": "go\n",
			"gates/tests.sh": "#!/bin/sh\n" +
				"echo real failure\n" +
				"exit 3\n",
		})
	wrapper := writeExec(t, t.TempDir(), "pi", fmt.Sprintf(`#!/bin/sh
mkdir -p "$(dirname "$LOOP_PROPOSAL_OUT")"
printf '%%s\n' '{"proposals":[{"tried":"the gate","failed":"exit 0","do_not_retry":false}]}' > "$LOOP_PROPOSAL_OUT"
exec '%s' "$@"
`, fakePi(t)))
	code, err := Run(Options{Dir: loopDir, Pi: wrapper, Quiet: true, MaxIter: 1})
	if err != nil {
		t.Fatal(err)
	}
	if code != 1 {
		t.Fatalf("exit %d want 1", code)
	}
	s := stateText(t, loopDir, "mend.md")
	facts := s
	if i := strings.Index(s, "## Settled"); i >= 0 {
		facts = s[:i]
	}
	if !strings.Contains(facts, "gate tests: FAIL exit 3 (required)") {
		t.Fatalf("facts missing the script exit:\n%s", facts)
	}
	if strings.Contains(facts, "exit 0") {
		t.Fatalf("facts took an exit code from the proposal:\n%s", facts)
	}
	if !strings.Contains(s, "claimed by writer at iter 1") || !strings.Contains(s, "failed: exit 0") {
		t.Fatalf("proposal should stay a claim:\n%s", s)
	}
}

func TestDiffStatUsesStartNotOlderCommit(t *testing.T) {
	root, loopDir := scratchLoop(t, "gate commit gates/commit.sh\n", map[string]string{
		"loop.env": "LOOP_MAX_ITER=1\nLOOP_SESSION=none\nLOOP_BRANCH=0\nLOOP_BRANCH_BASE=HEAD\n",
		"gates/commit.sh": "#!/bin/sh\n" +
			"set -eu\n" +
			"printf 'during\\n' > \"$LOOP_WORKROOT/during.txt\"\n" +
			"git -C \"$LOOP_WORKROOT\" add -- during.txt\n" +
			"git -C \"$LOOP_WORKROOT\" commit -qm 'during the run'\n" +
			"exit 0\n",
	})
	rootSHA := gitOut(t, root, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(root, "old.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, root, "add", "--", "old.txt")
	gitOut(t, root, "commit", "-qm", "older commit")
	startSHA := gitOut(t, root, "rev-parse", "HEAD")
	envPath := filepath.Join(loopDir, "loop.env")
	if err := os.WriteFile(envPath, []byte(
		"LOOP_MAX_ITER=1\nLOOP_SESSION=none\nLOOP_BRANCH=0\nLOOP_BRANCH_BASE="+rootSHA+"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit %d want 0", code)
	}
	meta := stateText(t, loopDir, "meta.env")
	if !strings.Contains(meta, "START="+startSHA) {
		t.Fatalf("START is not HEAD at run start:\n%s", meta)
	}
	if !strings.Contains(meta, "BASE="+rootSHA) {
		t.Fatalf("BASE should stay the older branch base:\n%s", meta)
	}
	if strings.Contains(meta, "START="+rootSHA) {
		t.Fatal("START was rewritten to BASE")
	}
	s := stateText(t, loopDir, "mend.md")
	committed := between(s, "#### Committed", "#### Staged")
	unstaged := between(s, "#### Unstaged", "## Settled")
	if strings.Contains(committed, "base unknown") {
		t.Fatalf("START was recorded; committed should not be unknown:\n%s", committed)
	}
	if !strings.Contains(committed, "during.txt") {
		t.Fatalf("commit during the run missing from committed diff:\n%s", committed)
	}
	if strings.Contains(s, "old.txt") {
		t.Fatalf("older commit leaked into the mend:\n%s", s)
	}
	if strings.Contains(unstaged, "during.txt") {
		t.Fatalf("committed file still showing as unstaged:\n%s", unstaged)
	}
	if gitOut(t, root, "diff", "--stat", "--cached") != "" {
		t.Fatal("index not clean after the during-run commit")
	}
	// loop.env was edited after the START commit, so it is unstaged, not committed.
	if !strings.Contains(unstaged, "loop.env") {
		t.Fatalf("unstaged loop.env missing:\n%s", unstaged)
	}
}

func TestStartEqualsBaseWhenBranchCreated(t *testing.T) {
	root, loopDir := scratchLoop(t, "gate g gates/g.sh\n", map[string]string{
		"loop.env":   "LOOP_MAX_ITER=1\nLOOP_SESSION=none\nLOOP_BRANCH=1\nLOOP_BRANCH_BASE=HEAD\n",
		"gates/g.sh": "#!/bin/sh\nexit 0\n",
	})
	before := gitOut(t, root, "rev-parse", "HEAD")
	code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit %d want 0", code)
	}
	meta := stateText(t, loopDir, "meta.env")
	start := metaField(meta, "START")
	base := metaField(meta, "BASE")
	if start == "" || start != base || start != before {
		t.Fatalf("START %q BASE %q before %q\n%s", start, base, before, meta)
	}
	idb, err := os.ReadFile(filepath.Join(loopDir, "state", "CURRENT_ID"))
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(string(idb))
	code, err = Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true, MaxIter: 2, ResumeID: id})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("resume exit %d", code)
	}
	if got := metaField(stateText(t, loopDir, "meta.env"), "START"); got != start {
		t.Fatalf("resume rewrote START from %s to %s", start, got)
	}
}

func TestResumeRequiresMendAndLedger(t *testing.T) {
	_, loopDir := scratchLoop(t, "gate g gates/g.sh\n", map[string]string{
		"gates/g.sh": "#!/bin/sh\nexit 0\n",
	})
	if _, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true}); err != nil {
		t.Fatal(err)
	}
	idb, err := os.ReadFile(filepath.Join(loopDir, "state", "CURRENT_ID"))
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(string(idb))
	dir := filepath.Join(loopDir, "state", id)
	mendPath := filepath.Join(dir, "mend.md")
	saved, err := os.ReadFile(mendPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(mendPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "handoff.md"), []byte("stale root goal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true, ResumeID: id})
	if err == nil || !strings.Contains(err.Error(), "state is missing mend.md; start a new run") {
		t.Fatalf("missing mend: %v", err)
	}
	if err := os.WriteFile(mendPath, saved, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "ledger.json")); err != nil {
		t.Fatal(err)
	}
	_, err = Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true, ResumeID: id})
	if err == nil || !strings.Contains(err.Error(), "state is missing ledger.json; start a new run") {
		t.Fatalf("missing ledger: %v", err)
	}
}

func TestMendAttachedNotHandoff(t *testing.T) {
	_, loopDir := scratchLoop(t,
		"turn writer prompts/w.md\nturn fixer prompts/f.md\n",
		map[string]string{
			"loop.env":     "LOOP_MAX_ITER=2\nLOOP_SESSION=none\nLOOP_BRANCH=0\n",
			"prompts/w.md": "go\n",
			"prompts/f.md": "go\n",
		})
	logPath := filepath.Join(t.TempDir(), "argv.log")
	wrapper := writeExec(t, t.TempDir(), "pi", fmt.Sprintf(
		"#!/bin/sh\nprintf '%%s\\n' '----' >> '%s'\nprintf '%%s\\n' \"$@\" >> '%s'\nexec '%s' \"$@\"\n",
		logPath, logPath, fakePi(t),
	))
	code, err := Run(Options{Dir: loopDir, Pi: wrapper, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	logb, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	inv := argvInvocations(t, string(logb))
	if len(inv) != 4 {
		t.Fatalf("pi ran %d times, want 4\n%s", len(inv), logb)
	}
	for i, args := range inv {
		if hasArgSuffix(args, "handoff.md") || hasArgSuffix(args, "gate-log.md") {
			t.Fatalf("turn %d attached handoff or gate log: %q", i, args)
		}
		for _, a := range args {
			if strings.Contains(a, "turn-") && strings.HasPrefix(a, "@") {
				t.Fatalf("turn %d attached a turn file: %s", i, a)
			}
		}
	}
	if hasArgSuffix(inv[0], "mend.md") || hasArgSuffix(inv[0], "brief.md") {
		t.Fatalf("first turn attached context: %q", inv[0])
	}
	if !hasArgSuffix(inv[1], "brief.md") || hasArgSuffix(inv[1], "mend.md") {
		t.Fatalf("later turn of iteration 1: %q", inv[1])
	}
	if !hasArgSuffix(inv[2], "mend.md") || hasArgSuffix(inv[2], "brief.md") {
		t.Fatalf("first turn of iteration 2: %q", inv[2])
	}
	if !hasArgSuffix(inv[3], "mend.md") || !hasArgSuffix(inv[3], "brief.md") {
		t.Fatalf("later turn of iteration 2: %q", inv[3])
	}
	if stub := stateText(t, loopDir, "handoff.md"); stub != mend.HandoffStub {
		t.Fatalf("stub %q", stub)
	}
}

func TestIterationBoundaryAbandonsSession(t *testing.T) {
	for _, mode := range []string{"shared", "fork"} {
		t.Run(mode, func(t *testing.T) {
			_, loopDir := scratchLoop(t, "turn writer prompts/w.md\n", map[string]string{
				"loop.env":     "LOOP_MAX_ITER=2\nLOOP_SESSION=" + mode + "\nLOOP_SESSION_TURNS=8\nLOOP_FORK_PERCENT=40\nLOOP_BRANCH=0\n",
				"prompts/w.md": "go\n",
			})
			logPath := filepath.Join(t.TempDir(), "argv.log")
			wrapper := writeExec(t, t.TempDir(), "pi", fmt.Sprintf(
				"#!/bin/sh\nprintf '%%s\\n' '----' >> '%s'\nprintf '%%s\\n' \"$@\" >> '%s'\nexec '%s' \"$@\"\n",
				logPath, logPath, fakePi(t),
			))
			if _, err := Run(Options{Dir: loopDir, Pi: wrapper, Quiet: true, Session: mode, MaxIter: 2}); err != nil {
				t.Fatal(err)
			}
			logb, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(logb), "--fork") {
				t.Fatalf("%s passed --fork:\n%s", mode, logb)
			}
			inv := turnInvocations(t, argvInvocations(t, string(logb)))
			if len(inv) != 2 {
				t.Fatalf("pi ran %d turns:\n%s", len(inv), logb)
			}
			a, b := sessionIDs(t, inv[0]), sessionIDs(t, inv[1])
			if a == b {
				t.Fatalf("%s reused session %s across iterations", mode, a)
			}
		})
	}
}

func TestForkCutDoesNotPassForkFlag(t *testing.T) {
	_, loopDir := scratchLoop(t, "turn writer prompts/w.md\nturn fixer prompts/f.md\n", map[string]string{
		"loop.env":     "LOOP_MAX_ITER=1\nLOOP_SESSION=fork\nLOOP_SESSION_TURNS=8\nLOOP_FORK_PERCENT=40\nLOOP_BRANCH=0\n",
		"prompts/w.md": "go\n",
		"prompts/f.md": "go\n",
	})
	logPath := filepath.Join(t.TempDir(), "argv.log")
	t.Setenv("FAKE_PI_PERCENT", "90")
	pi := writeExec(t, t.TempDir(), "pi", fmt.Sprintf(
		"#!/bin/sh\nprintf '%%s\\n' '----' >> '%s'\nprintf '%%s\\n' \"$@\" >> '%s'\nexec '%s' \"$@\"\n",
		logPath, logPath, fakePi(t),
	))
	if _, err := Run(Options{Dir: loopDir, Pi: pi, Quiet: true, Session: "fork"}); err != nil {
		t.Fatal(err)
	}
	logb, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logb), "--fork") {
		t.Fatalf("percent cut passed --fork:\n%s", logb)
	}
	inv := turnInvocations(t, argvInvocations(t, string(logb)))
	if len(inv) != 2 {
		t.Fatalf("pi ran %d turns:\n%s", len(inv), logb)
	}
	if sessionIDs(t, inv[0]) == sessionIDs(t, inv[1]) {
		t.Fatal("percent cut continued the same session")
	}
}

func TestJudgingMarksInNextMend(t *testing.T) {
	_, loopDir := scratchLoop(t,
		"turn writer prompts/w.md\nturn reviewer prompts/r.md required=0 scorecard=scorecards/review.card\n",
		map[string]string{
			"loop.env":               "LOOP_MAX_ITER=2\nLOOP_SESSION=none\nLOOP_BRANCH=0\n",
			"prompts/w.md":           "go\n",
			"prompts/r.md":           "review\n",
			"scorecards/review.card": "rule all\n\nitem auth\nThe check holds.\n\nitem tests\nTests stay.\n",
		})
	seen := filepath.Join(t.TempDir(), "seen-mend.md")
	wrapper := writeExec(t, t.TempDir(), "pi", fmt.Sprintf(`#!/bin/sh
for a in "$@"; do
  case $a in
  @*/mend.md)
    if test ! -f '%s'; then
      cp "${a#@}" '%s'
    fi
    ;;
  esac
done
exec '%s' "$@"
`, seen, seen, fakePi(t)))
	t.Setenv("FAKE_PI_CARD", "unmet")
	code, err := Run(Options{Dir: loopDir, Pi: wrapper, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	b, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"scorecard reviewer: FAIL rule all, 0/2 required met (soft)",
		"auth unmet — recorded by fake-pi",
		"tests unmet — recorded by fake-pi",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("next iteration mend missing %q\n%s", want, s)
		}
	}
}

func TestGateEnvMendKeysOnce(t *testing.T) {
	root, loopDir := scratchLoop(t, "gate env gates/env.sh\n", map[string]string{
		"loop.env": "LOOP_MAX_ITER=1\nLOOP_SESSION=none\nLOOP_BRANCH=0\n" +
			"LOOP_MEND=/tmp/stale-mend\nLOOP_BRIEF=/tmp/stale-brief\nLOOP_RETURN=/tmp/stale-return\n",
		"gates/env.sh": "#!/bin/sh\nenv > \"$LOOP_WORKROOT/gate-env\"\nexit 0\n",
	})
	t.Setenv("LOOP_MEND", "/tmp/from-process")
	code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	b, err := os.ReadFile(filepath.Join(root, "gate-env"))
	if err != nil {
		t.Fatal(err)
	}
	env := string(b)
	id := strings.TrimSpace(string(mustRead(t, filepath.Join(loopDir, "state", "CURRENT_ID"))))
	state := filepath.Join(loopDir, "state", id)
	for _, key := range []string{"LOOP_MEND", "LOOP_BRIEF", "LOOP_RETURN"} {
		n := 0
		var val string
		for _, line := range strings.Split(env, "\n") {
			k, v, ok := strings.Cut(line, "=")
			if ok && k == key {
				n++
				val = v
			}
		}
		if n != 1 {
			t.Fatalf("%s seen %d times\n%s", key, n, env)
		}
		if strings.Contains(val, "stale") || strings.Contains(val, "from-process") {
			t.Fatalf("%s kept a recipe or process value %q", key, val)
		}
		if !strings.HasPrefix(val, state+string(os.PathSeparator)) {
			t.Fatalf("%s %q is not under %s", key, val, state)
		}
	}
	if strings.Contains(env, "LOOP_SCORECARD_OUT=") || strings.Contains(env, "LOOP_PROPOSAL_OUT=") {
		t.Fatalf("gate saw a pi-only key:\n%s", env)
	}
}

func TestMissingStartRendersUnknown(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)
	d := collectDiff(root, "")
	if !d.CommittedUnknown {
		t.Fatal("empty START should be unknown")
	}
	path := filepath.Join(t.TempDir(), "mend.md")
	if err := mend.WriteMend(path, mend.Facts{Diff: d, Iter: 1, MaxIter: 1, Context: "n/a"}, mend.Ledger{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "committed: base unknown") {
		t.Fatalf("%s", b)
	}
}

func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	rest := s[i+len(start):]
	if j := strings.Index(rest, end); j >= 0 {
		return rest[:j]
	}
	return rest
}

func metaField(meta, key string) string {
	for _, line := range strings.Split(meta, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && k == key {
			return v
		}
	}
	return ""
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
