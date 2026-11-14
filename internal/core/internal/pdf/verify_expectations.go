// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
)

// VerifyAssembly checks reopened serialized facts against the resolved contract,
// before publication. It does not substitute producer caches for output facts.
func VerifyAssembly(c context.Context, path string, expected int64, a Assembly) error {
	return verify(c, path, expected, &a)
}

func verifyDocumentFacts(c context.Context, s *model.Context, a Assembly) error {
	cat, err := s.Catalog()
	if err != nil {
		return err
	}
	m := a.Metadata
	if m == nil {
		m = &packet.Metadata{}
	}
	text := func(o types.Object) (string, error) {
		if o == nil {
			return "", nil
		}
		return s.DereferenceText(o)
	}
	lang, err := text(cat["Lang"])
	if err != nil {
		return err
	}
	if m.Language == nil && cat["Lang"] != nil || m.Language != nil && lang != *m.Language {
		return errors.New("catalog language mismatch")
	}
	prefs, err := s.DereferenceDict(cat["ViewerPreferences"])
	if err != nil {
		return err
	}
	title := m.Title != nil && strings.TrimSpace(*m.Title) != ""
	if title {
		if len(prefs) != 1 || prefs["DisplayDocTitle"] != types.Boolean(true) {
			return errors.New("authored viewer preference mismatch")
		}
	} else if len(prefs) != 0 {
		return errors.New("unexpected viewer preferences")
	}
	info := types.Dict{}
	if s.Info != nil {
		info, err = s.DereferenceDict(*s.Info)
		if err != nil {
			return err
		}
	}
	if s.PDF20() && len(info) > 0 {
		return errors.New("PDF 2.0 output must use XMP identity")
	}
	if !s.PDF20() {
		for _, entry := range []struct {
			key   string
			value *string
		}{{"Title", m.Title}, {"Author", m.Author}, {"Subject", m.Subject}} {
			value, e := text(info[entry.key])
			if e != nil {
				return e
			}
			if entry.value == nil && info[entry.key] != nil || entry.value != nil && value != *entry.value {
				return fmt.Errorf("document Info %s mismatch", entry.key)
			}
		}
	}
	object, err := s.Dereference(cat["Metadata"])
	if err != nil {
		return err
	}
	stream, ok := object.(types.StreamDict)
	if !ok {
		return errors.New("missing XMP stream")
	}
	decoded, err := decodeDrawingStream(c, s, stream)
	if err != nil {
		return err
	}
	values, err := xmpFields(decoded)
	if err != nil {
		return err
	}
	for _, entry := range []struct {
		key   string
		value *string
	}{{"title", m.Title}, {"creator", m.Author}, {"description", m.Subject}, {"language", m.Language}} {
		value, found := values[entry.key]
		if entry.value == nil && found || entry.value != nil && (!found || value != *entry.value) {
			return fmt.Errorf("XMP %s mismatch", entry.key)
		}
	}
	for _, key := range []string{"CreateDate", "ModifyDate"} {
		value, found := values[key]
		if a.Timestamp == nil && found || a.Timestamp != nil && (!found || value != time.Unix(*a.Timestamp, 0).UTC().Format(time.RFC3339)) {
			return errors.New("XMP timestamp mismatch")
		}
	}
	if !s.PDF20() {
		for _, key := range []string{"CreationDate", "ModDate"} {
			value, e := text(info[key])
			if e != nil {
				return e
			}
			if a.Timestamp == nil && info[key] != nil || a.Timestamp != nil && value != types.DateString(time.Unix(*a.Timestamp, 0).UTC()) {
				return errors.New("document Info timestamp mismatch")
			}
		}
	}
	labels, err := numberTree(c, s, cat["PageLabels"])
	if err != nil {
		return err
	}
	if len(labels) != len(a.Numbering) {
		return errors.New("page-label span count mismatch")
	}
	for _, span := range a.Numbering {
		d, e := s.DereferenceDict(labels[int(span.Output[0]-1)])
		if e != nil || d == nil {
			return errors.New("missing page-label span")
		}
		if span.Start == nil {
			if d["S"] != nil || d["P"] != types.StringLiteral("") {
				return errors.New("unnumbered label mismatch")
			}
		} else {
			n, e := s.DereferenceInteger(d["St"])
			if e != nil || n == nil || int64(n.Value()) != *span.Start || d["S"] != types.Name("D") {
				return errors.New("numeric label mismatch")
			}
		}
	}
	if a.Structure == packet.StructureDiscard {
		if cat["StructTreeRoot"] != nil || cat["MarkInfo"] != nil {
			return errors.New("discarded structure claims remain")
		}
	} else if cat["StructTreeRoot"] == nil {
		return errors.New("preserved output structure is missing")
	}
	if a.Coverage != "" {
		facts, e := markedContent(c, s)
		if e != nil {
			return e
		}
		if facts.coverage() != a.Coverage {
			return errors.New("structural coverage disagrees with resolved expectations")
		}
	}
	return nil
}

func xmpFields(data []byte) (map[string]string, error) {
	out := map[string]string{}
	d := xml.NewDecoder(bytes.NewReader(data))
	var stack []xml.Name
	var value strings.Builder
	for {
		token, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name)
			if t.Name.Local == "li" || t.Name.Space == "http://ns.adobe.com/xap/1.0/" {
				value.Reset()
			}
			if t.Name.Local == "part" && strings.Contains(t.Name.Space, "pdfua") {
				return nil, errors.New("inherited PDF/UA claim")
			}
		case xml.CharData:
			value.Write(t)
		case xml.EndElement:
			if t.Name.Local == "li" && len(stack) >= 3 {
				key := stack[len(stack)-3].Local
				if _, duplicate := out[key]; duplicate {
					return nil, errors.New("multiple authored XMP values")
				}
				out[key] = value.String()
			}
			if t.Name.Space == "http://ns.adobe.com/xap/1.0/" {
				out[t.Name.Local] = value.String()
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return out, nil
}
