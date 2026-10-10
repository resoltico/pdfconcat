// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"os"
	"path/filepath"
	"strings"
)

// NativePath anchors a literal expression and resolves its parent using the host filesystem.
// The leaf remains unresolved so artifact policy can refuse symbolic links. When the parent
// cannot be resolved, the literal expression is retained for the normal actionable path error.
// In particular, unresolved parent traversal is never collapsed by lexical cleanup.
func NativePath(base, expression string) string {
	path := expression
	if !filepath.IsAbs(path) {
		path = base + string(os.PathSeparator) + path
	}
	parent, leaf := filepath.Split(path)
	if parent == "" {
		return path
	}
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return path
	}
	return filepath.Join(resolved, leaf)
}

// NativeParent returns the supplied parent without collapsing unresolved components.
func NativeParent(path string) string {
	parent, _ := filepath.Split(path)
	if parent == "" {
		return "."
	}
	// Keep root separators; EvalSymlinks accepts a trailing separator.
	if len(parent) > len(filepath.VolumeName(parent))+1 {
		separators := string(os.PathSeparator)
		if os.PathSeparator == '\\' {
			separators = "/\\"
		}
		trimmed := strings.TrimRight(parent, separators)
		if trimmed == "" || trimmed == filepath.VolumeName(parent) {
			return trimmed + string(os.PathSeparator)
		}
		return trimmed
	}
	return parent
}
