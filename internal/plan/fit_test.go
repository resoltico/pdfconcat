// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/plan"
)

const fitDeclarationPointer = "/fit_to"

func TestFitDeclarationKeepsTargetAndLocation(t *testing.T) {
	t.Parallel()

	for _, paper := range []assembly.FitTarget{assembly.FitA4, assembly.FitLegal} {
		document := "{\n\"version\":1,\n\"fit_to\":\"" + string(paper) + "\",\n\"items\":[\"source.pdf\"]}"

		job, err := plan.Decode(t.Context(), plan.Input{Name: inputName}, strings.NewReader(document))
		if err != nil {
			t.Fatal(err)
		}

		location := assembly.Locate(job.Source, job.FitTo.Origin, fitDeclarationPointer)
		if job.FitTo.Value != paper || location.Pointer != fitDeclarationPointer || location.Line != 3 || location.Column != 10 {
			t.Fatalf("effective declaration: %s %+v", job.FitTo.Value, location)
		}
	}
}

func TestFitDeclarationRejectsUnsupportedValuesWithLocation(t *testing.T) {
	t.Parallel()

	for _, value := range []string{`null`, `123`, `false`, `{}`, `[]`, `"a4"`, `"Letter"`, `""`} {
		document := `{"version":1,"fit_to":` + value + `,"items":["source.pdf"]}`
		_, err := plan.Decode(t.Context(), plan.Input{Name: inputName}, strings.NewReader(document))

		expected := plan.CodeWrongType
		if value == `null` {
			expected = plan.CodeNull
		}

		if strings.HasPrefix(value, `"`) {
			expected = plan.CodeBadValue
		}

		failure := codedError(t, err, expected)
		if failure.Location.Pointer != fitDeclarationPointer || failure.Stage != plan.StageShape {
			t.Fatalf("fit refusal lost declaration: %+v", failure)
		}
	}
}
