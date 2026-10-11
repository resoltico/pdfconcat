// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"slices"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/observation"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

type compiledPageMilestones struct{ counts []int64 }

func (recorded *compiledPageMilestones) Observe(milestone observation.Milestone) {
	if milestone.Phase == observation.Assembly && milestone.Unit == observation.CompiledPages {
		recorded.counts = append(recorded.counts, milestone.Completed)
	}
}

func TestPlaceReportsBoundedCumulativePageMilestones(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name           string
		initial, pages int
		want           []int64
	}{
		{name: "short run", pages: 3, want: []int64{3}},
		{name: "exact checkpoint", pages: cancelCheckInterval, want: []int64{cancelCheckInterval}},
		{name: "checkpoint and terminal", pages: cancelCheckInterval + 1, want: []int64{cancelCheckInterval, cancelCheckInterval + 1}},
		{name: "cumulative short run", initial: 1, pages: 3, want: []int64{4}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			recorded := &compiledPageMilestones{}
			built := pool{pdf: readUnvalidated(t, pdffixture.Plain("P")), observer: recorded}
			pages := []*poolPage{{dict: types.Dict{keyType: types.Name("Page")}}}
			final, err := built.place(t.Context(), "output.pdf", make([]*poolPage, scenario.initial), pages, scenario.pages)
			if err != nil || len(final) != scenario.initial+scenario.pages {
				t.Fatalf("placed %d pages, err=%v; want %d", len(final), err, scenario.initial+scenario.pages)
			}
			if !slices.Equal(recorded.counts, scenario.want) {
				t.Fatalf("compiled-page milestones %v, want %v", recorded.counts, scenario.want)
			}
		})
	}
}
