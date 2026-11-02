#!/bin/sh
# SPDX-License-Identifier: MPL-2.0
#
# Installs, updates or uninstalls PDFConcat for the current user on macOS and
# Linux. No administrator access is needed. See docs/INSTALL.md.
#
#   curl -fsSL https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh | sh
#   curl -fsSL https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh | sh -s -- --uninstall

set -eu

repo_url=https://github.com/resoltico/pdfconcat
docs_url=$repo_url/blob/main/docs/INSTALL.md
# Every line the installer adds to a shell startup file ends with this marker;
# uninstall removes exactly those lines.
marker='# added by the PDFConcat installer'

usage() {
    cat <<'EOF'
Install, update or uninstall PDFConcat for the current user.

Usage: install.sh [options]

Options:
  --version X.Y.Z    Install that release instead of the latest one
  --install-dir DIR  Install into DIR (default: $XDG_BIN_HOME or ~/.local/bin)
  --no-modify-path   Do not add the install directory to shell startup files
  --uninstall        Remove PDFConcat and the PATH lines this installer added
  -h, --help         Show this help

Environment: PDFCONCAT_VERSION, PDFCONCAT_INSTALL_DIR, PDFCONCAT_NO_MODIFY_PATH=1,
PDFCONCAT_DOWNLOAD_BASE (a release mirror URL or a local directory of release files).
EOF
}

# --- Presentation -----------------------------------------------------------

setup_ui() {
    bold='' dim='' red='' green='' yellow='' reset=''
    if [ -t 1 ] && [ -z "${NO_COLOR:-}" ] && [ "${TERM:-dumb}" != dumb ]; then
        esc=$(printf '\033')
        bold="${esc}[1m" dim="${esc}[2m" red="${esc}[31m" green="${esc}[32m" yellow="${esc}[33m" reset="${esc}[0m"
    fi
    case "${LC_ALL:-${LC_CTYPE:-${LANG:-}}}" in
        *UTF-8* | *utf-8* | *UTF8* | *utf8*) ok='✓' bad='✗' warn_mark='!' ;;
        *) ok='+' bad='x' warn_mark='!' ;;
    esac
}

title() { printf '\n%s%s%s\n\n' "$bold" "$1" "$reset"; }
step() { printf '  %s%s%s %s%-11s%s %s\n' "$green" "$ok" "$reset" "$bold" "$1" "$reset" "$2"; }
note() { printf '  %s%s%s %s%-11s%s %s\n' "$yellow" "$warn_mark" "$reset" "$bold" "$1" "$reset" "$2"; }
detail() { printf '                %s%s%s\n' "$dim" "$1" "$reset"; }
command_line() { printf '      %s\n' "$1"; }

die() {
    printf '\n  %s%s %s%s\n' "$red" "$bad" "$1" "$reset" >&2
    shift
    for line in "$@"; do printf '    %s\n' "$line" >&2; done
    printf '\n    Help: %s\n\n' "$docs_url" >&2
    exit 1
}

# Show paths under the home directory with ~ for readability.
tilde='~'
pretty_path() {
    case "$1" in
        "$HOME") printf '%s' "$tilde" ;;
        "$HOME"/*) printf '%s/%s' "$tilde" "${1#"$HOME"/}" ;;
        *) printf '%s' "$1" ;;
    esac
}

# --- Arguments --------------------------------------------------------------

parse_args() {
    version=${PDFCONCAT_VERSION:-}
    install_dir=${PDFCONCAT_INSTALL_DIR:-}
    modify_path=1
    [ "${PDFCONCAT_NO_MODIFY_PATH:-}" != 1 ] || modify_path=0
    action=install
    while [ $# -gt 0 ]; do
        case "$1" in
            --version)
                [ $# -ge 2 ] || die '--version needs a value, such as --version 0.7.0.'
                version=$2; shift ;;
            --version=*) version=${1#*=} ;;
            --install-dir)
                [ $# -ge 2 ] || die '--install-dir needs a directory.'
                install_dir=$2; shift ;;
            --install-dir=*) install_dir=${1#*=} ;;
            --no-modify-path) modify_path=0 ;;
            --uninstall) action=uninstall ;;
            -h | --help) usage; exit 0 ;;
            *) die "Unknown option: $1" 'Run with --help to see the available options.' ;;
        esac
        shift
    done
    version=${version#v}
    if [ -n "$version" ] && ! printf '%s\n' "$version" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'; then
        die "Not a release version: $version" 'Use a stable version such as 0.7.0.'
    fi
    [ -n "${HOME:-}" ] || die 'HOME is not set, so the default install directory is unknown.' 'Set HOME or use --install-dir.'
    if [ -z "$install_dir" ]; then
        install_dir=${XDG_BIN_HOME:-$HOME/.local/bin}
    fi
    case "$install_dir" in
        /*) ;;
        *) die "The install directory must be an absolute path: $install_dir" ;;
    esac
    install_dir=${install_dir%/}
    [ -n "$install_dir" ] || install_dir=/
    binary=$install_dir/pdfconcat
}

# --- Platform ---------------------------------------------------------------

detect_platform() {
    kernel=$(uname -s)
    machine=$(uname -m)
    case "$kernel" in
        Darwin)
            os=darwin
            macos_version=$(sw_vers -productVersion 2>/dev/null || printf unknown)
            case "${macos_version%%.*}" in
                '' | *[!0-9]*) die "Could not read the macOS version ($macos_version)." ;;
            esac
            [ "${macos_version%%.*}" -ge 15 ] ||
                die "PDFConcat needs macOS 15 or later; this Mac runs macOS $macos_version."
            # A Terminal running under Rosetta reports x86_64; install the
            # native build when the hardware supports arm64.
            if [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then
                machine=arm64
            fi
            ;;
        Linux) os=linux ;;
        MINGW* | MSYS* | CYGWIN* | Windows_NT)
            die 'This installer is for macOS and Linux.' \
                'On Windows, run this in PowerShell instead:' \
                "powershell -ExecutionPolicy ByPass -c \"irm $repo_url/releases/latest/download/install.ps1 | iex\""
            ;;
        *) die "PDFConcat has no release for $kernel." 'Build it from source; see the README.' ;;
    esac
    case "$machine" in
        x86_64 | amd64) arch=amd64 ;;
        arm64 | aarch64) arch=arm64 ;;
        *) die "PDFConcat has no $kernel release for the $machine processor." 'Build it from source; see the README.' ;;
    esac
    if [ "$os" = darwin ]; then
        case "$arch" in arm64) chip='Apple silicon' ;; *) chip='Intel' ;; esac
        system="macOS $macos_version · $chip ($arch)"
    else
        system="Linux · $machine ($arch)"
    fi
}

# --- Downloads and verification ---------------------------------------------

fetch() {
    case "$base" in
        https://*)
            if command -v curl >/dev/null 2>&1; then
                curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location \
                    --retry 3 --connect-timeout 20 --output "$2" "$base/$1"
            elif command -v wget >/dev/null 2>&1; then
                wget --https-only --quiet --output-document="$2" "$base/$1"
            else
                die 'Neither curl nor wget is available to download PDFConcat.' 'Install curl, then run the installer again.'
            fi
            ;;
        *) cp "$base/$1" "$2" ;;
    esac
}

sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | awk '{print $1}'
    elif command -v openssl >/dev/null 2>&1; then
        openssl dgst -sha256 "$1" | awk '{print $NF}'
    else
        die 'No SHA-256 tool (sha256sum, shasum or openssl) is available to verify the download.'
    fi
}

file_size() {
    wc -c < "$1" | awk '{ if ($1 >= 1048576) printf "%.1f MB", $1 / 1048576; else printf "%d KB", $1 / 1024 }'
}

# Prints "pdfconcat X.Y.Z" for a working PDFConcat executable, or fails.
reported_version() {
    out=$("$1" version 2>/dev/null) || return 1
    out=$(printf '%s\n' "$out" | head -n 1)
    case "$out" in
        'pdfconcat '*) printf '%s\n' "$out" | awk '{print $2}' ;;
        *) return 1 ;;
    esac
}

download_and_verify() {
    asset="pdfconcat_${os}_${arch}.tar.gz"
    if [ -n "${PDFCONCAT_DOWNLOAD_BASE:-}" ]; then
        base=${PDFCONCAT_DOWNLOAD_BASE%/}
        case "$base" in
            https://* | /*) ;;
            *) die 'PDFCONCAT_DOWNLOAD_BASE must be an https:// URL or an absolute directory.' ;;
        esac
        source_label=$(pretty_path "$base")
    elif [ -n "$version" ]; then
        base=$repo_url/releases/download/v$version
        source_label="the v$version release"
    else
        base=$repo_url/releases/latest/download
        source_label='the latest release'
    fi

    fetch "$asset" "$tmp/$asset" ||
        die "Could not download $asset from $source_label." \
            'Check your internet connection and that the release exists:' "$repo_url/releases"
    fetch checksums.txt "$tmp/checksums.txt" ||
        die "Could not download checksums.txt from $source_label."
    step Downloaded "$asset · $(file_size "$tmp/$asset")"
    detail "from $source_label"

    expected=$(awk -v name="$asset" '$2 == name || $2 == "*" name { print $1 }' "$tmp/checksums.txt")
    [ "$(printf '%s\n' "$expected" | grep -c .)" = 1 ] ||
        die "checksums.txt has no unique entry for $asset." 'Nothing was installed.'
    actual=$(sha256_of "$tmp/$asset")
    [ "$actual" = "$expected" ] ||
        die "The download does not match its published SHA-256 checksum." \
            'Nothing was installed. Try again; if this persists, please report it:' "$repo_url/issues"
    step Verified 'SHA-256 matches the release checksums'

    # Extract only the executable; other archive members are never written.
    mkdir "$tmp/extract"
    tar -xzf "$tmp/$asset" -C "$tmp/extract" pdfconcat 2>/dev/null ||
        die "$asset does not contain the pdfconcat executable."
    candidate=$tmp/extract/pdfconcat
    [ -f "$candidate" ] && [ ! -h "$candidate" ] || die "$asset does not contain a regular pdfconcat file."
    chmod 755 "$candidate"

    new_version=$(reported_version "$candidate") ||
        die 'The downloaded program does not run on this system.' 'Nothing was installed.' \
            "Running it directly shows the reason: $candidate version"
    if [ -n "$version" ] && [ "$new_version" != "$version" ]; then
        die "The downloaded program reports version $new_version, not $version." 'Nothing was installed.'
    fi
    step Tested "pdfconcat $new_version runs on this system"
}

# --- Installation -----------------------------------------------------------

install_binary() {
    if [ -h "$binary" ] || { [ -e "$binary" ] && [ ! -f "$binary" ]; }; then
        die "$(pretty_path "$binary") exists and is not a regular file, so it was left alone." \
            'Remove it yourself, or choose another location with --install-dir.'
    fi
    old_version=''
    if [ -f "$binary" ]; then
        old_version=$(reported_version "$binary" || printf 'an unknown version')
    fi
    mkdir -p "$install_dir" || die "Could not create $(pretty_path "$install_dir")."
    # Copy beside the destination, then rename: the switch is atomic and a
    # running pdfconcat keeps its old file until it exits.
    staged="$install_dir/.pdfconcat.$$"
    if ! { cp "$candidate" "$staged" && chmod 755 "$staged" && mv -f "$staged" "$binary"; }; then
        rm -f "$staged"
        die "Could not write $(pretty_path "$binary")." 'Check that you can write to that directory.'
    fi
    case "$old_version" in
        '') step Installed "$(pretty_path "$binary")" ;;
        "$new_version") step Installed "$(pretty_path "$binary") (reinstalled $new_version)" ;;
        *) step Installed "$(pretty_path "$binary") (updated from $old_version)" ;;
    esac
}

# The PATH expression written to startup files, preferring $HOME over a
# hard-coded home directory.
path_expression() {
    case "$install_dir" in
        "$HOME"/*) printf '$HOME/%s' "${install_dir#"$HOME"/}" ;;
        *) printf '%s' "$install_dir" ;;
    esac
}

posix_line() {
    dir=$(path_expression)
    printf 'case ":${PATH}:" in *:"%s":*) ;; *) export PATH="%s:$PATH" ;; esac %s' "$dir" "$dir" "$marker"
}

fish_file() { printf '%s/fish/conf.d/pdfconcat.fish' "${XDG_CONFIG_HOME:-$HOME/.config}"; }

# Startup files for the user's login shell. Bash reads one login file and,
# for interactive non-login shells, ~/.bashrc; both get the guarded line.
startup_files() {
    case "${SHELL:-}" in
        */zsh) printf '%s\n' "${ZDOTDIR:-$HOME}/.zshrc" ;;
        */bash)
            printf '%s\n' "$HOME/.bashrc"
            for login in "$HOME/.bash_profile" "$HOME/.bash_login" "$HOME/.profile"; do
                if [ -e "$login" ]; then printf '%s\n' "$login"; return; fi
            done
            printf '%s\n' "$HOME/.profile"
            ;;
        */fish) ;;
        *) printf '%s\n' "$HOME/.profile" ;;
    esac
}

append_line() {
    file=$1 line=$2
    if [ -f "$file" ] && grep -Fxq -- "$line" "$file"; then
        return 1
    fi
    # Keep the user's last line intact when it lacks a trailing newline.
    if [ -s "$file" ] && [ "$(tail -c 1 "$file" | wc -l | tr -d ' ')" = 0 ]; then
        printf '\n' >> "$file"
    fi
    printf '%s\n' "$line" >> "$file"
}

configure_path() {
    on_path=0
    case ":$PATH:" in *":$install_dir:"*) on_path=1 ;; esac
    added=''
    if [ "$on_path" = 1 ]; then
        step PATH "already includes $(pretty_path "$install_dir")"
        return
    fi
    if [ "$modify_path" = 0 ]; then
        note PATH "$(pretty_path "$install_dir") is not on your PATH (left unchanged)"
        return
    fi
    case "$install_dir" in
        *[\"\\\$\`]*)
            note PATH "$(pretty_path "$install_dir") contains characters that are unsafe in startup files"
            modify_path=0
            return
            ;;
    esac
    line=$(posix_line)
    startup_files > "$tmp/startup-files"
    while IFS= read -r file; do
        if append_line "$file" "$line"; then added="$added $(pretty_path "$file")"; fi
    done < "$tmp/startup-files"
    fish=$(fish_file)
    if [ "${SHELL:-}" != "${SHELL%/fish}" ] || [ -d "${fish%/conf.d/*}" ]; then
        if [ ! -f "$fish" ]; then
            mkdir -p "${fish%/*}"
            dir=$(path_expression)
            printf '%s\nif not contains -- "%s" $PATH\n    set -gx PATH "%s" $PATH\nend\n' "$marker" "$dir" "$dir" > "$fish"
            added="$added $(pretty_path "$fish")"
        fi
    fi
    if [ -n "$added" ]; then
        step PATH "added $(pretty_path "$install_dir") via${added}"
    else
        step PATH "$(pretty_path "$install_dir") is already set up in your shell startup files"
    fi
}

warn_if_shadowed() {
    found=$(command -v pdfconcat 2>/dev/null || true)
    if [ "$on_path" = 1 ] && [ -n "$found" ] && [ "$found" != "$binary" ]; then
        printf '\n'
        note Note "your PATH finds another pdfconcat first: $(pretty_path "$found")"
        detail "Remove that copy, or run this one as $(pretty_path "$binary")."
    fi
}

finish_install() {
    printf '\n  %sPDFConcat %s is installed.%s\n\n' "$bold" "$new_version" "$reset"
    if [ "$on_path" = 1 ]; then
        printf '  Get started:\n'
    elif [ "$modify_path" = 1 ]; then
        printf '  Open a new terminal, or run this to use it now:\n'
        command_line "export PATH=\"$(path_expression):\$PATH\""
        printf '\n  Then get started:\n'
    else
        printf '  Run it by its full path, or add %s to your PATH:\n' "$(pretty_path "$install_dir")"
        command_line "$binary --help"
        printf '\n  Get started:\n'
    fi
    command_line 'pdfconcat --help'
    command_line 'pdfconcat build -o blank.pdf --blank --fit-to A4'
    printf '\n  %sGuide:     %s%s\n' "$dim" "$docs_url" "$reset"
    uninstall_args='--uninstall'
    [ "$install_dir" = "${XDG_BIN_HOME:-$HOME/.local/bin}" ] || uninstall_args="$uninstall_args --install-dir '$install_dir'"
    printf '  %sUninstall: curl -fsSL %s/releases/latest/download/install.sh | sh -s -- %s%s\n\n' "$dim" "$repo_url" "$uninstall_args" "$reset"
}

# --- Uninstall --------------------------------------------------------------

remove_marked_lines() {
    file=$1
    [ -f "$file" ] && grep -Fq -- "$marker" "$file" || return 1
    filtered="$tmp/startup"
    grep -vF -- "$marker" "$file" > "$filtered" || true
    # Rewrite in place so a symlinked startup file stays a symlink.
    cat "$filtered" > "$file"
}

uninstall() {
    title 'PDFConcat uninstaller'
    removed=0
    if [ -h "$binary" ] || { [ -e "$binary" ] && [ ! -f "$binary" ]; }; then
        die "$(pretty_path "$binary") is not a regular file, so it was left alone." \
            'This installer only removes the file it installs.'
    fi
    if [ -f "$binary" ]; then
        old_version=$(reported_version "$binary" || printf 'unknown version')
        rm -f "$binary"
        step Removed "$(pretty_path "$binary") ($old_version)"
        removed=1
    fi
    cleaned=''
    for file in "${ZDOTDIR:-$HOME}/.zshrc" "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.bash_login" "$HOME/.profile"; do
        if remove_marked_lines "$file"; then cleaned="$cleaned $(pretty_path "$file")"; fi
    done
    fish=$(fish_file)
    if [ -f "$fish" ] && [ "$(head -n 1 "$fish")" = "$marker" ]; then
        rm -f "$fish"
        cleaned="$cleaned $(pretty_path "$fish")"
    fi
    if [ -n "$cleaned" ]; then
        step PATH "removed the installer's lines from${cleaned}"
        removed=1
    fi
    if [ "$removed" = 0 ]; then
        printf '  PDFConcat is not installed in %s. Nothing was changed.\n\n' "$(pretty_path "$install_dir")"
        return
    fi
    printf '\n  %sPDFConcat has been uninstalled.%s\n' "$bold" "$reset"
    printf '  Your PDFs, plans and reports were not touched.\n'
    found=$(command -v pdfconcat 2>/dev/null || true)
    if [ -n "$found" ] && [ "$found" != "$binary" ]; then
        printf '\n'
        note Note "another pdfconcat remains at $(pretty_path "$found"); it was not installed by this script."
    fi
    printf '\n'
}

# --- Main -------------------------------------------------------------------

main() {
    setup_ui
    parse_args "$@"
    tmp=$(mktemp -d "${TMPDIR:-/tmp}/pdfconcat-install.XXXXXX") || die 'Could not create a temporary directory.'
    trap 'rm -rf "$tmp"' EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM HUP
    if [ "$action" = uninstall ]; then
        uninstall
        return
    fi
    title 'PDFConcat installer'
    detect_platform
    step System "$system"
    download_and_verify
    install_binary
    configure_path
    warn_if_shadowed
    finish_install
}

main "$@"
