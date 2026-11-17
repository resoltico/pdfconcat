// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package files

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
)

// ValidatePath rejects empty/NUL paths and the two ambiguous Windows forms.
func ValidatePath(name string) error {
	if name == "" || strings.ContainsRune(name, 0) {
		return errors.New("path must be nonempty and contain no NUL")
	}
	if runtime.GOOS == "windows" {
		volume := filepath.VolumeName(name)
		if volume != "" && !filepath.IsAbs(name) || volume == "" && (strings.HasPrefix(name, `\`) || strings.HasPrefix(name, "/")) {
			return errors.New("use an absolute or ordinary relative path, not a drive-relative or rooted-without-drive path")
		}
	}
	return nil
}

// Path resolves lexically, following the host's path conventions.
func Path(base, name string) (string, error) {
	if err := ValidatePath(name); err != nil {
		return "", err
	}
	if !filepath.IsAbs(name) {
		name = filepath.Join(base, name)
	}
	if !filepath.IsAbs(base) {
		return "", errors.New("path base must be absolute")
	}
	return filepath.Clean(name), nil
}
