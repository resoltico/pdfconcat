// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package scale_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
	"github.com/resoltico/pdfconcat/test/scale"
)

const (
	imagePageMarker = "IMAGE"
)

func TestSourceAppearanceRejectsImageLossAndPlacementWithMarkerIntact(t *testing.T) {
	t.Parallel()
	tools := pdforacle.RequireTools(t)
	dir := t.TempDir()

	source := filepath.Join(dir, "source.pdf")
	if err := pdffixture.ImageRich(imagePageMarker, 512<<10).WriteFile(source); err != nil {
		t.Fatal(err)
	}

	engine, err := pdfengine.New()
	if err != nil {
		t.Fatal(err)
	}

	info, err := engine.Inspect(context.Background(), source, nil)
	if err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(dir, "assembled.pdf")

	req := &pdfengine.AssembleRequest{
		Destination:   output,
		Sources:       []pdfengine.SourceFile{{Path: source, Info: info}},
		Order:         []pdfengine.Run{pdfengine.SourcePages(0, 1, 1)},
		ExpectedPages: 1,
	}
	if assemblyErr := engine.Assemble(context.Background(), req); assemblyErr != nil {
		t.Fatal(assemblyErr)
	}

	workload := &scale.Workload{
		Expectation:   pdforacle.Expectation{Pages: []pdforacle.ExpectedPage{{Text: imagePageMarker}}},
		VisualSources: []scale.VisualSource{{Path: source, SourcePage: 1, OutputPage: 1}},
	}

	good, err := scale.Verify(tools, workload, output)
	if err != nil || len(good.Findings) > 0 {
		t.Fatalf("positive source appearance: %v %v", good.Findings, err)
	}

	for _, kind := range []string{"removed", "moved", "pixels"} {
		t.Run(kind, func(t *testing.T) { t.Parallel(); assertImageCorruptionDetected(t, tools, workload, dir, kind) })
	}
}

func assertImageCorruptionDetected(t *testing.T, tools pdforacle.Tools, workload *scale.Workload, dir, kind string) {
	t.Helper()

	doc := pdffixture.ImageRich(imagePageMarker, 512<<10)

	switch kind {
	case "removed":
		doc = pdffixture.Plain(imagePageMarker)
	case "moved":
		doc.Objs[3] = bytes.ReplaceAll(doc.Objs[3], []byte("50 500 cm"), []byte("200 500 cm"))
	case "pixels":
		doc.Objs[3] = bytes.ReplaceAll(doc.Objs[3], []byte("/Im0 Do"), []byte("0 g 0 0 1 1 re f"))
	default:
		t.Fatalf("unknown image corruption %s", kind)
	}

	bad := filepath.Join(dir, kind+".pdf")
	if err := doc.WriteFile(bad); err != nil {
		t.Fatal(err)
	}

	result, err := scale.Verify(tools, workload, bad)
	if err != nil {
		t.Fatal(err)
	}

	found := false

	for _, finding := range result.Findings {
		if strings.Contains(finding, "source appearance:") {
			found = true
		}
	}

	if !found {
		t.Fatalf("%s image corruption with intact text marker escaped oracle: %v", kind, result.Findings)
	}
}
