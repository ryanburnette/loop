package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These tests cover the cmd/loop paths the headline item names: argument
// plumbing and error handling in cmdFreeze, cmdStatus, cmdInit, cmdFrozen,
// writeOneShot, --model parsing, one-shot runs (gate as command vs path), and
// parseInterSpersed edge cases. They drive mainErr in-process and assert on
// exit codes and stderr text.

func TestMainErrNoArgsPrintsUsageExits2(t *testing.T) {
	clearLoopEnv(t)
	var out, errb bytes.Buffer
	code := mainErr(nil, &out, &errb)
	if code != 2 {
		t.Fatalf("code=%d want 2", code)
	}
	if errb.String() == "" {
		t.Fatal("no args should print usage to stderr")
	}
}

func TestMainErrUnknownCommandExits2(t *testing.T) {
	clearLoopEnv(t)
	var out, errb bytes.Buffer
	code := mainErr([]string{"frobnicate"}, &out, &errb)
	if code != 2 {
		t.Fatalf("code=%d want 2", code)
	}
	if !strings.Contains(errb.String(), "unknown command") || !strings.Contains(errb.String(), "frobnicate") {
		t.Fatalf("stderr should name the unknown command: %q", errb.String())
	}
}

func TestMainErrHelpAndVersionFlags(t *testing.T) {
	clearLoopEnv(t)
	var out, errb bytes.Buffer
	if code := mainErr([]string{"-h"}, &out, &errb); code != 0 {
		t.Fatalf("-h code=%d", code)
	}
	if !strings.Contains(out.String(), "loop run") {
		t.Fatalf("-h should print usage: %s", out.String())
	}
	out.Reset()
	errb.Reset()
	if code := mainErr([]string{"--help"}, &out, &errb); code != 0 {
		t.Fatalf("--help code=%d", code)
	}
	out.Reset()
	errb.Reset()
	if code := mainErr([]string{"-V"}, &out, &errb); code != 0 {
		t.Fatalf("-V code=%d", code)
	}
	if strings.TrimSpace(out.String()) != version {
		t.Fatalf("-V output=%q want %q", out.String(), version)
	}
}

func TestModelFlagWithoutRoleIDEqExits2(t *testing.T) {
	clearLoopEnv(t)
	_, dir := newFixtureLoop(t, "myloop")
	fp := fakePI(t)
	var out, errb bytes.Buffer
	code := mainErr([]string{"run", "-C", dir, "--pi", fp, "-q", "--model", "writer"}, &out, &errb)
	if code != 2 {
		t.Fatalf("code=%d want 2", code)
	}
	if !strings.Contains(errb.String(), "role=id") {
		t.Fatalf("stderr should explain --model wants role=id: %q", errb.String())
	}
}

func TestModelFlagWithRoleIDAccepted(t *testing.T) {
	clearLoopEnv(t)
	_, dir := newFixtureLoop(t, "myloop")
	fp := fakePI(t)
	var out, errb bytes.Buffer
	code := mainErr([]string{"run", "-C", dir, "--pi", fp, "-q", "--model", "writer=alpha", "--model", "reviewer=beta"}, &out, &errb)
	if code != 0 {
		t.Fatalf("model role=id should run, code=%d stderr=%s", code, errb.String())
	}
}

// multiFlag.Set is exercised by the previous test through the flag plumbing;
// this directly drives String() so the flag.Value interface is fully covered.
func TestMultiFlagString(t *testing.T) {
	var m multiFlag
	if m.String() != "" {
		t.Fatalf("empty multiFlag String=%q want empty", m.String())
	}
	m = multiFlag{"a=1", "b=2"}
	if m.String() != "a=1,b=2" {
		t.Fatalf("multiFlag String=%q want a=1,b=2", m.String())
	}
}

func TestOneShotBasesBranchOnHeadOnNonMainRepo(t *testing.T) {
	// A one-shot defaults LOOP_BRANCH_BASE to HEAD, so it works on a repo whose
	// trunk is not `main` (here `develop`) without a --base flag. The tree is
	// clean, so the loop/<id> branch is created off the current commit.
	clearLoopEnv(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)
	// Rename the trunk to `develop` so `main` would not resolve.
	if err := exec.Command("git", "-C", root, "branch", "-m", "develop").Run(); err != nil {
		t.Fatalf("git branch -m: %v", err)
	}

	prompt := filepath.Join(t.TempDir(), "p.md")
	if err := os.WriteFile(prompt, []byte("do the thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp := fakePI(t)

	var out, errb bytes.Buffer
	code := mainErr([]string{"run", "--prompt", prompt, "--gate", "exit 0", "--pi", fp, "-q", "-C", root}, &out, &errb)
	if code != 0 {
		t.Fatalf("one-shot on non-main repo: code=%d stderr=%s", code, errb.String())
	}
	// A loop/<id> branch should now exist off develop.
	branches, err := exec.Command("git", "-C", root, "branch", "--list").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(branches), "loop/") {
		t.Fatalf("expected a loop/<id> branch, got:\n%s", branches)
	}
	// And the repo should not have a `main` branch.
	if strings.Contains(string(branches), "main") {
		t.Fatalf("base should be HEAD (develop), not main:\n%s", branches)
	}
}

func TestOneShotRunGateAsCommand(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)

	prompt := filepath.Join(root, "p.md")
	if err := os.WriteFile(prompt, []byte("do the thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp := fakePI(t)

	var out, errb bytes.Buffer
	// --branch=false overrides the LOOP_BRANCH=1 writeOneShot writes, so this
	// runs in place without needing a clean-tree check or a `main` base.
	code := mainErr([]string{"run", "--prompt", prompt, "--gate", "exit 0", "--pi", fp, "-q", "--branch=false", "-C", root}, &out, &errb)
	if code != 0 {
		t.Fatalf("one-shot gate-as-command: code=%d stderr=%s", code, errb.String())
	}
}

func TestOneShotRunGateAsScriptPath(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)

	prompt := filepath.Join(root, "p.md")
	if err := os.WriteFile(prompt, []byte("do the thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gateScript := filepath.Join(root, "check.sh")
	if err := os.WriteFile(gateScript, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	fp := fakePI(t)

	var out, errb bytes.Buffer
	code := mainErr([]string{"run", "--prompt", prompt, "--gate", gateScript, "--pi", fp, "-q", "--branch=false", "-C", root}, &out, &errb)
	if code != 0 {
		t.Fatalf("one-shot gate-as-path: code=%d stderr=%s", code, errb.String())
	}
}

func TestOneShotMissingPromptFileExits2(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)
	fp := fakePI(t)

	var out, errb bytes.Buffer
	code := mainErr([]string{"run", "--prompt", filepath.Join(root, "nope.md"), "--gate", "exit 0", "--pi", fp, "-q", "--branch=false", "-C", root}, &out, &errb)
	if code != 2 {
		t.Fatalf("missing prompt file: code=%d want 2, stderr=%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "loop:") {
		t.Fatalf("stderr should report the read error: %q", errb.String())
	}
}

func TestOneShotNotAGitRepoExits2(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir() // no git init
	prompt := filepath.Join(root, "p.md")
	if err := os.WriteFile(prompt, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp := fakePI(t)
	var out, errb bytes.Buffer
	code := mainErr([]string{"run", "--prompt", prompt, "--gate", "exit 0", "--pi", fp, "-q", "-C", root}, &out, &errb)
	if code != 2 {
		t.Fatalf("one-shot non-git: code=%d want 2, stderr=%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "workroot") {
		t.Fatalf("stderr should mention workroot: %q", errb.String())
	}
}

func TestRunAbsoluteDashCForOneShot(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)
	prompt := filepath.Join(root, "p.md")
	if err := os.WriteFile(prompt, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp := fakePI(t)
	// -C as an absolute path hits the filepath.IsAbs branch in one-shot
	// project-dir resolution.
	var out, errb bytes.Buffer
	code := mainErr([]string{"run", "-C", root, "--prompt", prompt, "--gate", "exit 0", "--pi", fp, "-q", "--branch=false"}, &out, &errb)
	if code != 0 {
		t.Fatalf("one-shot abs -C: code=%d stderr=%s", code, errb.String())
	}
}

func TestStatusNoCurrentRunExits1(t *testing.T) {
	clearLoopEnv(t)
	_, dir := newFixtureLoop(t, "myloop")
	// A fresh fixture has a loop dir but no state/CURRENT_ID yet.
	var out, errb bytes.Buffer
	code := mainErr([]string{"status", "-C", dir}, &out, &errb)
	if code != 1 {
		t.Fatalf("status with no current run: code=%d want 1, stderr=%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "no current run") {
		t.Fatalf("stderr should say no current run: %q", errb.String())
	}
}

func TestStatusExtraArgExits2(t *testing.T) {
	clearLoopEnv(t)
	_, dir := newFixtureLoop(t, "myloop")
	var out, errb bytes.Buffer
	code := mainErr([]string{"status", "-C", dir, "bogus"}, &out, &errb)
	if code != 2 {
		t.Fatalf("status extra arg: code=%d want 2", code)
	}
	if !strings.Contains(errb.String(), "unexpected arguments") {
		t.Fatalf("stderr: %q", errb.String())
	}
}

func TestStatusBadDashCExits2(t *testing.T) {
	clearLoopEnv(t)
	// A -C that cannot be resolved to an existing dir and whose parent has no
	// .loop hits the Missing branch after resolveExistingLoopDir.
	missing := filepath.Join(t.TempDir(), "deep", "nope")
	var out, errb bytes.Buffer
	code := mainErr([]string{"status", "-C", missing}, &out, &errb)
	if code != 2 {
		t.Fatalf("status bad -C: code=%d want 2 (loop dir missing), stderr=%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "loop init") {
		t.Fatalf("stderr should suggest loop init: %q", errb.String())
	}
}

func TestStatusResolvesProjectDotLoop(t *testing.T) {
	// -C naming the project dir should use <dir>/.loop when it exists.
	clearLoopEnv(t)
	root, dir := newFixtureLoop(t, ".loop")
	fp := fakePI(t)
	if code := mainErr([]string{"run", "-C", root, "--pi", fp, "-q"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("run: code=%d", code)
	}
	var out, errb bytes.Buffer
	if code := mainErr([]string{"status", "-C", root}, &out, &errb); code != 0 {
		t.Fatalf("status via project -C: code=%d stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), filepath.Base(dir)) && !strings.Contains(out.String(), "id") {
		t.Fatalf("status via project -C should report the run: %s", out.String())
	}
}

func TestFreezeEmptyPatternPrintsNothing(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a_test.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)
	dir := filepath.Join(root, "myloop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "loop.env"), []byte("LOOP_MAX_ITER=1\nLOOP_FREEZE=\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := mainErr([]string{"freeze", "-C", dir}, &out, &errb)
	if code != 0 {
		t.Fatalf("freeze empty: code=%d stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "nothing to freeze") {
		t.Fatalf("freeze empty should say nothing to freeze: %s", out.String())
	}
}

func TestFreezeExtraArgExits2(t *testing.T) {
	clearLoopEnv(t)
	var out, errb bytes.Buffer
	code := mainErr([]string{"freeze", "bogus"}, &out, &errb)
	if code != 2 {
		t.Fatalf("freeze extra arg: code=%d want 2", code)
	}
}

func TestFreezeLoopDirMissingExits2(t *testing.T) {
	clearLoopEnv(t)
	missing := filepath.Join(t.TempDir(), "nope")
	var out, errb bytes.Buffer
	code := mainErr([]string{"freeze", "-C", missing}, &out, &errb)
	if code != 2 {
		t.Fatalf("freeze missing dir: code=%d want 2", code)
	}
	if !strings.Contains(errb.String(), "loop init") {
		t.Fatalf("stderr should suggest init: %q", errb.String())
	}
}

func TestFrozenNoStateDirExits2(t *testing.T) {
	clearLoopEnv(t)
	var out, errb bytes.Buffer
	code := mainErr([]string{"frozen?"}, &out, &errb)
	if code != 2 {
		t.Fatalf("frozen? with no LOOP_STATE_DIR: code=%d want 2", code)
	}
	if !strings.Contains(errb.String(), "LOOP_STATE_DIR") {
		t.Fatalf("stderr should mention LOOP_STATE_DIR: %q", errb.String())
	}
}

func TestFrozenWorkrootFallbackFromStateDir(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a_test.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)

	dir := filepath.Join(root, "myloop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "loop.env"), []byte("LOOP_FREEZE=*_test.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Freeze first to create the snapshot.
	if code := mainErr([]string{"freeze", "-C", dir}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("freeze failed")
	}

	// frozen? with only LOOP_STATE_DIR set must derive LOOP_WORKROOT via gitTop
	// of the state dir's parent chain (the workroot is the git repo containing
	// the state dir). Here the state dir lives under <root>/myloop/state, so
	// gitTop finds <root>.
	t.Setenv("LOOP_STATE_DIR", filepath.Join(dir, "state", ".freeze-tmp"))
	os.Unsetenv("LOOP_WORKROOT")

	var out, errb bytes.Buffer
	code := mainErr([]string{"frozen?"}, &out, &errb)
	if code != 0 {
		t.Fatalf("frozen? fallback: code=%d want 0, stdout=%s stderr=%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "ok") {
		t.Fatalf("frozen? fallback should print ok: %s", out.String())
	}
}

func TestFrozenWorkrootFallbackNotARepoExits2(t *testing.T) {
	clearLoopEnv(t)
	// LOOP_STATE_DIR set, LOOP_WORKROOT unset, and the state dir is not inside
	// a git repo → gitTop errors → exit 2.
	nonRepo := t.TempDir()
	t.Setenv("LOOP_STATE_DIR", nonRepo)
	os.Unsetenv("LOOP_WORKROOT")
	var out, errb bytes.Buffer
	code := mainErr([]string{"frozen?"}, &out, &errb)
	if code != 2 {
		t.Fatalf("frozen? non-repo fallback: code=%d want 2, stderr=%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "git repo") && !strings.Contains(errb.String(), "workroot") {
		t.Fatalf("stderr should report the git error: %q", errb.String())
	}
}

func TestInitUnknownTemplateExits2(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir()
	gitInit(t, root)
	var out, errb bytes.Buffer
	code := mainErr([]string{"init", "does-not-exist", "-C", filepath.Join(root, ".loop")}, &out, &errb)
	if code != 2 {
		t.Fatalf("init unknown template: code=%d want 2, stderr=%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "unknown template") || !strings.Contains(errb.String(), "available") {
		t.Fatalf("stderr should name available templates: %q", errb.String())
	}
}

func TestInitTooManyArgsExits2(t *testing.T) {
	clearLoopEnv(t)
	var out, errb bytes.Buffer
	code := mainErr([]string{"init", "a", "b"}, &out, &errb)
	if code != 2 {
		t.Fatalf("init too many args: code=%d want 2", code)
	}
	if !strings.Contains(errb.String(), "unexpected arguments") {
		t.Fatalf("stderr: %q", errb.String())
	}
}

func TestInitNotInGitRepoWarnsButSucceeds(t *testing.T) {
	clearLoopEnv(t)
	root := t.TempDir() // no git init
	var out, errb bytes.Buffer
	code := mainErr([]string{"init", "-C", filepath.Join(root, ".loop")}, &out, &errb)
	if code != 0 {
		t.Fatalf("init outside a repo should succeed with a warning, code=%d stderr=%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "not inside a git repo") {
		t.Fatalf("stderr should warn about the missing git repo: %q", errb.String())
	}
	if !strings.Contains(out.String(), "scaffolded") {
		t.Fatalf("stdout should confirm scaffolding: %s", out.String())
	}
}

func TestInitFlagsInterSpersed(t *testing.T) {
	// parseInterSpersed for init: a flag may come after the positional template
	// name. `loop init until-green -C DIR` must parse both.
	clearLoopEnv(t)
	root := t.TempDir()
	gitInit(t, root)
	var out, errb bytes.Buffer
	code := mainErr([]string{"init", "until-green", "-C", filepath.Join(root, ".loop")}, &out, &errb)
	if code != 0 {
		t.Fatalf("init interspersed: code=%d stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "until-green") {
		t.Fatalf("stdout should name the template: %s", out.String())
	}
}

func TestRunUnknownFlagAfterDashC(t *testing.T) {
	clearLoopEnv(t)
	_, dir := newFixtureLoop(t, "myloop")
	var out, errb bytes.Buffer
	code := mainErr([]string{"run", "-C", dir, "--bogus", "x"}, &out, &errb)
	if code != 2 {
		t.Fatalf("unknown flag: code=%d want 2", code)
	}
}

func TestGitTopNotARepo(t *testing.T) {
	// Directly exercise gitTop's error branch.
	nonRepo := t.TempDir()
	_, err := gitTop(nonRepo)
	if err == nil {
		t.Fatal("gitTop on a non-repo should error")
	}
	if !strings.Contains(err.Error(), "git repo") {
		t.Fatalf("gitTop error should mention git repo: %v", err)
	}
}

// TestWriteOneShotShapes drives writeOneShot directly so the two gate shapes
// (command string vs existing script path) and the prompt copy are asserted
// on the exact bytes written, not via a fragile temp-dir glob after a run.
func TestWriteOneShotShapes(t *testing.T) {
	t.Run("gateAsCommand", func(t *testing.T) {
		dir := t.TempDir()
		prompt := filepath.Join(dir, "src-p.md")
		if err := os.WriteFile(prompt, []byte("hello prompt\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		scratch := t.TempDir()
		if err := writeOneShot(scratch, prompt, "go test ./..."); err != nil {
			t.Fatal(err)
		}
		man, err := os.ReadFile(filepath.Join(scratch, "manifest"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(man), "turn writer prompts/oneshot.md") || !strings.Contains(string(man), "gate check gates/check.sh") {
			t.Fatalf("manifest should list the turn and gate:\n%s", man)
		}
		pp, err := os.ReadFile(filepath.Join(scratch, "prompts", "oneshot.md"))
		if err != nil || string(pp) != "hello prompt\n" {
			t.Fatalf("prompt copied verbatim, got %q (err=%v)", pp, err)
		}
		gate, err := os.ReadFile(filepath.Join(scratch, "gates", "check.sh"))
		if err != nil {
			t.Fatal(err)
		}
		// A command string is embedded as the script body, not exec'd.
		if !strings.Contains(string(gate), "go test ./...") {
			t.Fatalf("gate-as-command should embed the command:\n%s", gate)
		}
		if strings.Contains(string(gate), "exec ") {
			t.Fatalf("gate-as-command should not exec:\n%s", gate)
		}
		env, err := os.ReadFile(filepath.Join(scratch, "loop.env"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(env), "LOOP_BRANCH=1") || !strings.Contains(string(env), "LOOP_SESSION=none") || !strings.Contains(string(env), "LOOP_BRANCH_BASE=HEAD") {
			t.Fatalf("one-shot loop.env should default to branch+none+base HEAD:\n%s", env)
		}
	})
	t.Run("gateAsPath", func(t *testing.T) {
		script := filepath.Join(t.TempDir(), "check.sh")
		if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		scratch := t.TempDir()
		if err := writeOneShot(scratch, "", script); err != nil {
			t.Fatal(err)
		}
		man, _ := os.ReadFile(filepath.Join(scratch, "manifest"))
		// No prompt → only the gate line.
		if strings.Contains(string(man), "turn writer") {
			t.Fatalf("no prompt should not add a turn line:\n%s", man)
		}
		if !strings.Contains(string(man), "gate check") {
			t.Fatalf("manifest should list the gate:\n%s", man)
		}
		gate, err := os.ReadFile(filepath.Join(scratch, "gates", "check.sh"))
		if err != nil {
			t.Fatal(err)
		}
		abs, _ := filepath.Abs(script)
		if !strings.Contains(string(gate), "exec ") || !strings.Contains(string(gate), abs) {
			t.Fatalf("gate-as-path should exec the absolute script path:\n%s", gate)
		}
	})
	t.Run("promptOnlyNoGate", func(t *testing.T) {
		prompt := filepath.Join(t.TempDir(), "p.md")
		if err := os.WriteFile(prompt, []byte("just a prompt\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		scratch := t.TempDir()
		if err := writeOneShot(scratch, prompt, ""); err != nil {
			t.Fatal(err)
		}
		man, _ := os.ReadFile(filepath.Join(scratch, "manifest"))
		if !strings.Contains(string(man), "turn writer") || strings.Contains(string(man), "gate check") {
			t.Fatalf("prompt-only manifest should have only the turn:\n%s", man)
		}
	})
	t.Run("missingPromptErrors", func(t *testing.T) {
		scratch := t.TempDir()
		err := writeOneShot(scratch, filepath.Join(t.TempDir(), "nope.md"), "")
		if err == nil {
			t.Fatal("missing prompt should error")
		}
	})
}

// keep runtime import referenced on hosts where the test runner prunes it.
var _ = runtime.GOOS
