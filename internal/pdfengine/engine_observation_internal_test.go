// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestSourceObservationFailureStopsValidatedDocumentRead(t *testing.T) {
	t.Parallel()

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "observed.pdf")
	if writeErr := pdffixture.Plain("observed source").WriteFile(path); writeErr != nil {
		t.Fatal(writeErr)
	}

	calls := 0

	_, err = engine.readDocument(
		t.Context(),
		NoSource,
		path,
		func(context.Context, *model.Context) ([]SourceFeature, error) { calls++; return nil, errBoom },
	)
	if !errors.Is(err, errBoom) || calls != 1 || CodeOf(err) != CodeInvalid {
		t.Fatalf("observation error lost: %v calls=%d", err, calls)
	}
}

func TestReadBoundaryRechecksRenderingAfterAnalysis(t *testing.T) {
	t.Parallel()

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "observed-rendering.pdf")
	if writeErr := pdffixture.Plain("observed source").WriteFile(path); writeErr != nil {
		t.Fatal(writeErr)
	}

	_, err = engine.readDocument(t.Context(), NoSource, path, func(_ context.Context, pdf *model.Context) ([]SourceFeature, error) {
		pdf.RootDict["NeedsRendering"] = types.Boolean(true)
		return nil, nil
	})
	if CodeOf(err) != CodeUnsupportedRendering {
		t.Fatalf("analysis rendering state escaped defensive boundary: %v", err)
	}
}

func TestBackendGuardReportsLibraryPanicAsFailure(t *testing.T) {
	t.Parallel()

	err := guard(CodeInvalid, "panic.pdf", func() error { panic("backend fault") })
	if CodeOf(err) != CodeInvalid || !errors.Is(err, errLibraryFailure) {
		t.Fatalf("panic escaped backend failure: %v", err)
	}
}

func TestSourceFormPolicyStopsReadBeforeValidationRepairs(t *testing.T) {
	t.Parallel()

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}

	document := pdffixture.Form("unsupported field-local resources")
	for index, object := range document.Objs {
		document.Objs[index] = bytes.ReplaceAll(object, []byte("/FT /Tx"), []byte("/FT /Tx /DR << >>"))
	}

	path := filepath.Join(t.TempDir(), "unsupported-form.pdf")
	if writeErr := document.WriteFile(path); writeErr != nil {
		t.Fatal(writeErr)
	}

	_, err = engine.readContext(t.Context(), NoSource, path)
	if CodeOf(err) != CodeFormUnsupported {
		t.Fatalf("unsupported form passed read boundary: %v", err)
	}
}
