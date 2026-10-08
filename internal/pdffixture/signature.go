// SPDX-License-Identifier: MPL-2.0
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
