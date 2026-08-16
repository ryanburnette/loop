#!/bin/sh
# tests gate — exit 0 only if the Go suite passes.
# -count=1 so a parent loop's cached packages cannot hide a fail.
set -eu
echo "running: ${LOOP_TEST_CMD:-go test ./...} -count=1"
# shellcheck disable=SC2086
eval "${LOOP_TEST_CMD:-go test ./...}" -count=1
