#!/bin/sh
# tests gate — exit 0 only if the Go suite passes.
set -eu
echo "running: ${LOOP_TEST_CMD:-go test ./...}"
# shellcheck disable=SC2086
eval "${LOOP_TEST_CMD:-go test ./...}"
