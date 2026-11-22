// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

type pathEntry struct {
	file, parent     fs.FileInfo
	parentPath, leaf string
}

func (a pathEntry) aliases(b pathEntry, protectShortNames bool) bool {
	if a.file != nil && b.file != nil {
		return os.SameFile(a.file, b.file)
	}
	if a.parent == nil || b.parent == nil || !os.SameFile(a.parent, b.parent) {
		return false
	}
	prospectiveShortName := protectShortNames && a.file == nil && b.file == nil
	if possibleLeafAlias(a.leaf, b.leaf, runtime.GOOS == "windows", prospectiveShortName) {
		return true
	}
	return runtime.GOOS != "windows" && possibleLeafAlias(a.leaf, b.leaf, true, prospectiveShortName) && directoryMayUseDOSNames(a.parentPath)
}

// observeEntry resolves native file identity and, when a leaf is absent, the
// containing directory. Keep link targets lexical until the OS walks their
// ancestors so a relative target containing .. follows directory links correctly.
func observeEntry(path string, parents map[string]fs.FileInfo) (pathEntry, error) {
	for range 256 {
		parent, leaf := filepath.Split(path)
		file, err := os.Stat(path)
		if err != nil {
			if !missingEntry(err) {
				return pathEntry{}, err
			}
			link, err := os.Lstat(path)
			if err != nil && !missingEntry(err) {
				return pathEntry{}, err
			}
			if link != nil && link.Mode()&fs.ModeSymlink != 0 {
				next, err := os.Readlink(path)
				if err != nil {
					return pathEntry{}, err
				}
				if filepath.IsAbs(next) {
					path = next
				} else if err := ValidatePath(next); err != nil {
					return pathEntry{}, err
				} else if runtime.GOOS == "windows" {
					// Win32 normalizes .. before traversal. Resolve parent reparse
					// points before joining a relative substitute name.
					physical, err := filepath.EvalSymlinks(parent)
					if err != nil {
						return pathEntry{}, err
					}
					path = filepath.Join(physical, next)
				} else {
					path = parent + next
				}
				continue
			}
		}
		dir, ok := parents[parent]
		if !ok {
			dir, err = os.Stat(parent)
			if err != nil && !missingEntry(err) {
				return pathEntry{}, err
			}
			parents[parent] = dir
		}
		if dir != nil && !dir.IsDir() {
			dir = nil
		}
		return pathEntry{file: file, parent: dir, parentPath: parent, leaf: leaf}, nil
	}
	return pathEntry{}, errors.New("too many links while inspecting a protected path")
}

// possibleLeafAlias is intentionally conservative for absent sibling names.
// Filesystems on the same host can use different name comparison rules.
func possibleLeafAlias(a, b string, windows, prospectiveShortName bool) bool {
	if a == b {
		return true
	}
	if windows {
		a, b = windowsLeaf(a), windowsLeaf(b)
		if a == b || prospectiveShortName && (possibleDOSAlias(a, b) || possibleDOSAlias(b, a)) {
			return true
		}
	}
	return portableLeafKey(a) == portableLeafKey(b)
}

var foldLeaf = cases.Fold()

func portableLeafKey(name string) string {
	// Some normalized filesystems ignore default-ignorable characters. Include
	// their owning Unicode categories conservatively, including all format characters.
	visible := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r) || unicode.Is(unicode.Variation_Selector, r) {
			return -1
		}
		return r
	}, name)
	return norm.NFC.String(foldLeaf.String(norm.NFD.String(visible)))
}

func windowsLeaf(name string) string {
	base, _, _ := strings.Cut(name, ":")
	// Windows uppercase equivalence also covers pairs that Unicode case folding
	// intentionally distinguishes, such as I and dotless i.
	return strings.ToUpper(strings.TrimRight(strings.TrimLeft(base, " "), " ."))
}

// Long names can acquire a native 8.3 alias at creation. Do not predict its
// sequence number or stem. Reserve compatible short-name extensions instead.
func possibleDOSAlias(short, long string) bool {
	shortExt, longExt := filepath.Ext(short), filepath.Ext(long)
	if !dosName(short, shortExt) || dosName(long, longExt) {
		return false
	}
	if len(long) == len(longExt) {
		// A leading dot alone need not be treated as the extension delimiter.
		return true
	}
	extension := strings.ToUpper(strings.ReplaceAll(strings.TrimPrefix(longExt, "."), " ", ""))
	for _, r := range extension {
		if !dosCharacter(r) {
			// A DOS/OEM conversion can remove or substitute these characters.
			return true
		}
	}
	return strings.EqualFold(strings.TrimPrefix(shortExt, "."), extension[:min(3, len(extension))])
}

func dosName(name, ext string) bool {
	stem := strings.TrimSuffix(name, ext)
	if len(stem) == 0 || len(stem) > 8 || len(ext) > 4 {
		return false
	}
	for _, field := range []string{stem, strings.TrimPrefix(ext, ".")} {
		for _, r := range field {
			if !dosCharacter(r) {
				return false
			}
		}
	}
	return true
}

func dosCharacter(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
		strings.ContainsRune("!#$%&'()-@^_`{}~", r)
}
