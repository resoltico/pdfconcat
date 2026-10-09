// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdffixture

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
