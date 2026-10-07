// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/plan"
)

// withoutOrigins clears what legitimately differs between sources: where each value was written.
func withoutOrigins(layout *assembly.Layout) assembly.Layout {
	flat := *layout.Flat
	flat.Source = nil
	flat.Contributions = append([]assembly.Contribution(nil), flat.Contributions...)

	for index := range flat.Contributions {
		flat.Contributions[index].Origin = assembly.Origin{}
	}

	flat.Files = append([]assembly.SourceFile(nil), flat.Files...)
	for index := range flat.Files {
		flat.Files[index].FirstUse = assembly.Origin{}
	}

	flat.Styles = append([]assembly.LayeredStyle(nil), flat.Styles...)
	for index := range flat.Styles {
		flat.Styles[index].FirstUse = assembly.Origin{}
	}

	result := *layout
	result.Source, result.Flat = nil, &flat

	result.Specs = append([]assembly.ResolvedSpec(nil), layout.Specs...)

	for index := range result.Specs {
		result.Specs[index].Origins = nil
	}

	return result
}

func TestDirectOperandsFilePlanAndInlinePlanResolveIdentically(t *testing.T) {
	t.Parallel()

	cwd := hostPath("/work/cwd")
	operands := []assembly.Operand{
		{Position: 0, Path: aPath},
		{Position: 1, Blank: true},
		{Position: 3, Path: blankOperand},
		{Position: 4, Path: "sub/Ābols b.pdf"},
		{Position: 5, Blank: true},
		{Position: 6, Path: aPath},
	}
	planText := `{"version":1,"items":["a.pdf",{"blank":{}},"--blank","sub/Ābols b.pdf",{"blank":{},"count":1},"a.pdf"]}`

	direct, err := assembly.NewOperandJob(cwd, operands)
	if err != nil {
		t.Fatal(err)
	}

	jobs := map[string]*assembly.Job{"direct": direct}

	for _, name := range []string{"job.json", "<inline>"} {
		jobs[name], err = plan.Decode(context.Background(), plan.Input{Name: name, BaseDir: cwd}, strings.NewReader(planText))
		if err != nil {
			t.Fatal(err)
		}
	}

	byName := map[string]assembly.SourceGeometry{
		aPath: source(
			2,
			geom(100, 200),
			geom(110, 210),
		),
		blankOperand:  source(1, geom(5, 6), geom(5, 6)),
		"Ābols b.pdf": source(3, geom(70, 80), geom(71, 81)),
	}

	var reference assembly.Layout

	for name, job := range jobs {
		layout := withoutOrigins(resolveNamed(t, job, byName))
		if reference.Totals == (assembly.Totals{}) {
			reference = layout
		}

		if !reflect.DeepEqual(layout, reference) {
			t.Errorf("%s resolves differently:\n%+v\n%+v", name, layout, reference)
		}
	}

	if reference.Totals != (assembly.Totals{Source: 2 + 1 + 3 + 2, Generated: 2, Total: 10}) {
		t.Errorf(detailedValueFormat, reference.Totals)
	}
}
