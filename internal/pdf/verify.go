// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func readOutput(c context.Context, data []byte) (*model.Context, error) {
	return readSerializedOutput(c, bytes.NewReader(data), nil)
}

// Check raw reference, catalog, layer and navigation evidence before semantic validation.
func readSerializedOutput(c context.Context, reader io.ReadSeeker, navigation *navigationObservation) (*model.Context, error) {
	conf, err := configuration()
	if err != nil {
		return nil, err
	}
	conf.ValidationMode = model.ValidationStrict
	s, err := readPDFContext(c, reader, conf)
	if err != nil {
		return nil, err
	}
	if s.Encrypt != nil {
		return nil, errors.New("composed output must be unencrypted")
	}
	if err := checkReadReferences(c, s, true); err != nil {
		return nil, err
	}
	cat, err := s.Catalog()
	if err != nil {
		return nil, err
	}
	for key := range cat {
		if key == "OCProperties" {
			value, e := layerValue(s, cat[key], catalogSite(s).key(key))
			if e != nil {
				return nil, e
			}
			if value == nil {
				continue
			}
		}
		if !outputCatalogKey(key) {
			return nil, errors.New("unexpected output catalog key: " + key)
		}
	}
	if cat["ViewerPreferences"] != nil {
		prefs, e := s.DereferenceDict(cat["ViewerPreferences"])
		if e != nil || len(prefs) != 1 || prefs["DisplayDocTitle"] != types.Boolean(true) {
			return nil, errors.New("unexpected output viewer preferences")
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
	pages, err := rawPages(c, s)
	if err != nil {
		return nil, err
	}
	if err = scanLayerGraph(c, s, pages, &visibleLayer{}); err != nil {
		return nil, err
	}
	if navigation != nil {
		if err = navigation.verify(c, s); err != nil {
			return nil, err
		}
	}
	if navigation == nil {
		if _, err = readDestinations(c, s); err != nil {
			return nil, err
		}
		if err = inspectRetainedDestinations(c, s, pages, false); err != nil {
			return nil, err
		}
	}
	if err = checkDocument(c, s, pages); err != nil {
		return nil, err
	}
	observed, report, err := validateObservedPages(c, s, pages)
	if err != nil {
		return nil, err
	}
	if _, repair := validationNotices(report); repair != nil {
		return nil, repair
	}

	if err := validateObservedEncoding(c, s, observed); err != nil {
		return nil, err
	}
	if err := validateOutputFormEncoding(c, s); err != nil {
		return nil, err
	}
	if err = validateStructure(c, s); err != nil {
		return nil, err
	}
	return s, nil
}
