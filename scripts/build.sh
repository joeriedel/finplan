#!/usr/bin/env bash
# Copyright (c) 2026 Joseph Riedel
# SPDX-License-Identifier: MIT
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

mkdir -p "$ROOT/bin"
run_in_container go build -o bin/finplan ./cmd/finplan
