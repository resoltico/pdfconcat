// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"fmt"
	"strconv"

	"github.com/resoltico/pdfconcat/internal/core/internal/compose"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

type physicalViewport struct {
	size   packet.Size
	matrix [6]float64
}

func physicalPageViewport(g pageGeometry) physicalViewport {
	box, unit := g.box, g.unit
	inverse := 1 / unit
	m := [6]float64{inverse, 0, 0, inverse, box.LL.X, box.LL.Y}
	switch (g.attrs.Rotate%360 + 360) % 360 {
	case 90:
		m = [6]float64{0, inverse, -inverse, 0, box.UR.X, box.LL.Y}
	case 180:
		m = [6]float64{-inverse, 0, 0, -inverse, box.UR.X, box.UR.Y}
	case 270:
		m = [6]float64{0, -inverse, inverse, 0, box.LL.X, box.UR.Y}
	}
	return physicalViewport{size: g.size, matrix: m}
}

// applyPageNumbers adds fresh page-local wrappers and font dictionaries after
// fitting. Original streams, annotation coordinates and page geometry stay intact.
func applyPageNumbers(c context.Context, s *model.Context, parts []Part, prepared map[typeset.Digest]typeset.EmbeddedFont, names map[typeset.Digest]string, footer *compose.Footer) error {

	if footer == nil {
		return nil
	}
	face := footer.Font()
	font, ok := prepared[face.Identity()]
	if !ok {
		return ErrInvalidPage
	}
	name := names[face.Identity()]
	if name == "" {
		return ErrInvalidPage
	}
	fontRef, err := buildFontResource(c, s, font, name)
	if err != nil {
		return err
	}
	geometries, err := pageGeometries(c, s)
	if err != nil {
		return err
	}
	spanIndex := 0
	for page := 1; page <= s.PageCount; page++ {
		if err = c.Err(); err != nil {
			return err
		}
		for spanIndex < len(parts) && int64(page) > parts[spanIndex].LastPage {
			spanIndex++
		}
		if spanIndex >= len(parts) {
			return ErrInvalidPage
		}
		part := parts[spanIndex]
		if part.Kind == packet.PartPadding {
			continue
		}
		geometry := geometries[page-1]
		d, viewport := geometry.dict, physicalPageViewport(geometry)

		placed, e := footer.Place(viewport.size, int64(page))
		if e != nil {
			diagnostic := compose.Diagnostic(e, "")
			diagnostic.File, diagnostic.Items = part.Source, []string{part.Ref, "/page_numbers"}
			return &policyError{finding: diagnostic, err: e}
		}
		attrs := geometry.attrs
		resources := clonedDirectDict(attrs.Resources)
		fonts, e := s.DereferenceDict(resources["Font"])
		if e != nil {
			return e
		}
		fonts = clonedDirectDict(fonts)
		key := "PCFooter"
		for index := 1; ; index++ {
			if _, exists := fonts[key]; !exists {
				break
			}
			key = "PCFooter" + strconv.Itoa(index)
		}
		fonts[key] = *fontRef
		resources["Font"] = fonts
		d["Resources"] = resources
		contents := types.Array{}
		prefix, e := addStream(s, []byte("q\n"), nil)
		if e != nil {
			return e
		}
		contents = append(contents, *prefix)
		if original := d["Contents"]; original != nil {
			value, e := s.Dereference(original)
			if e != nil {
				return e
			}
			if array, ok := value.(types.Array); ok {
				contents = append(contents, array...)
			} else {
				contents = append(contents, original)
			}
		}
		doc := &document{fonts: &fontSet{index: map[typeset.Digest]int{face.Identity(): 0}, list: []*typeset.Font{face}, used: make([][glyphSetWords]uint64, 1)}, fontKeys: []string{key}, content: []byte("Q\nq\n")}
		m := viewport.matrix
		doc.content = fmt.Appendf(doc.content, "%s %s %s %s %s %s cm\n", number(m[0]), number(m[1]), number(m[2]), number(m[3]), number(m[4]), number(m[5]))
		doc.appendColor(packet.Color{})
		doc.appendText(placed, 0)
		doc.content = append(doc.content, "Q\n"...)
		overlay, e := addStream(s, doc.content, nil)
		if e != nil {
			return e
		}
		contents = append(contents, *overlay)
		d["Contents"] = contents
	}
	return nil
}
