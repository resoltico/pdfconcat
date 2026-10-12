// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine_test

import (
	"fmt"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

// BenchmarkRepeatedSourceCapacity holds source bytes/page count constant within each kind.
// Each case assembles one captured two-page source repeatedly; allocations and native process
// RSS observations are evidence of this workload, not a hard resource envelope.
func BenchmarkRepeatedSourceCapacity(b *testing.B) {
	for _, kind := range []string{"plain", "links", "forms"} {
		for _, occurrences := range []int{1, 10, 100} {
			b.Run(fmt.Sprintf("%s/occurrences=%d", kind, occurrences), func(b *testing.B) {
				engine := newEngine(b)
				document := pdffixture.Pages("repeat", 2)
				if kind == "links" {
					document = pdffixture.Links("repeat")
				}
				if kind == "forms" {
					document = pdffixture.Form("repeat")
				}
				path := writeDoc(b, b.TempDir(), "source", document)
				request := pdfengine.AssembleRequest{Sources: inspectAll(b, engine, []string{path}), ExpectedPages: 2 * occurrences}
				for range occurrences {
					request.Order = append(request.Order, pdfengine.SourcePages(0, 1, 2))
				}
				b.ReportAllocs()
				b.ResetTimer()
				runAssembleBenchmark(b, engine, request)
			})
		}
	}
}
