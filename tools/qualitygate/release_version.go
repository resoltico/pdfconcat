// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	releaseTagFlag                = "tag"
	portableArchiveComponentBytes = 255
	maxSnapshotCommitBytes        = 40
	semverIntegerBits             = 64
	gitHead                       = "HEAD"
	releaseVersionCommand         = "release-version"
	projectVersionFile            = "internal/app/version.txt"
	snapshotVersionMarker         = "-SNAPSHOT-"
	versionNumber                 = `(0|[1-9][0-9]*)`
	versionPrerelease             = `(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)`
)

var projectVersionPattern = regexp.MustCompile(`^` + versionNumber + `\.` + versionNumber + `\.` + versionNumber +
	`(-` + versionPrerelease + `(\.` + versionPrerelease + `)*)?$`)

func configuredProjectVersion(root string) (string, error) {
	data, err := readInRoot(root, projectVersionFile)
	if err != nil {
		return "", err
	}

	version := strings.TrimSuffix(string(data), "\n")
	if !projectVersionPattern.MatchString(version) {
		return "", fmt.Errorf(
			"%w: %s must contain one canonical release version without a v prefix or build metadata",
			errGate,
			projectVersionFile,
		)
	}

	if len(version) > maxConfiguredVersionBytes() {
		return "", fmt.Errorf(
			"%w: %s exceeds %d ASCII characters reserved for portable snapshot archive names",
			errGate,
			projectVersionFile,
			maxConfiguredVersionBytes(),
		)
	}

	core, _, _ := strings.Cut(version, "-")
	for component := range strings.SplitSeq(core, ".") {
		if _, parseErr := strconv.ParseUint(component, 10, semverIntegerBits); parseErr != nil {
			return "", fmt.Errorf("%w: %s core components must fit the release parser's uint64: %w", errGate, projectVersionFile, parseErr)
		}
	}

	return version, nil
}

func checkReleaseVersion(ctx context.Context, args []string) error {
	set := newFlags(releaseVersionCommand)
	tag := set.String(releaseTagFlag, "", "release tag, exactly v followed by the configured project version")

	snapshot := set.Bool("snapshot", false, "validate the configured version without requiring a release tag")
	if err := set.Parse(args); err != nil {
		return fmt.Errorf(parseFlagsError, err)
	}

	if set.NArg() != 0 {
		return fmt.Errorf("%w: release-version takes no operands", errGate)
	}

	root, err := repoRoot()
	if err != nil {
		return err
	}

	version, err := configuredProjectVersion(root)
	if err != nil {
		return err
	}

	if !*snapshot && *tag != "v"+version {
		return fmt.Errorf("%w: release tag %q must equal v%s", errGate, *tag, version)
	}

	if canceled := ctx.Err(); canceled != nil {
		return fmt.Errorf("validate release version: %w", canceled)
	}

	return report(releaseVersionCommand, nil, "configured project version "+version)
}

func archiveVersionProblems(ctx context.Context, root string, versions map[string]bool) ([]string, error) {
	configured, err := configuredProjectVersion(root)
	if err != nil {
		return nil, err
	}

	var problems []string

	for version := range versions {
		if version == configured {
			continue
		}

		expected, snapshotErr := snapshotProjectVersion(ctx, root, configured)
		if snapshotErr != nil {
			return nil, snapshotErr
		}

		if version != expected {
			problems = append(
				problems,
				fmt.Sprintf("archive version %q differs from configured release %q or source snapshot %q", version, configured, expected),
			)
		}
	}

	return problems, nil
}

func snapshotProjectVersion(ctx context.Context, root, configured string) (string, error) {
	revision, err := (&command{dir: root, name: gitTool, args: []string{"show", "--format=%h", gitHead, "--quiet"}}).output(ctx)
	if err != nil {
		return "", err
	}

	revision = strings.TrimSpace(revision)
	if !regexp.MustCompile(fmt.Sprintf(`^[0-9a-f]{4,%d}$`, maxSnapshotCommitBytes)).MatchString(revision) {
		return "", fmt.Errorf("%w: invalid snapshot source revision", errGate)
	}

	return configured + snapshotVersionMarker + revision, nil
}

// maxConfiguredVersionBytes reserves the longest supported snapshot suffix and archive filename.
func maxConfiguredVersionBytes() int {
	overhead := 0

	for _, target := range archiveTargets() {
		goos, goarch, _ := strings.Cut(target, "/")

		extension := ".tar.gz"
		if goos == windowsOS {
			extension = archiveZipExtension
		}

		overhead = max(overhead, len(archiveNamePrefix)+len(goos)+len(goarch)+2+len(extension))
	}

	return portableArchiveComponentBytes - overhead - len(snapshotVersionMarker) - maxSnapshotCommitBytes
}
