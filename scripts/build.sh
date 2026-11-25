#!/usr/bin/env bash
# Copyright (c) 2026 Joseph Riedel
# SPDX-License-Identifier: MIT
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

usage="usage: scripts/build.sh"
case "$#:${1:-}" in
0:) ;;
1:-h | 1:--help)
    echo "$usage"
    exit 0
    ;;
*)
    echo "$usage" >&2
    exit 2
    ;;
esac

mkdir -p "$ROOT/bin"
run_in_container go build -o bin/finplan ./cmd/finplan
