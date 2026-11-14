// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/filter"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
)

type contentReader struct {
	context context.Context
	reader  io.Reader
}

func (r contentReader) Read(p []byte) (int, error) {
	if err := r.context.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// strictFlate verifies EOF and checksum independently of pdfcpu's deliberate
// unexpected-EOF recovery. The backend has already accepted the stage's decoded
// length/predictor under its resource budget. PNG prediction adds at most one
// framing byte per decoded byte; TIFF/no prediction adds none.
func strictFlate(c context.Context, encoded []byte, decoded int, parms types.Dict) error {
	limit := int64(decoded)
	if predictor := parms.IntEntry("Predictor"); predictor != nil && *predictor >= 10 && *predictor <= 15 {
		if limit > (math.MaxInt64-1)/2 {
			return filter.ErrDecodeLimitExceeded
		}
		limit *= 2
	}
	if limit == math.MaxInt64 {
		return filter.ErrDecodeLimitExceeded
	}
	r, err := zlib.NewReader(contentReader{context: c, reader: bytes.NewReader(encoded)})
	if err != nil {
		return err
	}
	n, err := io.Copy(io.Discard, io.LimitReader(contentReader{context: c, reader: r}, limit+1))
	closeErr := r.Close()
	if err != nil {
		return err
	}
	if n > limit {
		return filter.ErrDecodeLimitExceeded
	}
	return closeErr
}

func checkContentLength(n, limit int64) error {
	if limit >= 0 && n > limit {
		return filter.ErrDecodeLimitExceeded
	}
	return nil
}

// decodeDrawingStream leaves Raw, Dict and FilterPipeline untouched. Singleton
// stages use the SAME xref table's decoder, so intermediate buffers also count
// toward its existing cumulative allowance. No private budget is reset or copied.
func decodeDrawingStream(c context.Context, s *model.Context, stream types.StreamDict) ([]byte, error) {
	data := stream.Raw
	limit := s.DecodeLimit()
	if len(stream.FilterPipeline) == 0 {
		return data, checkContentLength(int64(len(data)), limit)
	}
	for index, f := range stream.FilterPipeline {
		if err := c.Err(); err != nil {
			return nil, err
		}
		if index == 0 && f.Name == "Crypt" {
			// Encryption is refused before this boundary. An initial Identity
			// filter (also the default when Name is absent) changes no bytes.
			name, _, err := s.DereferenceNameEntry(f.DecodeParms, "Name")
			if err != nil {
				return nil, err
			}
			if name == nil || name.Value() == "Identity" {
				continue
			}
		}
		// These encodings describe images or cryptographic handling, not PDF
		// drawing commands. StreamDict can otherwise pass opaque images through.
		switch f.Name {
		case filter.DCT, filter.JPX, filter.JBIG2, filter.CCITTFax, "Crypt":
			return nil, fmt.Errorf("drawing-content filter[%d] %q: %w", index, f.Name, filter.ErrUnsupportedFilter)
		}
		stage := types.StreamDict{Dict: stream.Dict, Raw: data, FilterPipeline: []types.PDFFilter{f}}
		if err := s.DecodeStream(&stage); err != nil {
			return nil, fmt.Errorf("drawing-content filter[%d] %q: %w", index, f.Name, err)
		}
		if err := checkContentLength(int64(len(stage.Content)), limit); err != nil {
			return nil, err
		}
		if f.Name == filter.Flate {
			if err := strictFlate(c, data, len(stage.Content), f.DecodeParms); err != nil {
				return nil, fmt.Errorf("drawing-content filter[%d] %q integrity: %w", index, f.Name, err)
			}
		}
		data = stage.Content
	}
	if err := checkContentLength(int64(len(data)), limit); err != nil {
		return nil, err
	}
	return data, c.Err()
}

func contentEncodingError(page int, ref types.IndirectRef, err error) error {
	code := diag.SourceInvalid
	message := "Page content could not be decoded completely. Obtain a reviewed repair or deliver the original separately."
	if errors.Is(err, filter.ErrUnsupportedFilter) {
		code = diag.SourceUnsupported
		message = "Page content uses an unsupported encoding. Supply a reviewed, readable PDF or deliver the original separately."
	}
	d := diag.New(code, message)
	d.Detail = strings.ToValidUTF8(err.Error(), "�")
	d.PDFLocation = &diag.PDFLocation{Object: ref.ObjectNumber.Value(), Page: page, Path: "Pages.Contents"}
	return &policyError{finding: d, err: err}
}

// validateContentEncoding runs before occurrence selection. Each original stream
// is decoded once, but every array occurrence counts toward combined page length.
// Validated final caches are retained so later backend consumers reuse buffers
// already charged to this context, without changing the original encoded bytes.
func validateContentEncoding(c context.Context, s *model.Context) error {
	pages, err := capturePageContent(c, s)
	if err != nil {
		return err
	}
	lengths := map[types.IndirectRef]int64{}
	limit := s.DecodeLimit()
	for page, contents := range pages {
		var combined int64
		for _, content := range contents.streams {
			if err := c.Err(); err != nil {
				return err
			}
			length, known := lengths[content.ref]
			if !known {
				object, err := s.Dereference(content.ref)
				if err != nil {
					return contentEncodingError(page+1, content.ref, err)
				}
				stream, ok := object.(types.StreamDict)
				if !ok {
					return contentEncodingError(page+1, content.ref, errors.New("content object is not a stream"))
				}
				decoded, err := decodeDrawingStream(c, s, stream)
				if err != nil {
					return contentEncodingError(page+1, content.ref, err)
				}
				length = int64(len(decoded))
				lengths[content.ref] = length
				stream.Content = decoded
				s.Table[content.ref.ObjectNumber.Value()].Object = stream
			}
			if length > math.MaxInt64-combined || limit >= 0 && length > limit-combined {
				return contentEncodingError(page+1, content.ref, filter.ErrDecodeLimitExceeded)
			}
			combined += length
		}
	}
	return c.Err()
}

// validateOutputFormEncoding keeps serialized fitting content inside the same
// integrity boundary as page Contents. Forms now carry the original page program;
// validating only the short outer Do stream would leave that program unchecked.
func validateOutputFormEncoding(c context.Context, s *model.Context) error {
	for number, entry := range s.Table {
		if err := c.Err(); err != nil {
			return err
		}
		if entry == nil || entry.Free {
			continue
		}
		ref := types.IndirectRef{ObjectNumber: types.Integer(number), GenerationNumber: types.Integer(generation(entry))}
		object, err := s.Dereference(ref)
		if err != nil {
			return err
		}
		stream, ok := object.(types.StreamDict)
		if !ok {
			continue
		}
		subtype, err := s.Dereference(stream.Dict["Subtype"])
		if err != nil {
			return err
		}
		if subtype != types.Name("Form") {
			continue
		}
		decoded, err := decodeDrawingStream(c, s, stream)
		if err != nil {
			failure := contentEncodingError(0, ref, err)
			finding := diagnostic(failure, diag.SourceInvalid)
			finding.Message = "Output Form content could not be decoded completely."
			finding.PDFLocation.Path = "XObject.Form"
			return &policyError{finding: finding, err: err}
		}
		stream.Content = decoded
		entry.Object = stream
	}
	return c.Err()
}

// joinedPageContent preserves content-array token boundaries and the shared
// graphics/marked-content state. pdfcpu.PageContent concatenates without a
// separator, which can join an operator at one stream's end with the next one.
func joinedPageContent(c context.Context, s *model.Context, page pageContent, number int) ([]byte, error) {
	var data []byte
	for _, stream := range page.streams {
		if err := c.Err(); err != nil {
			return nil, err
		}
		part, err := s.PageContent(types.Dict{"Contents": stream.ref}, number)
		if err != nil && !errors.Is(err, model.ErrNoContent) {
			return nil, err
		}
		if err = checkContentLength(int64(len(data))+int64(len(part))+1, s.DecodeLimit()); err != nil {
			return nil, err
		}
		data = append(data, part...)
		data = append(data, '\n')
	}
	return data, nil
}
