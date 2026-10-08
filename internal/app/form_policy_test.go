// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/report"
)

func TestCheckAndBuildRefuseMalformedRegeneratingWidgetAppearance(t *testing.T) {
	t.Parallel()

	for _, command := range []string{commandCheck, commandBuild} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			for _, appearance := range []string{"7", "<< /N null /D 999 0 R >>", "<< /N null /R 999 0 R >>"} {
				verifyMalformedRegeneratingAppearance(t, command, appearance)
			}
		})
	}
}

func verifyMalformedRegeneratingAppearance(t *testing.T, command, appearance string) {
	t.Helper()
	dir := workDir(t)
	doc := pdffixture.Form("MALFORMED_AP")

	doc.Objs[0] = []byte(strings.Replace(string(doc.Objs[0]), "/AcroForm <<", "/AcroForm << /NeedAppearances true", 1))
	for index, body := range doc.Objs {
		if strings.Contains(string(body), "/Subtype /Widget") {
			doc.Objs[index] = []byte(strings.TrimSuffix(string(body), ">>") + " /AP " + appearance + " >>")
			break
		}
	}

	if err := doc.WriteFile(filepath.Join(dir, sourceA)); err != nil {
		t.Fatal(err)
	}

	before := readFile(t, filepath.Join(dir, sourceA))
	res := execute(t.Context(), t, nativeInventoryApp(), dir, command, sourceA, outputFlag, outputFile, reportFlag, reportFile)
	res.requireCode(t, 2, string(pdfengine.CodeFormUnsupported))

	saved := decodedInventoryReport(t, dir)
	if saved.Publication.Published || saved.Phases.Layout != report.PhaseNotRun {
		t.Fatalf("malformed widget reached layout/publication: %+v", saved)
	}

	if readFile(t, filepath.Join(dir, sourceA)) != before {
		t.Fatal("source bytes changed")
	}

	if _, err := os.Stat(filepath.Join(dir, outputFile)); !os.IsNotExist(err) {
		t.Fatalf("malformed widget published output: %v", err)
	}
}
