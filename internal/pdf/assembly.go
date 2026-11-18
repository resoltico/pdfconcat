// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"io"

	"github.com/resoltico/pdfconcat/internal/compose"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

// Occurrence binds one selection to its shared whole-file facts.
type Occurrence struct {
	File  *SourceFacts
	Facts *OccurrenceFacts
}

// Part carries everything required to assemble one physical span.
type Part struct {
	packet.Part
	Occurrence    *Occurrence
	Text          *compose.TextFacts
	FieldsRenamed []packet.FieldRename
	Input         SourceInput
	Generated     []compose.Page
	Repeat        int
}

// SourceInput acquires immutable bytes for one admission. Closing the reader
// releases the lease. The digest describes exactly those acquired bytes.
type SourceInput interface {
	Open(context.Context) (io.ReadSeekCloser, [32]byte, error)
}

// Assembly is the complete ordered packet and its shared resources.
type Assembly struct {
	Coverage     string
	Numbering    []packet.NumberSpan
	Metadata     *packet.Metadata
	Timestamp    *int64
	Structure    packet.StructurePolicy
	Parts        []Part
	Sections     []packet.Section
	Fitting      *packet.FitPolicy
	NumberLayout *compose.PageNumberLayout
	Fonts        map[typeset.Digest]typeset.EmbeddedFont
}

// PartResult reports established assembly facts for one physical span.
type PartResult struct {
	Known   bool
	Renames []packet.FieldRename
}

// Assembled binds actual facts to owners; absent or unestablished entries are unknown.
type Assembled struct {
	Parts       map[packet.PartIdentity]PartResult
	Diagnostics []diag.Diagnostic
}
