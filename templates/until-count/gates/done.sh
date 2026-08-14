#!/bin/sh
# done gate — exit 0 only if the findings file contains a lone DONE line.
# Soft stopping rule; the turn cap in loop.env is the hard backstop.
set -eu
f="${LOOP_FINDINGS:-FINDINGS.md}"
[ -f "$f" ] || exit 1
grep -qx DONE "$f"
