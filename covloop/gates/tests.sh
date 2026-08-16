#!/bin/sh
set -eu
echo "running: ${LOOP_TEST_CMD:-go test ./...} -count=1"
# shellcheck disable=SC2086
eval "${LOOP_TEST_CMD:-go test ./...}" -count=1
