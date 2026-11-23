// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdffixture

import "fmt"

const signatureWidgetObject = 6

// SignatureValue is a PDF with a populated signature field. Its placeholder value is intentionally
// not a cryptographically valid signature; it tests refusal of reachable signature state.
func SignatureValue(tag string) *Doc {
	return signatureField(
		tag,
		" /V << /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached /ByteRange [0 1 2 3] /Contents <00> >>",
	)
}

// EmptySignatureField is an unsigned PDF with an ordinary unpopulated signature field.
func EmptySignatureField(tag string) *Doc { return signatureField(tag, "") }

func signatureField(tag, value string) *Doc {
	form := fmt.Sprintf(" /AcroForm << /Fields [%s] >>", ref(signatureWidgetObject))

	return objectTable{
		catalogObject:  catalogBody(form),
		pageTreeObject: body("<< /Type /Pages /Kids [%s] /Count 1 >>", ref(firstPage)),
		firstPage:      pageBody(letterBox, plainFont, plainContents, " /Annots ["+ref(signatureWidgetObject)+"]"),
		plainContents:  stream("", marker(tag)),
		plainFont:      body(helvetica),
		signatureWidgetObject: body(
			"<< /Type /Annot /Subtype /Widget /FT /Sig /T (approval) /Rect [0 0 0 0] /F 4 /P %s%s >>",
			ref(firstPage),
			value,
		),
	}.document(version17)
}

// SignatureArtwork has a normal appearance with a nonzero BBox origin and a
// nonidentity Matrix. The original page ends with an altered CTM and clip.
func SignatureArtwork() *Doc {
	d := SignatureValue("signature artwork")
	d.Objs[2] = body("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 400] /CropBox [10 20 290 380] /Rotate 90 /Resources << >> /Contents 4 0 R /Annots [6 0 R] >>")
	d.Objs[3] = stream("", []byte("0 0 1 rg 15 25 20 30 re f 2 0 0 2 11 13 cm 0 0 1 1 re W n\n"))
	d.Objs[5] = body("<< /Type /Annot /Subtype /Widget /FT /Sig /T (approval) /Rect [75 85 175 145] /F 4 /P 3 0 R /V 7 0 R /AP << /N 8 0 R >> >>")
	d.Objs = append(d.Objs,
		body("<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached /ByteRange [0 1 2 3] /Contents <00> >>"),
		stream("/Type /XObject /Subtype /Form /BBox [10 15 60 45] /Matrix [1 0 0 2 5 3] /Resources << >>",
			[]byte("1 0 0 rg 10 15 50 30 re f\n")))
	return d
}

// SignatureColorArtwork exercises implicit device color spaces in either the
// signature appearance or the original page program.
func SignatureColorArtwork(appearance, inherited bool) *Doc {
	d := SignatureArtwork()
	const resources = "<< /ColorSpace << /DefaultRGB [/CalRGB << /WhitePoint [0.9505 1 1.089] /Gamma [2 2 2] /Matrix [0.4124 0.2126 0.0193 0.3576 0.7152 0.1192 0.1805 0.0722 0.9505] >>] >> >>"
	if inherited || !appearance {
		d.Objs[2] = body("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 400] /CropBox [10 20 290 380] /Rotate 90 /Resources %s /Contents 4 0 R /Annots [6 0 R] >>", resources)
	}
	if !appearance {
		d.Objs[3] = stream("", []byte("0 0 .5 rg 15 25 20 30 re f\n"))
		return d
	}
	form := "/Type /XObject /Subtype /Form /BBox [10 15 60 45] /Matrix [1 0 0 2 5 3]"
	if !inherited {
		form += " /Resources " + resources
	}
	d.Objs[7] = stream(form, []byte(".5 0 0 rg 10 15 50 30 re f\n"))
	return d
}
