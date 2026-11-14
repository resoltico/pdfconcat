// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package result_test

import (
	"reflect"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/result"
)

func TestFullPreservesDistinctFindingsAndMergesOnlyEquivalentConsumers(t *testing.T) {
	base := diag.New(diag.SourceViewerSettingsRemoved, "Viewer setting omitted.", "first")
	base.File = "/source.pdf"
	base.PDFLocation = &diag.PDFLocation{Object: 1, Path: "Catalog.PageLayout"}
	equivalent := base
	equivalent.PDFLocation = &diag.PDFLocation{Object: 1, Path: "Catalog.PageLayout"}
	equivalent.Items = []string{"second", "first"}
	otherEntry := base
	otherEntry.PDFLocation = &diag.PDFLocation{Object: 1, Path: "Catalog.PageMode"}
	otherMessage := base
	otherMessage.Message = "Another disposition."
	otherDetail := base
	otherDetail.Detail = "Additional backend evidence."
	otherLocation := base
	otherLocation.Location = &diag.Location{File: "/plan.json", Line: 2, Column: 3, Offset: 8, Ref: "/items/0"}
	r := result.New("check")
	r.Diagnostics = []diag.Diagnostic{base, equivalent, otherEntry, otherMessage, otherDetail, otherLocation}
	full := r.Full()
	if len(full.Diagnostics) != 5 || !reflect.DeepEqual(full.Diagnostics[0].Items, []string{"first", "second"}) {
		t.Fatalf("findings were lost or duplicated: %+v", full.Diagnostics)
	}
	if !reflect.DeepEqual(full.Full(), full) {
		t.Fatal("normalization is not idempotent")
	}
	if !reflect.DeepEqual(r.Diagnostics[0].Items, []string{"first"}) || len(r.Diagnostics) != 6 {
		t.Fatal("normalization mutated the input")
	}
}

func TestFullOrdersByOutcomePriorityAndPreservesPeerOrder(t *testing.T) {
	r := result.New("check")
	r.Diagnostics = []diag.Diagnostic{
		diag.New(diag.PlanNull, "invalid first"),
		diag.New(diag.SourceUnreadable, "failed first"),
		diag.New(diag.PlanValue, "invalid second"),
		diag.New(diag.SourceXfaRemoved, "warning"),
		diag.New(diag.ReportWriteFailed, "failed second"),
		diag.New(diag.Interrupted, "interrupted"),
	}
	full := r.Full()
	want := []string{"failed second", "interrupted", "failed first", "invalid first", "invalid second", "warning"}
	for i, message := range want {
		if full.Diagnostics[i].Message != message {
			t.Fatalf("priority or peer order changed at %d: %+v", i, full.Diagnostics)
		}
	}
	if full.Status != diag.StatusInterrupted || r.Diagnostics[0].Message != "invalid first" {
		t.Fatal("ordering changed outcome or input", full)
	}
}
