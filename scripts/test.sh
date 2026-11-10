#!/usr/bin/env bash
# Copyright (c) 2026 Joseph Riedel
# SPDX-License-Identifier: MIT
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

run_in_container bash -c '
set -euo pipefail
unformatted="$(gofmt -l $(find . -name "*.go"))"
if [ -n "$unformatted" ]; then
    echo "gofmt: unformatted files:" >&2
    echo "$unformatted" >&2
    exit 1
fi
go vet ./...
FINPLAN_SMOKE_OUT=/src/test-output/screenshots go test ./...
'
