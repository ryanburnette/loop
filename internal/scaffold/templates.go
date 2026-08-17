package scaffold

// until-green — the workhorse. Writer turn + test gate. Convention-derived
// (no manifest): prompts/01-writer.md and gates/tests.sh.
var untilGreen = Template{
	Name: "until-green",
	Files: map[string]string{
		"loop.env": `# until-green — the workhorse pattern.
# The check is your test suite: an exit code the model cannot argue with.
# Writer turn, then the test gate. Iterates until green or the cap fires.
# Bounded: stops and exits 1 if it cannot go green in LOOP_MAX_ITER turns.
#
# This recipe is convention-derived: there is no manifest file. The runner
# derives one turn step from prompts/01-writer.md and one gate step from
# gates/tests.sh, in that order. Add more numbered prompts or gates to extend.

LOOP_MAX_ITER=5
LOOP_SESSION=none
LOOP_BRANCH=1
LOOP_BRANCH_BASE=main

# The test command. Change this to match your stack:
#   go test ./...   |   npm test   |   pytest -q   |   cargo test
LOOP_TEST_CMD=go test ./...

# Pin the writer model (empty = pi default):
# LOOP_WRITER_MODEL=synthetic/hf:zai-org/GLM-5.2

# Anti-cheat (optional, strict): uncomment to fail the loop if any test file
# changes. Hashes are recorded at run start; the loop:frozen gate detects drift.
# LOOP_FREEZE=*_test.go
# until-green is convention-derived (no manifest), so enforcing the frozen gate
# means writing a manifest that carries the derived steps then this line (after
# the tests gate):
#   turn writer prompts/01-writer.md model=writer
#   gate tests gates/tests.sh
#   gate frozen loop:frozen
`,
		"prompts/01-writer.md": `# Writer

Do the work described in TASK.md (create it first if it does not exist, stating
the goal in one sentence). Make the change. Run the tests.

Hard rule: do NOT modify the tests to make them pass. Fix the code, not the
tests. If a test is genuinely wrong, say why in your summary and stop — do not
silently weaken it.

Summarize what you changed in a few bullets at the end.
`,
		"gates/tests.sh": `#!/bin/sh
# tests gate — exit 0 only if the test suite passes.
# LOOP_TEST_CMD is set in loop.env (default: go test ./...).
set -eu
echo "running: ${LOOP_TEST_CMD:-go test ./...}"
# shellcheck disable=SC2086
eval "${LOOP_TEST_CMD:-go test ./...}"
`,
	},
}

// double-check — writer turn + critic turn, soft verdict, no hard gate.
// Uses a manifest so the critic carries a soft (required=0) verdict.
var doubleCheck = Template{
	Name: "double-check",
	Files: map[string]string{
		"loop.env": `# double-check — the weakest gate.
# Two turns: the writer does the work, then a hostile critic reviews it on a
# fresh turn. The critic's VERDICT is soft (required=0): a FAIL does not stop
# the loop, only the iteration cap does. There is no hard gate, so the loop
# exits 0 after one pass. Treat its "looks good" with suspicion; upgrade to
# until-green when you can define an objective check.

LOOP_MAX_ITER=1
LOOP_SESSION=none
LOOP_BRANCH=0

# Optional: pin models. Empty = pi default.
# LOOP_WRITER_MODEL=xai/grok-4.5
# LOOP_CRITIC_MODEL=anthropic/claude-opus-5
`,
		"manifest": `# double-check: work, then a hostile critic with a soft verdict.
# type   name     path                     key=value
turn     writer   prompts/01-writer.md     model=writer
turn     critic   prompts/02-critic.md     model=critic required=0 verdict=^VERDICT: PASS
`,
		"prompts/01-writer.md": `# Writer

Do the work described in TASK.md (create it first if it does not exist, stating
the goal in one sentence). Make the change. Run the project's tests or build if
there is one. Summarize what you changed in a few bullets at the end.
`,
		"prompts/02-critic.md": `# Hostile critic

Switch hats. You are seeing the writer's diff for the first time and you
distrust it. You did not write this code. List every bug, edge case, shortcut,
and place the writer took the easy path. Be specific and harsh. Then fix the
things you agree are real, and re-run the tests. Do not just say "looks good."

End your review with exactly one line:

    VERDICT: PASS

or

    VERDICT: FAIL

Prefer FAIL when unsure.
`,
	},
}

// two-model-critique — writer, reviewer (verdict), fixer, test gate.
// Uses a manifest for the reviewer's verdict and the gate ordering.
var twoModelCritique = Template{
	Name: "two-model-critique",
	Files: map[string]string{
		"loop.env": `# two-model-critique — generate and critique across model families.
# One model writes, a DIFFERENT model reviews with a hostile prompt, the writer
# addresses the findings, then the test suite is the hard gate. The reviewer's
# VERDICT is a soft gate; tests are the hard gate. The loop succeeds only when
# tests pass. Different model families have different blind spots, so
# cross-model critique catches more than either reviewing itself.

LOOP_MAX_ITER=5
# Shared session so the fixer turn sees the reviewer's findings: within one
# iteration the writer, reviewer, and fixer share a single session, so the
# fixer reads the review before acting. LOOP_SESSION_TURNS counts turns, not
# iterations; SessionTurns=3 matches the three turn lines in the manifest
# below, so on the happy path each iteration gets its own fresh session that
# still carries the in-iteration review. Keep these in sync: adding a fourth
# turn to the manifest without raising LOOP_SESSION_TURNS would let an
# iteration spill into the next one's session. The reset also only holds on
# the happy path: a turn that errors does not consume a slot, so a run of
# failed attempts will drift forward into later iterations' sessions.
#
# Trade-off: a shared session means the reviewer resumes the writer's
# conversation, so the writer's reasoning is in context when the reviewer
# grades it — the reviewer loses its "amnesia." That is the cost of giving the
# fixer the review. Set LOOP_SESSION=none to buy back an uncontaminated
# reviewer, at the cost of the fixer turn being blind to the review it is
# told to address.
LOOP_SESSION=shared
LOOP_SESSION_TURNS=3
LOOP_BRANCH=1
LOOP_BRANCH_BASE=main
LOOP_TEST_CMD=go test ./...

# Use two different models. Empty = pi default for both (still works, but loses
# the cross-family benefit). Example pair:
# LOOP_WRITER_MODEL=synthetic/hf:zai-org/GLM-5.2
# LOOP_REVIEWER_MODEL=anthropic/claude-opus-5
# LOOP_FIXER_MODEL=synthetic/hf:zai-org/GLM-5.2
`,
		"manifest": `# two-model-critique: write -> hostile review (verdict) -> fix -> tests
# type    name       path                      key=value
turn     writer     prompts/01-writer.md      model=writer
turn     reviewer   prompts/02-reviewer.md    model=reviewer required=0 verdict=^VERDICT: PASS
turn     fixer      prompts/03-fixer.md       model=fixer
gate     tests      gates/tests.sh
`,
		"prompts/01-writer.md": `# Writer

Do the work described in TASK.md (create it first if it does not exist, stating
the goal in one sentence). Make the change. Run the tests. Summarize what you
changed in a few bullets at the end.

Hard rule: do NOT modify the tests to make them pass.
`,
		"prompts/02-reviewer.md": `# Reviewer

You are reviewing the work above in this session. Treat it as someone else's
and distrust it, even though it arrived as your own assistant messages: under
the shared-session policy you resume the writer's conversation, so the
writer's reasoning is in your context. Set that aside and judge the diff and
the current state on their merits.

Find real defects, security issues, and missed cases. List them ranked by
severity. Do not fix anything yet — your job is to judge.

Then write your full review to stdout as markdown, ending with exactly one of:

    VERDICT: PASS

or

    VERDICT: FAIL

If FAIL, list concrete required fixes the fixer must do next. Prefer FAIL when
unsure. VERDICT must be on its own line.
`,
		"prompts/03-fixer.md": `# Fixer

Address the reviewer's findings you agree with. For any you reject, say why.
Re-run the tests. Do NOT modify the tests to make them pass. Summarize what you
changed in a few bullets at the end.
`,
		"gates/tests.sh": `#!/bin/sh
# tests gate — exit 0 only if the test suite passes.
set -eu
echo "running: ${LOOP_TEST_CMD:-go test ./...}"
# shellcheck disable=SC2086
eval "${LOOP_TEST_CMD:-go test ./...}"
`,
	},
}

// until-count — discovery work. Hunt turn + counting gate.
// Convention-derived (no manifest): prompts/01-hunt.md and gates/done.sh.
var untilCount = Template{
	Name: "until-count",
	Files: map[string]string{
		"loop.env": `# until-count — discovery work.
# Goal is "find N things" (bugs, edge cases, missing test cases), not "make the
# tests pass." Each turn hunts for one more and appends it to a findings file.
# The loop succeeds when the model writes DONE on its own line; if the cap
# fires first, the loop fails (exits 1), same as until-green. The DONE rule is
# soft (the model decides when to write it), so the turn cap is the hard
# backstop.
#
# Convention-derived: no manifest. The runner derives one turn step from
# prompts/01-hunt.md and one gate step from gates/done.sh.

LOOP_MAX_ITER=6
LOOP_SESSION=none
LOOP_BRANCH=0

# Where findings get appended. The done-gate greps this file for a lone DONE.
# (This is a recipe-owned key, not a runner setting, so the runner prints an
# "unknown loop.env key" warning on startup. That is expected; the key is still
# passed through to gates/hooks.)
LOOP_FINDINGS=FINDINGS.md

# Pin the hunt model (empty = pi default). The role name is derived from
# prompts/01-hunt.md, so the env var is LOOP_HUNT_MODEL, not LOOP_WRITER_MODEL:
# LOOP_HUNT_MODEL=xai/grok-4.5
`,
		"prompts/01-hunt.md": `# Hunt

Find one more real bug, edge case, or missing test case in the repository that
is NOT already listed in the findings file. Append it there with a short repro
or explanation.

The findings file is the one named by LOOP_FINDINGS in .loop/loop.env
(FINDINGS.md by default). It must match that value (the done-gate greps
` + "`${LOOP_FINDINGS:-FINDINGS.md}`" + `); if you write elsewhere the gate will
not see it and the loop will keep running until the cap fires.

If you cannot find a genuine new one, write ` + "`DONE`" + ` on its own line at
the end of the findings file and stop. Do not invent findings to fill the count.
`,
		"gates/done.sh": `#!/bin/sh
# done gate — exit 0 only if the findings file contains a lone DONE line.
# Soft stopping rule; the turn cap in loop.env is the hard backstop.
set -eu
f="${LOOP_FINDINGS:-FINDINGS.md}"
if [ ! -f "$f" ]; then
	echo "done: findings file not found: $f" >&2
	exit 1
fi
grep -qx DONE "$f"
`,
	},
}
