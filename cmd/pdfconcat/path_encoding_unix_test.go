// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build unix

package main_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unicode/utf8"
)

const rawBaseDirectoryKind = "base-directory"

func TestRawBytePathArgumentsRejectBeforePublicationAndPreserveExistingFiles(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"source", "output", "report", "plan-file", rawBaseDirectoryKind} {
		t.Run(kind, func(t *testing.T) { t.Parallel(); exerciseRawBytePath(t, kind) })
	}
}

func exerciseRawBytePath(t *testing.T, kind string) {
	t.Helper()
	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")
	sourceBefore := string(readFile(t, filepath.Join(dir, fileA)))
	bad := string([]byte{0xff}) + ".pdf"
	path := filepath.Join(dir, bad)
	before := "preserve original raw filename bytes"
	args := []string{commandBuild, "-o", fileOut, flagReport, shortReportPath, flagOverwrite, fileA}

	switch kind {
	case "source":
		args[len(args)-1] = bad
	case "output":
		args[2] = bad
	case "report":
		args[4] = bad
	case "plan-file":
		args = []string{commandBuild, flagPlan, bad, "-o", fileOut, flagReport, shortReportPath, flagOverwrite}
	case rawBaseDirectoryKind:
		args = []string{
			commandBuild,
			inlinePlanFlag,
			`{"version":1,"items":["a.pdf"]}`,
			"--base-dir",
			bad,
			"-o",
			fileOut,
			flagReport,
			shortReportPath,
			flagOverwrite,
		}
	default:
		t.Fatal("unknown fixture kind")
	}

	created := createRawPathFixture(t, path, kind, before)
	res := run(t, dir, "", args...)
	requireExit(t, res, 2)
	parsed := summaryOf(t, res)
	requireCode(t, &parsed, "usage_invalid_utf8")
	requireNoLossyPathReference(t, res.stdout)

	if created && kind != rawBaseDirectoryKind && string(readFile(t, path)) != before {
		t.Fatal("rejected raw filename was modified")
	}

	if string(readFile(t, filepath.Join(dir, fileA))) != sourceBefore {
		t.Fatal("valid source bytes changed")
	}

	requireAbsent(t, filepath.Join(dir, fileOut))
	requireAbsent(t, filepath.Join(dir, shortReportPath))
	requireNoScratch(t, dir)
}

func requireNoLossyPathReference(t *testing.T, data string) {
	t.Helper()

	if !utf8.ValidString(data) || strings.ContainsRune(data, '�') || strings.Contains(data, `"next"`) {
		t.Fatalf("rejection exposes lossy executable reference: %s", data)
	}
}

func createRawPathFixture(t *testing.T, path, kind, before string) bool {
	t.Helper()

	var err error
	if kind == rawBaseDirectoryKind {
		err = os.Mkdir(path, 0o700)
	} else {
		err = os.WriteFile(path, []byte(before), 0o600)
	}

	if err == nil {
		return true
	}

	if runtime.GOOS == "darwin" && errors.Is(err, syscall.EILSEQ) {
		t.Log(
			"native filesystem rejects non-Unicode filenames with EILSEQ; raw argv rejection still executes",
		)
		t.Log("physical raw-file preservation requires the native Linux job")

		return false
	}

	t.Fatalf("required raw-filename filesystem capability unavailable: %v", err)

	return false
}
