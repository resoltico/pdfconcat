// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdffixture

import "fmt"

// URILink has one unpainted URI link and an optional catalog URI/Base. Target and base are
// PDF literal-string contents, allowing tests to exercise escapes without a library serializer.
func URILink(tag, target, base string, isMap bool) *Doc {
	doc := Plain(tag)
	if base != "" {
		doc.Objs[catalogObject-1] = body("<< /Type /Catalog /Pages 2 0 R /URI << /Base (%s) >> >>", base)
	}

	annotation := len(doc.Objs) + 1
	doc.Objs[firstPage-1] = pageBody(letterBox, plainFont, plainContents, fmt.Sprintf(" /Annots [%d 0 R]", annotation))
	doc.Objs = append(doc.Objs, body(
		"<< /Type /Annot /Subtype /Link /Rect [50 600 200 620] /Border [0 0 0] /P 3 0 R /A << /S /URI /URI (%s) /IsMap %t >> >>",
		target, isMap,
	))

	return doc
}
