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

# Build the container image unless one tagged with the current Containerfile hash exists.
ensure_image() {
    local hash
    hash="$(sha256sum "$ROOT/Containerfile" | cut -c1-16)"
    IMAGE="$IMAGE_NAME:$hash"
    if ! podman image exists "$IMAGE"; then
        timeout "$BUILD_TIMEOUT" podman build -t "$IMAGE" -f "$ROOT/Containerfile" "$ROOT"
    fi
}

# Go's module and build caches live in one named volume, made writable by the host user on creation.
# keep-id makes container uid $(id -u) the host user; the default rootless mapping would give a subuid.
ensure_volume() {
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
        -v finplan-go:/gopath:Z \
        -e HOME=/tmp \
        -e GOPATH=/gopath \
        -e GOCACHE=/gopath/cache \
        -e GOFLAGS=-buildvcs=false \
        -w /src \
        "$IMAGE" "$@"
}
