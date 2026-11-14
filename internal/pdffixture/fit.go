// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdffixture

import "fmt"

// FitMarks places blue visible ink and red hidden ink around a 50-point square visible frame.
// Prefix supplies graphics-state escape controls. The source has a real font resource and text decoys.
func FitMarks(prefix string) *Doc {
	doc := Plain("fit marks")
	doc.Objs[firstPage-1] = pageBody("[0 0 100 100]", plainFont, plainContents, " /CropBox [25 25 75 75]")
	doc.Objs[plainContents-1] = stream("", []byte(prefix+
		"1 0 0 rg 0 0 15 15 re f 0 0 1 rg 40 40 10 10 re f "+
		"% q Q are comments\nBT /F1 1 Tf 30 30 Td (q Q /q /Q) Tj ET\n"))

	return doc
}

// FitMarkStreams puts a color operator and its following number in separate streams without
// trailing/leading whitespace. A raw byte join loses the visible blue mark's color.
func FitMarkStreams() *Doc {
	doc := FitMarks("")
	doc.Objs[firstPage-1] = body("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /CropBox [25 25 75 75] " +
		"/Resources << /Font << /F1 5 0 R >> >> /Contents [4 0 R 6 0 R] >>")
	doc.Objs[plainContents-1] = stream("", []byte("0 0 1 rg"))
	doc.Objs = append(doc.Objs, stream("", []byte("40 40 10 10 re f BT /F1 1 Tf 30 30 Td (q Q /q /Q) Tj ET\n"+
		"1 0 0 rg 0 0 15 15 re f")))

	return doc
}

// FitPatterns uses one resource graph on differently sized/rotated pages. The
// same nonidentity tiling or shading pattern is used directly and inside a Form.
func FitPatterns(shading bool) *Doc {
	pattern := stream("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Matrix [0 1 -1 0 7 3] /Resources << >>", []byte("1 0 0 rg 0 0 5 10 re f 0 0 1 rg 5 0 5 10 re f"))
	if shading {
		pattern = body("<< /Type /Pattern /PatternType 2 /Matrix [0 1 -1 0 110 0] /Shading << /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 100 0] /Function << /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [0 0 1] /N 1 >> /Extend [true true] >> >>")
	}
	return &Doc{Version: "1.7", Objs: [][]byte{
		body("<< /Type /Catalog /Pages 2 0 R >>"),
		body("<< /Type /Pages /Count 2 /Kids [3 0 R 4 0 R] /MediaBox [10 20 110 120] /CropBox [10 20 110 120] /Resources 5 0 R >>"),
		body("<< /Type /Page /Parent 2 0 R /Contents 8 0 R >>"),
		body("<< /Type /Page /Parent 2 0 R /MediaBox [10 20 210 120] /CropBox [10 20 210 120] /Rotate 90 /Contents 8 0 R >>"),
		body("<< /Pattern << /P 6 0 R >> /XObject << /Nested 7 0 R >> >>"),
		pattern,
		stream("/Type /XObject /Subtype /Form /BBox [0 0 30 30] /Matrix [1 0 0 1 0 0] /Resources << /Pattern << /P 6 0 R >> >>", []byte("/Pattern cs /P scn 0 0 30 30 re f")),
		stream("", []byte("/Pattern cs /P scn 10 20 70 100 re f q 1 0 0 1 80 30 cm /Nested Do Q q 0 1 -1 0 110 70 cm /Nested Do Q")),
	}}
}

// FitAnnotation authors semantic paths and deliberately asymmetric appearances.
// The normal, rollover and down states share a stream with a nonidentity Matrix.
func FitAnnotation(subtype, geometry string, appearance bool, rotation, flags int) *Doc {
	doc := Plain("annotation")
	doc.Objs[firstPage-1] = pageBody("[0 0 100 100]", plainFont, plainContents, fmt.Sprintf(" /Rotate %d /Annots [6 0 R]", rotation))
	ap := ""
	if appearance {
		ap = " /AP << /N 7 0 R /R << /On 7 0 R /Off 7 0 R >> /D 7 0 R >> /AS /On"
	}
	doc.Objs = append(doc.Objs, body("<< /Type /Annot /Subtype /%s /Rect [10 10 90 90] /F %d /C [1 0 0] /BS << /W 2 >> %s%s >>", subtype, flags, geometry, ap))
	if appearance {
		doc.Objs = append(doc.Objs, stream("/Type /XObject /Subtype /Form /BBox [0 0 80 40] /Matrix [0 2 -1 0 40 0] /Resources << >>", []byte("1 0 0 RG 3 w 4 4 m 76 36 l S")))
	}
	return doc
}
