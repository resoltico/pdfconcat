// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import "github.com/resoltico/pdfconcat/internal/oracle"

// Physical itineraries are reviewed literals, not an implementation of layout.
var corpusProfiles = []corpusProfile{
	{ID: "profile-01", Factors: [8]bool{false, false, false, false, false, false, false, false}, Sequence: "page source:1:1 source:1:2 source:1:3 page page", Sections: [][3]int64{}, Outlines: corpusExpectedReportTree([]int{2, 3, 4}, []int{}, 0, 0)},
	{ID: "profile-02", Factors: [8]bool{true, false, false, false, true, true, true, false}, Sequence: "page contents divider source:1:1 source:1:2 source:1:3 page padding divider page", Sections: [][3]int64{{3, 3, 10}, {9, 9, 10}}, Outlines: corpusExpectedReportTree([]int{4, 5, 6}, []int{}, 3, 9)},
	{ID: "profile-03", Factors: [8]bool{false, true, false, false, true, false, false, true}, Sequence: "page source:1:1 source:1:3 page page", Sections: [][3]int64{}, Outlines: corpusExpectedReportTree([]int{2, 3}, []int{}, 0, 0)},
	{ID: "profile-04", Factors: [8]bool{true, true, false, false, false, true, true, true}, Sequence: "page contents divider source:1:1 source:1:3 page divider page", Sections: [][3]int64{{3, 3, 8}, {7, 7, 8}}, Outlines: corpusExpectedReportTree([]int{4, 5}, []int{}, 3, 7)},
	{ID: "profile-05", Factors: [8]bool{false, false, true, false, false, true, false, true}, Sequence: "page contents divider source:1:1 source:1:2 source:1:3 page divider source:2:1 source:2:2 source:2:3 page", Sections: [][3]int64{{3, 3, 12}, {8, 8, 12}}, Outlines: corpusExpectedReportTree([]int{4, 5, 6}, []int{9, 10, 11}, 3, 8)},
	{ID: "profile-06", Factors: [8]bool{true, false, true, false, true, false, true, true}, Sequence: "page padding source:1:1 source:1:2 source:1:3 page source:2:1 source:2:2 source:2:3 page", Sections: [][3]int64{}, Outlines: corpusExpectedReportTree([]int{3, 4, 5}, []int{7, 8, 9}, 0, 0)},
	{ID: "profile-07", Factors: [8]bool{false, true, true, false, true, true, false, false}, Sequence: "page contents divider source:1:1 source:1:3 page divider source:2:1 source:2:3 page", Sections: [][3]int64{{3, 3, 10}, {7, 7, 10}}, Outlines: corpusExpectedReportTree([]int{4, 5}, []int{8, 9}, 3, 7)},
	{ID: "profile-08", Factors: [8]bool{true, true, true, false, false, false, true, false}, Sequence: "page padding source:1:1 source:1:3 page padding source:2:1 source:2:3 page", Sections: [][3]int64{}, Outlines: corpusExpectedReportTree([]int{3, 4}, []int{7, 8}, 0, 0)},
	{ID: "profile-09", Factors: [8]bool{false, false, false, true, false, false, true, true}, Sequence: "page padding source:1:1 source:1:2 source:1:3 page page", Sections: [][3]int64{}, Outlines: corpusExpectedReportTree([]int{3, 4, 5}, []int{}, 0, 0)},
	{ID: "profile-10", Factors: [8]bool{true, false, false, true, true, true, false, true}, Sequence: "page contents divider source:1:1 source:1:2 source:1:3 page divider page", Sections: [][3]int64{{3, 3, 9}, {8, 8, 9}}, Outlines: corpusExpectedReportTree([]int{4, 5, 6}, []int{}, 3, 8)},
	{ID: "profile-11", Factors: [8]bool{false, true, false, true, true, false, true, false}, Sequence: "page padding source:1:1 source:1:3 page padding page", Sections: [][3]int64{}, Outlines: corpusExpectedReportTree([]int{3, 4}, []int{}, 0, 0)},
	{ID: "profile-12", Factors: [8]bool{true, true, false, true, false, true, false, false}, Sequence: "page contents divider source:1:1 source:1:3 page divider page", Sections: [][3]int64{{3, 3, 8}, {7, 7, 8}}, Outlines: corpusExpectedReportTree([]int{4, 5}, []int{}, 3, 7)},
	{ID: "profile-13", Factors: [8]bool{false, false, true, true, false, true, true, false}, Sequence: "page contents divider source:1:1 source:1:2 source:1:3 page padding divider source:2:1 source:2:2 source:2:3 page", Sections: [][3]int64{{3, 3, 13}, {9, 9, 13}}, Outlines: corpusExpectedReportTree([]int{4, 5, 6}, []int{10, 11, 12}, 3, 9)},
	{ID: "profile-14", Factors: [8]bool{true, false, true, true, true, false, false, false}, Sequence: "page source:1:1 source:1:2 source:1:3 page source:2:1 source:2:2 source:2:3 page", Sections: [][3]int64{}, Outlines: corpusExpectedReportTree([]int{2, 3, 4}, []int{6, 7, 8}, 0, 0)},
	{ID: "profile-15", Factors: [8]bool{false, true, true, true, true, true, true, true}, Sequence: "page contents divider source:1:1 source:1:3 page divider source:2:1 source:2:3 page", Sections: [][3]int64{{3, 3, 10}, {7, 7, 10}}, Outlines: corpusExpectedReportTree([]int{4, 5}, []int{8, 9}, 3, 7)},
	{ID: "profile-16", Factors: [8]bool{true, true, true, true, false, false, false, true}, Sequence: "page source:1:1 source:1:3 page source:2:1 source:2:3 page", Sections: [][3]int64{}, Outlines: corpusExpectedReportTree([]int{2, 3}, []int{5, 6}, 0, 0)},
	{ID: "all-profiles", Factors: [8]bool{true, true, true, true, true, true, true, true}, Sequence: "page contents divider source:1:1 source:1:3 page divider source:2:1 source:2:3 page", Sections: [][3]int64{{3, 3, 10}, {7, 7, 10}}, Outlines: corpusExpectedReportTree([]int{4, 5}, []int{8, 9}, 3, 7)},
}

func corpusExpectedReportRoots(pages []int) []oracle.Outline {
	if len(pages) == 0 {
		return nil
	}
	titles := []string{"Report summary", "Comparison chart", "Supporting notes"}
	if len(pages) == 2 {
		titles = []string{"Report summary", "Supporting notes"}
	}
	var roots []oracle.Outline
	for n, page := range pages {
		roots = append(roots, oracle.Outline{Title: titles[n], Targets: []int{page}})
	}
	return roots
}
func corpusExpectedReportTree(first, second []int, evidence, appendix int) []oracle.Outline {
	roots := corpusExpectedReportRoots(first)
	later := corpusExpectedReportRoots(second)
	if evidence == 0 {
		return append(roots, later...)
	}
	child := oracle.Outline{Title: "Appendix", Targets: []int{appendix}, Children: later, Open: len(later) > 0}
	return []oracle.Outline{{Title: "Evidence", Targets: []int{evidence}, Open: true, Children: append(roots, child)}}
}
