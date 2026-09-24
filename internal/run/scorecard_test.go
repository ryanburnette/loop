package run

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanburnette/loop/internal/scorecard"
)

const reviewCard = "rule all\n\nitem auth\nThe check holds.\n"

func gateLog(t *testing.T, loopDir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(loopDir, "state", "*", "gate-log.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("gate-log matches: %v", matches)
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRecipeHashIgnoresState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gate.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "state", "cards"), 0o755); err != nil {
		t.Fatal(err)
	}
	before, err := hashRecipe(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state", "cards", "1-reviewer.json"), []byte(`{"items":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := hashRecipe(dir)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("state/ must not affect the recipe hash")
	}
	if err := os.WriteFile(filepath.Join(dir, "gate.sh"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	changed, err := hashRecipe(dir)
	if err != nil {
		t.Fatal(err)
	}
	if changed == before {
		t.Fatal("a changed gate script must change the hash")
	}
}

func TestSoftVersusRequiredScorecard(t *testing.T) {
	t.Run("soft unmet continues", func(t *testing.T) {
		_, loopDir := scratchLoop(t,
			"turn reviewer prompts/r.md required=0 scorecard=scorecards/review.card\n",
			map[string]string{
				"prompts/r.md":           "review\n",
				"scorecards/review.card": reviewCard,
			})
		t.Setenv("FAKE_PI_CARD", "unmet")
		code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
		if err != nil {
			t.Fatal(err)
		}
		if code != 0 {
			t.Fatalf("exit %d want 0: a soft miss is not an objective", code)
		}
		log := gateLog(t, loopDir)
		if !strings.Contains(log, "SCORECARD reviewer: FAIL") || !strings.Contains(log, "rule all, 0/1 required met") {
			t.Fatalf("log:\n%s", log)
		}
	})

	t.Run("required unmet fails", func(t *testing.T) {
		_, loopDir := scratchLoop(t,
			"turn reviewer prompts/r.md scorecard=scorecards/review.card\n",
			map[string]string{
				"prompts/r.md":           "review\n",
				"scorecards/review.card": reviewCard,
			})
		t.Setenv("FAKE_PI_CARD", "unmet")
		code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
		if err != nil {
			t.Fatal(err)
		}
		if code != 1 {
			t.Fatalf("exit %d want 1", code)
		}
		log := gateLog(t, loopDir)
		if !strings.Contains(log, "SCORECARD reviewer: FAIL") || !strings.Contains(log, "rule all, 0/1 required met") {
			t.Fatalf("log:\n%s", log)
		}
	})

	t.Run("soft unreadable continues", func(t *testing.T) {
		root, loopDir := scratchLoop(t,
			"turn reviewer prompts/r.md required=0 scorecard=scorecards/review.card\n"+
				"gate marker gates/marker.sh\n",
			map[string]string{
				"prompts/r.md":           "review\n",
				"scorecards/review.card": reviewCard,
				"gates/marker.sh":        "#!/bin/sh\ntouch \"$LOOP_WORKROOT/MARKER\"\nexit 0\n",
			})
		t.Setenv("FAKE_PI_CARD", "garbage")
		code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
		if err != nil {
			t.Fatal(err)
		}
		if code != 0 {
			t.Fatalf("exit %d want 0: unreadable soft card must not fail the iteration", code)
		}
		if _, err := os.Stat(filepath.Join(root, "MARKER")); err != nil {
			t.Fatal("later gate did not run")
		}
		log := gateLog(t, loopDir)
		if !strings.Contains(log, "SCORECARD reviewer: UNREADABLE") {
			t.Fatalf("log:\n%s", log)
		}
	})

	t.Run("required unreadable fails", func(t *testing.T) {
		root, loopDir := scratchLoop(t,
			"turn reviewer prompts/r.md scorecard=scorecards/review.card\n"+
				"gate marker gates/marker.sh\n",
			map[string]string{
				"prompts/r.md":           "review\n",
				"scorecards/review.card": reviewCard,
				"gates/marker.sh":        "#!/bin/sh\ntouch \"$LOOP_WORKROOT/MARKER\"\nexit 0\n",
			})
		t.Setenv("FAKE_PI_CARD", "missing")
		code, err := Run(Options{Dir: loopDir, Pi: fakePi(t), Quiet: true})
		if err != nil {
			t.Fatal(err)
		}
		if code != 1 {
			t.Fatalf("exit %d want 1", code)
		}
		if _, err := os.Stat(filepath.Join(root, "MARKER")); err != nil {
			t.Fatal("unreadable required card should fail the iteration without aborting later steps")
		}
		log := gateLog(t, loopDir)
		if !strings.Contains(log, "SCORECARD reviewer: UNREADABLE") {
			t.Fatalf("log:\n%s", log)
		}
	})
}

func TestChangedGateFailsIteration(t *testing.T) {
	root, loopDir := scratchLoop(t,
		"turn reviewer prompts/r.md required=0 scorecard=scorecards/review.card\n"+
			"gate marker gates/marker.sh\n",
		map[string]string{
			"prompts/r.md":           "review\n",
			"scorecards/review.card": reviewCard,
			"gates/marker.sh":        "#!/bin/sh\ntouch \"$LOOP_WORKROOT/MARKER\"\nexit 0\n",
		})
	t.Setenv("FAKE_PI_CARD", "met")
	wrapper := writeExec(t, t.TempDir(), "pi", fmt.Sprintf("#!/bin/sh\nset -e\nprintf '\\n# tampered\\n' >> .loop/gates/marker.sh\nexec '%s' \"$@\"\n", fakePi(t)))
	code, err := Run(Options{Dir: loopDir, Pi: wrapper, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("a gate script edited during the judge must fail the iteration")
	}
	if _, err := os.Stat(filepath.Join(root, "MARKER")); err == nil {
		t.Fatal("later gate ran after the recipe changed")
	}
	log := gateLog(t, loopDir)
	if !strings.Contains(log, "SCORECARD reviewer: UNREADABLE") || !strings.Contains(log, "recipe changed during judge") {
		t.Fatalf("log:\n%s", log)
	}
}

func TestAskFileCountsCodePoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ask.md")
	err := writeAsk(path, scorecard.Card{
		Rule: "all",
		Items: []scorecard.Item{{
			ID: "auth", Required: true, Text: "The check holds.",
		}},
	}, "/tmp/out.json")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "1 to 200 Unicode code points") {
		t.Fatalf("ask file:\n%s", b)
	}
}

func TestPiCrashOnSoftScorecardAborts(t *testing.T) {
	root, loopDir := scratchLoop(t,
		"turn reviewer prompts/r.md required=0 scorecard=scorecards/review.card\n"+
			"gate marker gates/marker.sh\n",
		map[string]string{
			"prompts/r.md":           "review\n",
			"scorecards/review.card": reviewCard,
			"gates/marker.sh":        "#!/bin/sh\ntouch \"$LOOP_WORKROOT/MARKER\"\nexit 0\n",
		})
	pi := writeExec(t, t.TempDir(), "failing-pi", failingPi)
	code, err := Run(Options{Dir: loopDir, Pi: pi, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatal("a pi crash must not be scored a success")
	}
	if _, err := os.Stat(filepath.Join(root, "MARKER")); err == nil {
		t.Fatal("later gate ran after pi crashed")
	}
	log := gateLog(t, loopDir)
	if !strings.Contains(log, "TURN reviewer: ERROR") {
		t.Fatalf("log:\n%s", log)
	}
	if strings.Contains(log, "UNREADABLE") {
		t.Fatalf("a crash is not an unreadable card:\n%s", log)
	}
}

func TestJudgingArgvAndGateEnv(t *testing.T) {
	root, loopDir := scratchLoop(t,
		"turn reviewer prompts/r.md scorecard=scorecards/review.card system=be brief\n"+
			"gate env gates/env.sh\n",
		map[string]string{
			"prompts/r.md":           "review\n",
			"scorecards/review.card": reviewCard,
			"gates/env.sh":           "#!/bin/sh\nenv > \"$LOOP_WORKROOT/gate-env\"\nexit 0\n",
		})
	t.Setenv("FAKE_PI_CARD", "met")
	t.Setenv("LOOP_LEAK", "secret")
	t.Setenv("KEEP_TOKEN", "yes")
	wrapper := writeExec(t, t.TempDir(), "pi", fmt.Sprintf(
		"#!/bin/sh\nprintf '%%s\\n' \"$@\" > \"$PWD/pi-argv\"\nenv > \"$PWD/pi-env\"\nexec '%s' \"$@\"\n",
		fakePi(t)))
	code, err := Run(Options{Dir: loopDir, Pi: wrapper, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit %d want 0", code)
	}
	argvB, err := os.ReadFile(filepath.Join(root, "pi-argv"))
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimRight(string(argvB), "\n"), "\n")
	dash := -1
	for i, a := range args {
		if a == "--" {
			dash = i
			break
		}
	}
	if dash < 0 {
		t.Fatalf("argv missing --:\n%s", argvB)
	}
	toolsAt := -1
	for i, a := range args {
		if a == "--no-extensions" && i > dash {
			t.Fatalf("--no-extensions after --:\n%s", argvB)
		}
		if a == "--tools" {
			toolsAt = i
		}
		if a == "bash" || a == "edit" {
			t.Fatalf("judging argv passed %s:\n%s", a, argvB)
		}
	}
	if toolsAt < 0 || toolsAt+1 > dash || args[toolsAt+1] != "read,grep,find,ls,write" {
		t.Fatalf("tools:\n%s", argvB)
	}
	var prompts []string
	for i := 0; i < dash; i++ {
		if args[i] == "--append-system-prompt" && i+1 < len(args) {
			prompts = append(prompts, args[i+1])
		}
	}
	if len(prompts) != 2 || prompts[0] != "be brief" || !strings.Contains(prompts[1], "Facts outrank settled claims.") {
		t.Fatalf("prompts %q\n%s", prompts, argvB)
	}
	if !strings.Contains(prompts[1], "Write only the scorecard JSON at ") {
		t.Fatalf("runner line %q", prompts[1])
	}
	ask := false
	for i, a := range args {
		if i > dash && strings.HasPrefix(a, "@") && strings.HasSuffix(a, ".ask.md") {
			ask = true
		}
	}
	if !ask {
		t.Fatalf("ask file not attached:\n%s", argvB)
	}

	envB, err := os.ReadFile(filepath.Join(root, "pi-env"))
	if err != nil {
		t.Fatal(err)
	}
	piEnv := string(envB)
	if !strings.Contains(piEnv, "KEEP_TOKEN=yes") {
		t.Fatalf("non-LOOP env dropped:\n%s", piEnv)
	}
	if strings.Contains(piEnv, "LOOP_LEAK=") {
		t.Fatalf("LOOP_LEAK reached pi:\n%s", piEnv)
	}
	allowed := map[string]bool{}
	for _, line := range strings.Split(piEnv, "\n") {
		key, val, ok := strings.Cut(line, "=")
		if !ok || !strings.HasPrefix(key, "LOOP_") {
			continue
		}
		allowed[key] = true
		if key == "LOOP_SCORECARD_OUT" && !filepath.IsAbs(val) {
			t.Fatalf("LOOP_SCORECARD_OUT %q", val)
		}
	}
	for _, key := range []string{"LOOP_SCORECARD_OUT", "LOOP_PROPOSAL_OUT", "LOOP_MEND", "LOOP_BRIEF", "LOOP_RETURN"} {
		if !allowed[key] {
			t.Fatalf("pi env missing %s\n%s", key, piEnv)
		}
	}
	if len(allowed) != 5 {
		t.Fatalf("pi LOOP_* keys %v", allowed)
	}

	gateB, err := os.ReadFile(filepath.Join(root, "gate-env"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gateB), "LOOP_SCORECARD_OUT=") {
		t.Fatalf("gate saw LOOP_SCORECARD_OUT:\n%s", gateB)
	}
}

func TestActingTurnOmitsJudgingFlags(t *testing.T) {
	root, loopDir := scratchLoop(t, "turn writer prompts/w.md\n", map[string]string{
		"prompts/w.md": "go\n",
	})
	t.Setenv("LOOP_LEAK", "secret")
	wrapper := writeExec(t, t.TempDir(), "pi", "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$PWD/pi-argv\"\nenv > \"$PWD/pi-env\"\nexit 0\n")
	code, err := Run(Options{Dir: loopDir, Pi: wrapper, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	argv, err := os.ReadFile(filepath.Join(root, "pi-argv"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(argv)
	if strings.Contains(text, "--no-extensions") || strings.Contains(text, "--tools") {
		t.Fatalf("acting turn passed judging flags:\n%s", text)
	}
	if !strings.Contains(text, "--append-system-prompt\n") || !strings.Contains(text, "Facts outrank settled claims.") {
		t.Fatalf("acting turn missing runner line:\n%s", text)
	}
	if strings.Contains(text, "Write only the scorecard JSON") {
		t.Fatalf("acting turn must not mention the scorecard file:\n%s", text)
	}
	envB, err := os.ReadFile(filepath.Join(root, "pi-env"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(envB), "LOOP_SCORECARD_OUT=") || strings.Contains(string(envB), "LOOP_LEAK=") {
		t.Fatalf("acting pi env:\n%s", envB)
	}
	for _, key := range []string{"LOOP_PROPOSAL_OUT=", "LOOP_MEND=", "LOOP_BRIEF=", "LOOP_RETURN="} {
		if !strings.Contains(string(envB), key) {
			t.Fatalf("missing %s\n%s", key, envB)
		}
	}
}
