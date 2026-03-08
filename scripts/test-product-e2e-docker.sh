#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

export CGO_ENABLED=1
export BEADS_TEST_MODE=1
export BEADS_DOLT_SERVER_MODE=1

go test -tags cgo -run '^TestProductCoreCommandsWithIsolatedDoltContainer$' ./tests/producte2edocker
