#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
IMAGE="dolthub/dolt-sql-server:1.83.0"

if ! command -v docker >/dev/null 2>&1; then
    echo "Docker is required for agent workflow smoke tests." >&2
    exit 1
fi

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
    echo "Pulling $IMAGE ..." >&2
    docker pull "$IMAGE" >/dev/null
fi

cd "$REPO_ROOT"
go test -tags cgo -run '^TestAgentWorkflowCommandsWithIsolatedDoltContainer$' ./tests/agentworkflowdocker
