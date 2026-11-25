# Copyright (c) 2026 Joseph Riedel
# SPDX-License-Identifier: MIT
#
# Shared helpers for build.sh and test.sh. Source this file; do not execute it.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE_NAME="localhost/finplan-build"

# The most seconds a container may run; podman stops and, with --rm, removes it after that, which killing the client would not.
CONTAINER_TIMEOUT="${FINPLAN_CONTAINER_TIMEOUT:-1800}"

# The most seconds an image build may take.
BUILD_TIMEOUT="${FINPLAN_BUILD_TIMEOUT:-3600}"

# A host directory to use as Go's module and build cache instead of the named volume finplan-go. Created if missing; must not be the repo or inside it, or contain a colon.
GO_DIR="${FINPLAN_GO_DIR:-}"

# Set IMAGE to the name:tag for the current Containerfile hash.
set_image() {
    IMAGE="$IMAGE_NAME:$(sha256sum "$ROOT/Containerfile" | cut -c1-16)"
}

# Build the container image unless one tagged with the current Containerfile hash exists.
ensure_image() {
    set_image
    if ! podman image exists "$IMAGE"; then
        timeout "$BUILD_TIMEOUT" podman build -t "$IMAGE" -f "$ROOT/Containerfile" "$ROOT"
    fi
}

# Go's module and build caches live in $FINPLAN_GO_DIR if set, else in one named volume made writable by the host user on creation.
# keep-id makes container uid $(id -u) the host user; the default rootless mapping would give a subuid.
# Sets GO_MOUNT to the -v source for /gopath.
ensure_volume() {
    if [ -n "$GO_DIR" ]; then
        local root_real
        root_real="$(realpath -- "$ROOT")"
        GO_MOUNT="$(realpath -m -- "$GO_DIR")"
        case "$GO_MOUNT" in
        *:*) echo "FINPLAN_GO_DIR must not contain a colon: $GO_MOUNT" >&2; return 1 ;;
        esac
        case "$GO_MOUNT/" in
        "$root_real"/*) echo "FINPLAN_GO_DIR must be outside the repo ($root_real): $GO_MOUNT" >&2; return 1 ;;
        esac
        mkdir -p -- "$GO_MOUNT"
        return
    fi
    GO_MOUNT=finplan-go
    if ! podman volume exists finplan-go; then
        podman volume create finplan-go >/dev/null
        podman run --rm --timeout "$CONTAINER_TIMEOUT" --userns=keep-id --user 0:0 -v finplan-go:/gopath:Z "$IMAGE" \
            chown "$(id -u):$(id -g)" /gopath
    fi
}

# Run a command in the build container with the repo mounted at /src.
# --userns=keep-id maps the host user into the container on both podman 4.9 and 5.x,
# so files written to the mounted repo are owned by the host user.
run_in_container() {
    ensure_image
    ensure_volume
    podman run --rm \
        --timeout "$CONTAINER_TIMEOUT" \
        --userns=keep-id \
        --user "$(id -u):$(id -g)" \
        -v "$ROOT:/src:Z" \
        -v "$GO_MOUNT:/gopath:Z" \
        -e HOME=/tmp \
        -e GOTELEMETRY=off \
        -e GOPATH=/gopath \
        -e GOCACHE=/gopath/cache \
        -e GOFLAGS=-buildvcs=false \
        -w /src \
        "$IMAGE" "$@"
}
