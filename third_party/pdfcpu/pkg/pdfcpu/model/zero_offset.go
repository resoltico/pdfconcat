// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package model

import "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

// ZeroOffsetPolicy controls the sole additional classic-xref input tolerance.
type ZeroOffsetPolicy uint8

const (
	RejectZeroOffsets ZeroOffsetPolicy = iota
	AdmitUnreferencedZeroOffsets
	// AdmitDiscardedStructureZeroOffsets also proves exclusive index slots.
	// The source adapter must discard the proved slots and structure.
	AdmitDiscardedStructureZeroOffsets
)

// ZeroOffsetEntry identifies an effective tombstone, preserving revision precedence.
type ZeroOffsetEntry struct {
	Object, Generation, Revision int
	XRefOffset                   int64
}

// ZeroOffsetStructureReference identifies a proved original discarded index edge.
type ZeroOffsetStructureReference struct {
	Root  types.IndirectRef
	Slot  string
	Entry ZeroOffsetEntry
}
