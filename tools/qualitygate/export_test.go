// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"context"
	"io"
)

type stringsReaderType struct {
	text string
	pos  int
}

// Exports for the external test package.

// AnchoredReplacement exposes replaceAnchoredLine.
func AnchoredReplacement(file string, content []byte, anchor, replacement string) ([]byte, error) {
	return replaceAnchoredLine(file, content, anchor, replacement)
}

// ArchiveInspection exposes archiveProblems with the CLI version command.
func ArchiveInspection(root, dist string) ([]string, error) {
	return archiveProblems(context.Background(), root, dist, "version")
}

// ParseEvents feeds go test -json lines through the outcome parser and judges them.
func ParseEvents(lines string, withTests, require []string) error {
	outcome := newTestOutcome()
	outcome.consume(stringsReader(lines))

	return judgeTests(outcome, withTests, testOptions{require: require}, nil)
}

// MutationOperatorFlags exposes operatorFlags.
func MutationOperatorFlags() []string { return operatorFlags() }

func stringsReader(text string) *stringsReaderType { return &stringsReaderType{text: text} }

func (r *stringsReaderType) Read(buffer []byte) (int, error) {
	if r.pos >= len(r.text) {
		return 0, io.EOF
	}

	count := copy(buffer, r.text[r.pos:])
	r.pos += count

	return count, nil
}

// VersionOutputCheck exposes versionOutputProblems.
func VersionOutputCheck(version, output string) []string {
	return versionOutputProblems("archive", version, output)
}
