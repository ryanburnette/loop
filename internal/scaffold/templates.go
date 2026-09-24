package scaffold

// until-green — the workhorse. Writer turn + test gate. No scorecard.
// Convention-derived (no manifest): prompts/01-writer.md and gates/tests.sh.
// The loop.env comment points at return.md.
var untilGreen = Template{
	Name: "until-green",
	Files: map[string]string{
		"TODO.md": `# Goal

Replace this line with what this loop is for, in one sentence. The runner
reads the first non-heading line and puts it in mend.md, so the next turn
knows what it is working toward.

Add detail below: what "done" looks like, what must not change, anything the
model would otherwise have to guess.
`,
		"loop.env": `# until-green — the workhorse pattern.
# The check is your test suite: an exit code the model cannot argue with.
# Writer turn, then the test gate. No scorecard. Iterates until green or
# the cap fires. Bounded: stops and exits 1 if it cannot go green in
# LOOP_MAX_ITER iterations (the cap counts iterations, not turns).
#
# Come back to loop status. The page is state/<id>/return.md. That file
# says whether the required gate passed. The runner does not merge.
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

# Anti-cheat (optional, strict): fail the loop if any test file changes.
# It takes two parts: LOOP_FREEZE records hashes at run start, and a loop:frozen
# gate re-hashes and checks for drift each iteration. Uncommenting LOOP_FREEZE
# alone only records; without the gate nothing enforces it, so the loop will not
# fail on a changed test file. A missing frozen/index does not pass that gate.
# The check fails closed. Patterns are basename globs: LOOP_FREEZE matches
# only against the file's base name (e.g. *_test.go), so a path like
# internal/foo_test.go is a silent no-op — use the basename form.
# LOOP_FREEZE=*_test.go
# until-green is convention-derived (no manifest), and the frozen gate is a
# manifest step, so enforcing it means writing a manifest that carries the
# derived steps then this line (after the tests gate):
#   turn writer prompts/01-writer.md model=writer
#   gate tests gates/tests.sh
#   gate frozen loop:frozen
`,
		"prompts/01-writer.md": `# Writer

Do the work described in .loop/TODO.md — read it first, all of it. Make the
change. Run the tests.

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

// double-check — writer turn, then a read-only critic scorecard.
// required=0, LOOP_MAX_ITER=1, LOOP_SESSION=none. The card is rule all.
// The critic writes only the JSON path the runner names. It does not edit.
var doubleCheck = Template{
	Name: "double-check",
	Files: map[string]string{
		"TODO.md": `# Goal

Replace this line with what this loop is for, in one sentence. The runner
reads the first non-heading line and puts it in mend.md, so the next turn
knows what it is working toward.

Add detail below: what "done" looks like, what must not change, anything the
model would otherwise have to guess.
`,
		"loop.env": `# double-check — a review aid, not a pass.
# Writer turn, then a read-only critic scorecard. The critic does not edit
# the project. Its only write is the JSON path the runner names. The card
# is rule all. required=0, so a failed rule is advice. There is no shell
# gate and no required check. The loop runs once and exits 0 with result
# done. That is not a pass.
#
# Come back to loop status and state/<id>/return.md.
# LOOP_SESSION=none. The critic does not resume the writer's session.
#
# Drop required=0 only when the review itself is the acceptance signal.
# Unset or identical model pins are then self-graded. Prefer a shell gate.

LOOP_MAX_ITER=1
LOOP_SESSION=none
LOOP_BRANCH=0

# Optional pins. Empty = pi default. Empty is not a distinct model.
# LOOP_WRITER_MODEL=xai/grok-4.5
# LOOP_CRITIC_MODEL=anthropic/claude-opus-5
`,
		"manifest": `# double-check: writer, then a read-only critic scorecard.
# type   name     path                     key=value
turn     writer   prompts/01-writer.md     model=writer
turn     critic   prompts/02-critic.md     model=critic required=0 scorecard=scorecards/critic.card
`,
		"prompts/01-writer.md": `# Writer

Do the work described in .loop/TODO.md — read it first, all of it. Make the
change. Run the project's tests or build if there is one. Summarize what you
changed in a few bullets at the end.
`,
		"prompts/02-critic.md": `# Hostile critic

You are seeing this diff for the first time and you distrust it. You did not
write this code. Read .loop/TODO.md and the diff. List every bug, edge case,
shortcut, and place the writer took the easy path. Be specific. Do not edit
the project. Do not fix what you find.

Write only the JSON path the runner names. That path is in the ask file the
runner attached and in the runner line. Do not print the JSON in chat instead
of writing the file. Do not write any other path. Do not add a passed field.
The runner applies the rule. Mark an item unmet when you are unsure.
`,
		"scorecards/critic.card": `# Critic scorecard. rule all: every required item must be met.
# The runner applies the rule. The critic does not add rows.
rule all

item defects
The diff has no bug, security hole, or missed case that the goal in TODO.md
names. A shortcut that leaves that goal unmet is unmet.

item honesty
The writer's summary matches the diff. A claim that tests or the build passed
is unmet unless the diff or the command output shows it.
`,
	},
}

// two-model-critique — writer, reviewer scorecard (required=0), fixer, tests.
// LOOP_SESSION=none. The fixer reads the brief. Three model pins.
// The required tests gate makes assurance gated.
var twoModelCritique = Template{
	Name: "two-model-critique",
	Files: map[string]string{
		"TODO.md": `# Goal

Replace this line with what this loop is for, in one sentence. The runner
reads the first non-heading line and puts it in mend.md, so the next turn
knows what it is working toward.

Add detail below: what "done" looks like, what must not change, anything the
model would otherwise have to guess.
`,
		"loop.env": `# two-model-critique — write, review, fix, then the test gate.
# One model writes. A reviewer fills a scorecard and does not edit. The
# fixer reads the brief the runner attached, not a shared session. Tests
# are the required gate. The reviewer's scorecard is soft (required=0).
#
# LOOP_SESSION=none. The brief carries the marks, so the reviewer is not
# reading the writer's transcript and the fixer is not blind.
#
# Three pins. Empty = pi default. Unset pins are self-graded unless a
# required gate makes the loop gated. This template's tests gate is
# required, so assurance is gated. Do not read it as cross-model. That
# label needs no required gate, plus writer and reviewer pins set to
# different non-empty ids.
#
# Come back to loop status and state/<id>/return.md.

LOOP_MAX_ITER=5
LOOP_SESSION=none
LOOP_BRANCH=1
LOOP_BRANCH_BASE=main
LOOP_TEST_CMD=go test ./...

# LOOP_WRITER_MODEL=synthetic/hf:zai-org/GLM-5.2
# LOOP_REVIEWER_MODEL=anthropic/claude-opus-5
# LOOP_FIXER_MODEL=synthetic/hf:zai-org/GLM-5.2
`,
		"manifest": `# two-model-critique: write, scorecard, fix, tests.
# type    name       path                      key=value
turn     writer     prompts/01-writer.md      model=writer
turn     reviewer   prompts/02-reviewer.md    model=reviewer required=0 scorecard=scorecards/review.card
turn     fixer      prompts/03-fixer.md       model=fixer
gate     tests      gates/tests.sh
`,
		"prompts/01-writer.md": `# Writer

Do the work described in .loop/TODO.md — read it first, all of it. Make the
change. Run the tests. Summarize what you changed in a few bullets at the end.

Hard rule: do NOT modify the tests to make them pass.
`,
		"prompts/02-reviewer.md": `# Reviewer

Judge the diff and the current tree. You do not share the writer's session.
Read .loop/TODO.md. Find real defects, security issues, and missed cases.
List them by severity. Do not edit the project. Do not fix anything. The
fixer reads the brief the runner writes from this scorecard.

Write only the JSON path the runner names. That path is in the ask file the
runner attached and in the runner line. Do not print the JSON in chat instead
of writing the file. Do not write any other path. Do not add a passed field.
The runner applies the rule. Mark an item unmet when you are unsure.
`,
		"prompts/03-fixer.md": `# Fixer

Read the brief the runner attached. It has the reviewer's scorecard marks.
Address the findings you agree with. For any you reject, say why.
Re-run the tests. Do NOT modify the tests to make them pass. Summarize what
you changed in a few bullets at the end.
`,
		"gates/tests.sh": `#!/bin/sh
# tests gate — exit 0 only if the test suite passes.
set -eu
echo "running: ${LOOP_TEST_CMD:-go test ./...}"
# shellcheck disable=SC2086
eval "${LOOP_TEST_CMD:-go test ./...}"
`,
		"scorecards/review.card": `# Reviewer scorecard. rule all: every required item must be met.
# The runner applies the rule. The reviewer does not add rows.
rule all

item defects
The diff has no bug, security issue, or missed case that the goal in TODO.md
requires.

item tests
Existing tests still describe the behavior the change claims. Deleting or
weakening a test to look finished is unmet.
`,
	},
}

// until-count — discovery work. Hunt turn + DONE script. No scorecard.
// The DONE line is the model declaring done. Not a stronger check than a
// scorecard. FINDINGS.md is a normal untracked file, so stall sees a new one.
var untilCount = Template{
	Name: "until-count",
	Files: map[string]string{
		"TODO.md": `# Goal

Replace this line with what this loop is for, in one sentence. The runner
reads the first non-heading line and puts it in mend.md, so the next turn
knows what it is working toward.

Add detail below: what "done" looks like, what must not change, anything the
model would otherwise have to guess.
`,
		"loop.env": `# until-count — discovery work.
# Goal is "find N things" (bugs, edge cases, missing test cases), not "make
# the tests pass." Each turn hunts for one more and appends it to a findings
# file. The done gate is a script that looks for a lone DONE line. That line
# is the model declaring done. The cap is the backstop. This pattern is not a stronger check than a scorecard.
# Assurance is gated because the script is a required gate. That label does
# not mean the findings were tested.
#
# Stall sees a new finding because FINDINGS.md is a normal untracked file.
# Porcelain does not list ignored files. Do not gitignore the findings file
# and expect stall to treat an append as progress.
#
# Come back to loop status and state/<id>/return.md.
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
# The model declares done. The iteration cap is the backstop.
# This script is not a stronger check than a scorecard.
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
