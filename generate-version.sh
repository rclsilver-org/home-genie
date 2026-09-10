#!/usr/bin/env bash

set -e

# Default computed version
COMPUTED_VERSION=$(git describe --tag --match 'v*.*.*' 2>/dev/null || true)

# Compute the version if the previous command has failed
if [ -z "${COMPUTED_VERSION}" ]; then
    COMMIT_COUNT=$(git rev-list --all --count)
    COMPUTED_VERSION="0.0.0-${COMMIT_COUNT}-$(git rev-parse --short HEAD)"
fi

# Strip a leading v: the tag is v0.2.0, the version is 0.2.0. A v-prefixed
# Debian version starts with a letter and dpkg then orders it badly against a
# later plain one, so the prefix is dropped here rather than worked around
# downstream.
echo ${COMPUTED_VERSION#v}
