// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

// configuration isolates each operation from user configuration and network access.
func configuration() (*model.Configuration, error) {
	conf, err := api.LoadConfiguration(api.ConfigurationOptions{Mode: api.ConfigurationModeStateless})
	if err != nil {
		return nil, err
	}
	conf.Offline = true
	conf.RejectEncryptedInput = true
	conf.KeepDirectInfo = true
	conf.ValidationMode = model.ValidationRelaxed
	conf.CreateBookmarks = true
	conf.MergeBookmarkMode = model.MergeBookmarkModePreserve
	return conf, nil
}

// Locate effective xref and catalog-version errors on source and output reads.
func readPDFContext(c context.Context, reader io.ReadSeeker, conf *model.Configuration) (*model.Context, error) {
	s, err := api.ReadContext(c, reader, conf)
	if errors.Is(err, model.ErrInvalidRootVersionReference) {
		loc := diag.PDFLocation{Path: "Catalog.Version"}
		var attributed *model.ValidationError
		if errors.As(err, &attributed) {
			loc.Object = attributed.ObjectNumber()
		}
		return s, structureAt(err, loc)
	}
	if errors.Is(err, pdfcpu.ErrOccupiedFreeListHead) {
		return s, sourceSite{path: "XRef[0]"}.wrap(refuse(diag.SourceDamaged, diag.PDFObjectZeroMustBeFree()))
	}
	var mismatch *pdfcpu.ObjectIdentityError
	if errors.As(err, &mismatch) {
		n := mismatch.Expected.ObjectNumber.Value()
		return s, structureAt(err, diag.PDFLocation{Object: n, Path: fmt.Sprintf("XRef[%d]", n)})
	}
	return s, err
}
