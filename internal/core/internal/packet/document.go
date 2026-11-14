// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// Metadata is authored document identity. Nil fields are absent, including in XMP.
type Metadata struct {
	Title    *string `json:"title,omitempty"`
	Author   *string `json:"author,omitempty"`
	Subject  *string `json:"subject,omitempty"`
	Language *string `json:"language,omitempty"`
}

// MaxTimestamp is the final UTC second in year 9999, representable in PDF/XMP.
const MaxTimestamp int64 = 253402300799

// ValidateMetadata enforces the document contract independently of transport.
func ValidateMetadata(m Metadata) error {
	for _, s := range []*string{m.Title, m.Author, m.Subject, m.Language} {
		if s != nil {
			for _, r := range *s {
				if r != 9 && r != 10 && r != 13 && (r < 0x20 || r > 0xD7FF) && (r < 0xE000 || r > 0xFFFD) && (r < 0x10000 || r > 0x10FFFF) {
					return errors.New("metadata contains a character forbidden in XMP XML 1.0")
				}
			}
		}
		if s != nil && (!utf8.ValidString(*s) || strings.ContainsRune(*s, 0) || utf8.RuneCountInString(*s) > 10000) {
			return errors.New("metadata must be valid UTF-8 without NUL, at most 10000 characters per value")
		}
	}
	if m.Language != nil && !validLanguageTag(*m.Language) {
		return errors.New("language must be a syntactically valid BCP 47 tag with unique variants and extension singletons")
	}
	return nil
}

// StructurePolicy controls preservation of source semantics.
type StructurePolicy string

const (
	// StructurePreserve retains supported source structure and tags generated text.
	StructurePreserve StructurePolicy = "preserve"
	// StructureDiscard explicitly chooses output without a structure tree.
	StructureDiscard StructurePolicy = "discard"
)
