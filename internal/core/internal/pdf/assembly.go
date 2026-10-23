// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"github.com/resoltico/pdfconcat/internal/core/internal/compose"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

// Occurrence binds one selection to its shared whole-file facts.
type Occurrence struct {
	File  *SourceFacts
	Facts *OccurrenceFacts
}

// Part carries everything required to assemble one physical span.
type Part struct {
	packet.Part
	Occurrence *Occurrence
	Generated  []compose.Page
	Repeat     int
}

// Assembly is the complete ordered packet and its shared resources.
type Assembly struct {
	Parts    []Part
	Sections []packet.Section
	FitTo    *packet.Size
	Footer   *compose.Footer
	Fonts    map[typeset.Digest]typeset.EmbeddedFont
}

// Assembled records actual renames by part index and assembly diagnostics.
type Assembled struct {
	Renames     [][]packet.FieldRename
	Diagnostics []diag.Diagnostic
}
