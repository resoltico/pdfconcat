// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

const maxLayerProgramBytes = 256 << 20

type layerPrograms struct {
	remaining int64
}

func (p *layerPrograms) stream(c context.Context, s *model.Context, stream types.StreamDict, role layerRole, kind, subtype types.Name, site sourceSite) error {
	bad := func() error { return layerRefusal(site.key("Stream"), diag.OptionalContentProgramUnproved()) }
	if stream.Dict["F"] != nil {
		return bad()
	}
	pattern, err := s.Dereference(stream.Dict["PatternType"])
	if err != nil {
		return err
	}
	switch {
	case subtype == "Form", pattern == types.Integer(1), role == layerGlyph, role == layerAppearance:
		data, e := decodeDrawingStream(c, s, stream)
		if e != nil {
			if c.Err() != nil {
				return c.Err()
			}
			return bad()
		}
		return p.program(c, data, site.key("Stream"))
	case subtype == "Image", kind == "Metadata", role == layerPassive, role == layerPageContent:
		return nil
	default:
		return bad()
	}
}

func verifyLayerPrograms(c context.Context, s *model.Context, contents pageContentSnapshot) error {
	p := layerPrograms{remaining: maxLayerProgramBytes}
	for i, page := range contents {
		site := sourceSite{ref: page.ref, page: i + 1}.key("Contents")
		var data []byte
		for _, entry := range page.streams {
			obj, err := s.Dereference(entry.ref)
			if err != nil {
				return err
			}
			stream, ok := obj.(types.StreamDict)
			if !ok {
				return layerRefusal(site, diag.OptionalContentProgramUnproved())
			}
			part, err := decodeDrawingStream(c, s, stream)
			if err != nil || int64(len(data))+int64(len(part))+1 > p.remaining ||
				checkContentLength(int64(len(data))+int64(len(part))+1, s.DecodeLimit()) != nil {
				if c.Err() != nil {
					return c.Err()
				}
				return layerRefusal(site, diag.OptionalContentProgramUnproved())
			}
			data = append(data, part...)
			data = append(data, '\n')
		}
		if err := p.program(c, data, site); err != nil {
			return err
		}
	}
	return nil
}

// program checks marked-content operands in every proved drawing stream.
// Non-operator payloads remain opaque, including strings and inline images.
func (p *layerPrograms) program(c context.Context, data []byte, site sourceSite) error {
	bad := layerRefusal(site, diag.OptionalContentProgramUnproved())
	p.remaining -= int64(len(data)) + 1
	if p.remaining < 0 {
		return bad
	}
	rest, args := string(data), make([]types.Object, 0, 8)
	depth := 0
	for len(rest) > 0 {
		if err := c.Err(); err != nil {
			return err
		}
		rest = strings.TrimLeft(rest, "\x00\t\n\f\r ")
		if rest == "" {
			break
		}
		if rest[0] == '%' {
			n := strings.IndexAny(rest, "\r\n")
			if n < 0 {
				break
			}
			rest = rest[n:]
			continue
		}
		if strings.ContainsRune("/[(<+-.0123456789", rune(rest[0])) || strings.HasPrefix(rest, "true") || strings.HasPrefix(rest, "false") || strings.HasPrefix(rest, "null") {
			before := len(rest)
			value, err := model.ParseObjectWithPolicy(c, &rest, 0, model.ValidationStrict, 256)
			if err != nil || value.StrictFailure != nil || len(rest) >= before || len(args) == 256 {
				if c.Err() != nil {
					return c.Err()
				}
				return bad
			}
			args = append(args, value.Object)
			continue
		}
		n := strings.IndexAny(rest, "\x00\t\n\f\r /[]<>()%")
		if n < 0 {
			n = len(rest)
		}
		if n == 0 {
			return bad
		}
		op := rest[:n]
		rest = rest[n:]
		switch op {
		case "BDC", "DP":
			if len(args) != 2 || !ordinaryMarkedTag(args[0]) || !ordinaryMarkedProperty(args[1]) {
				return bad
			}
		case "BMC", "MP":
			if len(args) != 1 || !ordinaryMarkedTag(args[0]) {
				return bad
			}
		case "BI":
			if len(args) != 0 {
				return bad
			}
			var err error
			rest, err = inlineTail(c, rest, &p.remaining, 0)
			if err != nil {
				if c.Err() != nil {
					return c.Err()
				}
				return bad
			}
		case "BX":
			depth++
		case "EX":
			depth--
		}
		if depth < 0 || depth == 0 && !strings.Contains(drawingOperators, " "+op+" ") && op != "BI" {
			return bad
		}
		args = args[:0]
	}
	if depth != 0 || len(args) != 0 {
		return bad
	}
	return c.Err()
}

func ordinaryMarkedTag(value types.Object) bool {
	tag, ok := value.(types.Name)
	return ok && tag != "OC"
}

func ordinaryMarkedProperty(value types.Object) bool {
	switch p := value.(type) {
	case types.Name:
		return true
	case types.Dict:
		return p["Type"] != types.Name("OCG") && p["Type"] != types.Name("OCMD") && p["OCGs"] == nil && p["VE"] == nil
	default:
		return false
	}
}
