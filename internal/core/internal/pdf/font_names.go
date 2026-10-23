// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

const subsetPrefixCount = 26 * 26 * 26 * 26 * 26 * 26

// ValidateFontName applies the same canonical-name identity policy as imported
// PDFs before a generated font can reach parsing or files. Mixed hashes
// and encoded NULs are unsafe too, even when decoding the whole name fails.
func ValidateFontName(font *typeset.Font) error {
	name := font.PostScriptName()
	for index := 0; index+2 < len(name); index++ {
		if name[index] != '#' {
			continue
		}
		if _, err := hex.DecodeString(name[index+1 : index+3]); err == nil {
			return refuse(diag.FontInvalid, fmt.Sprintf("Font PostScript name %q contains a literal hash followed by two hexadecimal digits that the PDF backend may decode again; use a font with a different PostScript name.", name))
		}
	}
	return nil
}

// subsetPrefix consumes canonical decoded PDF names, never raw name syntax.
func subsetPrefix(name string) string {
	if len(name) < 8 || name[6] != '+' {
		return ""
	}
	for _, c := range name[:6] {
		if c < 'A' || c > 'Z' {
			return ""
		}
	}
	return name[:6]
}

func importedSubsetPrefixes(c context.Context, s *model.Context) ([]string, error) {
	prefixes := map[string]bool{}
	err := walkObjects(c, s, func(d types.Dict) error {
		for _, key := range []string{"BaseFont", "FontName"} {
			value, err := s.Dereference(d[key])
			if err != nil {
				return err
			}
			if name, ok := value.(types.Name); ok {
				if prefix := subsetPrefix(string(name)); prefix != "" {
					prefixes[prefix] = true
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(prefixes))
	for prefix := range prefixes {
		result = append(result, prefix)
	}
	slices.Sort(result)
	return result, nil
}

// generatedFontNames reserves imported tags before allocating once per font identity.
// Sorted original-byte identities make allocation independent of map and item order.
func generatedFontNames(fonts map[typeset.Digest]typeset.EmbeddedFont, parts []Part) (map[typeset.Digest]string, error) {
	reserved := map[string]bool{}
	for _, part := range parts {
		if part.Occurrence == nil || part.Occurrence.File == nil {
			continue
		}
		for _, prefix := range part.Occurrence.File.SubsetPrefixes {
			reserved[prefix] = true
		}
	}
	identities := make([]typeset.Digest, 0, len(fonts))
	for identity, font := range fonts {
		if err := ValidateFontName(font.Font); err != nil {
			return nil, err
		}
		identities = append(identities, identity)
		// A full font may already have a tag-like PostScript name. Keep that
		// name intact and reserve its prefix before assigning any subsets.
		if !font.Subset {
			if prefix := subsetPrefix(font.Font.PostScriptName()); prefix != "" {
				reserved[prefix] = true
			}
		}
	}
	slices.SortFunc(identities, func(a, b typeset.Digest) int { return bytes.Compare(a[:], b[:]) })
	names := make(map[typeset.Digest]string, len(fonts))
	candidate := 0
	for _, identity := range identities {
		font := fonts[identity]
		name := font.Font.PostScriptName()
		if font.Subset {
			for {
				if candidate == subsetPrefixCount {
					return nil, errors.New("PDF subset font prefix space exhausted")
				}
				prefix := subsetTag(candidate)
				candidate++
				if reserved[prefix] {
					continue
				}
				reserved[prefix] = true
				name = prefix + "+" + name
				break
			}
		}
		names[identity] = name
	}
	return names, nil
}

func subsetTag(index int) string {
	var tag [6]byte
	for i := len(tag) - 1; i >= 0; i-- {
		tag[i] = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"[index%26]
		index /= 26
	}
	return string(tag[:])
}
