// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package cli

import "testing"

func TestEveryOptionSingleEditFitsBelowSuggestionCutoff(t *testing.T) {
	t.Parallel()

	for _, spec := range optionSpecs() {
		if len(spec.name)+1 >= maxSuggestedOptionBytes {
			t.Errorf("one-edit spellings of %q must fit below suggestion cutoff %d", spec.name, maxSuggestedOptionBytes)
		}
	}
}
