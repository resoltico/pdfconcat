// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build linux

package main_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNonUnicodeWorkingDirectoryRejectsPathActionsBeforePublication(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(tempDir(t), string([]byte{0xff}))
	ensure(t, os.Mkdir(dir, 0o700))
	writePDFs(t, dir, 1, "a")
	before := string(readFile(t, filepath.Join(dir, fileA)))
	outcome := run(t, dir, "", commandBuild, "-o", fileOut, flagReport, shortReportPath, flagOverwrite, fileA)
	requireExit(t, outcome, 1)
	parsed := summaryOf(t, outcome)
	requireCode(t, &parsed, "working_directory_invalid_utf8")
	requireNoLossyPathReference(t, outcome.stdout)

	if string(readFile(t, filepath.Join(dir, fileA))) != before {
		t.Fatal("invalid CWD command modified source")
	}

	requireAbsent(t, filepath.Join(dir, fileOut))
	requireAbsent(t, filepath.Join(dir, shortReportPath))
	requireNoScratch(t, dir)
	requireExit(t, run(t, dir, "", commandVersion), 0)
}
