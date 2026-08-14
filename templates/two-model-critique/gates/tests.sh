#!/bin/sh
# tests gate — exit 0 only if the test suite passes.
# LOOP_TEST_CMD is set in loop.env (default: go test ./...).
# Output goes to $LOOP_LOG automatically (the runner redirects us there).
set -eu
echo "running: ${LOOP_TEST_CMD:-go test ./...}"
# shellcheck disable=SC2086
eval "${LOOP_TEST_CMD:-go test ./...}"
