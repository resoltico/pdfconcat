#!/bin/bash
# Native lifecycle checks. Run only on a disposable macOS test machine.
# The real installation paths must be absent before the checks begin.
set -euo pipefail
umask 077

TC_NETWORK=0
TC_PACKAGE_DIR=
tc_usage() { printf 'Usage: /bin/bash install/macos_test.sh [--network | --package-dir DIRECTORY]\n' >&2; }
case "${1:-}" in
    '') ;;
    --network) TC_NETWORK=1; shift ;;
    --package-dir)
        if [ "$#" -ne 2 ] || [ -z "${2:-}" ]; then
            tc_usage
            exit 2
        fi
        TC_PACKAGE_DIR=$(CDPATH='' cd -- "$2" && pwd -P) || exit 2
        shift 2 ;;
    *) tc_usage; exit 2 ;;
esac
if [ "$#" -ne 0 ]; then
    printf 'Unexpected test arguments.\n' >&2
    exit 2
fi
if [ "$(/usr/bin/uname -s)" != Darwin ]; then
    printf 'These checks require native macOS; Linux syntax checks are separate.\n' >&2
    exit 1
fi

TC_REPO=$(CDPATH='' cd -- "$(/usr/bin/dirname -- "$0")/.." && pwd -P)
readonly TC_SETUP="$TC_REPO/install/macos.sh"
readonly TC_SIGN="$TC_REPO/install/sign-macos.sh"
readonly TC_ROOT=/usr/local/libexec/pdfconcat
readonly TC_BIN=/usr/local/bin/pdfconcat
readonly TC_LOCK=/var/run/pdfconcat-setup.lock
TC_UID=$(/usr/bin/id -u)
if [ "$TC_UID" -eq 0 ]; then
    printf 'Run these checks as the ordinary runner account, not as root.\n' >&2
    exit 1
fi
if [ -e "$TC_BIN" ] || [ -L "$TC_BIN" ] || [ -e "$TC_ROOT" ] || [ -L "$TC_ROOT" ]; then
    printf 'Refusing to test over an existing PDFConcat installation or destination.\n' >&2
    exit 1
fi
if ! /usr/bin/sudo -n /usr/bin/true; then
    printf 'Administrator authorization is unavailable. In an interactive Terminal, run sudo -v, then retry these checks.\n' >&2
    exit 1
fi
TC_TEMP=$(/usr/bin/mktemp -d "${TMPDIR:-/tmp}/pdfconcat-installer-tests.XXXXXX")
TC_TEMP=$(CDPATH='' cd -- "$TC_TEMP" && pwd -P)
TC_CASES=0
TC_CHILD_PID=
TC_HOLDER_PID=
TC_RETIRED_STAGE=
TC_OWNS_PATHS=1

tc_cleanup() {
    local status=$?
    local holder_command
    trap - EXIT INT TERM
    if [ -n "$TC_HOLDER_PID" ]; then
        holder_command=$(/bin/ps -p "$TC_HOLDER_PID" -o command= 2>/dev/null || true)
        case "$holder_command" in
            *"$TC_TEMP/lock-ready"*) /usr/bin/sudo /bin/kill -TERM "$TC_HOLDER_PID" 2>/dev/null || true ;;
        esac
    fi
    if [ -n "$TC_CHILD_PID" ]; then
        wait "$TC_CHILD_PID" 2>/dev/null || true
    fi
    if [ "$TC_OWNS_PATHS" -eq 1 ]; then
        # These two paths were absent before this disposable-machine test.
        # Never remove or change permissions on their shared parent directories.
        /usr/bin/sudo /bin/rm -f -- "$TC_BIN" || true
        /usr/bin/sudo /bin/rm -rf -- "$TC_ROOT" || true
    fi
    if [[ "$TC_RETIRED_STAGE" =~ ^/private/tmp/pdfconcat-stage\.[A-Za-z0-9]+$ ]]; then
        /usr/bin/sudo /bin/rm -rf -- "$TC_RETIRED_STAGE" || true
    fi
    if [ "$status" -eq 0 ]; then
        /bin/rm -rf -- "$TC_TEMP"
    else
        printf 'Installer test evidence retained at %s\n' "$TC_TEMP" >&2
    fi
    exit "$status"
}
trap tc_cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

tc_fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

tc_case() {
    TC_CASES=$((TC_CASES + 1))
    printf '\n[%s] %s\n' "$TC_CASES" "$1"
}

tc_expect_failure() {
    local label=$1
    shift
    if "$@" >"$TC_TEMP/$label.log" 2>&1; then
        /bin/cat "$TC_TEMP/$label.log" >&2
        tc_fail "$label unexpectedly succeeded"
    fi
    /bin/cat "$TC_TEMP/$label.log"
}

tc_sha256() {
    /usr/bin/shasum -a 256 "$1" | /usr/bin/awk '{print $1}'
}

# Run sourced functions in a fresh shell. Calling a function in an `if` test
# would disable errexit inside that function and could hide real failures.
TC_SOURCE_BODY=$(/bin/cat <<'BASH'
set -euo pipefail
source "$1"
pc_init_paths
PC_SCRIPT=$1
shift
"$@"
BASH
)

tc_source() {
    /bin/bash -c "$TC_SOURCE_BODY" bash "$TC_SETUP" "$@"
}

TC_ROOT_BODY=$(/bin/cat <<'BASH'
set -euo pipefail
umask 077
exec 9<> "$1"
/usr/bin/lockf -s -t 0 9
shift
source "$1"
pc_init_paths
PC_SCRIPT=$1
shift
"$@"
BASH
)

tc_root() {
    /usr/bin/sudo /bin/bash -c "$TC_ROOT_BODY" bash "$TC_LOCK" "$TC_SETUP" "$@"
}

tc_assert_absent() {
    [[ ! -e "$TC_BIN" && ! -L "$TC_BIN" ]] || tc_fail 'Public command remains'
    [[ ! -e "$TC_ROOT" && ! -L "$TC_ROOT" ]] || tc_fail 'Managed installation remains'
}

tc_assert_readable_links() {
    local public_target current_target
    [[ -L "$TC_BIN" && -L "$TC_ROOT/current" ]] || tc_fail 'Managed command and current-release links are required'
    [ "$(/usr/bin/stat -f '%Lp' "$TC_BIN")" = 755 ] || tc_fail 'Public command link is not readable by ordinary users'
    [ "$(/usr/bin/stat -f '%Lp' "$TC_ROOT/current")" = 755 ] || tc_fail 'Current-release link is not readable by ordinary users'
    # On macOS a root-owned mode-700 symlink can prevent ordinary readlink and
    # ACL inspection. Check those operations without sudo as well as execution.
    public_target=$(/usr/bin/readlink "$TC_BIN") || tc_fail 'Ordinary-user readlink failed on the public command'
    current_target=$(/usr/bin/readlink "$TC_ROOT/current") || tc_fail 'Ordinary-user readlink failed on the current release'
    [ "$public_target" = "$TC_ROOT/current/payload/pdfconcat" ] || tc_fail 'Public command link has an unexpected target'
    case "$current_target" in releases/*) ;; *) tc_fail 'Current-release link has an unexpected target' ;; esac
    /bin/ls -lde "$TC_BIN" "$TC_ROOT/current" >"$TC_TEMP/link-metadata.log"
}

tc_assert_version() {
    local expected=$1 actual
    actual=$("$TC_BIN" version)
    case "$actual" in
        *"${expected#v}"*) ;;
        *) tc_fail "Expected version $expected; received $actual" ;;
    esac
    tc_assert_readable_links
    tc_source pc_validate_state
    /usr/bin/codesign --verify --strict --verbose=2 "$TC_BIN"
    /usr/bin/codesign -d --verbose=4 "$TC_BIN" >"$TC_TEMP/signature.log" 2>&1
    /usr/bin/grep -q 'Signature=adhoc' "$TC_TEMP/signature.log" || tc_fail 'Installed signature is not ad hoc'
}

tc_assert_running_version() {
    local expected=$1 actual
    actual=$("$TC_BIN" version)
    case "$actual" in
        *"${expected#v}"*) ;;
        *) tc_fail "Working command changed unexpectedly: $actual" ;;
    esac
}

tc_current_dir() {
    printf '%s/%s\n' "$TC_ROOT" "$(/usr/bin/readlink "$TC_ROOT/current")"
}

tc_assert_command_hash() {
    [ "$(tc_sha256 "$TC_BIN")" = "$1" ] || tc_fail 'A refused operation changed the executable'
}

tc_build_fixture() {
    local key=$1 version=$2 dir asset minimum
    dir="$TC_TEMP/$key"
    asset="pdfconcat_${version#v}_darwin_${TC_ARCH}.tar.gz"
    /bin/mkdir -p "$dir/raw/examples"
    (
        cd "$TC_REPO"
        CGO_ENABLED=0 GOOS=darwin GOARCH="$TC_ARCH" "$TC_GO" build -trimpath \
            -ldflags="-s -w -X main.version=${version#v} -macos=15.0.0" \
            -o "$dir/raw/pdfconcat" ./cmd/pdfconcat
    )
    /bin/bash "$TC_SIGN" darwin "$dir/raw/pdfconcat"
    minimum=$(/usr/bin/otool -l "$dir/raw/pdfconcat" | /usr/bin/awk '
        $1 == "cmd" { build = ($2 == "LC_BUILD_VERSION") }
        build && $1 == "minos" { print $2 }
    ')
    case "$minimum" in
        15.0|15.0.0) ;;
        *) tc_fail "Expected LC_BUILD_VERSION minimum 15.0; found $minimum" ;;
    esac
    printf 'Installer lifecycle fixture %s\n' "$version" >"$dir/raw/README.md"
    /bin/cp "$TC_REPO/examples/plans/generated-only.json" "$dir/raw/examples/generated-only.json"
    /usr/bin/tar -czf "$dir/$asset" -C "$dir/raw" pdfconcat README.md examples
    printf '%s  %s\n' "$(tc_sha256 "$dir/$asset")" "$asset" >"$dir/checksums.txt"
    printf '%s\n' "$version" >"$dir/version"
    printf '%s\n' "$asset" >"$dir/asset"
}

tc_prepare() {
    local key=$1 stage=$2 dir version asset work
    dir="$TC_TEMP/$key"
    version=$(/bin/cat "$dir/version")
    asset=$(/bin/cat "$dir/asset")
    work=$(/usr/bin/mktemp -d "$TC_TEMP/prepare.XXXXXX")
    tc_source pc_prepare_archive "$dir/$asset" "$dir/checksums.txt" "$asset" "$version" "$TC_ARCH" "$work"
    /bin/mv "$work/generation" "$stage"
}

tc_publish() {
    /usr/bin/sudo /usr/bin/env -u BASH_ENV -u ENV /bin/bash "$TC_SETUP" __root-install "$TC_UID" "$1"
}

# Private-prefix checks exercise the real worker's policy and locking, without
# changing shared destination parents or exposing path overrides in setup's CLI.
TC_WORKER_BODY=$(/bin/cat <<'BASH'
set -euo pipefail
source "$1"
pc_init_paths
PC_SCRIPT=$1
PC_ROOT=$2/libexec/pdfconcat
PC_BIN=$2/bin/pdfconcat
PC_LOCK=$2/setup.lock
shift 2
pc_root_worker "$@"
BASH
)

tc_worker() {
    local prefix=$1
    shift
    /usr/bin/sudo /usr/bin/env -u BASH_ENV -u ENV /bin/bash -c "$TC_WORKER_BODY" bash "$TC_SETUP" "$prefix" "$@"
}

# Independently required representative release assets, not a second copy of
# the full GoReleaser manifest. Explicit returns also keep negative controls valid.
tc_package_payload() {
    local dir=$1 reference=${2:-} rel
    for rel in README.md docs/MACOS.md THIRD_PARTY_NOTICES.md \
        examples/plans/generated-only.json schemas/plan.schema.json schemas/result.schema.json; do
        if [[ ! -f "$dir/$rel" || -L "$dir/$rel" || ! -s "$dir/$rel" ]]; then
            printf 'Required package asset is missing or invalid: %s\n' "$rel" >&2
            return 1
        fi
        if [[ -n "$reference" ]]; then
            /usr/bin/cmp "$reference/$rel" "$dir/$rel" || return
        fi
    done
}

tc_replace_binary() {
    local source=$1 current
    current=$(tc_current_dir)
    /usr/bin/sudo /usr/bin/install -o root -g wheel -m 755 "$source" "$current/payload/pdfconcat.test-swap"
    /usr/bin/sudo /bin/mv -f "$current/payload/pdfconcat.test-swap" "$current/payload/pdfconcat"
}

tc_uninstall_helper() {
    local helper profile endpoint sandbox_usable=0
    helper="$TC_ROOT/current/setup.sh"
    profile='(version 1) (allow default) (deny network*)'
    endpoint=https://github.com/resoltico/pdfconcat
    if [ -x /usr/bin/sandbox-exec ]; then
        # Controls use the same ordinary-user -> sudo chain as the helper.
        # A failed curl alone is insufficient: verify unsandboxed connectivity
        # and that sandboxed sudo can execute a non-networked command first.
        if /usr/bin/curl -q --fail --silent --show-error --connect-timeout 3 --max-time 6 \
                --output /dev/null "$endpoint" >"$TC_TEMP/network-control.log" 2>&1 &&
            { /usr/bin/sudo /usr/bin/curl -q --fail --silent --show-error --connect-timeout 3 --max-time 6 \
                --output /dev/null "$endpoint"; } >>"$TC_TEMP/network-control.log" 2>&1 &&
            /usr/bin/sandbox-exec -p "$profile" /usr/bin/sudo -n /usr/bin/true \
                >>"$TC_TEMP/network-control.log" 2>&1; then
            if /usr/bin/sandbox-exec -p "$profile" /usr/bin/curl -q --fail --silent --show-error \
                    --connect-timeout 3 --max-time 6 --output /dev/null "$endpoint" \
                    >"$TC_TEMP/network-denied-user.log" 2>&1; then
                sandbox_usable=0
            elif /usr/bin/sandbox-exec -p "$profile" /usr/bin/sudo /usr/bin/curl -q \
                    --fail --silent --show-error --connect-timeout 3 --max-time 6 \
                    --output /dev/null "$endpoint" >"$TC_TEMP/network-denied-root.log" 2>&1; then
                sandbox_usable=0
            else
                sandbox_usable=1
            fi
        fi
    fi
    if [ "$sandbox_usable" -eq 1 ]; then
        printf 'Verified network denial for ordinary and privileged subprocesses.\n'
        /usr/bin/sandbox-exec -p "$profile" /bin/bash "$helper" uninstall --yes
    else
        printf 'UNVERIFIED: network-disconnected removal needs a separate Mac check; the sandbox controls were unavailable or ineffective.\n'
        /bin/bash "$helper" uninstall --yes
    fi
    tc_assert_absent
}

tc_smoke_pdf() {
    local smoke
    smoke=$(/usr/bin/mktemp -d "$TC_TEMP/smoke.XXXXXX")
    /bin/cp "$TC_REPO/examples/plans/generated-only.json" "$smoke/plan.json"
    if ! "$TC_BIN" build --plan "$smoke/plan.json" -o "$smoke/Dividers.pdf" >"$smoke/build.log" 2>&1; then
        /bin/cat "$smoke/build.log" >&2
        tc_fail 'The source-free PDF build failed'
    fi
    [ -s "$smoke/Dividers.pdf" ] || tc_fail 'Generated-only PDF is missing'
    [ "$(/usr/bin/head -c 5 "$smoke/Dividers.pdf")" = '%PDF-' ] || tc_fail 'Output lacks a PDF header'
    TC_USER_PDF="$smoke/Dividers.pdf"
    TC_USER_PDF_HASH=$(tc_sha256 "$TC_USER_PDF")
}

tc_snapshot_environment() {
    local path
    while IFS= read -r path; do
        /usr/bin/stat -f '%N %u %g %Lp' "$path"
    done <"$TC_TEMP/existing-parents.txt"
    for path in "$HOME/.zprofile" "$HOME/.zshrc" "$HOME/.zshenv" \
        "$HOME/.bash_profile" "$HOME/.bash_login" "$HOME/.bashrc" \
        "$HOME/.profile" "$HOME/.config/fish/config.fish"; do
        if [ -L "$path" ]; then
            printf 'link %s %s\n' "$path" "$(/usr/bin/readlink "$path")"
        elif [ -f "$path" ]; then
            printf 'file %s %s\n' "$path" "$(tc_sha256 "$path")"
        else
            printf 'absent %s\n' "$path"
        fi
    done
}

/usr/bin/sw_vers
/usr/bin/uname -m
/bin/bash --version
case "$(/usr/bin/uname -m)" in
    arm64) TC_ARCH=arm64 ;;
    x86_64) TC_ARCH=amd64 ;;
    *) tc_fail 'Unsupported native test architecture' ;;
esac
for TC_PARENT in /usr/local /usr/local/bin /usr/local/libexec /var/run; do
    if [ -d "$TC_PARENT" ]; then
        printf '%s\n' "$TC_PARENT"
    fi
done >"$TC_TEMP/existing-parents.txt"
tc_snapshot_environment >"$TC_TEMP/environment-before.txt"

if [[ -n "$TC_PACKAGE_DIR" ]]; then
    tc_case 'Install the exact current GoReleaser archive through the production worker'
    TC_PACKAGES=("$TC_PACKAGE_DIR"/pdfconcat_*_darwin_"$TC_ARCH".tar.gz)
    [[ ${#TC_PACKAGES[@]} == 1 && -f "${TC_PACKAGES[0]}" && ! -L "${TC_PACKAGES[0]}" ]] || tc_fail 'Expected exactly one native package archive'
    TC_PACKAGE=${TC_PACKAGES[0]}
    TC_ASSET=${TC_PACKAGE##*/}
    TC_VERSION=${TC_ASSET#pdfconcat_}
    TC_VERSION=v${TC_VERSION%_darwin_"$TC_ARCH".tar.gz}
    tc_source pc_valid_version "$TC_VERSION" || tc_fail 'Native package filename must contain a stable version'
    /bin/mkdir "$TC_TEMP/package-work" "$TC_TEMP/package-raw"
    tc_source pc_prepare_archive "$TC_PACKAGE" "$TC_PACKAGE_DIR/checksums.txt" \
        "$TC_ASSET" "$TC_VERSION" "$TC_ARCH" "$TC_TEMP/package-work"
    TC_CANDIDATE="$TC_TEMP/package-work/generation"
    # Extract only after production preparation has checked archive paths/types.
    /usr/bin/tar -xzf "$TC_PACKAGE" -C "$TC_TEMP/package-raw"
    tc_package_payload "$TC_CANDIDATE/payload" "$TC_TEMP/package-raw"
    printf 'Outside user file must survive package installation and removal.\n' >"$TC_TEMP/user-file.txt"
    TC_OUTSIDE_HASH=$(tc_sha256 "$TC_TEMP/user-file.txt")
    tc_publish "$TC_CANDIDATE"
    tc_assert_version "$TC_VERSION"
    "$TC_BIN" version --json >"$TC_TEMP/installed-version.json"
    [[ $(/usr/bin/plutil -extract version raw -o - "$TC_TEMP/installed-version.json") == "${TC_VERSION#v}" ]] || tc_fail 'Installed package version differs'
    [[ $(/usr/bin/plutil -extract platform raw -o - "$TC_TEMP/installed-version.json") == "darwin/$TC_ARCH" ]] || tc_fail 'Installed package platform differs'
    tc_package_payload "$TC_ROOT/current/payload" "$TC_CANDIDATE/payload"
    /usr/bin/cmp "$TC_CANDIDATE/payload/pdfconcat" "$TC_BIN"
    /usr/bin/cmp "$TC_SETUP" "$TC_ROOT/current/setup.sh"
    /bin/bash "$TC_ROOT/current/setup.sh" uninstall --yes
    tc_assert_absent
    [[ $(tc_sha256 "$TC_TEMP/user-file.txt") == "$TC_OUTSIDE_HASH" ]] || tc_fail 'Package lifecycle changed an outside user file'

    tc_case 'Package assertions detect missing documentation independently of preparation'
    /usr/bin/ditto "$TC_TEMP/package-raw" "$TC_TEMP/missing-doc"
    /bin/rm "$TC_TEMP/missing-doc/docs/MACOS.md"
    tc_expect_failure package-missing-doc tc_package_payload "$TC_TEMP/missing-doc"
    /usr/bin/grep -Fxq 'Required package asset is missing or invalid: docs/MACOS.md' "$TC_TEMP/package-missing-doc.log" || tc_fail 'Missing-document control failed for another reason'

    tc_case 'Production preparation rejects an archive missing its executable'
    /bin/mkdir "$TC_TEMP/missing-executable" "$TC_TEMP/missing-executable/work"
    /bin/rm "$TC_TEMP/package-raw/pdfconcat"
    (
        cd "$TC_TEMP/package-raw"
        /usr/bin/tar -czf "$TC_TEMP/missing-executable/$TC_ASSET" -- *
    )
    printf '%s  %s\n' "$(tc_sha256 "$TC_TEMP/missing-executable/$TC_ASSET")" "$TC_ASSET" >"$TC_TEMP/missing-executable/checksums.txt"
    tc_expect_failure package-missing-executable tc_source pc_prepare_archive \
        "$TC_TEMP/missing-executable/$TC_ASSET" "$TC_TEMP/missing-executable/checksums.txt" \
        "$TC_ASSET" "$TC_VERSION" "$TC_ARCH" "$TC_TEMP/missing-executable/work"
    /usr/bin/grep -Fq 'does not contain the PDFConcat executable' "$TC_TEMP/package-missing-executable.log" || tc_fail 'Missing-executable control failed for another reason'
    tc_assert_absent
    tc_snapshot_environment >"$TC_TEMP/environment-after.txt"
    /usr/bin/diff -u "$TC_TEMP/environment-before.txt" "$TC_TEMP/environment-after.txt"
    printf '\nPASS: %s current-package install/uninstall cases completed.\n' "$TC_CASES"
    exit 0
fi

TC_GO=$(command -v go)

tc_case 'Diagnostic paths escape control characters and keep ordinary paths readable'
[[ $(tc_source pc_display_path /usr/local/bin/pdfconcat) == /usr/local/bin/pdfconcat ]] || tc_fail 'Ordinary path display changed'
[[ $(/usr/bin/env LC_ALL=C /bin/bash -c "$TC_SOURCE_BODY" bash "$TC_SETUP" pc_display_path '/Users/Rīga/customer file.pdf') == "\$'/Users/R\\304\\253ga/customer file.pdf'" ]] || tc_fail 'Unicode path bytes were not escaped consistently'
[[ $(tc_source pc_display_path $'customer\nreceipt') == "\$'customer\\nreceipt'" ]] || tc_fail 'Newline path was not escaped'
[[ $(tc_source pc_display_path $'customer\treceipt') == "\$'customer\\treceipt'" ]] || tc_fail 'Tab path was not escaped'
[[ $(tc_source pc_display_path $'customer\rreceipt') == "\$'customer\\rreceipt'" ]] || tc_fail 'Carriage-return path was not escaped'
[[ $(tc_source pc_display_path $'customer\033receipt') == "\$'customer\\Ereceipt'" ]] || tc_fail 'Escape path was not escaped'
[[ $(/usr/bin/env LC_ALL=en_US.UTF-8 /bin/bash -c "$TC_SOURCE_BODY" bash "$TC_SETUP" pc_display_path $'customer\342\200\256receipt') == "\$'customer\\342\\200\\256receipt'" ]] || tc_fail 'Bidirectional-control bytes were not escaped as ASCII'

tc_case 'Release discovery accepts only an exact stable tag URL in this repository'
for TC_VALID_VERSION in v0.0.0 v90.0.1; do
    [ "$(tc_source pc_version_from_release_url "https://github.com/resoltico/pdfconcat/releases/tag/$TC_VALID_VERSION")" = "$TC_VALID_VERSION" ] || tc_fail 'A stable release URL was not parsed exactly'
done
TC_URL_CASE=0
for TC_BAD_RELEASE_URL in \
    'http://github.com/resoltico/pdfconcat/releases/tag/v1.2.3' \
    'https://github.com.example.invalid/resoltico/pdfconcat/releases/tag/v1.2.3' \
    'https://github.com/another-owner/pdfconcat/releases/tag/v1.2.3' \
    'https://github.com/resoltico/another-repository/releases/tag/v1.2.3' \
    'https://github.com/resoltico/pdfconcat/tree/v1.2.3' \
    'https://github.com/resoltico/pdfconcat/releases/tag/v1.2.3-rc.1' \
    'https://github.com/resoltico/pdfconcat/releases/tag/v1.2.3?download=1' \
    'https://github.com/resoltico/pdfconcat/releases/tag/v1.2.3#fragment' \
    'https://github.com/resoltico/pdfconcat/releases/tag/v1.2.3%2fextra' \
    'https://github.com/resoltico%2fpdfconcat/releases/tag/v1.2.3' \
    'https://github.com/resoltico/pdfconcat/releases/tag/v01.2.3' \
    'https://github.com/resoltico/pdfconcat/releases/tag/v1.2.3/'; do
    TC_URL_CASE=$((TC_URL_CASE + 1))
    tc_expect_failure "release-url-$TC_URL_CASE" tc_source pc_version_from_release_url "$TC_BAD_RELEASE_URL"
done

tc_case 'Build and ad hoc sign two native executable fixtures with minimum macOS 15'
tc_build_fixture fixture-a v90.0.0
tc_build_fixture fixture-b v90.0.1

if [ "$TC_NETWORK" -eq 1 ]; then
    tc_case 'Install the current public GitHub release while ignoring conflicting curl configuration'
    /bin/mkdir "$TC_TEMP/curl-config"
    printf 'proxy = "http://127.0.0.1:1"\nnoproxy = ""\n' >"$TC_TEMP/curl-config/.curlrc"
    tc_expect_failure curl-config-control /usr/bin/env CURL_HOME="$TC_TEMP/curl-config" \
        /usr/bin/curl --connect-timeout 1 --max-time 3 --silent --show-error \
        --output /dev/null https://github.com/resoltico/pdfconcat/releases/latest
    /usr/bin/grep -Fq '127.0.0.1' "$TC_TEMP/curl-config-control.log" || tc_fail 'The curl control did not exercise the configured unreachable proxy'
    # Exercise the actual success path from a zsh parent. Its unexported
    # autoload function is invisible to setup's Bash child, even with /usr/local/bin
    # first in PATH. These private fixtures never read or change user dotfiles.
    TC_SHADOW="$TC_TEMP/zsh-shadow"
    /bin/mkdir -p "$TC_SHADOW/functions" "$TC_SHADOW/bin" "$TC_SHADOW/zdot"
    /bin/cat >"$TC_SHADOW/functions/pdfconcat" <<'ZSH'
case "${1:-}" in
    --help) print -r -- 'Private legacy pdfconcat fixture: BLK/pdfcpu/gs' ;;
    *) print -u2 -r -- "ERROR Unknown option: ${1:-}"; return 2 ;;
esac
ZSH
    /bin/cat >"$TC_SHADOW/alias-probe.zsh" <<'ZSH'
pdfconcat version
ZSH
    /bin/cat >"$TC_SHADOW/bin/pdfconcat" <<'SH'
#!/bin/sh
printf 'private PATH shadow\n'
SH
    /bin/chmod 755 "$TC_SHADOW/bin/pdfconcat"
    /bin/cat >"$TC_SHADOW/parent.zsh" <<'ZSH'
set -eu
mode=$1
fixture=$2
setup=$3
arch=$4
export PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin
case "$mode" in
    autoload-*) fpath=("$fixture/functions"); autoload -Uz pdfconcat ;;
    alias-*) alias pdfconcat='print -r -- private alias shadow' ;;
    path-*) export PATH="$fixture/bin:/usr/bin:/bin" ;;
    *) print -u2 -r -- 'Unknown shadow test mode'; exit 1 ;;
esac

assert_shadow() {
    case "$mode" in
        autoload-*)
            if pdfconcat version >"$fixture/$mode-shadow.log" 2>&1; then
                print -u2 -r -- 'Expected the private legacy function to reject version'
                return 1
            fi
            /usr/bin/grep -Fxq 'ERROR Unknown option: version' "$fixture/$mode-shadow.log"
            [[ $(pdfconcat --help) == 'Private legacy pdfconcat fixture: BLK/pdfcpu/gs' ]]
            ;;
        alias-*)
            # Source a fixed probe so zsh expands the alias after its definition.
            # No setup output or dynamically constructed command is evaluated.
            [[ $(source "$fixture/alias-probe.zsh") == 'private alias shadow version' ]]
            ;;
        path-*) [[ $(pdfconcat version) == 'private PATH shadow' ]] ;;
    esac
}
assert_shadow

case "$mode" in
    *-install)
        [[ "$mode" != alias-install ]] || export PATH=/nonexistent
        /bin/bash "$setup" install --yes >"$fixture/$mode-setup.log" 2>&1
        /bin/cat "$fixture/$mode-setup.log"
        recommendation=$(/usr/bin/sed -n 's/^Check the installed command: //p' "$fixture/$mode-setup.log")
        [[ "$recommendation" == '/usr/local/bin/pdfconcat version' ]]
        /usr/bin/grep -Fxq 'A bare pdfconcat command may select an alias, shell function, or another executable.' "$fixture/$mode-setup.log"
        /usr/bin/grep -Fxq 'Help: https://github.com/resoltico/pdfconcat/blob/main/docs/MACOS.md#troubleshooting' "$fixture/$mode-setup.log"
        # Execute the exact verified recommendation without evaluating log text.
        /usr/local/bin/pdfconcat version >"$fixture/$mode-version.txt"
        /usr/local/bin/pdfconcat version --json >"$fixture/$mode-version.json"
        installed_version=$(/usr/bin/sed -n 's/^Installed PDFConcat v\([0-9.]*\)\.$/\1/p' "$fixture/$mode-setup.log")
        [[ -n "$installed_version" ]]
        [[ $(/usr/bin/plutil -extract version raw -o - "$fixture/$mode-version.json") == "$installed_version" ]]
        [[ $(/usr/bin/plutil -extract platform raw -o - "$fixture/$mode-version.json") == "darwin/$arch" ]]
        ;;
    path-check)
        # The captured recommendation also works when PATH finds a foreign file.
        recommendation=$(/usr/bin/sed -n 's/^Check the installed command: //p' "$fixture/alias-install-setup.log")
        [[ "$recommendation" == '/usr/local/bin/pdfconcat version' ]]
        /usr/local/bin/pdfconcat version
        /usr/local/bin/pdfconcat version --json >"$fixture/path-version.json"
        [[ $(/usr/bin/plutil -extract version raw -o - "$fixture/path-version.json") == $(/usr/bin/plutil -extract version raw -o - "$fixture/alias-install-version.json") ]]
        [[ $(/usr/bin/plutil -extract platform raw -o - "$fixture/path-version.json") == "darwin/$arch" ]]
        ;;
    *-removed)
        # Shell shadows can remain after managed files are successfully removed.
        [[ ! -e /usr/local/bin/pdfconcat && ! -L /usr/local/bin/pdfconcat ]]
        [[ ! -e /usr/local/libexec/pdfconcat && ! -L /usr/local/libexec/pdfconcat ]]
        ;;
esac
assert_shadow
ZSH
    /usr/bin/shasum -a 256 "$TC_SHADOW/functions/pdfconcat" "$TC_SHADOW/alias-probe.zsh" \
        "$TC_SHADOW/bin/pdfconcat" "$TC_SHADOW/parent.zsh" >"$TC_SHADOW/fixtures-before.txt"
    /usr/bin/env CURL_HOME="$TC_TEMP/curl-config" ZDOTDIR="$TC_SHADOW/zdot" /bin/zsh -f \
        "$TC_SHADOW/parent.zsh" autoload-install "$TC_SHADOW" "$TC_SETUP" "$TC_ARCH"
    [ -L "$TC_BIN" ] || tc_fail 'Public installation did not create the command link'
    tc_assert_readable_links
    tc_source pc_validate_state
    /usr/bin/codesign --verify --strict --verbose=2 "$TC_BIN"
    /usr/bin/codesign -d --verbose=4 "$TC_BIN" >"$TC_TEMP/network-signature.log" 2>&1
    /usr/bin/grep -q 'Signature=adhoc' "$TC_TEMP/network-signature.log" || tc_fail 'Public release was not installed with an ad hoc signature'
    "$TC_BIN" version
    tc_smoke_pdf
    tc_case 'Repeat the real installation beneath a private zsh alias and unusable PATH'
    /usr/bin/env ZDOTDIR="$TC_SHADOW/zdot" /bin/zsh -f \
        "$TC_SHADOW/parent.zsh" alias-install "$TC_SHADOW" "$TC_SETUP" "$TC_ARCH"
    tc_assert_readable_links
    tc_source pc_validate_state
    tc_case 'The absolute recommendation bypasses a private executable earlier in PATH'
    /usr/bin/env ZDOTDIR="$TC_SHADOW/zdot" /bin/zsh -f \
        "$TC_SHADOW/parent.zsh" path-check "$TC_SHADOW" "$TC_SETUP" "$TC_ARCH"
    tc_case 'Installed-helper removal preserves private function, alias and PATH shadows'
    tc_uninstall_helper
    for TC_SHADOW_KIND in autoload alias path; do
        /usr/bin/env ZDOTDIR="$TC_SHADOW/zdot" /bin/zsh -f \
            "$TC_SHADOW/parent.zsh" "$TC_SHADOW_KIND-removed" "$TC_SHADOW" "$TC_SETUP" "$TC_ARCH"
    done
    /usr/bin/shasum -a 256 "$TC_SHADOW/functions/pdfconcat" "$TC_SHADOW/alias-probe.zsh" \
        "$TC_SHADOW/bin/pdfconcat" "$TC_SHADOW/parent.zsh" >"$TC_SHADOW/fixtures-after.txt"
    /usr/bin/cmp "$TC_SHADOW/fixtures-before.txt" "$TC_SHADOW/fixtures-after.txt"
fi

tc_case 'Reject a checksum mismatch before any installation is created'
TC_ASSET_A=$(/bin/cat "$TC_TEMP/fixture-a/asset")
printf '%064d  %s\n' 0 "$TC_ASSET_A" >"$TC_TEMP/bad-checksums.txt"
/bin/mkdir "$TC_TEMP/bad-stage"
tc_expect_failure checksum-mismatch tc_source pc_prepare_archive \
    "$TC_TEMP/fixture-a/$TC_ASSET_A" "$TC_TEMP/bad-checksums.txt" "$TC_ASSET_A" \
    v90.0.0 "$TC_ARCH" "$TC_TEMP/bad-stage"
tc_assert_absent

tc_case 'A valid checksum does not permit archive symlinks or absolute member paths'
TC_ARCHIVE_SENTINEL="$TC_TEMP/archive-sentinel"
printf 'external archive sentinel\n' >"$TC_ARCHIVE_SENTINEL"
TC_ARCHIVE_SENTINEL_HASH=$(tc_sha256 "$TC_ARCHIVE_SENTINEL")
/bin/mkdir -p "$TC_TEMP/link-archive/raw" "$TC_TEMP/link-archive/work" "$TC_TEMP/path-archive/work"
/bin/ln -s "$TC_ARCHIVE_SENTINEL" "$TC_TEMP/link-archive/raw/pdfconcat"
/usr/bin/tar -czf "$TC_TEMP/link-archive/$TC_ASSET_A" -C "$TC_TEMP/link-archive/raw" pdfconcat
printf '%s  %s\n' "$(tc_sha256 "$TC_TEMP/link-archive/$TC_ASSET_A")" "$TC_ASSET_A" >"$TC_TEMP/link-archive/checksums.txt"
tc_expect_failure archive-symlink tc_source pc_prepare_archive \
    "$TC_TEMP/link-archive/$TC_ASSET_A" "$TC_TEMP/link-archive/checksums.txt" \
    "$TC_ASSET_A" v90.0.0 "$TC_ARCH" "$TC_TEMP/link-archive/work"
/usr/bin/grep -Fq 'link or special file' "$TC_TEMP/archive-symlink.log" || tc_fail 'Archive link was not rejected by the file-type check'
[ ! -e "$TC_TEMP/link-archive/work/generation" ] || tc_fail 'Unsafe link archive was extracted'
[ "$(tc_sha256 "$TC_ARCHIVE_SENTINEL")" = "$TC_ARCHIVE_SENTINEL_HASH" ] || tc_fail 'Archive link target was changed'
/usr/bin/tar -Pczf "$TC_TEMP/path-archive/$TC_ASSET_A" "$TC_ARCHIVE_SENTINEL"
/usr/bin/tar -Ptf "$TC_TEMP/path-archive/$TC_ASSET_A" >"$TC_TEMP/path-archive/members"
/usr/bin/grep -Fxq "$TC_ARCHIVE_SENTINEL" "$TC_TEMP/path-archive/members" || tc_fail 'Unsafe path fixture lost its absolute member name'
printf 'new external content must survive extraction\n' >"$TC_ARCHIVE_SENTINEL"
TC_ARCHIVE_SENTINEL_HASH=$(tc_sha256 "$TC_ARCHIVE_SENTINEL")
printf '%s  %s\n' "$(tc_sha256 "$TC_TEMP/path-archive/$TC_ASSET_A")" "$TC_ASSET_A" >"$TC_TEMP/path-archive/checksums.txt"
tc_expect_failure archive-absolute-path tc_source pc_prepare_archive \
    "$TC_TEMP/path-archive/$TC_ASSET_A" "$TC_TEMP/path-archive/checksums.txt" \
    "$TC_ASSET_A" v90.0.0 "$TC_ARCH" "$TC_TEMP/path-archive/work"
/usr/bin/grep -Fq 'Unsafe archive member' "$TC_TEMP/archive-absolute-path.log" || tc_fail 'Absolute member was not rejected by the path check'
[ ! -e "$TC_TEMP/path-archive/work/generation" ] || tc_fail 'Unsafe path archive was extracted'
[ "$(tc_sha256 "$TC_ARCHIVE_SENTINEL")" = "$TC_ARCHIVE_SENTINEL_HASH" ] || tc_fail 'Absolute archive member changed external content'
tc_assert_absent

tc_case 'A valid checksum does not permit a binary with the wrong release version'
TC_ASSET_B=$(/bin/cat "$TC_TEMP/fixture-b/asset")
/bin/mkdir -p "$TC_TEMP/wrong-version/work"
/bin/cp "$TC_TEMP/fixture-a/$TC_ASSET_A" "$TC_TEMP/wrong-version/$TC_ASSET_B"
printf '%s  %s\n' "$(tc_sha256 "$TC_TEMP/wrong-version/$TC_ASSET_B")" "$TC_ASSET_B" >"$TC_TEMP/wrong-version/checksums.txt"
tc_expect_failure archive-version-mismatch tc_source pc_prepare_archive \
    "$TC_TEMP/wrong-version/$TC_ASSET_B" "$TC_TEMP/wrong-version/checksums.txt" \
    "$TC_ASSET_B" v90.0.1 "$TC_ARCH" "$TC_TEMP/wrong-version/work"
/usr/bin/grep -Fq 'unexpected release or platform' "$TC_TEMP/archive-version-mismatch.log" || tc_fail 'Mislabeled binary was not rejected by its actual version output'
tc_assert_absent

tc_case 'The command-link primitive never replaces a file, symlink, or directory'
TC_LINK_BODY=$(/bin/cat <<'BASH'
set -euo pipefail
source "$1"
pc_init_paths
PC_ROOT=$2
PC_BIN=$3
pc_create_command_link
BASH
)
TC_LINK_DIR="$TC_TEMP/link-primitive"
/bin/mkdir "$TC_LINK_DIR"
printf 'link destination sentinel\n' >"$TC_LINK_DIR/sentinel"
/bin/cp "$TC_LINK_DIR/sentinel" "$TC_LINK_DIR/destination"
tc_expect_failure link-existing-file /bin/bash -c "$TC_LINK_BODY" bash "$TC_SETUP" "$TC_LINK_DIR/root" "$TC_LINK_DIR/destination"
/usr/bin/cmp "$TC_LINK_DIR/sentinel" "$TC_LINK_DIR/destination" || tc_fail 'Link primitive replaced a regular file'
/bin/rm "$TC_LINK_DIR/destination"
/bin/ln -s "$TC_LINK_DIR/sentinel" "$TC_LINK_DIR/destination"
tc_expect_failure link-existing-symlink /bin/bash -c "$TC_LINK_BODY" bash "$TC_SETUP" "$TC_LINK_DIR/root" "$TC_LINK_DIR/destination"
[ "$(/usr/bin/readlink "$TC_LINK_DIR/destination")" = "$TC_LINK_DIR/sentinel" ] || tc_fail 'Link primitive replaced a symlink'
[ "$(/bin/cat "$TC_LINK_DIR/sentinel")" = 'link destination sentinel' ] || tc_fail 'Link primitive changed the symlink target'
/bin/rm "$TC_LINK_DIR/destination"
/bin/mkdir "$TC_LINK_DIR/destination"
/bin/cp "$TC_LINK_DIR/sentinel" "$TC_LINK_DIR/destination/keep.txt"
tc_expect_failure link-existing-directory /bin/bash -c "$TC_LINK_BODY" bash "$TC_SETUP" "$TC_LINK_DIR/root" "$TC_LINK_DIR/destination"
/usr/bin/cmp "$TC_LINK_DIR/sentinel" "$TC_LINK_DIR/destination/keep.txt" || tc_fail 'Link primitive replaced a directory or its content'
[[ ! -e "$TC_LINK_DIR/destination/pdfconcat" && ! -L "$TC_LINK_DIR/destination/pdfconcat" ]] || tc_fail 'Link primitive treated an existing directory as a container'
/bin/bash -c "$TC_LINK_BODY" bash "$TC_SETUP" "$TC_LINK_DIR/root" "$TC_LINK_DIR/new-command"
[ "$(/usr/bin/readlink "$TC_LINK_DIR/new-command")" = "$TC_LINK_DIR/root/current/payload/pdfconcat" ] || tc_fail 'Link primitive failed to create an absent destination'

tc_case 'Prepare a complete verified candidate'
tc_prepare fixture-a "$TC_TEMP/stage-a"
tc_source pc_validate_generation "$TC_TEMP/stage-a" "$TC_UID" 0

tc_case 'Preserve a foreign executable at the public destination'
/usr/bin/sudo /bin/mkdir -p /usr/local/bin
printf 'foreign executable sentinel\n' >"$TC_TEMP/foreign"
/usr/bin/sudo /usr/bin/install -o root -g wheel -m 755 "$TC_TEMP/foreign" "$TC_BIN"
tc_expect_failure foreign-executable tc_publish "$TC_TEMP/stage-a"
/usr/bin/cmp "$TC_TEMP/foreign" "$TC_BIN" || tc_fail 'Foreign executable was changed'
[ ! -e "$TC_ROOT" ] || tc_fail 'Refused installation created managed state'
/usr/bin/sudo /bin/rm "$TC_BIN"

tc_case 'Preserve a foreign symlink and its target'
/usr/bin/sudo /bin/bash -c 'umask 022; /bin/ln -s "$1" "$2"' bash "$TC_TEMP/foreign" "$TC_BIN"
TC_FOREIGN_LINK_METADATA=$(/usr/bin/sudo /usr/bin/stat -f '%u:%g:%Lp' "$TC_BIN")
[ "$(/usr/bin/sudo /usr/bin/readlink "$TC_BIN")" = "$TC_TEMP/foreign" ] || tc_fail 'Foreign link fixture has the wrong target'
[ "$(/usr/bin/readlink "$TC_BIN")" = "$TC_TEMP/foreign" ] || tc_fail 'Foreign link fixture is not readable by the ordinary test account'
tc_expect_failure foreign-symlink tc_publish "$TC_TEMP/stage-a"
[ "$(/usr/bin/readlink "$TC_BIN")" = "$TC_TEMP/foreign" ] || tc_fail 'Foreign symlink was changed'
[ "$(/usr/bin/sudo /usr/bin/stat -f '%u:%g:%Lp' "$TC_BIN")" = "$TC_FOREIGN_LINK_METADATA" ] || tc_fail 'Foreign symlink ownership or permissions were changed'
[ "$(/bin/cat "$TC_TEMP/foreign")" = 'foreign executable sentinel' ] || tc_fail 'Foreign symlink target was changed'
/usr/bin/sudo /bin/rm "$TC_BIN"

tc_case 'Publish a fresh installation and run an actual PDF build'
tc_publish "$TC_TEMP/stage-a"
tc_assert_version v90.0.0
tc_smoke_pdf
TC_HASH_A=$(tc_sha256 "$TC_BIN")
TC_CURRENT_A=$(/usr/bin/readlink "$TC_ROOT/current")
TC_INODE_A=$(/usr/bin/stat -L -f '%i' "$TC_BIN")
/bin/cp "$TC_BIN" "$TC_TEMP/installed-a"

tc_case 'Repeated installation remains valid and keeps the same current generation'
tc_prepare fixture-a "$TC_TEMP/stage-a-repeat"
tc_publish "$TC_TEMP/stage-a-repeat"
tc_assert_version v90.0.0
[ "$(/usr/bin/readlink "$TC_ROOT/current")" = "$TC_CURRENT_A" ] || tc_fail 'Same version unexpectedly changed generation identity'
tc_assert_command_hash "$TC_HASH_A"

tc_case 'A failed staged update preserves the working installation'
/bin/mkdir "$TC_TEMP/failed-update-stage"
tc_expect_failure installed-checksum-mismatch tc_source pc_prepare_archive \
    "$TC_TEMP/fixture-a/$TC_ASSET_A" "$TC_TEMP/bad-checksums.txt" "$TC_ASSET_A" \
    v90.0.0 "$TC_ARCH" "$TC_TEMP/failed-update-stage"
tc_assert_command_hash "$TC_HASH_A"
tc_assert_version v90.0.0

tc_prepare fixture-b "$TC_TEMP/stage-b"
TC_CURRENT_DIR=$(tc_current_dir)
/bin/cp "$TC_CURRENT_DIR/receipt" "$TC_TEMP/receipt-a"

tc_case 'A modified receipt blocks updates and removal while preserving the usable command'
printf 'unrecognized receipt record\n' >"$TC_TEMP/receipt-extra"
/bin/cat "$TC_TEMP/receipt-a" "$TC_TEMP/receipt-extra" >"$TC_TEMP/receipt-changed"
/usr/bin/sudo /usr/bin/install -o root -g wheel -m 644 "$TC_TEMP/receipt-changed" "$TC_CURRENT_DIR/receipt"
tc_expect_failure changed-receipt-update tc_publish "$TC_TEMP/stage-b"
tc_expect_failure changed-receipt-remove tc_root pc_remove_installation
/usr/bin/cmp "$TC_TEMP/receipt-changed" "$TC_CURRENT_DIR/receipt" || tc_fail 'Changed receipt was removed or rewritten'
tc_assert_command_hash "$TC_HASH_A"
tc_assert_running_version v90.0.0
tc_smoke_pdf
/usr/bin/sudo /usr/bin/install -o root -g wheel -m 644 "$TC_TEMP/receipt-a" "$TC_CURRENT_DIR/receipt"
tc_assert_version v90.0.0

tc_case 'Unknown files block updates and removal and remain intact'
/usr/bin/sudo /usr/bin/install -o root -g wheel -m 644 "$TC_TEMP/foreign" "$TC_CURRENT_DIR/payload/do-not-delete.txt"
tc_expect_failure unknown-file-update tc_publish "$TC_TEMP/stage-b"
tc_expect_failure unknown-file-remove tc_root pc_remove_installation
/usr/bin/cmp "$TC_TEMP/foreign" "$TC_CURRENT_DIR/payload/do-not-delete.txt" || tc_fail 'Unknown file was changed'
tc_assert_command_hash "$TC_HASH_A"
tc_assert_running_version v90.0.0
/usr/bin/sudo /bin/rm "$TC_CURRENT_DIR/payload/do-not-delete.txt"
tc_assert_version v90.0.0

tc_case 'A newline in an unknown filename cannot hide it from the inventory'
TC_ODD_FILE="$TC_CURRENT_DIR/payload/"$'pdfconcat\nreceipt'
/usr/bin/sudo /usr/bin/install -o root -g wheel -m 644 "$TC_TEMP/foreign" "$TC_ODD_FILE"
tc_expect_failure newline-file-update tc_publish "$TC_TEMP/stage-b"
tc_expect_failure newline-file-remove tc_root pc_remove_installation
for TC_LOG in "$TC_TEMP/newline-file-update.log" "$TC_TEMP/newline-file-remove.log"; do
    [[ $(/usr/bin/wc -l < "$TC_LOG") -eq 1 ]] || tc_fail 'Filename newline split a preservation diagnostic'
    /usr/bin/grep -Fq 'unrecognized file has been preserved' "$TC_LOG" || tc_fail 'Newline filename failed for another reason'
    /usr/bin/grep -Fq '\nreceipt' "$TC_LOG" || tc_fail 'Preservation diagnostic did not escape the filename newline'
done
/usr/bin/cmp "$TC_TEMP/foreign" "$TC_ODD_FILE" || tc_fail 'Unknown newline-containing file was changed'
tc_assert_command_hash "$TC_HASH_A"
tc_assert_running_version v90.0.0
/usr/bin/sudo /bin/rm "$TC_ODD_FILE"
tc_assert_version v90.0.0

tc_case 'A replaced executable is preserved and remains runnable after refused operations'
tc_replace_binary "$TC_TEMP/stage-b/payload/pdfconcat"
TC_CHANGED_HASH=$(tc_sha256 "$TC_BIN")
tc_expect_failure changed-binary-update tc_publish "$TC_TEMP/stage-b"
tc_expect_failure changed-binary-remove tc_root pc_remove_installation
tc_assert_command_hash "$TC_CHANGED_HASH"
tc_assert_running_version v90.0.1
tc_smoke_pdf
tc_replace_binary "$TC_TEMP/installed-a"
tc_assert_version v90.0.0

tc_case 'Upgrade changes the current generation and replaces the executable file'
tc_publish "$TC_TEMP/stage-b"
tc_assert_version v90.0.1
[ "$(/usr/bin/readlink "$TC_ROOT/current")" != "$TC_CURRENT_A" ] || tc_fail 'Upgrade did not change the current generation'
[ "$(/usr/bin/stat -L -f '%i' "$TC_BIN")" != "$TC_INODE_A" ] || tc_fail 'Upgrade reused the old executable inode'
tc_smoke_pdf

tc_case 'Retirement removes an old generation from managed state before partial cleanup'
# Reconstruct a complete old generation alongside the active release: the
# legitimate state immediately before cleanup in a successful update.
[[ ! -e "$TC_ROOT/$TC_CURRENT_A" && ! -L "$TC_ROOT/$TC_CURRENT_A" ]] || tc_fail 'The old generation was not cleaned after update'
tc_root /usr/bin/ditto "$TC_TEMP/stage-a" "$TC_ROOT/$TC_CURRENT_A"
tc_root pc_normalize_stage "$TC_ROOT/$TC_CURRENT_A"
tc_source pc_validate_state
TC_RETIRED_STAGE=$(/usr/bin/sudo /usr/bin/mktemp -d /private/tmp/pdfconcat-stage.XXXXXXXX)
tc_expect_failure retire-current tc_root pc_retire_generation "$(tc_current_dir)" "$TC_RETIRED_STAGE"
tc_assert_version v90.0.1
tc_root pc_retire_generation "$TC_ROOT/$TC_CURRENT_A" "$TC_RETIRED_STAGE"
[[ ! -e "$TC_ROOT/$TC_CURRENT_A" && ! -L "$TC_ROOT/$TC_CURRENT_A" ]] || tc_fail 'Retirement left the old generation in managed state'
# Stop at the real atomic filesystem boundary, then leave a partially removed
# retired tree. This models interrupted cleanup without a timing-dependent kill.
/usr/bin/sudo /bin/rm "$TC_RETIRED_STAGE/generation/payload/README.md"
tc_assert_version v90.0.1
tc_publish "$TC_TEMP/stage-b"
tc_assert_version v90.0.1

tc_case 'Refuse a downgrade and preserve the newer working command'
TC_HASH_B=$(tc_sha256 "$TC_BIN")
tc_expect_failure downgrade tc_publish "$TC_TEMP/stage-a"
tc_assert_command_hash "$TC_HASH_B"
tc_assert_version v90.0.1

tc_case 'A missing receipt blocks removal without deleting the usable command'
TC_CURRENT_DIR=$(tc_current_dir)
/bin/cp "$TC_CURRENT_DIR/receipt" "$TC_TEMP/receipt-b"
/usr/bin/sudo /bin/rm "$TC_CURRENT_DIR/receipt"
tc_expect_failure missing-receipt-remove /bin/bash "$TC_ROOT/current/setup.sh" uninstall --yes
tc_assert_command_hash "$TC_HASH_B"
tc_assert_running_version v90.0.1
[ -d "$TC_CURRENT_DIR/payload" ] || tc_fail 'Removal deleted content without its receipt'
/usr/bin/sudo /usr/bin/install -o root -g wheel -m 644 "$TC_TEMP/receipt-b" "$TC_CURRENT_DIR/receipt"
tc_assert_version v90.0.1

tc_case 'A second manager refuses an active native lock; an interrupted holder releases it'
{
    /usr/bin/sudo /bin/bash -c '
    set -eu
    umask 077
    exec 9<> "$2"
    /usr/bin/lockf -s -t 0 9
    /bin/sleep 20 &
    sleeper=$!
    trap '\''/bin/kill -TERM "$sleeper" 2>/dev/null || true; wait "$sleeper" 2>/dev/null || true; exit 143'\'' TERM HUP INT
    printf "%s\n" "$$" > "$1"
    wait "$sleeper"
' bash "$TC_TEMP/lock-ready" "$TC_LOCK"
} >"$TC_TEMP/lock-holder.log" 2>&1 &
TC_CHILD_PID=$!
for ((TC_POLL = 0; TC_POLL < 50; TC_POLL++)); do
    [ ! -f "$TC_TEMP/lock-ready" ] || break
    /bin/sleep 0.1
done
[ -f "$TC_TEMP/lock-ready" ] || tc_fail 'Native lock holder did not start'
TC_HOLDER_PID=$(/usr/bin/sudo /bin/cat "$TC_TEMP/lock-ready")
case "$TC_HOLDER_PID" in ''|*[!0-9]*) tc_fail 'Invalid lock holder PID' ;; esac
tc_expect_failure concurrent-removal /bin/bash "$TC_ROOT/current/setup.sh" uninstall --yes
tc_assert_command_hash "$TC_HASH_B"
tc_assert_version v90.0.1
/usr/bin/sudo /bin/kill -TERM "$TC_HOLDER_PID"
wait "$TC_CHILD_PID" 2>/dev/null || true
TC_CHILD_PID=
TC_HOLDER_PID=
tc_root pc_validate_state

tc_case 'The installed helper removes a missing-binary installation with an empty PATH'
/usr/bin/sudo /bin/rm "$TC_CURRENT_DIR/payload/pdfconcat"
[[ -L "$TC_BIN" && ! -e "$TC_BIN" ]] || tc_fail 'Missing-binary fixture is not a dangling managed command link'
/usr/bin/env PATH= /bin/bash "$TC_ROOT/current/setup.sh" uninstall --yes
tc_assert_absent

tc_case 'Repeated removal succeeds without recreating installation state'
/usr/bin/env PATH=/nonexistent /bin/bash "$TC_SETUP" uninstall --yes
tc_assert_absent

tc_case 'The isolated production worker verifies caller identity before creating state'
TC_ISOLATED="$TC_TEMP/isolated-prefix"
/bin/mkdir "$TC_ISOLATED"
printf 'Private lock target must remain unchanged.\n' >"$TC_TEMP/lock-sentinel"
TC_LOCK_SENTINEL_HASH=$(tc_sha256 "$TC_TEMP/lock-sentinel")
tc_expect_failure worker-caller tc_worker "$TC_ISOLATED" uninstall "$((TC_UID + 1))"
/usr/bin/grep -Fq 'requesting user could not be verified' "$TC_TEMP/worker-caller.log" || tc_fail 'Caller mismatch did not reach the caller guard'
[[ ! -e "$TC_ISOLATED/setup.lock" ]] || tc_fail 'Caller rejection created a lock'

tc_case 'The isolated worker preserves symlink and nonempty locks'
/usr/bin/sudo /bin/ln -s "$TC_TEMP/lock-sentinel" "$TC_ISOLATED/setup.lock"
TC_LOCK_LINK_METADATA=$(/usr/bin/sudo /usr/bin/stat -f '%u:%g:%Lp' "$TC_ISOLATED/setup.lock")
tc_expect_failure worker-link-lock tc_worker "$TC_ISOLATED" uninstall "$TC_UID"
/usr/bin/grep -Fq 'Expected a regular file' "$TC_TEMP/worker-link-lock.log" || tc_fail 'Symlink lock did not reach the file guard'
[[ $(/usr/bin/sudo /usr/bin/readlink "$TC_ISOLATED/setup.lock") == "$TC_TEMP/lock-sentinel" ]] || tc_fail 'Symlink lock target changed'
[[ $(/usr/bin/sudo /usr/bin/stat -f '%u:%g:%Lp' "$TC_ISOLATED/setup.lock") == "$TC_LOCK_LINK_METADATA" ]] || tc_fail 'Symlink lock metadata changed'
[[ $(tc_sha256 "$TC_TEMP/lock-sentinel") == "$TC_LOCK_SENTINEL_HASH" ]] || tc_fail 'Symlink lock target bytes changed'
/usr/bin/sudo /bin/rm "$TC_ISOLATED/setup.lock"
/usr/bin/sudo /usr/bin/install -o root -g wheel -m 600 "$TC_TEMP/lock-sentinel" "$TC_ISOLATED/setup.lock"
tc_expect_failure worker-nonempty-lock tc_worker "$TC_ISOLATED" uninstall "$TC_UID"
/usr/bin/grep -Fq 'lock file is unexpected' "$TC_TEMP/worker-nonempty-lock.log" || tc_fail 'Nonempty lock did not reach the size guard'
/usr/bin/sudo /usr/bin/cmp "$TC_TEMP/lock-sentinel" "$TC_ISOLATED/setup.lock"
[[ $(/usr/bin/sudo /usr/bin/stat -f '%u:%g:%Lp' "$TC_ISOLATED/setup.lock") == 0:0:600 ]] || tc_fail 'Nonempty lock metadata changed'
/usr/bin/sudo /bin/rm "$TC_ISOLATED/setup.lock"

tc_case 'The isolated worker refuses unsafe parent modes and unrelated ownership'
/bin/chmod 777 "$TC_ISOLATED"
TC_PARENT_METADATA=$(/usr/bin/stat -f '%u:%g:%Lp' "$TC_ISOLATED")
tc_expect_failure worker-writable-parent tc_worker "$TC_ISOLATED" uninstall "$TC_UID"
/usr/bin/grep -Fq 'allows writes by other users' "$TC_TEMP/worker-writable-parent.log" || tc_fail 'Writable parent did not reach its guard'
[[ $(/usr/bin/stat -f '%u:%g:%Lp' "$TC_ISOLATED") == "$TC_PARENT_METADATA" ]] || tc_fail 'Unsafe parent metadata changed'
[[ ! -e "$TC_ISOLATED/setup.lock" ]] || tc_fail 'Parent refusal created a lock'
/bin/chmod 755 "$TC_ISOLATED"
/usr/bin/sudo /usr/sbin/chown 65534:0 "$TC_ISOLATED"
tc_expect_failure worker-foreign-parent tc_worker "$TC_ISOLATED" uninstall "$TC_UID"
/usr/bin/grep -Fq 'belongs to another user' "$TC_TEMP/worker-foreign-parent.log" || tc_fail 'Foreign ownership did not reach its guard'
[[ $(/usr/bin/stat -f '%u:%g:%Lp' "$TC_ISOLATED") == 65534:0:755 ]] || tc_fail 'Foreign parent metadata changed'
[[ ! -e "$TC_ISOLATED/setup.lock" ]] || tc_fail 'Ownership refusal created a lock'
/usr/bin/sudo /usr/sbin/chown "$TC_UID":0 "$TC_ISOLATED"

tc_case 'The ACL primitive preserves custom access controls and accepts their absence'
# The production parent policy checks ACLs under /usr/local only. Exercise its
# ACL primitive privately rather than changing an existing shared parent.
/bin/mkdir "$TC_TEMP/acl-control"
tc_source pc_check_no_acl "$TC_TEMP/acl-control"
/bin/chmod +a "user:$(/usr/bin/id -un) allow read,readattr,readextattr,readsecurity" "$TC_TEMP/acl-control"
/bin/ls -lde "$TC_TEMP/acl-control" >"$TC_TEMP/acl-before.txt"
tc_expect_failure worker-parent-acl tc_source pc_check_no_acl "$TC_TEMP/acl-control"
/usr/bin/grep -Fq 'Custom access controls were preserved' "$TC_TEMP/worker-parent-acl.log" || tc_fail 'ACL rejection did not reach the ACL guard'
/bin/ls -lde "$TC_TEMP/acl-control" >"$TC_TEMP/acl-after.txt"
/usr/bin/cmp "$TC_TEMP/acl-before.txt" "$TC_TEMP/acl-after.txt"
/bin/chmod -N "$TC_TEMP/acl-control"
tc_source pc_check_no_acl "$TC_TEMP/acl-control"

tc_case 'The production worker accepts caller-owned parents and creates its own lock'
tc_worker "$TC_ISOLATED" uninstall "$TC_UID"
[[ $(/usr/bin/sudo /usr/bin/stat -f '%u:%g:%Lp:%z' "$TC_ISOLATED/setup.lock") == 0:0:600:0 ]] || tc_fail 'Worker created an unexpected lock'
[[ ! -e "$TC_ISOLATED/bin" && ! -e "$TC_ISOLATED/libexec" ]] || tc_fail 'Absent uninstall created destination directories'

tc_case 'The production worker accepts root-owned parents and creates missing destinations'
# Do not delete /usr/local/bin on a runner to simulate a newly installed Mac.
/usr/bin/sudo /usr/sbin/chown 0:0 "$TC_ISOLATED"
tc_worker "$TC_ISOLATED" install "$TC_UID" "$TC_TEMP/stage-a"
[ -x "$TC_ISOLATED/bin/pdfconcat" ] || tc_fail 'Missing-directory installation did not create a runnable command'
"$TC_ISOLATED/bin/pdfconcat" version
[ "$(/usr/bin/stat -f '%Lp' "$TC_ISOLATED/bin")" = 755 ] || tc_fail 'Created command parent has unexpected permissions'
tc_worker "$TC_ISOLATED" uninstall "$TC_UID"
[[ ! -e "$TC_ISOLATED/bin/pdfconcat" && ! -L "$TC_ISOLATED/bin/pdfconcat" ]] || tc_fail 'Isolated command remains'
[ ! -e "$TC_ISOLATED/libexec/pdfconcat" ] || tc_fail 'Isolated supporting files remain'
[[ -d "$TC_ISOLATED/bin" && -d "$TC_ISOLATED/libexec" ]] || tc_fail 'Removal deleted shared parent directories'
/usr/bin/sudo /bin/rm "$TC_ISOLATED/setup.lock"
/usr/bin/sudo /bin/rmdir "$TC_ISOLATED/bin" "$TC_ISOLATED/libexec" "$TC_ISOLATED"

tc_case 'Existing parent ownership and permissions, and shell configuration, remain unchanged'
tc_snapshot_environment >"$TC_TEMP/environment-after.txt"
/usr/bin/diff -u "$TC_TEMP/environment-before.txt" "$TC_TEMP/environment-after.txt"
tc_assert_absent
[[ -s "$TC_USER_PDF" && "$(tc_sha256 "$TC_USER_PDF")" = "$TC_USER_PDF_HASH" ]] || tc_fail 'Removal changed a user-generated PDF outside the installation'
[ "$(tc_sha256 "$TC_ARCHIVE_SENTINEL")" = "$TC_ARCHIVE_SENTINEL_HASH" ] || tc_fail 'Lifecycle changed the external archive sentinel'
/usr/bin/sudo /bin/bash -c '[[ -d "$1/generation" && ! -e "$1/generation/payload/README.md" ]]' bash "$TC_RETIRED_STAGE"
/usr/bin/sudo /bin/rm -rf -- "$TC_RETIRED_STAGE"
TC_RETIRED_STAGE=

printf '\nPASS: %s native lifecycle cases completed.\n' "$TC_CASES"
printf 'Manual acceptance remains: real Terminal password/confirmation interaction, browser quarantine and Gatekeeper or organization policy, Rosetta, and a clean macOS 15.0 account.\n'


