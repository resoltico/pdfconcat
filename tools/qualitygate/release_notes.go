// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	releaseNotesCommand  = "release-notes"
	minimumMarkdownFence = 3
)

func writeReleaseNotes(ctx context.Context, args []string) error {
	set := newFlags(releaseNotesCommand)
	tag := set.String(releaseTagFlag, "", "tag matching the configured project version and changelog section")

	output := set.String("output", "", "new file for the exact changelog release notes")
	if err := set.Parse(args); err != nil {
		return fmt.Errorf(parseFlagsError, err)
	}

	if set.NArg() != 0 || *output == "" {
		return fmt.Errorf("%w: release-notes requires -tag and -output without operands", errGate)
	}

	root, err := repoRoot()
	if err != nil {
		return err
	}

	version, err := configuredProjectVersion(root)
	if err != nil {
		return err
	}

	if *tag != "v"+version {
		return fmt.Errorf("%w: release tag %q must equal v%s", errGate, *tag, version)
	}

	data, err := readInRoot(root, "CHANGELOG.md")
	if err != nil {
		return err
	}

	notes, err := changelogReleaseNotes(string(data), version)
	if err != nil {
		return err
	}

	if canceled := ctx.Err(); canceled != nil {
		return fmt.Errorf("release notes canceled: %w", canceled)
	}

	file, err := createEvidenceFile(*output)
	if err != nil {
		return err
	}

	_, writeErr := file.WriteString(notes + "\n")
	if finishErr := errors.Join(writeErr, file.Close()); finishErr != nil {
		return fmt.Errorf("write release notes: %w", finishErr)
	}

	return report(releaseNotesCommand, nil, "exact changelog notes written to "+*output)
}

func changelogReleaseNotes(content, version string) (string, error) {
	lines := strings.Split(content, "\n")

	headings, err := markdownHeadingIndexes(lines)
	if err != nil {
		return "", err
	}

	start, end := -1, len(lines)
	heading := "## [" + version + "]"

	for _, index := range headings {
		if start >= 0 && end == len(lines) {
			end = index
		}

		if !headingForVersion(lines[index], heading) {
			continue
		}

		if start >= 0 {
			return "", fmt.Errorf("%w: duplicate changelog section for %s", errGate, version)
		}

		date, ok := strings.CutPrefix(strings.TrimPrefix(lines[index], heading), " - ")
		if _, dateErr := time.Parse(time.DateOnly, date); !ok || dateErr != nil {
			return "", fmt.Errorf("%w: changelog section %s needs a valid release date", errGate, version)
		}

		start = index
	}

	if start < 0 {
		return "", fmt.Errorf("%w: changelog has no section for %s", errGate, version)
	}

	body := strings.TrimSpace(strings.Join(lines[start+1:end], "\n"))
	if !releaseBodyHasContent(body) {
		return "", fmt.Errorf("%w: changelog section %s has no release notes", errGate, version)
	}

	return strings.TrimSpace(strings.Join(lines[start:end], "\n")), nil
}

func headingForVersion(line, heading string) bool {
	if !strings.HasPrefix(line, heading) {
		return false
	}

	rest := strings.TrimPrefix(line, heading)

	return rest == "" || strings.HasPrefix(rest, " ")
}

func markdownHeadingIndexes(lines []string) ([]int, error) {
	var (
		headings []int
		fence    byte
	)

	fenceSize := 0

	for index, line := range lines {
		marker, count, tail := markdownFence(line)
		if fence != 0 {
			if marker == fence && count >= fenceSize && strings.TrimSpace(tail) == "" {
				fence, fenceSize = 0, 0
			}

			continue
		}

		if count >= minimumMarkdownFence {
			fence, fenceSize = marker, count
			continue
		}

		if strings.HasPrefix(line, "## ") {
			headings = append(headings, index)
		}
	}

	if fence != 0 {
		return nil, fmt.Errorf("%w: changelog has an unclosed code fence", errGate)
	}

	return headings, nil
}

func markdownFence(line string) (byte, int, string) {
	line = strings.TrimLeft(line, " ")
	if line == "" || (line[0] != '`' && line[0] != '~') {
		return 0, 0, ""
	}

	marker := line[0]

	count := 0
	for count < len(line) && line[count] == marker {
		count++
	}

	return marker, count, line[count:]
}

func releaseBodyHasContent(body string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return true
		}
	}

	return false
}
