// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

// The general reader can recover a stream length, or return exactly a
// declared prefix. Removing appearance ink needs stronger source evidence:
// the original declaration and the physical closing tokens must agree.
func appearanceBoundaryFailure(c context.Context, s *model.Context, ref types.IndirectRef, stream types.StreamDict, site sourceSite) (diag.SignatureDiscardReason, error) {
	site = site.key("AP")
	if err := c.Err(); err != nil {
		return "", site.wrap(err)
	}
	if s.Read == nil || s.Read.RS == nil {
		return "", appearanceBoundaryIOFailure(site, "cannot inspect the appearance stream", errors.New("source snapshot reader is unavailable"))
	}
	if !liveReference(s, ref) {
		return diag.SignatureAppearanceBoundary, nil
	}
	entry := s.Table[ref.ObjectNumber.Value()]
	start, length := stream.StreamOffset, int64(len(stream.Raw))
	if entry.Offset == nil || *entry.Offset < 0 || *entry.Offset >= start ||
		start > s.Read.FileSize || length > s.Read.FileSize-start {
		return diag.SignatureAppearanceBoundary, nil
	}
	// Parse only the original object declaration, through the backend's
	// existing bounded object parser. It does not load the stream payload.
	snapshot := s.Read.RS
	reader := &observedReadSeeker{ReadSeeker: snapshot}
	s.Read.RS = reader
	defer func() { s.Read.RS = snapshot }()
	raw, err := pdfcpu.ParseObject(c, s, *entry.Offset, ref.ObjectNumber.Value(), ref.GenerationNumber.Value())
	if canceled := c.Err(); canceled != nil {
		return "", site.wrap(canceled)
	}
	if reader.err != nil {
		return "", appearanceBoundaryIOFailure(site, "cannot read the appearance stream declaration", reader.err)
	}
	original, ok := raw.(types.StreamDict)
	if err != nil || !ok || original.StreamOffset != start {
		return diag.SignatureAppearanceBoundary, nil
	}
	if _, mismatched := s.Read.MismatchedObjectHeaders[ref]; mismatched {
		return diag.SignatureAppearanceBoundary, nil
	}
	declared, err := signatureSlot(s, original.Dict["Length"], site.key("Length"), diag.SignatureAppearance)
	n, integer := declared.(types.Integer)
	if err != nil || !integer || int64(n) != length {
		return diag.SignatureAppearanceBoundary, nil
	}

	// Look only at the claimed end. Searching ahead would bless a shorter
	// declaration that hides data between this position and the delimiter.
	const maxClosingBytes = diag.SignatureAppearanceClosingLookaheadBytes
	var tail [maxClosingBytes]byte
	end := start + length
	remaining := s.Read.FileSize - end
	window := tail[:min(remaining, maxClosingBytes)]
	pos, err := reader.Seek(end, io.SeekStart)
	if err != nil {
		return "", appearanceBoundaryIOFailure(site, "cannot seek to the appearance stream boundary", err)
	}
	if pos != end {
		err := fmt.Errorf("source seek returned offset %d for requested offset %d", pos, end)
		return "", appearanceBoundaryIOFailure(site, "cannot seek to the appearance stream boundary", err)
	}
	_, err = io.ReadFull(contentReader{context: c, reader: reader}, window)
	if canceled := c.Err(); canceled != nil {
		return "", site.wrap(canceled)
	}
	if reader.err != nil {
		err = reader.err
	}
	if err != nil {
		// The window is within the measured snapshot size. A short read
		// here is an I/O failure. A malformed token in a complete window
		// remains an unproved appearance.
		return "", appearanceBoundaryIOFailure(site, "cannot read the appearance stream boundary", err)
	}
	return appearanceClosingFailure(window, remaining <= maxClosingBytes), nil
}

func appearanceBoundaryIOFailure(site sourceSite, reason string, err error) error {
	finding := diag.IOFailure(diag.SourceUnreadable, diag.IOCause{Reason: reason, NativeDetail: err.Error()})
	finding.PDFLocation = site.location()
	return &policyError{finding: finding, err: err}
}

func missingAppearanceClosing(atEOF bool) diag.SignatureDiscardReason {
	if atEOF {
		return diag.SignatureAppearanceBoundary
	}
	return diag.SignatureAppearanceClosingLimit
}

func appearanceClosingToken(tail, token []byte, atEOF bool) ([]byte, diag.SignatureDiscardReason) {
	rest, ok := bytes.CutPrefix(tail, token)
	if ok {
		return rest, ""
	}
	if bytes.HasPrefix(token, tail) {
		return nil, missingAppearanceClosing(atEOF)
	}
	return nil, diag.SignatureAppearanceBoundary
}

func appearanceClosingFailure(tail []byte, atEOF bool) diag.SignatureDiscardReason {
	// A writer can put the separator inside Length or omit it. Length
	// delimits the encoded bytes in either case.
	switch {
	case bytes.HasPrefix(tail, []byte("\r\n")):
		tail = tail[2:]
	case len(tail) != 0 && (tail[0] == '\r' || tail[0] == '\n'):
		tail = tail[1:]
	}
	tail, reason := appearanceClosingToken(tail, []byte("endstream"), atEOF)
	if reason != "" {
		return reason
	}
	if len(tail) == 0 {
		return missingAppearanceClosing(atEOF)
	}
	if !pdfSpace(tail[0]) && tail[0] != '%' {
		return diag.SignatureAppearanceBoundary
	}
	// PDF whitespace and comments can separate the two closing tokens.
	for len(tail) > 0 {
		if pdfSpace(tail[0]) {
			tail = tail[1:]
			continue
		}
		if tail[0] != '%' {
			break
		}
		eol := bytes.IndexAny(tail, "\r\n")
		if eol < 0 {
			return missingAppearanceClosing(atEOF)
		}
		tail = tail[eol:]
	}
	tail, reason = appearanceClosingToken(tail, []byte("endobj"), atEOF)
	if reason != "" {
		return reason
	}
	if len(tail) == 0 && !atEOF {
		return diag.SignatureAppearanceClosingLimit
	}
	if len(tail) > 0 && !pdfSpace(tail[0]) && tail[0] != '%' {
		return diag.SignatureAppearanceBoundary
	}
	return ""
}
