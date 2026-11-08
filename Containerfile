# Copyright (c) 2026 Joseph Riedel
# SPDX-License-Identifier: MIT

FROM registry.fedoraproject.org/fedora:43

RUN dnf -y install \
        golang gcc gcc-c++ pkgconf-pkg-config git \
        SDL3-devel \
        wayland-devel wayland-protocols-devel libxkbcommon-devel \
        mesa-libGL-devel mesa-libEGL-devel libglvnd-devel \
    && dnf clean all
