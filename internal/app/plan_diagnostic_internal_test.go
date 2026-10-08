// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/plan"
	"github.com/resoltico/pdfconcat/internal/report"
)

func TestPlanDiagnosticConvertsALocatedDecodingFailure(t *testing.T) {
	t.Parallel()
	_, err := plan.Decode(
		t.Context(),
		plan.Input{Name: inlineName},
		strings.NewReader("{\n  \"version\": 1,\n  \"items\": [\"a.pdf\"],\n  \"items\": []}"),
	)

	planError, found := errors.AsType[*plan.Error](err)
	if !found {
		t.Fatal(err)
	}

	diagnostic := planDiagnostic(planError)
	located := diagnostic.Location.File == inlineName && diagnostic.Location.Line == 4

	if diagnostic.Stage != report.StageSyntax || diagnostic.Code != "json_duplicate_member" || !located || diagnostic.Message == "" {
		t.Fatalf("decoder diagnostic adaptation: %+v", diagnostic)
	}

	builder := report.NewBuilder(checkName)
	builder.AddDiagnostic(0, diagnostic)

	if validationErr := builder.Build(report.StatusInvalid).Validate(); validationErr != nil {
		t.Fatal(validationErr)
	}
}
