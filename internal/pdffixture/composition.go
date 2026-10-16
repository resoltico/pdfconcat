// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdffixture

import "fmt"

// StaticForm has one text widget, optional normal appearance and explicit defaults.
// It complements Form's multi-page/shared-field topology.
func StaticForm(tag, font string, appearance bool) *Doc {
	table := plainTable(tag)
	widget := 6
	extra := ""
	table[plainFont] = body("<< /Type /Font /Subtype /Type1 /BaseFont /%s /Encoding /WinAnsiEncoding >>", font)
	if appearance {
		table[7] = stream("/Type /XObject /Subtype /Form /BBox [0 0 200 40] /Resources << /Font << /Helv 5 0 R >> >>", []byte("BT /Helv 12 Tf 5 10 Td (VALUE) Tj ET"))
		extra = " /AP << /N 7 0 R >>"
	}
	table[catalogObject] = catalogBody(" /AcroForm << /Fields [6 0 R] /DA (/Helv 12 Tf 0 g) /DR << /Font << /Helv 5 0 R >> >> >>")
	table[firstPage] = pageBody(letterBox, plainFont, plainContents, " /Annots [6 0 R]")
	table[widget] = body("<< /Type /Annot /Subtype /Widget /FT /Tx /T (field) /V (VALUE) /Rect [50 300 250 340] /P 3 0 R /F 4%s >>", extra)
	return table.document(version17)
}

// AnnotationPages places a note on page one; page two has no annotation.
func AnnotationPages(tag string) *Doc {
	table := pagesTable(tag, 2)
	note := len(table) + 1
	table[note] = body("<< /Type /Annot /Subtype /Text /Rect [50 100 70 120] /P %s /Contents (fictional note) >>", ref(pageObject(0)))
	table[pageObject(0)] = pageBody(letterBox, indexedFont, contentsObject(0), " /Annots ["+ref(note)+"]")
	return table.document(version17)
}

// NavigationPages has a local page-one link to page two with exact destination
// form and optional indirection, for retained/omitted fitting controls.
func NavigationPages(tag string, numeric, indirect bool) *Doc {
	table := pagesTable(tag, 2)
	link := len(table) + 1
	dest := "[" + ref(pageObject(1)) + " /Fit]"
	if numeric {
		dest = "[" + ref(pageObject(1)) + " /XYZ 20 30 null]"
	}
	if indirect {
		target := link + 1
		table[target] = []byte(dest)
		dest = ref(target)
	}
	table[link] = body("<< /Type /Annot /Subtype /Link /Rect [50 100 180 120] /P %s /Dest %s >>", ref(pageObject(0)), dest)
	table[pageObject(0)] = pageBody(letterBox, indexedFont, contentsObject(0), fmt.Sprintf(" /Annots [%s]", ref(link)))
	return table.document(version17)
}
