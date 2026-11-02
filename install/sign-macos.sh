#!/bin/bash
# GoReleaser post-build hook. Ad hoc signing has no signing identity or notary step.
set -euo pipefail

if [ "$#" -ne 2 ]; then
  printf 'Usage: sign-macos.sh GOOS BINARY\n' >&2
  exit 2
fi

if [ "$1" != darwin ]; then
  exit 0
fi

if [ "$(/usr/bin/uname -s)" != Darwin ]; then
  printf 'Darwin release binaries must be signed on macOS.\n' >&2
  exit 1
fi

/usr/bin/codesign --force --sign - --timestamp=none "$2"
/usr/bin/codesign --verify --strict "$2"
if ! /usr/bin/codesign --display --verbose=4 "$2" 2>&1 | /usr/bin/grep -q '^Signature=adhoc$'; then
  printf 'Expected an ad hoc signature: %s\n' "$2" >&2
  exit 1
fi
