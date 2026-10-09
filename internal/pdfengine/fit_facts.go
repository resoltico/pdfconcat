// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import "fmt"

func (s SourceInfo) checkFits(target PageSize) error {
	next := 1

	for index := range s.Fits {
		interval := &s.Fits[index]
		if interval.First != next || interval.Last < next || interval.Last > s.Pages || interval.Fit.Target != target {
			return fmt.Errorf("%w: fitting facts must cover every source page at the requested target", errFitUnsupported)
		}

		next = interval.Last + 1
	}

	if next != s.Pages+1 {
		return fmt.Errorf("%w: missing all-page fit inspection", errFitUnsupported)
	}

	return nil
}
