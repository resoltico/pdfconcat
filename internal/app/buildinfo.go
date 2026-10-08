// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	_ "embed"
	"runtime/debug"
	"strings"
)

// embedded is the build information the Go toolchain recorded in the executable.
type embedded struct {
	BuildInfo

	// dirty reports a version-control tree with uncommitted changes.
	dirty bool
}

// Names a build reports when the Go toolchain recorded no provenance.
const (
	unknownMetadata = "unknown"
	// revisionLength is the number of hexadecimal digits of the commit shown in a source build.
	revisionLength = 12
)

// The project version is shared by ordinary builds and release tooling.
//
//go:embed version.txt
var projectVersion string

// configuredVersion returns the configured application version, without the file terminator.
func configuredVersion() string {
	return strings.TrimSpace(projectVersion)
}

// ResolveBuild decides the release metadata of the executable. A version set at link time wins; otherwise the
// version comes from version.txt. Module pseudo-versions do not override the project version. The commit and
// its time come from the toolchain's version-control records; missing provenance is unknown. info may be nil.
func ResolveBuild(version string, info *debug.BuildInfo) BuildInfo {
	recorded := embeddedBuild(info)

	build := BuildInfo{
		Version:    firstNonEmpty(configuredVersion(), version),
		Commit:     firstNonEmpty(unknownMetadata, recorded.Commit),
		CommitDate: firstNonEmpty(unknownMetadata, recorded.CommitDate),
	}

	// A source build from a modified tree says so.
	if recorded.Commit != "" && recorded.dirty {
		build.Commit += "+dirty"
	}

	return build
}

// embeddedBuild reads the version-control settings; info may be nil.
func embeddedBuild(info *debug.BuildInfo) embedded {
	var found embedded

	if info == nil {
		return found
	}

	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			found.Commit = setting.Value[:min(len(setting.Value), revisionLength)]
		case "vcs.time":
			found.CommitDate = setting.Value
		case "vcs.modified":
			found.dirty = setting.Value == "true"
		default:
		}
	}

	return found
}

// firstNonEmpty returns the first value that is not empty, or fallback when every value is.
func firstNonEmpty(fallback string, values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return fallback
}
