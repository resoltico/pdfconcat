// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestSourceViewerSettingsAreOmittedBeforeValidation(t *testing.T) {
	for _, entry := range []string{
		"/PageLayout /SinglePage", "/PageLayout /PDLayoutDontCare", "/PageLayout 42",
		"/PageMode /UseOutlines", "/PageMode /Unknown",
		"/ViewerPreferences << /HideToolbar true >>", "/ViewerPreferences (invalid)",
		"/PageLayout 6 0 R", "/ViewerPreferences 6 0 R",
	} {
		t.Run(entry, func(t *testing.T) {
			doc := insertEntry(pdffixture.Plain("viewer"), 1, entry)
			doc.Objs = append(doc.Objs, []byte("<< /HideToolbar true >>"))
			data := doc.Bytes()
			original := bytes.Clone(data)
			facts, _, ds := pdf.Inspect(t.Context(), data, nil)
			if status, _ := diag.Outcome(ds); status != "ok" || facts.Pages != 1 || len(ds) != 1 || ds[0].Code != diag.SourceViewerSettingsRemoved {
				t.Fatalf("facts %+v diagnostics %+v", facts, ds)
			}
			if ds[0].PDFLocation == nil || ds[0].PDFLocation.Object != 1 || ds[0].PDFLocation.Path == "" {
				t.Fatalf("missing finding location: %+v", ds)
			}
			if !bytes.Equal(data, original) || facts.SHA256 != sha256.Sum256(original) {
				t.Fatal("normalization changed source bytes or identity")
			}
		})
	}
	// PDF null is equivalent to an absent dictionary entry; it requires no repair.
	for _, key := range []string{"PageLayout", "PageMode", "ViewerPreferences"} {
		_, _, ds := pdf.Inspect(t.Context(), insertEntry(pdffixture.Plain("null"), 1, "/"+key+" null").Bytes(), nil)
		if status, _ := diag.Outcome(ds); status != "ok" {
			t.Fatal(ds)
		}
	}
}

func TestViewerSettingsDoNotHideWholeSourceRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  *pdffixture.Doc
		code diag.Code
	}{
		{"signature", pdffixture.SignatureValue("signed"), diag.SourceSigned},
		{"certification", insertEntry(pdffixture.Plain("certified"), 1, "/Perms << >>"), diag.SourceSigned},
		{"optional content", insertEntry(pdffixture.Plain("layer"), 1, "/OCProperties << >>"), diag.SourceOptionalContent},
		{"damaged tree", insertEntry(pdffixture.Plain("broken"), 3, "/Kids []"), diag.SourceDamaged},
		{"invalid content", insertEntry(pdffixture.Plain("invalid"), 3, "/Rotate (invalid)"), diag.SourceInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			insertEntry(tc.doc, 1, "/PageLayout /PDLayoutDontCare")
			_, _, ds := pdf.Inspect(t.Context(), tc.doc.Bytes(), nil)
			if !codePresent(ds, tc.code) {
				t.Fatalf("expected %s: %+v", tc.code, ds)
			}
			if tc.code == diag.SourceInvalid && !codePresent(ds, diag.SourceViewerSettingsRemoved) {
				t.Fatal("later validation failure lost the normalization finding", ds)
			}
		})
	}
}

func TestViewerPreferenceRemovalPreservesSharedResources(t *testing.T) {
	doc := pdffixture.Plain("shared-resource")
	doc.Objs = append(doc.Objs, []byte("<< /Font << /F1 5 0 R >> /HideToolbar true >>"))
	doc.Objs[2] = bytes.ReplaceAll(doc.Objs[2], []byte("/Resources << /Font << /F1 5 0 R >> >>"), []byte("/Resources 6 0 R"))
	insertEntry(doc, 1, "/ViewerPreferences 6 0 R")
	out := assembleSourceBytes(t, doc.Bytes())
	// Poppler versions differ on one terminal LF even with -eol unix. The
	// complete marker, not that formatting difference, proves font retention.
	if got := extractRaw(t, out); len(got) != 1 || strings.TrimSuffix(got[0], "\n") != "shared-resource" {
		t.Fatalf("shared resource was changed: %q", got)
	}
}

func TestViewerSettingsPreservePagesAndNavigation(t *testing.T) {
	tools := oracle.RequireTools(t)
	doc := insertEntry(pdffixture.NamedDests("viewer-links"), 1, "/PageLayout /PDLayoutDontCare /PageMode /Unknown /ViewerPreferences (invalid)")
	source := filepath.Join(t.TempDir(), "source.pdf")
	if err := doc.WriteFile(source); err != nil {
		t.Fatal(err)
	}
	out := assembleSourceBytes(t, doc.Bytes())
	before, err := oracle.Load(tools, source)
	if err != nil {
		t.Fatal(err)
	}
	after, err := oracle.Load(tools, out)
	if err != nil {
		t.Fatal(err)
	}
	if before.PageCount() != 2 || after.PageCount() != 2 {
		t.Fatal("page count changed")
	}
	if !reflect.DeepEqual(extractRaw(t, source), extractRaw(t, out)) {
		t.Fatal("page text changed")
	}
	for page, target := range []int{2, 1} {
		difference, e := after.AppearanceDifference(page+1, before, page+1, 0)
		if e != nil || difference != 0 {
			t.Fatalf("page %d rendering changed: %g %v", page+1, difference, e)
		}
		got, e := after.ActionTargets(page + 1)
		if e != nil || !reflect.DeepEqual(got, []int{target}) {
			t.Fatalf("navigation changed: %v %v", got, e)
		}
		original, e := before.AnnotationRects(page + 1)
		if e != nil {
			t.Fatal(e)
		}
		actual, e := after.AnnotationRects(page + 1)
		if e != nil || !reflect.DeepEqual(original, actual) {
			t.Fatalf("annotation rectangles changed: %v %v", actual, e)
		}
	}
}

func TestOutputVerificationNeverNormalizesViewerSettings(t *testing.T) {
	for _, entry := range []string{"/PageLayout /SinglePage", "/PageLayout /PDLayoutDontCare", "/PageMode /UseNone", "/ViewerPreferences << >>"} {
		t.Run(entry, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "output.pdf")
			if err := insertEntry(pdffixture.Plain("output"), 1, entry).WriteFile(path); err != nil {
				t.Fatal(err)
			}
			if err := pdf.Verify(t.Context(), path, 1); err == nil || pdf.Diagnostic(err).Code != diag.OutputInvalid {
				t.Fatalf("forbidden serialized output was accepted: %v", err)
			}
		})
	}
}

func TestEncryptedViewerSettingsRemainRefused(t *testing.T) {
	tools := oracle.RequireTools(t)
	for _, password := range []string{"", "secret"} {
		path := filepath.Join(t.TempDir(), "encrypted.pdf")
		if err := insertEntry(pdffixture.Plain("encrypted"), 1, "/PageLayout /PDLayoutDontCare").WriteFile(path); err != nil {
			t.Fatal(err)
		}
		if err := tools.Encrypt(path, password); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		_, _, ds := pdf.Inspect(t.Context(), data, nil)
		if len(ds) != 1 || ds[0].Code != diag.SourceEncrypted {
			t.Fatal(fmt.Sprint(ds))
		}
	}
}

func TestOutputVerificationRetainsWholeDocumentSafeguards(t *testing.T) {
	for name, doc := range map[string]*pdffixture.Doc{
		"plain":          pdffixture.Plain("plain"),
		"signed":         pdffixture.SignatureValue("signed"),
		"damaged":        insertEntry(pdffixture.Plain("damaged"), 3, "/Kids []"),
		"ambiguous name": insertEntry(pdffixture.Plain("ambiguous"), 3, "/Cr#236fpBox [20 20 80 80]"),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "output.pdf")
			if err := doc.WriteFile(path); err != nil {
				t.Fatal(err)
			}
			err := pdf.Verify(t.Context(), path, 1)
			if name == "plain" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || pdf.Diagnostic(err).Code != diag.OutputInvalid {
				t.Fatalf("whole-document safeguard lost: %v", err)
			}
		})
	}
}
