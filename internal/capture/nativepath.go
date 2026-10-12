// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"os"
	"path/filepath"
	"strings"
)

// NativeArtifactPath resolves an already-bound artifact's parent before a creation API joins
// a new filename. Native selection precedes lexical joining; the artifact leaf remains unresolved.
func NativeArtifactPath(path string) (string, error) {
	if err := validateBoundPath(path); err != nil {
		return "", &os.PathError{Op: "validate artifact path", Path: path, Err: err}
	}
	parent, leaf := filepath.Split(path)
	resolved, err := resolvePrivateParent(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, leaf), nil
}

// resolvePrivateParent observes the directory used by a native creation, with no ambient cwd base.
func resolvePrivateParent(parent string) (string, error) {
	if parent == "" {
		parent = os.TempDir()
	}
	if err := validateBoundPath(parent); err != nil {
		return "", &os.PathError{Op: "validate private parent", Path: parent, Err: err}
	}
	resolved, err := nativeParentPath(parent)
	if err != nil {
		return "", &os.PathError{Op: "resolve private parent", Path: parent, Err: err}
	}
	return resolved, nil
}

// NativeParent returns the supplied parent without collapsing unresolved components.
func NativeParent(path string) string {
	parent, _ := filepath.Split(path)
	if parent == "" {
		return "."
	}
	return trimNativeParent(parent, filepath.VolumeName(parent), os.PathSeparator)
}

// trimNativeParent receives a nonempty native split parent and its volume and separator.
// Keep root separators; EvalSymlinks accepts a trailing separator.
func trimNativeParent(parent, volume string, separator byte) string {
	if len(parent) > len(volume)+1 {
		separators := string(separator)
		if separator == '\\' {
			separators = "/\\"
		}
		trimmed := strings.TrimRight(parent, separators)
		if trimmed == "" || trimmed == volume {
			return trimmed + string(separator)
		}
		return trimmed
	}
	return parent
}
