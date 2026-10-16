// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/oracle"
)

func TestCorpusSourceFacts(t *testing.T) {
	// Product asset availability/identity has a separate tools-free authority.
	for _, name := range []string{"provenance.json", "NOTICE.txt", "licenses/Go-font-license.txt", "licenses/Libertinus-OFL-1.1.txt", "sources/office-report.fodt"} {
		data, err := os.ReadFile(filepath.Join("testdata/corpus", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, private := range []string{"/Users/", "/home/", "implementation-inputs.private.json", "localhost"} {
			if bytes.Contains(data, []byte(private)) {
				t.Fatal("private material in public source", name, private)
			}
		}
	}
	t.Run("oracle", func(t *testing.T) {
		tools := oracle.RequireTools(t)
		report := filepath.Join("testdata/corpus", "business-report.pdf")
		encoded := filepath.Join("testdata/corpus", "business-report-object-streams.pdf")
		if raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--check-linearization", encoded).CombinedOutput(); err != nil {
			t.Fatal(string(raw), err)
		}
		xref, err := exec.CommandContext(t.Context(), tools.QPDF, "--show-xref", encoded).Output()
		if err != nil || !corpusHasCompressedEntry(string(xref)) {
			t.Fatal("object-stream source has no compressed entries", err)
		}
		classic, err := exec.CommandContext(t.Context(), tools.QPDF, "--show-xref", report).Output()
		if err != nil {
			t.Fatal(err)
		}
		if corpusHasCompressedEntry(string(classic)) {
			t.Fatal("traditional source incorrectly counted as object-stream entries")
		}

		data, err := os.ReadFile(encoded)
		if err != nil || !bytes.Contains(data, []byte("/ObjStm")) || !bytes.Contains(data, []byte("/XRef")) {
			t.Fatal("encoding source traits", err)
		}
		a, err := oracle.Load(tools, report)
		if err != nil {
			t.Fatal(err)
		}
		b, err := oracle.Load(tools, encoded)
		if err != nil {
			t.Fatal(err)
		}
		for page := 1; page <= 3; page++ {
			difference, err := b.AppearanceDifference(page, a, page, 0)
			if err != nil || difference != 0 {
				t.Fatal("encoding variant changed source pixels", page, difference, err)
			}
		}
		form, err := os.ReadFile("testdata/corpus/approval-form.pdf")
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Count(form, []byte("%%EOF")) != 2 || !bytes.Contains(form, []byte("/Prev")) {
			t.Fatal("incremental revision history missing")
		}
		end := bytes.Index(form, []byte("%%EOF")) + len("%%EOF")
		base := filepath.Join(t.ArtifactDir(), "base.pdf")
		if err = os.WriteFile(base, append(bytes.Clone(form[:end]), '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		old, err := oracle.Load(tools, base)
		if err != nil {
			t.Fatal(err)
		}
		latest, err := oracle.Load(tools, "testdata/corpus/approval-form.pdf")
		if err != nil {
			t.Fatal(err)
		}
		oldText, err := old.RawTexts()
		if err != nil {
			t.Fatal(err)
		}
		newText, err := latest.RawTexts()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(oldText[1], "Supporting page without widgets") || strings.Contains(oldText[1], "FINAL REVISION") || !strings.Contains(newText[1], "FINAL REVISION") || strings.Contains(newText[1], "Supporting page without widgets") {
			t.Fatal("latest revision selection/history", oldText, newText)
		}
	})
}

// qpdf prints "N/G: compressed; stream = S, index = I" for real compressed
// entries. The complete prefix excludes its different "uncompressed;" grammar.
func corpusHasCompressedEntry(xref string) bool {
	for line := range strings.SplitSeq(xref, "\n") {
		if matched, _ := regexp.MatchString(`^[0-9]+/[0-9]+: compressed; stream = [0-9]+, index = [0-9]+$`, line); matched {
			return true
		}
	}
	return false
}
func TestCorpusCompressedEntryWitness(t *testing.T) {
	if !corpusHasCompressedEntry("4/0: compressed; stream = 3, index = 0") || corpusHasCompressedEntry("4/0: uncompressed; offset = 237") {
		t.Fatal("compressed entry grammar lost its positive/negative distinction")
	}
}
