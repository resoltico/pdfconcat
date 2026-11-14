// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"encoding/xml"
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
)

func pdfText(value string) types.HexLiteral { return types.HexLiteral("FEFF" + utf16Hex(value)) }

// setDocumentMetadata owns output identity, irrespective of source order or
// version. XMP is canonical; Info is emitted only for PDF 1.x compatibility.
func setDocumentMetadata(s *model.Context, m *packet.Metadata, epoch *int64) error {
	cat, err := s.Catalog()
	if err != nil {
		return err
	}
	if m == nil {
		m = &packet.Metadata{}
	}
	info := types.Dict{"Producer": types.StringLiteral("PDFConcat")}
	var x strings.Builder
	x.WriteString(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmlns:pdf="http://ns.adobe.com/pdf/1.3/"><pdf:Producer>PDFConcat</pdf:Producer>`)
	escape := func(value string) string {
		var out bytes.Buffer
		_ = xml.EscapeText(&out, []byte(value))
		return out.String()
	}
	for _, entry := range []struct {
		value                *string
		info, tag, container string
	}{{m.Title, "Title", "title", "Alt"}, {m.Author, "Author", "creator", "Seq"}, {m.Subject, "Subject", "description", "Alt"}, {m.Language, "", "language", "Bag"}} {
		if entry.value == nil {
			continue
		}
		value := *entry.value
		if entry.info != "" {
			info[entry.info] = pdfText(value)
		}
		x.WriteString("<dc:" + entry.tag + "><rdf:" + entry.container + "><rdf:li")
		if entry.container == "Alt" {
			x.WriteString(` xml:lang="x-default"`)
		}
		x.WriteString(">" + escape(value) + "</rdf:li></rdf:" + entry.container + "></dc:" + entry.tag + ">")
	}
	if m.Language != nil {
		cat["Lang"] = pdfText(*m.Language)
	}
	if m.Title != nil && strings.TrimSpace(*m.Title) != "" {
		cat["ViewerPreferences"] = types.Dict{"DisplayDocTitle": types.Boolean(true)}
	}
	if epoch != nil {
		t := time.Unix(*epoch, 0).UTC()
		date := t.Format(time.RFC3339)
		info["CreationDate"], info["ModDate"] = types.StringLiteral(types.DateString(t)), types.StringLiteral(types.DateString(t))
		x.WriteString("<xmp:CreateDate>" + date + "</xmp:CreateDate><xmp:ModifyDate>" + date + "</xmp:ModifyDate>")
	}
	x.WriteString("</rdf:Description></rdf:RDF></x:xmpmeta>")
	ref, err := addStream(s, []byte(x.String()), types.Dict{"Type": types.Name("Metadata"), "Subtype": types.Name("XML")})
	if err != nil {
		return err
	}
	cat["Metadata"] = *ref
	s.Info = nil
	if !s.PDF20() {
		s.Info, err = s.IndRefForNewObject(info)
	}
	s.ID = nil
	return err
}
