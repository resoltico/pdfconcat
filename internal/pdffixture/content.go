// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdffixture

import (
	"bytes"
	"strings"
)

// EncodedStream declares raw bytes and literal PDF Filter/DecodeParms syntax.
// Empty Filter means unfiltered content; an array can describe a filter chain.
type EncodedStream struct {
	Data                []byte
	Filter, DecodeParms string
}

// EncodedPages supplies each page's content-stream array independently of any
// PDF decoder. Existing marker streams are left unreferenced, not displayed.
func EncodedPages(tag string, pages ...[]EncodedStream) *Doc {
	d := pagesTable(tag, len(pages)).document(version17)
	for i, streams := range pages {
		refs := make([]string, 0, len(streams))
		for _, encoded := range streams {
			extra := ""
			if encoded.Filter != "" {
				extra += "/Filter " + encoded.Filter
			}
			if encoded.DecodeParms != "" {
				extra += " /DecodeParms " + encoded.DecodeParms
			}
			d.Objs = append(d.Objs, stream(extra, encoded.Data))
			refs = append(refs, ref(len(d.Objs)))
		}
		d.Objs[pageObject(i)-1] = bytes.Replace(d.Objs[pageObject(i)-1],
			[]byte("/Contents "+ref(contentsObject(i))), []byte("/Contents ["+strings.Join(refs, " ")+"]"), 1)
	}
	return d
}
