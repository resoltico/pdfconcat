#!/bin/bash
# PDFConcat's single macOS installation and removal route. Requires Bash 3.2.
# Sourceable lifecycle functions are also used by the native macOS tests.

pc_init_paths() {
    PC_ROOT=/usr/local/libexec/pdfconcat
    PC_BIN=/usr/local/bin/pdfconcat
    PC_LOCK=/var/run/pdfconcat-setup.lock
    PC_SCRIPT=${BASH_SOURCE[0]}
    PC_CURRENT_ID=''
    PC_CURRENT_VERSION=''
    PC_ROOT_PRESENT=0
}

pc_error() { printf 'PDFConcat setup: %s\n' "$*" >&2; return 1; }
pc_exists() { [[ -e "$1" || -L "$1" ]]; }
pc_sha256() { /usr/bin/shasum -a 256 "$1" | /usr/bin/awk '{print $1}'; }
pc_uid() { /usr/bin/stat -f '%u' "$1"; }
pc_mode() { /usr/bin/stat -f '%Lp' "$1"; }
pc_links() { /usr/bin/stat -f '%l' "$1"; }
pc_device() { /usr/bin/stat -f '%d' "$1"; }

pc_check_no_acl() {
    local metadata
    metadata=$(/bin/ls -lde "$1") || return
    # With -e, macOS ls prints each ACL entry on another line. Extended
    # attributes alone (including quarantine) do not add lines here.
    [[ "$metadata" != *$'\n'* ]] || {
        pc_error "Custom access controls were preserved: $1. Ask your Mac administrator for help."; return 1;
    }
}

pc_valid_hash() { [[ "$1" =~ ^[0-9a-f]{64}$ ]]; }
pc_valid_version() { [[ "$1" =~ ^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$ ]]; }
pc_valid_path() {
    [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9._/-]*$ && ${#1} -le 240 ]] || return 1
    case "/$1/" in *'/../'*|*'/./'*|*'//'*) return 1 ;; esac
    case "$1" in payload|payload/*|setup.sh) return 0 ;; *) return 1 ;; esac
}

pc_check_dir() {
    local path=$1 owner=$2
    [[ -d "$path" && ! -L "$path" ]] || { pc_error "Expected a real directory: $path"; return 1; }
    [[ $(pc_uid "$path") == "$owner" && $(pc_mode "$path") == 755 ]] || {
        pc_error "Unexpected ownership or permissions; nothing was removed: $path"; return 1;
    }
    pc_check_no_acl "$path"
}

pc_check_file() {
    local path=$1 owner=$2 mode=$3
    [[ -f "$path" && ! -L "$path" ]] || { pc_error "Expected a regular file: $path"; return 1; }
    [[ $(pc_uid "$path") == "$owner" && $(pc_mode "$path") == "$mode" && $(pc_links "$path") == 1 ]] || {
        pc_error "Unexpected ownership, permissions, or hard links: $path"; return 1;
    }
    pc_check_no_acl "$path"
}

# Do not follow a symlink in a destination prefix. Existing parent attributes
# belong to the Mac's administrator (and may belong to Homebrew), not to us.
pc_check_parents() {
    local path=$1
    while [[ "$path" != / ]]; do
        if pc_exists "$path"; then
            [[ -d "$path" && ! -L "$path" ]] || { pc_error "Destination parent is not a real directory: $path"; return 1; }
        fi
        path=${path%/*}
        [[ -n "$path" ]] || path=/
    done
}

pc_ensure_parent() {
    local path=$1
    pc_check_parents "$path" || return
    [[ -d "$path" ]] && return 0
    pc_ensure_parent "${path%/*}" || return
    /bin/mkdir -m 755 "$path"
}

# The inventory records every directory and regular file in the generation.
# Restricted relative names make the receipt unambiguous without evaluating it.
pc_make_receipt() {
    local dir=$1 version=$2 arch=$3 rel mode hash
    pc_valid_version "$version" || { pc_error 'Invalid release version.'; return 1; }
    [[ "$arch" == arm64 || "$arch" == amd64 ]] || { pc_error 'Invalid CPU architecture.'; return 1; }
    [[ -d "$dir/payload" && -f "$dir/payload/pdfconcat" && -f "$dir/setup.sh" ]] || {
        pc_error 'The staged release is incomplete.'; return 1;
    }
    {
        printf 'PDFCONCAT-MACOS-1\nversion %s\narchitecture %s\n' "$version" "$arch"
        while IFS= read -r -d '' rel; do
            rel=${rel#./}
            [[ "$rel" == . || "$rel" == receipt ]] && continue
            pc_valid_path "$rel" || { pc_error "Unsupported archive path: $rel"; return 1; }
            [[ ! -L "$dir/$rel" ]] || { pc_error "Links are not allowed in the release: $rel"; return 1; }
            if [[ -d "$dir/$rel" ]]; then
                /bin/chmod 755 "$dir/$rel" || return
                printf 'd %s\n' "$rel"
            elif [[ -f "$dir/$rel" && $(pc_links "$dir/$rel") == 1 ]]; then
                mode=644
                [[ "$rel" == payload/pdfconcat || "$rel" == setup.sh ]] && mode=755
                /bin/chmod "$mode" "$dir/$rel" || return
                hash=$(pc_sha256 "$dir/$rel") || return
                printf 'f %s %s %s\n' "$mode" "$hash" "$rel"
            else
                pc_error "Unsupported release file type: $rel"; return 1
            fi
        done < <(cd "$dir" && /usr/bin/find . -print0) | LC_ALL=C /usr/bin/sort
    } > "$dir/receipt" || return
    /bin/chmod 755 "$dir" || return
    /bin/chmod 644 "$dir/receipt"
}

pc_validate_generation() {
    local dir=$1 owner=$2 allow_missing=$3 line kind mode hash rel extra actual found item count=0
    local binary=0 helper=0 payload=0
    local names=()
    PC_GENERATION_VERSION=''
    PC_MISSING_BINARY=0
    pc_check_dir "$dir" "$owner" || return
    pc_check_file "$dir/receipt" "$owner" 644 || return
    [[ $(/usr/bin/stat -f '%z' "$dir/receipt") -le 131072 ]] || { pc_error 'The receipt is too large.'; return 1; }
    {
    IFS= read -r line <&3 || return
    [[ "$line" == PDFCONCAT-MACOS-1 ]] || { pc_error 'Unknown installation receipt format.'; return 1; }
    IFS= read -r line <&3 || return
    [[ "$line" == 'version '* ]] || { pc_error 'Invalid receipt version.'; return 1; }
    PC_GENERATION_VERSION=${line#version }
    pc_valid_version "$PC_GENERATION_VERSION" || { pc_error 'Invalid receipt version.'; return 1; }
    IFS= read -r line <&3 || return
    [[ "$line" == 'architecture arm64' || "$line" == 'architecture amd64' ]] || { pc_error 'Invalid receipt architecture.'; return 1; }
    while IFS= read -r line <&3 || [[ -n "$line" ]]; do
        kind='' mode='' hash='' rel='' extra=''
        case "$line" in
            'd '*) kind=d; rel=${line#d } ;;
            'f '*) IFS=' ' read -r kind mode hash rel extra <<< "$line" ;;
            *) pc_error 'Invalid inventory record.'; return 1 ;;
        esac
        if ! pc_valid_path "$rel" || [[ -n "$extra" ]]; then
            pc_error 'Invalid inventory path.'; return 1
        fi
        if [[ $count -gt 0 ]]; then
            for item in "${names[@]}"; do
                [[ "$item" != "$rel" ]] || { pc_error "Duplicate inventory entry: $rel"; return 1; }
            done
        fi
        names+=("$rel")
        count=$((count + 1))
        [[ $count -le 512 ]] || { pc_error 'Too many release files.'; return 1; }
        if [[ "$kind" == d ]]; then
            [[ "$rel" != setup.sh && "$rel" != payload/pdfconcat ]] || { pc_error 'A required executable is a directory.'; return 1; }
            pc_check_dir "$dir/$rel" "$owner" || return
            [[ "$rel" != payload ]] || payload=1
        else
            pc_valid_hash "$hash" || { pc_error 'Invalid inventory hash.'; return 1; }
            case "$rel" in
                setup.sh) [[ "$mode" == 755 ]] || return 1; helper=1 ;;
                payload/pdfconcat) [[ "$mode" == 755 ]] || return 1; binary=1 ;;
                payload/*) [[ "$mode" == 644 ]] || return 1 ;;
                *) pc_error 'Invalid inventory file.'; return 1 ;;
            esac
            if [[ "$rel" == payload/pdfconcat && "$allow_missing" == 1 ]] && ! pc_exists "$dir/$rel"; then
                PC_MISSING_BINARY=1
                continue
            fi
            pc_check_file "$dir/$rel" "$owner" "$mode" || return
            actual=$(pc_sha256 "$dir/$rel") || return
            [[ "$actual" == "$hash" ]] || { pc_error "A managed file was changed; it has been preserved: $dir/$rel"; return 1; }
        fi
    done
    } 3< "$dir/receipt"
    [[ "$binary" == 1 && "$helper" == 1 && "$payload" == 1 ]] || { pc_error 'The receipt omits required files.'; return 1; }
    while IFS= read -r -d '' rel; do
        rel=${rel#./}
        [[ "$rel" == . || "$rel" == receipt ]] && continue
        found=0
        for item in "${names[@]}"; do [[ "$item" != "$rel" ]] || found=1; done
        [[ "$found" == 1 ]] || { pc_error "An unrecognized file has been preserved: $dir/$rel"; return 1; }
    done < <(cd "$dir" && /usr/bin/find . -print0)
}

pc_validate_state() {
    local entry id target
    PC_CURRENT_ID=''
    PC_CURRENT_VERSION=''
    PC_ROOT_PRESENT=0
    pc_check_parents "${PC_ROOT%/*}" || return
    pc_check_parents "${PC_BIN%/*}" || return
    if ! pc_exists "$PC_ROOT"; then
        ! pc_exists "$PC_BIN" || { pc_error "A different installation already occupies $PC_BIN; nothing was changed."; return 1; }
        return 0
    fi
    PC_ROOT_PRESENT=1
    pc_check_dir "$PC_ROOT" 0 || return
    for entry in "$PC_ROOT"/* "$PC_ROOT"/.[!.]* "$PC_ROOT"/..?*; do
        pc_exists "$entry" || continue
        case "${entry##*/}" in current|releases) ;; *) pc_error "An unrecognized item has been preserved: $entry"; return 1 ;; esac
    done
    if pc_exists "$PC_ROOT/releases"; then
        pc_check_dir "$PC_ROOT/releases" 0 || return
        for entry in "$PC_ROOT/releases"/* "$PC_ROOT/releases"/.[!.]* "$PC_ROOT/releases"/..?*; do
            pc_exists "$entry" || continue
            id=${entry##*/}
            pc_valid_hash "$id" || { pc_error "Unrecognized release directory: $entry"; return 1; }
            pc_validate_generation "$entry" 0 1 || return
            [[ $(pc_sha256 "$entry/receipt") == "$id" ]] || { pc_error "The installation receipt was changed: $entry/receipt"; return 1; }
        done
    fi
    if pc_exists "$PC_ROOT/current"; then
        [[ -L "$PC_ROOT/current" && $(pc_uid "$PC_ROOT/current") == 0 && $(pc_mode "$PC_ROOT/current") == 755 ]] || {
            pc_error 'The current-release pointer has unexpected ownership or permissions; it has been preserved.'; return 1;
        }
        pc_check_no_acl "$PC_ROOT/current" || return
        target=$(/usr/bin/readlink "$PC_ROOT/current") || return
        id=${target#releases/}
        if [[ "$target" != "releases/$id" ]] || ! pc_valid_hash "$id"; then
            pc_error 'The current-release pointer is invalid.'; return 1
        fi
        pc_validate_generation "$PC_ROOT/releases/$id" 0 1 || return
        PC_CURRENT_ID=$id
        PC_CURRENT_VERSION=$PC_GENERATION_VERSION
    fi
    if pc_exists "$PC_BIN"; then
        [[ -L "$PC_BIN" && $(pc_uid "$PC_BIN") == 0 && $(pc_mode "$PC_BIN") == 755 ]] || {
            pc_error "An unexpected command or changed permissions were preserved: $PC_BIN"; return 1;
        }
        pc_check_no_acl "$PC_BIN" || return
        [[ $(/usr/bin/readlink "$PC_BIN") == "$PC_ROOT/current/payload/pdfconcat" ]] || { pc_error "An unexpected command link has been preserved: $PC_BIN"; return 1; }
        [[ -n "$PC_CURRENT_ID" ]] || { pc_error 'The command link has no managed current release.'; return 1; }
    fi
}

pc_retire_generation() {
    local dir=$1 stage=$2 id=${1##*/}
    if [[ "${dir%/*}" != "$PC_ROOT/releases" ]] || ! pc_valid_hash "$id"; then
        pc_error 'Refusing to remove an unexpected directory.'; return 1
    fi
    pc_validate_generation "$dir" 0 1 || return
    [[ $(pc_sha256 "$dir/receipt") == "$id" ]] || { pc_error 'The receipt does not match its directory.'; return 1; }
    if [[ -L "$PC_ROOT/current" && $(/usr/bin/readlink "$PC_ROOT/current") == "releases/$id" ]]; then
        pc_error 'Refusing to retire the active release.'; return 1
    fi
    [[ "${stage%/*}" == /private/tmp && "$stage" == /private/tmp/pdfconcat-stage.* &&
       -d "$stage" && ! -L "$stage" && $(pc_uid "$stage") == 0 && $(pc_mode "$stage") == 700 ]] || {
        pc_error 'A private retirement directory is required.'; return 1;
    }
    pc_check_no_acl "$stage" || return
    ! pc_exists "$stage/generation" || { pc_error 'The retirement destination is not empty.'; return 1; }
    [[ $(pc_device "$dir") == $(pc_device "$stage") ]] || {
        pc_error 'Temporary storage and the installed release must be on the same filesystem; the release was preserved.'; return 1;
    }
    # Move the complete checked inventory out atomically before recursive
    # deletion. An interruption cannot leave a torn receipt in releases/.
    /bin/mv "$dir" "$stage/generation"
}

pc_delete_generation() (
    local dir=$1 stage=''
    umask 077
    stage=$(/usr/bin/mktemp -d /private/tmp/pdfconcat-stage.XXXXXXXX) || return
    trap 'if [[ -n "$stage" && -d "$stage" && ! -L "$stage" ]]; then /bin/rm -rf -- "$stage"; fi' EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM HUP
    pc_retire_generation "$dir" "$stage" || return
    /bin/rm -rf -- "$stage" || return
    stage=''
)

pc_clean_old_generations() {
    local entry
    for entry in "$PC_ROOT/releases"/*; do
        pc_exists "$entry" || continue
        [[ "${entry##*/}" != "$PC_CURRENT_ID" ]] || continue
        pc_delete_generation "$entry" || return
    done
}

pc_version_at_least() {
    local newer=${1#v} older=${2#v} index
    local new_parts old_parts
    IFS=. read -r -a new_parts <<< "$newer"
    IFS=. read -r -a old_parts <<< "$older"
    for index in 0 1 2; do
        ((10#${new_parts[$index]} >= 10#${old_parts[$index]})) || return 1
        ((10#${new_parts[$index]} == 10#${old_parts[$index]})) || return 0
    done
}

pc_normalize_stage() {
    local dir=$1 rel mode
    # Only a newly copied, private staging tree is normalized. Never apply
    # recursive ownership or permission changes to /usr/local or live content.
    while IFS= read -r -d '' rel; do
        rel=${rel#./}
        [[ "$rel" == . || "$rel" == receipt ]] || pc_valid_path "$rel" || {
            pc_error "Unsupported staged path: $rel"; return 1;
        }
        [[ ! -L "$dir/$rel" ]] || { pc_error 'A staged link was refused.'; return 1; }
        if [[ -d "$dir/$rel" ]]; then
            mode=755
        elif [[ -f "$dir/$rel" && $(pc_links "$dir/$rel") == 1 ]]; then
            mode=644
            [[ "$rel" == setup.sh || "$rel" == payload/pdfconcat ]] && mode=755
        else
            pc_error 'A staged special file or hard link was refused.'; return 1
        fi
        /usr/sbin/chown 0:0 "$dir/$rel" || return
        /bin/chmod -N "$dir/$rel" || return
        /bin/chmod "$mode" "$dir/$rel" || return
    done < <(cd "$dir" && /usr/bin/find . -print0)
}

pc_create_command_link() (
    # BSD ln normally treats an existing destination directory as a container.
    # -F prevents that; -i with closed input refuses every replacement instead
    # of letting -F imply force. -h also refuses to follow a destination link.
    # If a destination appears later, symlink(2) fails atomically with EEXIST.
    # macOS applies umask to symlinks; ordinary users must be able to read this
    # link even though the surrounding privileged worker uses private defaults.
    umask 022
    /bin/ln -sFhi "$PC_ROOT/current/payload/pdfconcat" "$PC_BIN"
) < /dev/null

# The caller holds PC_LOCK. Tests source these functions and hold the same
# native lock around their isolated worker; main never accepts path overrides.
pc_publish_generation() (
    local candidate=$1 source_uid=$2 id stage='' new_version
    [[ $EUID == 0 && "$source_uid" =~ ^[0-9]+$ ]] || { pc_error 'A privileged installation worker is required.'; return 1; }
    pc_validate_generation "$candidate" "$source_uid" 0 || return
    new_version=$PC_GENERATION_VERSION
    id=$(pc_sha256 "$candidate/receipt") || return
    pc_validate_state || return
    if [[ -d "$PC_ROOT" && $(pc_device "$PC_ROOT") != $(pc_device "${PC_ROOT%/*}") ]]; then
        pc_error 'A separately mounted installation directory is not supported; nothing was changed.'; return 1
    fi
    if [[ -d "$PC_ROOT/releases" && $(pc_device "$PC_ROOT/releases") != $(pc_device "$PC_ROOT") ]]; then
        pc_error 'A separately mounted releases directory is not supported; nothing was changed.'; return 1
    fi
    if [[ -n "$PC_CURRENT_VERSION" ]] && ! pc_version_at_least "$new_version" "$PC_CURRENT_VERSION"; then
        pc_error "The installed release $PC_CURRENT_VERSION is newer than $new_version; no downgrade was performed."; return 1
    fi
    pc_ensure_parent "${PC_ROOT%/*}" || return
    pc_ensure_parent "${PC_BIN%/*}" || return
    stage=$(/usr/bin/mktemp -d /private/tmp/pdfconcat-stage.XXXXXXXX) || return
    trap 'if [[ -n "$stage" && -d "$stage" && ! -L "$stage" ]]; then /bin/rm -rf -- "$stage"; fi' EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM HUP
    [[ $(pc_device "$stage") == $(pc_device "${PC_ROOT%/*}") ]] || {
        pc_error 'Temporary storage and the installation must be on the same filesystem; nothing was installed.'; return 1;
    }
    /usr/bin/ditto "$candidate" "$stage/generation" || return
    pc_normalize_stage "$stage/generation" || return
    pc_validate_generation "$stage/generation" 0 0 || return
    [[ $(pc_sha256 "$stage/generation/receipt") == "$id" ]] || { pc_error 'Staging changed while it was being copied.'; return 1; }
    /usr/bin/codesign --verify --strict "$stage/generation/payload/pdfconcat" || {
        pc_error 'The staged executable signature is invalid.'; return 1;
    }
    # Recheck after the potentially slow copy, before modifying live state.
    pc_validate_state || return
    if [[ ! -d "$PC_ROOT" ]]; then
        /bin/mkdir -m 755 "$PC_ROOT" || return
        /bin/chmod -N "$PC_ROOT" || return
    fi
    if [[ ! -d "$PC_ROOT/releases" ]]; then
        /bin/mkdir -m 755 "$PC_ROOT/releases" || return
        /bin/chmod -N "$PC_ROOT/releases" || return
    fi
    if pc_exists "$PC_ROOT/releases/$id"; then
        pc_validate_generation "$PC_ROOT/releases/$id" 0 1 || return
        if [[ "$PC_MISSING_BINARY" == 1 ]]; then
            # A manually deleted executable is the one repair we permit. A
            # new inode restores it; all other generation content stays put.
            /bin/mv -f "$stage/generation/payload/pdfconcat" "$PC_ROOT/releases/$id/payload/pdfconcat" || return
        fi
    else
        /bin/mv "$stage/generation" "$PC_ROOT/releases/$id" || return
    fi
    # Both rename sources share the destination filesystem. BSD mv -h replaces
    # the symlink itself instead of following an existing directory symlink.
    (umask 022; /bin/ln -s "releases/$id" "$stage/current") || return
    /bin/mv -fh "$stage/current" "$PC_ROOT/current" || return
    if ! pc_exists "$PC_BIN"; then
        # Creating a symlink is atomic and refuses a destination that appeared
        # concurrently. Do not use a replacing rename for a first installation.
        pc_create_command_link || return
    fi
    pc_validate_state || return
    pc_clean_old_generations || {
        pc_error 'The new release is active, but old-release cleanup did not finish. Run setup again.'; return 1;
    }
    printf 'Installed PDFConcat %s.\nCommand: %s\n' "$new_version" "$PC_BIN"
)

pc_remove_installation() {
    local entry
    [[ $EUID == 0 ]] || { pc_error 'A privileged removal worker is required.'; return 1; }
    pc_validate_state || return
    if [[ "$PC_ROOT_PRESENT" == 0 ]]; then
        printf 'No installation managed by this setup was found. Nothing was removed.\n'
        return 0
    fi
    # Validation covers every generation before the first deletion. Unknown or
    # modified files abort removal instead of being swept up with the directory.
    if pc_exists "$PC_BIN"; then /bin/rm -- "$PC_BIN" || return; fi
    if pc_exists "$PC_ROOT/current"; then /bin/rm -- "$PC_ROOT/current" || return; fi
    if [[ -d "$PC_ROOT/releases" ]]; then
        for entry in "$PC_ROOT/releases"/*; do
            pc_exists "$entry" || continue
            pc_delete_generation "$entry" || return
        done
        /bin/rmdir "$PC_ROOT/releases" || return
    fi
    /bin/rmdir "$PC_ROOT" || return
    printf 'Removed the PDFConcat installation managed by this setup.\nYour PDFs, plans and reports were not removed.\n'
}

pc_download() {
    /usr/bin/curl -q --proto '=https' --proto-redir '=https' --tlsv1.2 \
        --fail --location --silent --show-error --connect-timeout 15 \
        --max-time 300 --retry 2 --retry-delay 2 --retry-max-time 600 \
        --max-filesize 104857600 --output "$2" "$1"
}

pc_version_from_release_url() {
    local url=$1 version
    case "$url" in
        https://github.com/resoltico/pdfconcat/releases/tag/*)
            version=${url#https://github.com/resoltico/pdfconcat/releases/tag/}
            ;;
        *) pc_error 'GitHub returned an unexpected release destination.'; return 1 ;;
    esac
    pc_valid_version "$version" || { pc_error 'GitHub returned an unsupported release version.'; return 1; }
    printf '%s\n' "$version"
}

pc_latest_version() {
    local destination
    # GitHub documents /releases/latest as its stable latest-release link.
    # Read the final URL rather than depending on anonymous REST API quotas.
    destination=$(/usr/bin/curl -q --proto '=https' --proto-redir '=https' --tlsv1.2 \
        --fail --location --head --silent --show-error --connect-timeout 15 \
        --max-time 60 --retry 2 --retry-delay 2 --retry-max-time 120 \
        --max-redirs 5 --output /dev/null --write-out '%{url_effective}' \
        https://github.com/resoltico/pdfconcat/releases/latest) || return
    pc_version_from_release_url "$destination"
}

pc_prepare_archive() {
    local archive=$1 checksums=$2 asset=$3 version=$4 arch=$5 dir=$6
    local expected='' actual hash name extra rel archive_count=0 matches=0
    [[ "$asset" == "pdfconcat_${version#v}_darwin_${arch}.tar.gz" ]] || { pc_error 'Unexpected archive name.'; return 1; }
    pc_valid_version "$version" || { pc_error 'Invalid release version.'; return 1; }
    [[ "$arch" == arm64 || "$arch" == amd64 ]] || { pc_error 'Invalid CPU architecture.'; return 1; }
    [[ -f "$archive" && ! -L "$archive" && -f "$checksums" && ! -L "$checksums" ]] || return 1
    while IFS=' ' read -r hash name extra || [[ -n "$hash$name$extra" ]]; do
        [[ "$name" == "$asset" ]] || continue
        if [[ -n "$extra" ]] || ! pc_valid_hash "$hash"; then
            pc_error 'The release checksum entry is invalid.'; return 1
        fi
        expected=$hash
        matches=$((matches + 1))
    done < "$checksums"
    [[ "$matches" == 1 ]] || { pc_error 'The release has no unique checksum for this Mac archive.'; return 1; }
    actual=$(pc_sha256 "$archive") || return
    [[ "$actual" == "$expected" ]] || { pc_error 'The download checksum does not match. Nothing was installed.'; return 1; }
    # Reject absolute paths, dot components, links and special files before
    # extracting. The fixed release layout has no whitespace in member names.
    /usr/bin/tar -tf "$archive" > "$dir/archive-list" || return
    while IFS= read -r rel || [[ -n "$rel" ]]; do
        rel=${rel%/}
        pc_valid_path "payload/$rel" || { pc_error "Unsafe archive member: $rel"; return 1; }
        archive_count=$((archive_count + 1))
        [[ "$archive_count" -le 512 ]] || { pc_error 'The release archive contains too many entries.'; return 1; }
    done < "$dir/archive-list"
    /usr/bin/tar -tvf "$archive" > "$dir/archive-types" || return
    while IFS= read -r rel; do
        case "$rel" in -?????????*|d?????????*) ;; *) pc_error 'The release archive contains a link or special file.'; return 1 ;; esac
    done < "$dir/archive-types"
    /bin/mkdir -m 755 "$dir/generation" "$dir/generation/payload" || return
    /usr/bin/tar -xf "$archive" --no-same-owner --no-same-permissions -C "$dir/generation/payload" || return
    [[ -f "$dir/generation/payload/pdfconcat" && ! -L "$dir/generation/payload/pdfconcat" ]] || { pc_error 'The archive does not contain the PDFConcat executable.'; return 1; }
    local description
    description=$(/usr/bin/file -b "$dir/generation/payload/pdfconcat") || return
    case "$arch:$description" in
        'arm64:Mach-O 64-bit executable arm64'*|'amd64:Mach-O 64-bit executable x86_64'*) ;;
        *) pc_error 'The downloaded executable does not match this Mac architecture.'; return 1 ;;
    esac
    # Ad hoc signing seals these bytes without identifying a publisher. It is
    # deliberately not Developer ID signing or notarization. Older releases
    # are supported by signing the verified staging copy on the user's Mac.
    /usr/bin/codesign --force --sign - --timestamp=none "$dir/generation/payload/pdfconcat" || return
    /usr/bin/codesign --verify --strict "$dir/generation/payload/pdfconcat" || return
    # Execute only as the downloading user. This catches unusable or mislabeled
    # releases before any administrator privileges are requested.
    "$dir/generation/payload/pdfconcat" version --json > "$dir/version.json" || {
        pc_error 'This Mac could not run the verified executable. Nothing was installed.'; return 1;
    }
    local reported_version reported_platform
    reported_version=$(/usr/bin/plutil -extract version raw -o - "$dir/version.json") || return
    reported_platform=$(/usr/bin/plutil -extract platform raw -o - "$dir/version.json") || return
    [[ "$reported_version" == "${version#v}" && "$reported_platform" == "darwin/$arch" ]] || {
        pc_error 'The executable reports an unexpected release or platform. Nothing was installed.'; return 1;
    }
    /usr/bin/ditto "$PC_SCRIPT" "$dir/generation/setup.sh" || return
    pc_make_receipt "$dir/generation" "$version" "$arch" || return
    pc_validate_generation "$dir/generation" "$(/usr/bin/id -u)" 0
}

pc_require_macos() {
    local version major
    [[ $(/usr/bin/uname -s) == Darwin ]] || { pc_error 'This setup is for macOS 15 or later.'; return 1; }
    version=$(/usr/bin/sw_vers -productVersion) || return
    major=${version%%.*}
    if ! [[ "$major" =~ ^[0-9]{1,3}$ ]] || ((10#$major < 15)); then
        pc_error 'macOS 15 or later is required.'; return 1
    fi
}

pc_architecture() {
    local arm
    arm=$(/usr/sbin/sysctl -n hw.optional.arm64 2>/dev/null) || arm=0
    if [[ "$arm" == 1 ]]; then
        printf 'arm64\n'
    else
        case "$(/usr/bin/uname -m)" in
            arm64) printf 'arm64\n' ;;
            x86_64) printf 'amd64\n' ;;
            *) pc_error 'This Mac CPU architecture is not supported.'; return 1 ;;
        esac
    fi
}

pc_confirm() {
    local question=$1 answer
    [[ "${PC_YES:-0}" != 1 ]] || return 0
    [[ -r /dev/tty && -w /dev/tty ]] || { pc_error 'An interactive Terminal is required, or explicitly use --yes.'; return 1; }
    printf '%s [y/N] ' "$question" > /dev/tty
    IFS= read -r answer < /dev/tty || return 1
    case "$answer" in y|Y|yes|YES) return 0 ;; *) printf 'Cancelled. Nothing was changed.\n'; return 2 ;; esac
}

pc_check_privileged_parents() {
    local path=$1 caller=$2 owner mode group
    pc_check_parents "$path" || return
    while [[ "$path" != / ]]; do
        if [[ -d "$path" ]]; then
            owner=$(pc_uid "$path") || return
            mode=$(pc_mode "$path") || return
            group=$(/usr/bin/stat -f '%g' "$path") || return
            [[ "$owner" == 0 || "$owner" == "$caller" ]] || {
                pc_error "Destination parent belongs to another user: $path. Ask your Mac administrator for help."; return 1;
            }
            if (( (8#$mode & 2) != 0 && (8#$mode & 512) == 0 )); then
                pc_error "Destination parent allows writes by other users: $path"; return 1
            fi
            if (( (8#$mode & 16) != 0 )) && [[ "$group" != 0 && "$group" != 80 ]]; then
                pc_error "Destination parent has an unsupported writable group: $path"; return 1
            fi
            case "$path" in /usr/local|/usr/local/*) pc_check_no_acl "$path" || return ;; esac
        fi
        path=${path%/*}
        [[ -n "$path" ]] || path=/
    done
}

pc_root_worker() (
    local action=$1 caller=$2 candidate=${3:-} status
    [[ $EUID == 0 && "$caller" =~ ^[0-9]+$ ]] || { pc_error 'Invalid privileged worker invocation.'; return 1; }
    [[ "${SUDO_UID:-0}" == "$caller" ]] || { pc_error 'The requesting user could not be verified.'; return 1; }
    pc_check_privileged_parents "${PC_ROOT%/*}" "$caller" || return
    pc_check_privileged_parents "${PC_BIN%/*}" "$caller" || return
    if pc_exists "$PC_LOCK"; then
        pc_check_file "$PC_LOCK" 0 600 || return
        [[ $(/usr/bin/stat -f '%z' "$PC_LOCK") == 0 ]] || { pc_error 'The installation lock file is unexpected.'; return 1; }
    fi
    umask 077
    # Keep the locked descriptor in this worker and its children. A separate
    # lock supervisor could be killed while its privileged child keeps running.
    exec 9<> "$PC_LOCK"
    if /usr/bin/lockf -s -t 0 9; then
        :
    else
        status=$?
        if [[ "$status" == 75 ]]; then
            pc_error 'Another PDFConcat setup is running. Let it finish, then try again. Do not delete the lock file.' || :
        fi
        return "$status"
    fi
    case "$action" in
        install) pc_publish_generation "$candidate" "$caller" ;;
        uninstall) pc_remove_installation ;;
        *) pc_error 'Unknown privileged action.'; return 1 ;;
    esac
)

pc_help() {
    /bin/cat <<'HELP'
PDFConcat setup for macOS 15 or later

Run without arguments for the menu, or use:
  /bin/bash macos.sh install [--yes]
  /bin/bash macos.sh uninstall [--yes]
  /bin/bash macos.sh --help

Install downloads and verifies the latest stable release, then installs or
updates /usr/local/bin/pdfconcat. Uninstall works without a download and removes
only an unchanged installation managed by this setup. Administrator access is
required for changes. Run this script as your ordinary user, without sudo.

--yes skips setup's confirmation. It does not bypass macOS administrator access
or replace a conflicting installation. No shell profiles or PATH are edited.
HELP
}

pc_install() (
    local work='' version arch asset base caller status resolved
    umask 077
    pc_validate_state || return
    arch=$(pc_architecture) || return
    caller=$(/usr/bin/id -u) || return
    work=$(/usr/bin/mktemp -d /private/tmp/pdfconcat-download.XXXXXXXX) || return
    trap 'if [[ -n "$work" && -d "$work" && ! -L "$work" ]]; then /bin/rm -rf -- "$work"; fi' EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM HUP
    printf 'Checking the latest stable PDFConcat release on GitHub...\n'
    version=$(pc_latest_version) || {
        pc_error 'Could not read the release information. Check your connection and try again.'; return 1;
    }
    asset="pdfconcat_${version#v}_darwin_${arch}.tar.gz"
    base="https://github.com/resoltico/pdfconcat/releases/download/$version"
    printf 'Downloading PDFConcat %s for %s and its checksums...\n' "$version" "$arch"
    pc_download "$base/$asset" "$work/$asset" || return
    pc_download "$base/checksums.txt" "$work/checksums.txt" || return
    printf 'Verifying the archive and testing the program as your user.\n'
    printf 'PDFConcat uses ad hoc signing; it is not notarized or signed with an Apple Developer ID.\n'
    pc_prepare_archive "$work/$asset" "$work/checksums.txt" "$asset" "$version" "$arch" "$work" || return
    printf '\nReady to install PDFConcat %s.\nCommand: %s\nSupporting files: %s\n' "$version" "$PC_BIN" "$PC_ROOT"
    printf 'An existing installation managed by this setup will be updated.\n'
    if pc_confirm 'Install or update PDFConcat?'; then
        :
    else
        status=$?
        [[ "$status" != 2 ]] || return 0
        return "$status"
    fi
    printf 'macOS may now ask for your administrator password.\n'
    /usr/bin/sudo /usr/bin/env -u BASH_ENV -u ENV /bin/bash "$PC_SCRIPT" __root-install "$caller" "$work/generation" || return
    resolved=$(command -v pdfconcat) || resolved=''
    if [[ "$resolved" == "$PC_BIN" ]]; then
        printf '\nOpen a new Terminal window, then check: pdfconcat version\n'
    elif [[ -n "$resolved" ]]; then
        printf '\nYour current PATH finds another command first: %s\n' "$resolved"
        printf 'The installed copy is available as: %s\n' "$PC_BIN"
        printf 'Help: https://github.com/resoltico/pdfconcat/blob/main/docs/MACOS.md#troubleshooting\n'
    else
        printf '\nYour current PATH does not find /usr/local/bin/pdfconcat.\n'
        printf 'The installed copy is available as: %s\n' "$PC_BIN"
        printf 'Help: https://github.com/resoltico/pdfconcat/blob/main/docs/MACOS.md#troubleshooting\n'
    fi
)

pc_uninstall() {
    local caller status
    pc_validate_state || return
    if [[ "$PC_ROOT_PRESENT" == 0 ]]; then
        printf 'No installation managed by this setup was found. Nothing was removed.\n'
        return 0
    fi
    printf 'Remove the managed command at %s and its supporting files at %s.\n' "$PC_BIN" "$PC_ROOT"
    printf 'Your PDFs, plans and reports outside that installation will be kept.\n'
    if pc_confirm 'Uninstall PDFConcat?'; then
        :
    else
        status=$?
        [[ "$status" != 2 ]] || return 0
        return "$status"
    fi
    caller=$(/usr/bin/id -u) || return
    printf 'macOS may now ask for your administrator password.\n'
    /usr/bin/sudo /usr/bin/env -u BASH_ENV -u ENV /bin/bash "$PC_SCRIPT" __root-uninstall "$caller"
}

pc_main() {
    local action=${1:-} choice script_dir
    pc_init_paths
    script_dir=$(cd -- "$(/usr/bin/dirname -- "$PC_SCRIPT")" && pwd -P) || return
    PC_SCRIPT="$script_dir/$(/usr/bin/basename -- "$PC_SCRIPT")"
    export LC_ALL=C
    case "$action" in
        --help|-h) [[ $# == 1 ]] || { pc_error 'Use --help by itself.'; return 1; }; pc_help; return ;;
        __root-install)
            [[ $# == 3 ]] || { pc_error 'Invalid privileged worker arguments.'; return 1; }
            pc_require_macos || return
            pc_root_worker install "$2" "$3"; return ;;
        __root-uninstall)
            [[ $# == 2 ]] || { pc_error 'Invalid privileged worker arguments.'; return 1; }
            pc_require_macos || return
            pc_root_worker uninstall "$2"; return ;;
    esac
    pc_require_macos || return
    [[ $EUID != 0 ]] || { pc_error 'Run setup as your ordinary user, without sudo. It requests administrator access only when needed.'; return 1; }
    PC_YES=0
    if [[ -n "$action" ]]; then
        [[ "$action" == install || "$action" == uninstall ]] || { pc_error 'Choose install or uninstall, or use --help.'; return 1; }
        if [[ $# == 2 && "$2" == --yes ]]; then
            PC_YES=1
        elif [[ $# != 1 ]]; then
            pc_error 'The only action option is --yes. Use --help for usage.'; return 1
        fi
    else
        [[ $# == 0 ]] || { pc_error 'An empty action is not valid.'; return 1; }
        [[ -r /dev/tty && -w /dev/tty ]] || { pc_error 'Open an interactive Terminal, or specify install or uninstall with --yes.'; return 1; }
        printf 'PDFConcat setup\n\n  1. Install or update PDFConcat\n  2. Uninstall PDFConcat\n  3. Cancel\n\nChoose 1, 2 or 3: ' > /dev/tty
        IFS= read -r choice < /dev/tty || return 1
        case "$choice" in
            1) action=install ;;
            2) action=uninstall ;;
            3|'') printf 'Cancelled. Nothing was changed.\n'; return 0 ;;
            *) pc_error 'Choose 1, 2 or 3 next time. Nothing was changed.'; return 1 ;;
        esac
    fi
    case "$action" in install) pc_install ;; uninstall) pc_uninstall ;; esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    set -euo pipefail
    pc_main "$@"
fi
