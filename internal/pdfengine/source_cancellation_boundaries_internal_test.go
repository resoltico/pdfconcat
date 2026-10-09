// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestActualRenderingCapabilityLookupPreservesCancellationCause(t *testing.T) {
	t.Parallel()
	pdf := readUnvalidated(t, rawDoc("<< /Type /Catalog /Pages 2 0 R /NeedsRendering false >>", singlePageTreeBody, page))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := checkDynamicForms(ctx, pdf); !errors.Is(err, context.Canceled) || !errors.Is(err, errRenderingState) {
		t.Fatalf("native capability resolver cancellation lost: %v", err)
	}
}

func TestActualAssociatedFileTraversalKeepsEveryCancellationCheckpoint(t *testing.T) {
	t.Parallel()
	pdf := readUnvalidated(
		t,
		rawDoc("<< /Type /Catalog /Pages 2 0 R /AF [<< /Type /Filespec /F (attachment) >>] >>", singlePageTreeBody, page),
	)
	observer := featureObserver{pdf: pdf, found: map[FeatureKind]bool{}}

	assertFitResourceCancellationCheckpoints(
		t,
		func(ctx context.Context) error { return observer.associatedFiles(ctx, pdf.RootDict["AF"]) },
	)
}

func TestActualSignatureWidgetParentTraversalKeepsCancellation(t *testing.T) {
	t.Parallel()
	pdf := readUnvalidated(
		t,
		rawDoc(
			"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
			singlePageTreeBody,
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Annots [4 0 R] >>",
			"<< /Type /Annot /Subtype /Widget /FT /Tx /Rect [1 1 20 20] /P 3 0 R >>",
		),
	)

	field, err := pdf.DereferenceDictContext(t.Context(), *types.NewIndirectRef(4, 0))
	if err != nil {
		t.Fatal(err)
	}

	assertFitResourceCancellationCheckpoints(t, func(ctx context.Context) error { return checkWidgetParents(ctx, pdf, field) })
}
