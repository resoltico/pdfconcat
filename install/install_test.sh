#!/bin/bash
# SPDX-License-Identifier: MPL-2.0
#
# Exercises install.sh against release files from a GoReleaser dist directory.
# Every case uses a temporary HOME; the real home directory, shell startup
# files and PATH are never read or changed.
#
#   bash install/install_test.sh /absolute/path/to/dist

set -euo pipefail

dist=${1:?usage: install_test.sh DIST_DIR}
[[ "$dist" == /* && -f "$dist/checksums.txt" ]] || { echo "Expected an absolute dist directory with checksums.txt: $dist" >&2; exit 2; }
installer=$(cd "$(dirname "$0")" && pwd)/install.sh
work=$(mktemp -d "${TMPDIR:-/tmp}/pdfconcat-install-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
base_path=/usr/bin:/bin:/usr/sbin:/sbin
failures=0
case_number=0

fail() { echo "    FAIL: $*" >&2; failures=$((failures + 1)); }
check() { local why=$1; shift; "$@" || fail "$why"; }
begin() {
    case_number=$((case_number + 1))
    home=$work/home$case_number
    mkdir -p "$home"
    echo "--- $*"
}

# run_installer SHELL [ARGS...]: runs the installer as a pipe would, from stdin.
run_installer() {
    local login_shell=$1; shift
    env -i HOME="$home" PATH="${test_path:-$base_path}" SHELL="$login_shell" TMPDIR="$work" \
        PDFCONCAT_DOWNLOAD_BASE="${download_base:-$dist}" sh -s -- "$@" < "$installer"
}

marker_count() { if [[ -f "$1" ]]; then grep -cF '# added by the PDFConcat installer' "$1" || true; else echo 0; fi; }
resolve_in() { env -i HOME="$home" PATH="$base_path" "$@" 2>/dev/null | tail -n 1; }

case "$(uname -s)" in Darwin) os=darwin ;; *) os=linux ;; esac
case "$(uname -m)" in x86_64 | amd64) arch=amd64 ;; *) arch=arm64 ;; esac
tar -xzf "$dist/pdfconcat_${os}_${arch}.tar.gz" -C "$work" pdfconcat
expected_version=$("$work/pdfconcat" version | awk '{print $2; exit}')
rm -f "$work/pdfconcat"
echo "Release under test reports version: ${expected_version:-unknown}"

begin 'zsh: fresh install, PATH line, new-shell resolution'
if command -v zsh > /dev/null; then
    printf 'export KEEP=1' > "$home/.zshrc"   # no trailing newline on purpose
    check 'install succeeds' run_installer /bin/zsh > "$work/out"
    check 'binary installed' test -x "$home/.local/bin/pdfconcat"
    check 'reports the release version' grep -q "PDFConcat $expected_version is installed" "$work/out"
    check 'user line kept intact' grep -qx 'export KEEP=1' "$home/.zshrc"
    check 'one PATH line' test "$(marker_count "$home/.zshrc")" = 1
    check 'new zsh finds the command' test "$(resolve_in zsh -i -c 'command -v pdfconcat')" = "$home/.local/bin/pdfconcat"
    check 'installed program builds a PDF' "$home/.local/bin/pdfconcat" build -o "$home/blank.pdf" --blank --fit-to A4
    check 'PDF written' test -s "$home/blank.pdf"
    check 'reinstall succeeds' run_installer /bin/zsh > "$work/out"
    check 'reinstall reported' grep -q 'reinstalled' "$work/out"
    check 'still one PATH line' test "$(marker_count "$home/.zshrc")" = 1
    check 'uninstall succeeds' run_installer /bin/zsh --uninstall > "$work/out"
    check 'binary removed' test ! -e "$home/.local/bin/pdfconcat"
    check 'PATH line removed' test "$(marker_count "$home/.zshrc")" = 0
    check 'user line still present' grep -qx 'export KEEP=1' "$home/.zshrc"
    check 'second uninstall is a no-op' run_installer /bin/zsh --uninstall > "$work/out"
    check 'no-op reported' grep -q 'Nothing was changed' "$work/out"
else
    echo '    (zsh unavailable; skipped)'
fi

begin 'bash: .bashrc and login file, symlinked startup file stays a symlink'
mkdir -p "$home/dotfiles"
echo 'alias keep=true' > "$home/dotfiles/bashrc"
ln -s dotfiles/bashrc "$home/.bashrc"
check 'install succeeds' run_installer /bin/bash > "$work/out"
check '.bashrc line' test "$(marker_count "$home/dotfiles/bashrc")" = 1
check '.bashrc is still a symlink' test -L "$home/.bashrc"
check 'login file line' test "$(marker_count "$home/.profile")" = 1
check 'login bash finds the command' test "$(resolve_in bash -l -c 'command -v pdfconcat')" = "$home/.local/bin/pdfconcat"
check 'uninstall succeeds' run_installer /bin/bash --uninstall > "$work/out"
check '.bashrc line removed' test "$(marker_count "$home/dotfiles/bashrc")" = 0
check '.bashrc is still a symlink' test -L "$home/.bashrc"
check 'user alias kept' grep -qx 'alias keep=true' "$home/dotfiles/bashrc"
check 'login file line removed' test "$(marker_count "$home/.profile")" = 0

begin 'fish: own conf.d file'
check 'install succeeds' run_installer /usr/bin/fish > "$work/out"
check 'fish file written' test -f "$home/.config/fish/conf.d/pdfconcat.fish"
if command -v fish > /dev/null; then
    check 'fish finds the command' test "$(resolve_in fish -c 'command -v pdfconcat')" = "$home/.local/bin/pdfconcat"
fi
check 'uninstall succeeds' run_installer /usr/bin/fish --uninstall > "$work/out"
check 'fish file removed' test ! -e "$home/.config/fish/conf.d/pdfconcat.fish"

begin 'already on PATH: startup files untouched'
test_path="$home/.local/bin:$base_path" check 'install succeeds' run_installer /bin/sh > "$work/out"
check 'no startup file created' test ! -e "$home/.profile"
check 'reported as already on PATH' grep -q 'already includes' "$work/out"

begin '--no-modify-path and a custom install directory'
check 'install succeeds' run_installer /bin/sh --no-modify-path --install-dir "$home/tools" > "$work/out"
check 'binary in custom directory' test -x "$home/tools/pdfconcat"
check 'no startup file created' test ! -e "$home/.profile"
check 'uninstall hint names the directory' grep -q -- "--install-dir '$home/tools'" "$work/out"
check 'uninstall succeeds' run_installer /bin/sh --uninstall --install-dir "$home/tools" > "$work/out"
check 'binary removed' test ! -e "$home/tools/pdfconcat"

begin 'checksum mismatch leaves the existing installation untouched'
check 'install succeeds' run_installer /bin/sh --no-modify-path > "$work/out"
before=$(cksum < "$home/.local/bin/pdfconcat")
tampered=$work/tampered
mkdir -p "$tampered"
cp "$dist"/pdfconcat_*.tar.gz "$tampered/"
awk '{ print "0000000000000000000000000000000000000000000000000000000000000000", $2 }' "$dist/checksums.txt" > "$tampered/checksums.txt"
if download_base=$tampered run_installer /bin/sh --no-modify-path > "$work/out" 2>&1; then fail 'tampered install must fail'; fi
check 'mismatch reported' grep -q 'does not match its published SHA-256' "$work/out"
check 'existing binary unchanged' test "$(cksum < "$home/.local/bin/pdfconcat")" = "$before"

begin 'missing release files and version mismatches fail cleanly'
mkdir -p "$work/empty"
if download_base=$work/empty run_installer /bin/sh > "$work/out" 2>&1; then fail 'empty release must fail'; fi
check 'download failure reported' grep -q 'Could not download' "$work/out"
if run_installer /bin/sh --version 999.0.0 > "$work/out" 2>&1; then fail 'version mismatch must fail'; fi
check 'version mismatch reported' grep -q 'not 999.0.0' "$work/out"
if run_installer /bin/sh --version latest > "$work/out" 2>&1; then fail 'invalid version must fail'; fi
check 'nothing installed' test ! -e "$home/.local/bin/pdfconcat"

begin 'non-regular destinations are left alone'
mkdir -p "$home/.local/bin"
ln -s /bin/sh "$home/.local/bin/pdfconcat"
if run_installer /bin/sh > "$work/out" 2>&1; then fail 'symlink destination must be refused'; fi
check 'symlink kept' test -L "$home/.local/bin/pdfconcat"
if run_installer /bin/sh --uninstall > "$work/out" 2>&1; then fail 'symlink uninstall must be refused'; fi
check 'symlink still kept' test -L "$home/.local/bin/pdfconcat"

echo
if [[ "$failures" -gt 0 ]]; then
    echo "$failures check(s) failed." >&2
    exit 1
fi
echo "All installer checks passed."
