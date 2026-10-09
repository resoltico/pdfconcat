// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build unix

package main_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

// deleteWorkingDirectory is a shell script that removes its own working directory and then runs its arguments.
const deleteWorkingDirectory = `mkdir doomed && cd doomed && rmdir ../doomed && exec "$0" "$@"`

// requireUnprivilegedUser requires the operating system to enforce owned-file permissions.
func requireUnprivilegedUser(tb testing.TB) {
	tb.Helper()

	if os.Geteuid() == 0 {
		tb.Fatal("required non-root Unix permission-test prerequisite unavailable")
	}
}

func TestPermissionFailuresAreReportedByTheirStage(t *testing.T) {
	t.Parallel()
	requireUnprivilegedUser(t)

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")
	locked := writeFile(t, dir, "locked.json", planJSON(t, itemsOf(fileA)))
	ensure(t, os.Chmod(locked, 0))

	// The directory stays empty: nothing can be created in it, so the test directory's cleanup can remove it.
	ensure(t, os.Mkdir(filepath.Join(dir, "readonly"), 0o500))

	// A plan file the user cannot read.
	res := run(t, dir, "", commandCheck, flagPlan, "locked.json")
	requireExit(t, res, 1)

	parsed := summaryOf(t, res)
	requireCode(t, &parsed, "plan_unreadable")

	// A check that cannot save its report is a failure, and says so.
	res = run(t, dir, "", commandCheck, flagReport, readOnlyReportPath, fileA)
	requireExit(t, res, 1)

	// An output directory the user cannot write to cannot hold the private workspace beside it.
	res = run(t, dir, "", commandBuild, "-o", "readonly/out.pdf", fileA)
	requireExit(t, res, 1)

	parsed = summaryOf(t, res)
	requireCode(t, &parsed, "scratch_failed")

	// A malformed plan whose failure report cannot be saved keeps its own status and diagnostic.
	res = run(t, dir, "", commandCheck, inlinePlanFlag, `{"version":`, flagReport, readOnlyReportPath)
	requireExit(t, res, 2)

	parsed = summaryOf(t, res)
	if parsed.DiagnosticCount != 2 || parsed.Diagnostics[1].Code != codeReportWriteBad || parsed.Publication.ReportStatus != reportFailed {
		t.Errorf("secondary report failure: %+v", parsed)
	}

	// A font file that can be inspected but not read is an input failure.
	font := writeFile(t, dir, "locked.ttf", string(fontBytes(t)))
	ensure(t, os.Chmod(font, 0))

	plan := planJSON(t, itemsOf(fileA, obj{keyBlank: obj{keyText: obj{keyValue: "x", keyFont: obj{keyFile: "locked.ttf"}}}}))
	res = run(t, dir, "", commandCheck, inlinePlanFlag, plan)
	requireExit(t, res, 1)

	parsed = summaryOf(t, res)
	requireCode(t, &parsed, fontUnreadableCode)
}

func TestSourcesThatAreNamedPipesAreRejectedWithoutBlocking(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")
	ensure(t, syscall.Mkfifo(filepath.Join(dir, "pipe.pdf"), 0o600))

	res := run(t, dir, "", commandCheck, fileA, "pipe.pdf")
	requireExit(t, res, 2)

	parsed := summaryOf(t, res)
	requireCode(t, &parsed, "source_not_regular")
}

func TestADeletedWorkingDirectoryIsReported(t *testing.T) {
	t.Parallel()

	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatalf("required POSIX shell prerequisite unavailable: %v", err)
	}

	dir := tempDir(t)

	command := exectest.Command(t.Context(), shell, "-c", deleteWorkingDirectory, binary(t), commandCheck, fileA)
	command.Dir = dir

	res := finish(t, command)
	requireExit(t, res, 1)

	// Where the operating system cannot name the deleted directory the command says so; where it still
	// reports the old path (macOS) the source is simply not found there.
	parsed := summaryOf(t, res)
	if parsed.Diagnostics[0].Code != "working_directory_unavailable" && parsed.Diagnostics[0].Code != codeSourceUnreadable {
		t.Errorf("diagnostics: %+v", parsed.Diagnostics)
	}

	// Commands that need no directory still work there.
	command = exectest.Command(t.Context(), shell, "-c", deleteWorkingDirectory, binary(t), commandVersion)
	command.Dir = dir
	requireExit(t, finish(t, command), 0)
}

func TestProgressAppearsOnlyOnARealTerminal(t *testing.T) {
	t.Parallel()

	script, err := exec.LookPath("script")
	if err != nil {
		t.Skip("the script program is needed to give the command a pseudo-terminal")
	}

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a", "b")

	// Run under a pseudo-terminal: the BSD form on macOS, the util-linux form elsewhere.
	args := []string{"-q", "/dev/null", binary(t), commandBuild, "-o", fileOut, fileA, fileB}

	_, err = os.Stat("/proc/self")
	if err == nil {
		args = []string{"-qec", binary(t) + " build -o out.pdf a.pdf b.pdf", "/dev/null"}
	}

	command := exectest.Command(t.Context(), script, args...)
	command.Dir = dir
	command.Env = append(command.Env, "TMPDIR="+t.TempDir())

	output, err := command.CombinedOutput()
	if err != nil {
		t.Skipf("script could not allocate a terminal: %v\n%s", err, output)
	}

	text := string(output)
	stages := []string{
		"pdfconcat: preparation", "pdfconcat: input inspection", "pdfconcat: layout",
		"pdfconcat: assembly", "pdfconcat: optimization", "pdfconcat: output writing",
		"pdfconcat: output verification", "pdfconcat: publication", "pdfconcat: finalization",
	}

	for _, stage := range stages {
		if !strings.Contains(text, stage) {
			t.Errorf("the terminal never saw %q", stage)
		}
	}

	if !strings.Contains(text, `"status":"ok"`) {
		t.Errorf("the result is missing: %q", text)
	}

	verifyPages(t, filepath.Join(dir, fileOut), sourceAMarker, sourceBFirstMarker)
}

func TestPublishedPDFHasThePermissionsOfAnOrdinaryFile(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")

	res := run(t, dir, "", commandBuild, "-o", fileOut, fileA)
	requireExit(t, res, 0)

	// The umask can only be read by setting it; the value is restored immediately.
	umask := syscall.Umask(0)
	syscall.Umask(umask)

	info, err := os.Stat(filepath.Join(dir, fileOut))
	ensure(t, err)

	want := 0o666 &^ umask
	if got := int(info.Mode().Perm()); got != want {
		t.Errorf("published mode %04o, want %04o (0666 narrowed by umask %04o)", got, want, umask)
	}
}

func TestReportStagingFailurePreservesAttemptOwnership(t *testing.T) {
	t.Parallel()
	requireUnprivilegedUser(t)
	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")
	ensure(t, os.Mkdir(filepath.Join(dir, "readonly"), 0o500))

	sourceBefore := readFile(t, filepath.Join(dir, fileA))
	earlier := run(t, dir, "", commandCheck, flagReport, fileSavedReport, fileA)
	requireExit(t, earlier, 0)

	// A report that cannot be staged beside its target stops the run before anything is published.
	res := run(t, dir, "", commandBuild, "-o", fileOut, flagReport, readOnlyReportPath, fileA)
	requireExit(t, res, 1)

	parsed := summaryOf(t, res)
	requireCode(t, &parsed, codeReportWriteBad)

	if parsed.Publication.Published || parsed.Publication.ReportStatus != reportFailed {
		t.Errorf("publication: %+v", parsed.Publication)
	}

	requireAbsent(t, filepath.Join(dir, fileOut))
	requireAbsent(t, filepath.Join(dir, readOnlyReportPath))
	receipt := contractObject(t, res.stdout)
	attempt := textAt(t, receipt, keyAttemptID)

	if attempt == "" || attempt == textAt(t, contractObject(t, earlier.stdout), keyAttemptID) {
		t.Fatal("late report failure lost independent attempt identity")
	}

	if textAt(t, receipt, keyPublication, "report_write") != "not_written" ||
		textAt(t, receipt, "phases", "output_verification") != phaseComplete {
		t.Fatal("report staging failure lost completed verification or write receipt")
	}

	if !bytes.Equal(sourceBefore, readFile(t, filepath.Join(dir, fileA))) {
		t.Fatal("late report failure modified its source")
	}

	requireSavedAttempt(t, dir, fileSavedReport, contractObject(t, earlier.stdout), "ok")
}
