#!/usr/bin/env bash
# Copyright (c) 2026 Joseph Riedel
# SPDX-License-Identifier: MIT
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

usage="usage: scripts/test.sh [--quick]"
quick=0
case "$#:${1:-}" in
0:) ;;
1:--quick) quick=1 ;;
1:-h | 1:--help)
    echo "$usage"
    exit 0
    ;;
*)
    echo "$usage" >&2
    exit 2
    ;;
esac

run_in_container env QUICK="$quick" bash -c '
set -euo pipefail
unformatted="$(gofmt -l $(find . -name "*.go"))"
if [ -n "$unformatted" ]; then
    echo "gofmt: unformatted files:" >&2
    echo "$unformatted" >&2
    exit 1
fi
go vet ./...
if [ "$QUICK" = 1 ]; then
    # The fast tests only: slow ones skip themselves under -short.
    go test -short -timeout 10m ./...
    exit 0
fi
FINPLAN_SMOKE_OUT=/src/test-output/screenshots go test -timeout 20m ./...
# The background pipeline under the race detector, which finds a job that touches what the UI thread owns.
go test -race -timeout 10m -run TestPipeline ./cmd/finplan
'
