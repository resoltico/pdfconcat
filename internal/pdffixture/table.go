// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdffixture

import "fmt"

// MultipageTable has a TH on page 1 and a TD on page 3 whose Headers
// association names that TH. Page 2 is an independent data row.
func MultipageTable() *Doc {
	table := indexedTable(3, "", func(i int) pageParts {
		return pageParts{dict: pageBody(letterBox, indexedFont, contentsObject(i), fmt.Sprintf(" /StructParents %d", i)), contents: stream("", []byte(fmt.Sprintf("/P <</MCID 0>> BDC %s EMC", marker(fmt.Sprintf("cell%d", i)))))}
	})
	table[catalogObject] = catalogBody(" /MarkInfo << /Marked true >> /StructTreeRoot 10 0 R")
	table[10] = []byte("<< /Type /StructTreeRoot /K 16 0 R /ParentTree 11 0 R /IDTree 15 0 R >>")
	table[11] = []byte("<< /Nums [0 [12 0 R] 1 [13 0 R] 2 [14 0 R]] >>")
	for i := range 3 {
		role, attrs := "TD", ""
		if i == 0 {
			role = "TH"
		}
		if i == 2 {
			attrs = " /A << /O /Table /Headers [(header)] >>"
		}
		id := fmt.Sprintf("cell%d", i)
		if i == 0 {
			id = "header"
		}
		table[12+i] = []byte(fmt.Sprintf("<< /Type /StructElem /S /%s /P %d 0 R /Pg %d 0 R /K 0 /ID (%s)%s >>", role, 17+i, 4+2*i, id, attrs))
		table[17+i] = []byte(fmt.Sprintf("<< /Type /StructElem /S /TR /P 16 0 R /K %d 0 R >>", 12+i))
	}
	table[15] = []byte("<< /Names [(cell1) 13 0 R (cell2) 14 0 R (header) 12 0 R] >>")
	table[16] = []byte("<< /Type /StructElem /S /Table /P 10 0 R /K [17 0 R 18 0 R 19 0 R] >>")
	return table.document(version17)
}
