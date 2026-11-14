// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestPageAttachmentRetainsPayloadAfterCatalogIndexesRemoved(t *testing.T) {
	payload := []byte("field attachment\x00\xff\nretained bytes\n")
	doc := pdffixture.Plain("PAGE-ATTACHMENT")
	stream := len(doc.Objs) + 1
	doc.Objs = append(doc.Objs, append(append([]byte(fmt.Sprintf("<< /Type /EmbeddedFile /Length %d >>\nstream\n", len(payload))), payload...), []byte("endstream")...))
	filespec := len(doc.Objs) + 1
	doc.Objs = append(doc.Objs, []byte(fmt.Sprintf("<< /Type /Filespec /F (field-note.bin) /UF (field-note.bin) /EF << /F %d 0 R >> >>", stream)))
	annotation := len(doc.Objs) + 1
	doc.Objs = append(doc.Objs, []byte(fmt.Sprintf("<< /Type /Annot /Subtype /FileAttachment /Rect [20 600 45 625] /P 3 0 R /FS %d 0 R /Name /PushPin /Contents (Field note) >>", filespec)))
	insertEntry(doc, 1, fmt.Sprintf("/Names << /EmbeddedFiles << /Names [(field-note.bin) %d 0 R] >> >> /AF [%d 0 R]", filespec, filespec))
	insertEntry(doc, 3, fmt.Sprintf("/Annots [%d 0 R]", annotation))
	path, _, ds := assembleDocs(t, []*pdffixture.Doc{doc}, nil)
	if status, _ := diag.Outcome(ds); status != "ok" || !codePresent(ds, diag.SourceAttachmentsRemoved) {
		t.Fatalf("attachment disposition: %+v", ds)
	}

	// Follow semantic references in the independently serialized qpdf graph;
	// merely counting the removed catalog index would miss a lost page payload.
	var dump struct {
		Pages []struct{ Object string }
		QPDF  []json.RawMessage
	}
	if err := json.Unmarshal(run(t, qpdfTool, "--json", path), &dump); err != nil {
		t.Fatal(err)
	}
	if len(dump.Pages) != 1 || len(dump.QPDF) != 2 {
		t.Fatal("unexpected qpdf graph shape")
	}
	var objects map[string]struct {
		Value json.RawMessage
	}
	if err := json.Unmarshal(dump.QPDF[1], &objects); err != nil {
		t.Fatal(err)
	}
	ref := func(value json.RawMessage) string {
		t.Helper()
		var valueRef string
		if err := json.Unmarshal(value, &valueRef); err != nil {
			t.Fatal(err)
		}
		return valueRef
	}
	resolve := func(value json.RawMessage) json.RawMessage {
		for range 20 {
			if len(value) == 0 || value[0] != '"' {
				return value
			}
			value = objects["obj:"+ref(value)].Value
		}
		t.Fatal("cyclic qpdf object references")
		return nil
	}
	dictionary := func(value json.RawMessage) map[string]json.RawMessage {
		t.Helper()
		var result map[string]json.RawMessage
		if err := json.Unmarshal(resolve(value), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	root := dictionary(dictionary(objects["trailer"].Value)["/Root"])
	if root["/AF"] != nil {
		t.Fatal("catalog associated-file index survived")
	}
	if root["/Names"] != nil && dictionary(root["/Names"])["/EmbeddedFiles"] != nil {
		t.Fatal("catalog embedded-file index survived")
	}
	page := dictionary(objects["obj:"+dump.Pages[0].Object].Value)
	var annotations []json.RawMessage
	if err := json.Unmarshal(resolve(page["/Annots"]), &annotations); err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 1 {
		t.Fatal("page attachment annotation was lost")
	}
	annot := dictionary(annotations[0])
	var subtype string
	if err := json.Unmarshal(annot["/Subtype"], &subtype); err != nil || subtype != "/FileAttachment" {
		t.Fatalf("retained annotation is not a file attachment: %s %v", subtype, err)
	}
	file := dictionary(annot["/FS"])
	embedded := dictionary(file["/EF"])
	streamRef := strings.Fields(ref(embedded["/F"]))
	if len(streamRef) != 3 {
		t.Fatal("invalid embedded stream reference")
	}
	actual := run(t, qpdfTool, "--show-object="+streamRef[0], "--filtered-stream-data", path)
	if !bytes.Equal(actual, payload) {
		t.Fatalf("page attachment payload changed: %x", actual)
	}
}
