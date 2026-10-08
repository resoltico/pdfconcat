// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/report"
)

const signatureSourceFile = "populated-signature.pdf"

func TestCheckAndBuildRefuseSignedSourceBeforeMergeAndPreserveSources(t *testing.T) {
	t.Parallel()

	for _, command := range []string{commandCheck, commandBuild} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			verifySignatureRefusal(t, command)
		})
	}
}

func verifySignatureRefusal(t *testing.T, command string) {
	t.Helper()
	dir := workDir(t)
	writePDF(t, dir, sourceA)

	source := filepath.Join(dir, signatureSourceFile)
	if err := pdffixture.SignatureValue("SIGNATURE_STATE").WriteFile(source); err != nil {
		t.Fatal(err)
	}

	before := readFile(t, source)

	fake := newFake(t)
	fake.assemble = func(context.Context, *pdfengine.AssembleRequest) error {
		t.Fatal("signed instructions reached merge")
		return nil
	}
	res := execute(
		t.Context(),
		t,
		appOf(fake),
		dir,
		command,
		sourceA,
		blankFlag,
		signatureSourceFile,
		signatureSourceFile,
		outputFlag,
		outputFile,
		reportFlag,
		reportFile,
	)
	res.requireCode(t, 2, string(pdfengine.CodeSignatureUnsupported))

	saved := decodedInventoryReport(t, dir)
	if saved.Publication.Published || saved.Phases.Layout != report.PhaseNotRun {
		t.Fatalf("signed source reached output layout: %+v", saved)
	}

	after := readFile(t, source)
	if after != before {
		t.Fatal("signed source bytes changed")
	}

	if _, statErr := os.Stat(filepath.Join(dir, outputFile)); !os.IsNotExist(statErr) {
		t.Fatalf("signed source published output: %v", statErr)
	}
}

func TestSignatureFailureReportUsesOrdinaryOverwritePolicy(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	if err := pdffixture.SignatureValue("SIGNATURE_STATE").WriteFile(filepath.Join(dir, signatureSourceFile)); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(dir, outputFile), "KEEP_OUTPUT")
	writeFile(t, filepath.Join(dir, reportFile), "KEEP_REPORT")

	runner := nativeInventoryApp()
	// Explicit overwrite is permitted after complete alias/input inventory, but the PDF remains untouched.
	res := execute(
		t.Context(),
		t,
		runner,
		dir,
		commandBuild,
		signatureSourceFile,
		outputFlag,
		outputFile,
		reportFlag,
		reportFile,
		overwriteFlag,
	)
	res.requireCode(t, 2, string(pdfengine.CodeSignatureUnsupported))

	if readFile(t, filepath.Join(dir, outputFile)) != "KEEP_OUTPUT" {
		t.Fatal("signed refusal replaced an existing PDF")
	}

	decodedInventoryReport(t, dir)
	prior := readFile(t, filepath.Join(dir, reportFile))

	res = execute(t.Context(), t, runner, dir, commandCheck, signatureSourceFile, reportFlag, reportFile)
	if res.code != 2 || readFile(t, filepath.Join(dir, reportFile)) != prior {
		t.Fatal("default no-clobber changed prior evidence")
	}
	// A malformed plan never establishes the alias/input inventory, even with explicit overwrite.
	res = executeWith(
		t.Context(),
		t,
		runner,
		app.Env{WorkingDir: dir, Executable: applicationToolName, Stdin: strings.NewReader(`{"version":1,"items":[`)},
		commandBuild,
		planFlag,
		"-",
		reportFlag,
		reportFile,
		overwriteFlag,
	)
	if res.code != 2 || readFile(t, filepath.Join(dir, reportFile)) != prior {
		t.Fatal("unknown inventory changed prior evidence")
	}
}
