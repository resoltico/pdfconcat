// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"errors"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

type rewordedAppearanceError struct{ cause error }

func (e rewordedAppearanceError) Error() string {
	return "the rendering of this cause is completely different"
}
func (e rewordedAppearanceError) Unwrap() error { return e.cause }
func TestAppearanceAcceptanceUsesCauseNotWording(t *testing.T) {
	// The real checker supplies the same typed category after dictionary observation.
	s, _, _, err := readSource(t.Context(), readPDF(t, "testdata/xref/clean.pdf"), "")
	if err != nil {
		t.Fatal(err)
	}
	version := model.V20
	s.HeaderVersion = &version
	s.RootVersion = &version
	cause := s.CheckAnnotationAppearance(types.Dict{"Subtype": types.Name("FreeText"), "Rect": types.Array{types.Integer(0), types.Integer(0), types.Integer(10), types.Integer(20)}})
	var missing *model.MissingAnnotationAppearanceError
	if !errors.As(cause, &missing) {
		t.Fatalf("checker lacks typed cause: %v", cause)
	}
	original, e := appearanceCompatibilityFact(cause)
	alternate, e2 := appearanceCompatibilityFact(rewordedAppearanceError{cause})
	if e != nil || e2 != nil || original != alternate || original.Kind != diag.PDF20AppearanceMissing || original.Subtype != "FreeText" {
		t.Fatalf("wording selected policy: %+v %+v %v %v", original, alternate, e, e2)
	}
	unrelated := errors.New(cause.Error())
	if _, err := appearanceCompatibilityFact(unrelated); !errors.Is(err, unrelated) {
		t.Fatal("matching words swallowed an unrelated failure")
	}
}
