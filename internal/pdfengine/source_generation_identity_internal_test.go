// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestSourceGenerationIdentityPreservesOnlyMatchingContent(t *testing.T) {
	t.Parallel()

	for _, generation := range []int{0, 7} {
		for _, referenceGeneration := range []int{generation, generation + 1} {
			t.Run(fmt.Sprintf("object-%d-reference-%d", generation, referenceGeneration), func(t *testing.T) {
				t.Parallel()

				content := "1 0 0 rg 0 0 100 100 re f"
				bodies := []string{
					catalog,
					guardSinglePageTree,
					fmt.Sprintf(
						"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources <<>> /Contents 4 %d R >>",
						referenceGeneration,
					),
					string(fitFixtureStream("", []byte(content))),
				}
				data := sourceIdentityPDF(fitCatalogSourceVersion, "", bodies, map[int]int{4: generation}, nil)
				dir := t.TempDir()

				path := filepath.Join(dir, fitRenderSource)
				sourceWrite(t, path, data)

				engine := sourceNewEngine(t)

				for _, target := range []*PageSize{nil, {595, 842}, {612, 1008}} {
					sourceGenerationRoundTrip(t, engine, path, target, content, generation, referenceGeneration)
				}
			})
		}
	}
}

func TestSourceIncrementalGenerationKeepsTheCurrentIdentity(t *testing.T) {
	t.Parallel()

	content := "1 0 0 rg 0 0 100 100 re f"
	data := sourceIncrementalPDF(t, content)
	dir := t.TempDir()

	path := filepath.Join(dir, "incremental.pdf")
	sourceWrite(t, path, data)

	engine := sourceNewEngine(t)

	for _, target := range []*PageSize{nil, {595, 842}, {612, 1008}} {
		sourceCurrentGeneration(t, engine, path, target, content)
	}
}
