// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"io"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

// proveZeroOffsetDiscardHint keeps the failed preserve admission. Only the
// original typed candidate can authorize one source-level discard probe.
func proveZeroOffsetDiscardHint(c context.Context, reader io.ReadSeeker, policies SourcePolicies, refusal error) error {
	var candidate *pdfcpu.PossibleZeroOffsetStructureIndex
	if (policies.Structure != "" && policies.Structure != packet.StructurePreserve) || !errors.As(refusal, &candidate) {
		return refusal
	}
	if err := c.Err(); err != nil {
		return err
	}
	probe := &discardHintReader{ReadSeeker: reader}
	if _, err := probe.Seek(0, io.SeekStart); err != nil {
		return err
	}
	policies.Structure = packet.StructureDiscard
	s, origins, findings, err := readSourceReader(c, probe, policies)
	var location *diag.PDFLocation
	if err == nil {
		proved := false
		for _, reference := range s.Read.DiscardedZeroOffsetReferences {
			if reference.Root == candidate.Owner && reference.Slot == candidate.Slot &&
				reference.Entry.Object == candidate.Target.ObjectNumber.Value() &&
				reference.Entry.Generation == candidate.Target.GenerationNumber.Value() {
				proved = true
				break
			}
		}
		if proved {
			for _, finding := range findings {
				if finding.Code == diag.SourceXRefStructureReferenceDiscarded && finding.PDFLocation != nil &&
					finding.PDFLocation.Object == candidate.Owner.ObjectNumber.Value() && finding.PDFLocation.Path == candidate.Slot {
					location = finding.PDFLocation
					break
				}
			}
		}
		if location != nil {
			_, _, err = observeSource(c, s, origins)
		}
	}
	if canceled := c.Err(); canceled != nil {
		return canceled
	}
	if probe.err != nil {
		return probe.err
	}
	if err != nil || location == nil {
		return refusal
	}
	return structureAt(pdfFailure(diag.SourceInvalid, diag.ProvedZeroOffsetDiscardHint(), refusal), *location)
}

// A failed read or seek is an acquisition failure during the probe. Distinguish
// it from the ordinary PDF refusals that leave the preserve diagnostic intact.
type discardHintReader struct {
	io.ReadSeeker
	err error
}

func (r *discardHintReader) Read(p []byte) (int, error) {
	n, err := r.ReadSeeker.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && r.err == nil {
		r.err = err
	}
	return n, err
}

func (r *discardHintReader) Seek(offset int64, whence int) (int64, error) {
	n, err := r.ReadSeeker.Seek(offset, whence)
	if err != nil && r.err == nil {
		r.err = err
	}
	return n, err
}
