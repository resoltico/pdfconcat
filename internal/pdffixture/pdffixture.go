// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package pdffixture writes small, deterministic PDF files for tests and benchmarks.
//
// The files are hand-numbered, uncompressed and use a classic cross-reference table, so a fixture's
// structure is exactly what its constructor shows and does not depend on the PDF library under test.
// Every page carries a text marker (the tag, plus " p<N>" for multi-page documents) that independent
// extractors can read back.
package pdffixture

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
)

type (
	// Doc is a list of PDF object bodies. Object n is Objs[n-1]; object 1 is always the catalog.
	Doc struct {
		// Version is the header version, for example "1.7" or "2.0".
		Version string
		// Info, when not empty, is the body of a document information dictionary referenced from the trailer.
		Info string
		// Objs holds the serialized object bodies.
		Objs [][]byte
	}

	// PageBoxes lists page-dictionary entries as PDF source text, so malformed values can be expressed.
	// An empty field omits the entry.
	PageBoxes struct {
		// Media, Crop are arrays such as "[0 0 612 792]".
		Media, Crop string
		// Rotate and UserUnit are numbers such as "90" and "2.5".
		Rotate, UserUnit string
	}

	// objectTable holds object bodies by object number. Naming each number once, as a constant, keeps the
	// references between objects and the objects they point at in agreement.
	objectTable map[int][]byte

	// pageParts are the page dictionary and the content stream of one page.
	pageParts struct {
		dict, contents []byte
	}

	// point is a position on a page in points.
	point struct {
		x, y float64
	}
)

const (
	helvetica = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"
	letterBox = "[0 0 612 792]"
	a4Box     = "[0 0 595 842]"

	version17 = "1.7"

	// emptyDictionary fills object numbers a fixture leaves unused, so the numbering stays stable.
	emptyDictionary = "<< >>"

	// Objects every fixture numbers alike: the catalog, and the root of the page tree.
	catalogObject  = 1
	pageTreeObject = 2

	// Pages of the fixtures that have one or two fixed pages.
	firstPage  = 3
	secondPage = 4

	// Objects of Plain, and of the fixtures built from it.
	plainContents = 4
	plainFont     = 5

	// Objects of the multi-page fixtures (Pages, WithBoxes, Resource, Outlined): the font, then a page and
	// its contents for each index.
	indexedFont           = 3
	firstIndexedPage      = 4
	firstIndexedContents  = 5
	objectsPerIndexedPage = 2

	// Objects of the two-page fixtures with annotations, destinations or forms (Links, NamedDests,
	// LegacyDests, Form).
	annotatedContents1 = 7
	annotatedContents2 = 8
	annotatedFont      = 9

	// Objects of Links.
	linksForward     = 5
	linksBack        = 6
	linksGoToForward = 10
	linksPage2Annots = 11
	linksGoToBack    = 12

	// Objects of NamedDests and LegacyDests.
	destsPlaceholder = 5
	destsTable       = 6
	destsAnnot1      = 10
	destsAnnot2      = 11

	// Objects of Form: the placeholders keep the field objects at numbers 20 to 23.
	formParentField = 20
	formDupField    = 21
	formWidgetA     = 22
	formWidgetB     = 23

	// Objects of PageActions.
	actionsFont      = 5
	actionsContents1 = 6
	actionsContents2 = 7

	// Objects of NestedBoxes.
	nestedInnerNode = 5
	nestedFont      = 6
	nestedContents1 = 7
	nestedContents2 = 8

	// Objects Outlined adds to Pages(tag, 2).
	outlineRoot   = 8
	outlineFirst  = 9
	outlineSecond = 10

	// Objects of Tagged.
	taggedStructRoot = 6
	taggedStructElem = 7
	taggedParentTree = 8

	// Objects Attachment adds to Plain.
	attachmentFilespec = 6
	attachmentData     = 7

	// Object ImageRich adds to Plain.
	imageObject = 6

	// defaultMarkerX and defaultMarkerY place a marker when the page boxes do not say where the page is.
	defaultMarkerX = 50
	defaultMarkerY = 700
	// markerMargin keeps a marker just inside the lower-left corner of the MediaBox.
	markerMargin = 12

	// imageWidth is the width of ImageRich's image in pixels; its height follows from the payload size.
	imageWidth = 1024
	// wordBytes is the number of bytes one generator word fills.
	wordBytes = 8

	// The xorshift64 shifts (13, 7, 17) and the seed's FNV-1a offset basis and prime.
	shiftLeftFirst  = 13
	shiftRight      = 7
	shiftLeftSecond = 17
	fnvOffsetBasis  = 14695981039346656037
	fnvPrime        = 1099511628211
)

// Bytes serializes the document.
func (d *Doc) Bytes() []byte {
	var out bytes.Buffer

	fmt.Fprintf(&out, "%%PDF-%s\n%%\xE2\xE3\xCF\xD3\n", d.Version)

	// Entry n holds the offset of object n; entry 0 is the free object.
	offsets := make([]int, 1, len(d.Objs)+2)

	for index, body := range d.Objs {
		offsets = append(offsets, out.Len())

		fmt.Fprintf(&out, "%d 0 obj\n", index+1)
		out.Write(body)
		out.WriteString("\nendobj\n")
	}

	trailerExtra := ""

	if d.Info != "" {
		offsets = append(offsets, out.Len())

		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", len(d.Objs)+1, d.Info)

		trailerExtra = fmt.Sprintf(" /Info %d 0 R", len(d.Objs)+1)
	}

	xrefOffset := out.Len()
	entries := len(offsets) // object 0 included

	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", entries)

	for number := 1; number < entries; number++ {
		fmt.Fprintf(&out, "%010d 00000 n \n", offsets[number])
	}

	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R%s >>\nstartxref\n%d\n%%%%EOF\n", entries, trailerExtra, xrefOffset)

	return out.Bytes()
}

// WriteFile writes the document to path with owner-only permissions.
func (d *Doc) WriteFile(path string) error {
	const permissions = 0o600

	err := os.WriteFile(path, d.Bytes(), permissions)
	if err != nil {
		return fmt.Errorf("write fixture: %w", err)
	}

	return nil
}

// document returns the Doc whose object n holds t[n]. Numbers the table skips hold empty dictionaries.
func (t objectTable) document(version string) *Doc {
	highest := 0
	for number := range t {
		highest = max(highest, number)
	}

	doc := &Doc{Version: version, Objs: make([][]byte, highest)}
	for number := 1; number <= highest; number++ {
		doc.Objs[number-1] = t[number]
		if doc.Objs[number-1] == nil {
			doc.Objs[number-1] = body(emptyDictionary)
		}
	}

	return doc
}

// stream returns the body of a stream object with extra dictionary entries.
func stream(extra string, data []byte) []byte {
	var body bytes.Buffer

	fmt.Fprintf(&body, "<< %s /Length %d >>\nstream\n", extra, len(data))
	body.Write(data)
	body.WriteString("\nendstream")

	return body.Bytes()
}

// marker is a content stream that draws tag as one Helvetica line (font resource /F1) at (50, 700).
func marker(tag string) []byte {
	return markerAt(tag, defaultMarkerX, defaultMarkerY)
}

func markerAt(tag string, x, y float64) []byte {
	escaper := strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`)

	return fmt.Appendf(nil, "BT /F1 24 Tf %g %g Td (%s) Tj ET", x, y, escaper.Replace(tag))
}

// markerOrigin places the marker just inside the lower-left corner of the effective MediaBox, so it is
// visible whatever the box. Unparsable boxes fall back to the default position.
func markerOrigin(own, inherited PageBoxes) point {
	media := own.Media
	if media == "" {
		media = inherited.Media
	}

	var x0, y0, x1, y1 float64
	if _, err := fmt.Sscanf(media, "[%g %g %g %g]", &x0, &y0, &x1, &y1); err != nil {
		return point{defaultMarkerX, defaultMarkerY}
	}

	return point{min(x0, x1) + markerMargin, min(y0, y1) + markerMargin}
}

func body(format string, args ...any) []byte {
	return fmt.Appendf(nil, format, args...)
}

// ref is the indirect reference to object number.
func ref(number int) string {
	return fmt.Sprintf("%d 0 R", number)
}

// catalogBody is the document catalog with extra entries after /Pages.
func catalogBody(extra string) []byte {
	return body("<< /Type /Catalog /Pages %s%s >>", ref(pageTreeObject), extra)
}

// pageBody is a page of the root page tree with the given MediaBox, drawing the contents object with the
// font object. extra holds further entries, each with a leading space.
func pageBody(box string, font, contents int, extra string) []byte {
	return body(
		"<< /Type /Page /Parent %s /MediaBox %s /Resources << /Font << /F1 %s >> >> /Contents %s%s >>",
		ref(pageTreeObject), box, ref(font), ref(contents), extra,
	)
}

// twoPageTree is the page tree of the fixtures with the fixed pages 3 and 4.
func twoPageTree() []byte {
	return body("<< /Type /Pages /Kids [%s %s] /Count 2 >>", ref(firstPage), ref(secondPage))
}

// plainTable is the object table of Plain.
func plainTable(tag string) objectTable {
	return objectTable{
		catalogObject:  catalogBody(""),
		pageTreeObject: body("<< /Type /Pages /Kids [%s] /Count 1 >>", ref(firstPage)),
		firstPage:      pageBody(letterBox, plainFont, plainContents, ""),
		plainContents:  stream("", marker(tag)),
		plainFont:      body(helvetica),
	}
}

// Plain is a one-page document marked tag with a US Letter MediaBox.
func Plain(tag string) *Doc {
	return plainTable(tag).document(version17)
}

// pageObject is the object number of the page with the given index in the multi-page fixtures.
func pageObject(index int) int { return firstIndexedPage + objectsPerIndexedPage*index }

// contentsObject is the object number of the contents of the page with the given index.
func contentsObject(index int) int { return firstIndexedContents + objectsPerIndexedPage*index }

// indexedTable is the layout of the multi-page fixtures: the catalog, the page tree with the treeExtra
// entries, the font, and for each index the page and contents that page(index) returns.
func indexedTable(count int, treeExtra string, page func(index int) pageParts) objectTable {
	var kids strings.Builder

	for index := range count {
		fmt.Fprintf(&kids, "%s ", ref(pageObject(index)))
	}

	table := objectTable{
		catalogObject:  catalogBody(""),
		pageTreeObject: body("<< /Type /Pages /Kids [%s] /Count %d%s >>", kids.String(), count, treeExtra),
		indexedFont:    body(helvetica),
	}

	for index := range count {
		parts := page(index)
		table[pageObject(index)] = parts.dict
		table[contentsObject(index)] = parts.contents
	}

	return table
}

// pagesTable is the object table of Pages.
func pagesTable(tag string, count int) objectTable {
	return indexedTable(count, "", func(index int) pageParts {
		return pageParts{
			dict:     pageBody(letterBox, indexedFont, contentsObject(index), ""),
			contents: stream("", marker(fmt.Sprintf("%s p%d", tag, index+1))),
		}
	})
}

// Pages is an annotation-free document of count pages marked "<tag> p<N>".
func Pages(tag string, count int) *Doc {
	return pagesTable(tag, count).document(version17)
}

// Links is a two-page document ("<tag> p1", "<tag> p2") whose link annotations point directly at the
// other page, through /Dest and through GoTo actions, forwards and backwards. Every annotation names its
// own page in /P; page 1 has a direct /Annots array and page 2 an indirect one.
func Links(tag string) *Doc {
	link := "<< /Type /Annot /Subtype /Link /Rect [50 600 200 620] /Border [0 0 1] /P %s %s >>"
	goTo := "<< /Type /Annot /Subtype /Link /Rect [50 500 200 520] /Border [0 0 1] /P %s /A << /S /GoTo /D %s >> >>"

	return objectTable{
		catalogObject:  catalogBody(""),
		pageTreeObject: twoPageTree(),
		firstPage: pageBody(
			letterBox,
			annotatedFont,
			annotatedContents1,
			fmt.Sprintf(" /Annots [%s %s]", ref(linksForward), ref(linksGoToForward)),
		),
		secondPage:         pageBody(letterBox, annotatedFont, annotatedContents2, " /Annots "+ref(linksPage2Annots)),
		linksForward:       body(link, ref(firstPage), fmt.Sprintf("/Dest [%s /XYZ 0 792 0]", ref(secondPage))),
		linksBack:          body(link, ref(secondPage), fmt.Sprintf("/Dest [%s /Fit]", ref(firstPage))),
		annotatedContents1: stream("", marker(tag+" p1")),
		annotatedContents2: stream("", marker(tag+" p2")),
		annotatedFont:      body(helvetica),
		linksGoToForward:   body(goTo, ref(firstPage), fmt.Sprintf("[%s /Fit]", ref(secondPage))),
		linksPage2Annots:   body("[%s %s]", ref(linksBack), ref(linksGoToBack)),
		linksGoToBack:      body(goTo, ref(secondPage), fmt.Sprintf("[%s /FitH 700]", ref(firstPage))),
	}.document(version17)
}

// destinationFixture is the layout of NamedDests and LegacyDests: the catalog entries, the destination
// table object, and the two link annotations that use the names chap1 and chap2 in the given spellings.
func destinationFixture(tag, catalogExtra, table, linkToChap2, linkToChap1 string) *Doc {
	return objectTable{
		catalogObject:      catalogBody(catalogExtra),
		pageTreeObject:     twoPageTree(),
		firstPage:          pageBody(letterBox, annotatedFont, annotatedContents1, " /Annots ["+ref(destsAnnot1)+"]"),
		secondPage:         pageBody(letterBox, annotatedFont, annotatedContents2, " /Annots ["+ref(destsAnnot2)+"]"),
		destsPlaceholder:   body(emptyDictionary),
		destsTable:         body(table, ref(firstPage), ref(secondPage)),
		annotatedContents1: stream("", marker(tag+" p1")),
		annotatedContents2: stream("", marker(tag+" p2")),
		annotatedFont:      body(helvetica),
		destsAnnot1:        body("<< /Type /Annot /Subtype /Link /Rect [50 600 200 620] /P %s %s >>", ref(firstPage), linkToChap2),
		destsAnnot2:        body("<< /Type /Annot /Subtype /Link /Rect [50 600 200 620] /P %s %s >>", ref(secondPage), linkToChap1),
	}.document(version17)
}

// NamedDests is a two-page document ("<tag> p1", "<tag> p2") with name-tree destinations chap1 and chap2
// that link annotations use through /Dest strings and GoTo actions.
func NamedDests(tag string) *Doc {
	return destinationFixture(tag,
		" /Names << /Dests "+ref(destsTable)+" >>",
		"<< /Names [(chap1) [%s /Fit] (chap2) [%s /Fit]] >>",
		"/Dest (chap2)",
		"/A << /S /GoTo /D (chap1) >>",
	)
}

// LegacyDests is NamedDests expressed with the legacy catalog /Dests dictionary and name objects.
func LegacyDests(tag string) *Doc {
	return destinationFixture(tag,
		" /Dests "+ref(destsTable),
		"<< /chap1 [%s /Fit] /chap2 [%s /Fit] >>",
		"/Dest /chap2",
		"/Dest /chap1",
	)
}

// Form is a two-page document ("<tag> p1", "<tag> p2") with an AcroForm: a non-widget parent field
// "grp" whose two child widgets "a" and "b" sit on different pages, and a merged field/widget "dup".
// Two Form documents therefore carry the same qualified field names.
func Form(tag string) *Doc {
	acroForm := fmt.Sprintf(
		" /AcroForm << /Fields [%s %s] /DA (/Helv 0 Tf 0 g) /DR << /Font << /Helv %s >> >> >>",
		ref(formParentField), ref(formDupField), ref(annotatedFont),
	)
	widget := "<< /Type /Annot /Subtype /Widget /Parent %s /T (%s) /Rect [50 300 200 320] /P %s /F 4 >>"

	return objectTable{
		catalogObject:  catalogBody(acroForm),
		pageTreeObject: twoPageTree(),
		firstPage: pageBody(
			letterBox,
			annotatedFont,
			annotatedContents1,
			fmt.Sprintf(" /Annots [%s %s]", ref(formWidgetA), ref(formDupField)),
		),
		secondPage:         pageBody(letterBox, annotatedFont, annotatedContents2, " /Annots ["+ref(formWidgetB)+"]"),
		annotatedContents1: stream("", marker(tag+" p1")),
		annotatedContents2: stream("", marker(tag+" p2")),
		annotatedFont:      body(helvetica),
		formParentField:    body("<< /FT /Tx /T (grp) /V (shared) /Kids [%s %s] >>", ref(formWidgetA), ref(formWidgetB)),
		formDupField: body("<< /Type /Annot /Subtype /Widget /FT /Tx /T (dup) /V (x) /Rect [50 400 200 420] /P %s /F 4 >>",
			ref(firstPage)),
		formWidgetA: body(widget, ref(formParentField), "a", ref(firstPage)),
		formWidgetB: body(widget, ref(formParentField), "b", ref(secondPage)),
	}.document(version17)
}

// PageActions is a two-page document ("<tag> p1", "<tag> p2") whose pages carry /AA page-open actions
// that jump to the other page, and no /Annots.
func PageActions(tag string) *Doc {
	openAction := " /AA << /O << /S /GoTo /D [%s /Fit] >> >>"

	return objectTable{
		catalogObject:    catalogBody(""),
		pageTreeObject:   twoPageTree(),
		firstPage:        pageBody(letterBox, actionsFont, actionsContents1, fmt.Sprintf(openAction, ref(secondPage))),
		secondPage:       pageBody(letterBox, actionsFont, actionsContents2, fmt.Sprintf(openAction, ref(firstPage))),
		actionsFont:      body(helvetica),
		actionsContents1: stream("", marker(tag+" p1")),
		actionsContents2: stream("", marker(tag+" p2")),
	}.document(version17)
}

// inheritable lists the entries a page-tree node can pass to its descendants, as dictionary text with a
// leading space for each.
func (b PageBoxes) inheritable() string {
	var out strings.Builder

	for _, entry := range []struct{ key, value string }{
		{"MediaBox", b.Media}, {"CropBox", b.Crop}, {"Rotate", b.Rotate},
	} {
		if entry.value != "" {
			fmt.Fprintf(&out, " /%s %s", entry.key, entry.value)
		}
	}

	return out.String()
}

// own lists the entries of a page itself: the inheritable ones and UserUnit, which is not inheritable.
func (b PageBoxes) own() string {
	if b.UserUnit == "" {
		return b.inheritable()
	}

	return b.inheritable() + " /UserUnit " + b.UserUnit
}

// WithBoxes is a document with one page per entry of own ("<tag> p<N>"; a single page when own is
// empty). The marker sits near the lower-left corner of the MediaBox. Entries of inherited go on the root
// page-tree node, where pages without their own entry inherit them; UserUnit is not inheritable and is
// ignored there.
func WithBoxes(tag string, inherited PageBoxes, own ...PageBoxes) *Doc {
	if len(own) == 0 {
		own = []PageBoxes{{}}
	}

	treeExtra := inherited.inheritable() + " /Resources << /Font << /F1 " + ref(indexedFont) + " >> >>"

	return indexedTable(len(own), treeExtra, func(index int) pageParts {
		origin := markerOrigin(own[index], inherited)

		return pageParts{
			dict: body(
				"<< /Type /Page /Parent %s%s /Contents %s >>",
				ref(pageTreeObject),
				own[index].own(),
				ref(contentsObject(index)),
			),
			contents: stream("", markerAt(fmt.Sprintf("%s p%d", tag, index+1), origin.x, origin.y)),
		}
	}).document(version17)
}

// NestedBoxes is a two-page document ("<tag> p1", "<tag> p2") in a two-level page tree: the root
// supplies MediaBox [0 0 612 792], CropBox [50 60 500 700] and Rotate 90; an intermediate node holding
// page 2 overrides only CropBox with [10 10 300 400].
func NestedBoxes(tag string) *Doc {
	return objectTable{
		catalogObject: catalogBody(""),
		pageTreeObject: body(
			"<< /Type /Pages /Kids [%s %s] /Count 2 /MediaBox %s /CropBox [50 60 500 700] /Rotate 90 "+
				"/Resources << /Font << /F1 %s >> >> >>",
			ref(firstPage),
			ref(nestedInnerNode),
			letterBox,
			ref(nestedFont),
		),
		firstPage:  body("<< /Type /Page /Parent %s /Contents %s >>", ref(pageTreeObject), ref(nestedContents1)),
		secondPage: body("<< /Type /Page /Parent %s /Contents %s >>", ref(nestedInnerNode), ref(nestedContents2)),
		nestedInnerNode: body(
			"<< /Type /Pages /Parent %s /Kids [%s] /Count 1 /CropBox [10 10 300 400] >>", ref(pageTreeObject), ref(secondPage),
		),
		nestedFont:      body(helvetica),
		nestedContents1: stream("", marker(tag+" p1")),
		nestedContents2: stream("", marker(tag+" p2")),
	}.document(version17)
}

// Versioned is Plain with the given header version and, when catalog is not empty, a catalog /Version.
func Versioned(tag, header, catalog string) *Doc {
	table := plainTable(tag)

	if catalog != "" {
		table[catalogObject] = catalogBody(" /Version /" + catalog)
	}

	return table.document(header)
}

// Outlined is a two-page document ("<tag> p1", "<tag> p2") with an outline, page mode and language.
func Outlined(tag string) *Doc {
	table := pagesTable(tag, 2)

	table[catalogObject] = catalogBody(" /Outlines " + ref(outlineRoot) + " /PageMode /UseOutlines /Lang (en-US)")
	table[outlineRoot] = body("<< /Type /Outlines /First %s /Last %s /Count 2 >>", ref(outlineFirst), ref(outlineSecond))
	table[outlineFirst] = body(
		"<< /Title (First) /Parent %s /Next %s /Dest [%s /Fit] >>", ref(outlineRoot), ref(outlineSecond), ref(pageObject(0)),
	)
	table[outlineSecond] = body(
		"<< /Title (Second) /Parent %s /Prev %s /Dest [%s /Fit] >>", ref(outlineRoot), ref(outlineFirst), ref(pageObject(1)),
	)

	return table.document(version17)
}

// Tagged is a one-page document with a structure tree, /MarkInfo and a page /StructParents entry.
func Tagged(tag string) *Doc {
	table := plainTable(tag)

	table[catalogObject] = catalogBody(
		" /MarkInfo << /Marked true >> /StructTreeRoot " + ref(taggedStructRoot) + " /Lang (en)",
	)
	table[firstPage] = pageBody(letterBox, plainFont, plainContents, " /StructParents 0")
	table[plainContents] = stream("", []byte("/P <</MCID 0>> BDC "+string(marker(tag))+" EMC"))
	table[taggedStructRoot] = body("<< /Type /StructTreeRoot /K %s /ParentTree %s >>", ref(taggedStructElem), ref(taggedParentTree))
	table[taggedStructElem] = body("<< /Type /StructElem /S /P /P %s /Pg %s /K 0 >>", ref(taggedStructRoot), ref(firstPage))
	table[taggedParentTree] = body("<< /Nums [0 [%s]] >>", ref(taggedStructElem))

	return table.document(version17)
}

// Attachment is a one-page document with an embedded file "note.txt".
func Attachment(tag string) *Doc {
	table := plainTable(tag)

	table[catalogObject] = catalogBody(" /Names << /EmbeddedFiles << /Names [(note.txt) " + ref(attachmentFilespec) + "] >> >>")
	table[attachmentFilespec] = body(
		"<< /Type /Filespec /F (note.txt) /UF (note.txt) /EF << /F %s /UF %s >> >>", ref(attachmentData), ref(attachmentData),
	)
	table[attachmentData] = stream("/Type /EmbeddedFile", []byte("attachment of "+tag))

	return table.document(version17)
}

// Resource is a generated-pages document: page i shows label(i) on an A4 page when i is even and on a
// US Letter page when i is odd, all sharing one font object.
func Resource(pages int, label func(index int) string) *Doc {
	return indexedTable(pages, "", func(index int) pageParts {
		box := letterBox
		if index%2 == 0 {
			box = a4Box
		}

		return pageParts{
			dict:     pageBody(box, indexedFont, contentsObject(index), ""),
			contents: stream("", marker(label(index))),
		}
	}).document(version17)
}

// ImageRich is a one-page document marked tag whose page references a DeviceGray image XObject of about
// payload bytes (a multiple of 1024, at least 1024) of incompressible pseudo-random data seeded from the
// tag, a stand-in for a scanned page: distinct tags give distinct images, so an optimizer cannot merge them.
func ImageRich(tag string, payload int) *Doc {
	height := max(1, payload/imageWidth)
	data := make([]byte, imageWidth*height)
	state := seedOf(tag)

	for offset := 0; offset+wordBytes <= len(data); offset += wordBytes {
		state ^= state << shiftLeftFirst
		state ^= state >> shiftRight
		state ^= state << shiftLeftSecond

		binary.LittleEndian.PutUint64(data[offset:], state)
	}

	extra := fmt.Sprintf(
		"/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8",
		imageWidth,
		height,
	)

	table := plainTable(tag)
	table[firstPage] = body(
		"<< /Type /Page /Parent %s /MediaBox %s /Resources << /Font << /F1 %s >> /XObject << /Im0 %s >> >> /Contents %s >>",
		ref(pageTreeObject), letterBox, ref(plainFont), ref(imageObject), ref(plainContents),
	)
	table[plainContents] = stream("", []byte(string(marker(tag))+" q 100 0 0 100 50 500 cm /Im0 Do Q"))
	table[imageObject] = stream(extra, data)

	return table.document(version17)
}

// Titled is Plain with a document information dictionary carrying a title and author.
func Titled(tag string) *Doc {
	doc := Plain(tag)
	doc.Info = "<< /Title (SOURCE TITLE) /Author (SOURCE AUTHOR) /Subject (SOURCE SUBJECT) >>"

	return doc
}

// seedOf derives a nonzero xorshift state from tag (FNV-1a).
func seedOf(tag string) uint64 {
	state := uint64(fnvOffsetBasis)
	for index := range len(tag) {
		state ^= uint64(tag[index])
		state *= fnvPrime
	}

	return state | 1
}
