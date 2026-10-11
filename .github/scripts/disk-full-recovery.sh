#!/usr/bin/env bash
# SPDX-License-Identifier: MPL-2.0
# Copyright (c) 2026 Ervins Strauhmanis

# Sourced by the Linux CI step from the checkout root; cleanup owns the parent EXIT trap.
set -euo pipefail
full_dir="$(mktemp -d "$RUNNER_TEMP/pdfconcat-enospc.XXXXXX")"
mounted=false
cleanup() {
  status=$?
  if [ "$mounted" = true ]; then
    if ! sudo umount "$full_dir"; then status=1; fi
  fi
  if ! rmdir "$full_dir"; then status=1; fi
  exit "$status"
}
trap cleanup EXIT
sudo mount -t tmpfs -o "size=1m,uid=$(id -u),gid=$(id -g),mode=0700" tmpfs "$full_dir"
mounted=true
PDFCONCAT_ENOSPC_DIR="$full_dir" go run ./tools/qualitygate test \
  -events "$RUNNER_TEMP/test-events-enospc.json" \
  -run '^TestRecoveryRefreshDiskFullKeepsOriginalLayoutAndPublishedPDF$' \
  -require TestRecoveryRefreshDiskFullKeepsOriginalLayoutAndPublishedPDF ./internal/app
