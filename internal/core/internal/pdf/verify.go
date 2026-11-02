// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"errors"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// readOutput checks serialized output without source normalization. Catalog
// policy is checked before validation, which may otherwise alter the document.
func readOutput(c context.Context, data []byte) (*model.Context, error) {
	conf, err := configuration()
	if err != nil {
		return nil, err
	}
	conf.ValidationMode = model.ValidationStrict
	s, err := api.ReadContext(c, bytes.NewReader(data), conf)
	if err != nil {
		return nil, err
	}
	if s.Encrypt != nil {
		return nil, errors.New("composed output must be unencrypted")
	}
	cat, err := s.Catalog()
	if err != nil {
		return nil, err
	}
	for key := range cat {
		if !catalogKeys[key] {
			return nil, errors.New("unexpected output catalog key: " + key)
		}
	}
	names, err := s.DereferenceDict(cat["Names"])
	if err != nil {
		return nil, err
	}
	for key := range names {
		if key != "Dests" {
			return nil, errors.New("unexpected output name tree: " + key)
		}
	}
	form, err := s.DereferenceDict(cat["AcroForm"])
	if err != nil {
		return nil, err
	}
	if form["XFA"] != nil {
		return nil, errors.New("XFA remains in output")
	}
	if err = checkDocument(c, s); err != nil {
		return nil, err
	}
	report, err := validatePreservingPages(c, s)
	if err != nil {
		return nil, err
	}
	if _, repair := validationNotices(report); repair != nil {
		return nil, repair
	}

	if err := validateContentEncoding(c, s); err != nil {
		return nil, err
	}
	return s, nil
}
