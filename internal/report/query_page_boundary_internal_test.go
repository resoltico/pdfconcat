// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestOversizedPageStopsBeforeMaterializingNextValidRecord(t *testing.T) {
	t.Parallel()

	saved := nodeSamples()[0]
	saved.Fonts[0].File = "/" + strings.Repeat("f", 128<<10)
	saved.Parts = append(saved.Parts, Part{
		ID: "/items/2", Kind: PartBlank, Origin: Position{File: internalFile},
		Range: &PageRange{Start: 3, End: 3}, Pages: new(int64(1)), Style: new(int),
	})
	saved.Counts.GeneratedPages = new(int64(2))
	saved.Counts.TotalPages = new(int64(3))

	saved.FinalizeDiagnostics()

	if err := saved.Validate(); err != nil {
		t.Fatalf("materialization fixture must be a supported report: %v", err)
	}

	var calls []int

	view, err := pageOf(
		ViewParts,
		len(saved.Parts),
		Request{Offset: new(int64(1)), Limit: new(int64(2)), Details: true},
		func(index int) PartView {
			calls = append(calls, index)

			return saved.partView(index, true)
		},
	)
	if err != nil || view.Returned != 1 || !view.OversizedRecord || view.NextOffset == nil || *view.NextOffset != 2 {
		t.Fatalf("oversized first record must determine this page: %+v, %v", view, err)
	}

	if !reflect.DeepEqual(calls, []int{1}) {
		t.Fatalf("oversized page unnecessarily materialized another valid record: %v", calls)
	}
}

func TestQueryAtAndBeyondEndReturnsHonestEmptyPage(t *testing.T) {
	t.Parallel()

	saved := nodeSamples()[0]
	saved.FinalizeDiagnostics()

	if err := saved.Validate(); err != nil {
		t.Fatal(err)
	}

	for _, offset := range []int64{int64(len(saved.Parts)), int64(len(saved.Parts)) + 1, math.MaxInt64} {
		response, err := saved.Query(Request{View: ViewParts, Offset: &offset})
		if err != nil {
			t.Fatal(err)
		}

		query := QueryResultOf(saved, response, "/pdfconcat", "evidence.json")
		view, ok := ContentOf[ViewResponse[PartView]](query.Result)

		if !ok || view.Offset != offset || view.Total != len(saved.Parts) || view.Returned != 0 || len(view.Records) != 0 ||
			view.NextOffset != nil ||
			view.OversizedRecord {
			t.Fatalf("empty terminal page fabricated records or continuation: %+v", view)
		}
	}
}

func TestDiagnosticPageJSONBudgetIncludesItsFinalNewline(t *testing.T) {
	t.Parallel()

	const transportBytes = 128 << 10

	saved, base := diagnosticNewlineBoundaryFixture(t)

	for _, finalBytes := range []int{transportBytes, transportBytes + 1} {
		t.Run(strconv.Itoa(finalBytes), func(t *testing.T) {
			t.Parallel()

			copyReport := *saved
			copyReport.Diagnostics = append([]Diagnostic{}, saved.Diagnostics...)
			copyReport.Diagnostics[0].Cause = strings.Repeat("x", finalBytes-base)

			response, queryErr := copyReport.Query(Request{View: ViewDiagnostics, Limit: new(int64(1)), Details: true})
			if queryErr != nil {
				t.Fatal(queryErr)
			}

			view, selected := ContentOf[ViewResponse[DiagnosticView]](response)
			if !selected || view.Returned != 1 || view.OversizedRecord != (finalBytes > transportBytes) {
				t.Fatalf("newline boundary changed oversized receipt: returned=%d oversized=%t", view.Returned, view.OversizedRecord)
			}

			encoded, encodeErr := EncodePretty(view)
			if encodeErr != nil || (finalBytes == transportBytes && len(encoded)+1 != transportBytes) {
				t.Fatalf("exact fitting page byte accounting: %d, %v", len(encoded)+1, encodeErr)
			}
		})
	}
}

func diagnosticNewlineBoundaryFixture(t *testing.T) (*Report, int) {
	t.Helper()

	saved := nodeSamples()[0]
	saved.Diagnostics = []Diagnostic{{Stage: "inspect", Code: "missing", Message: "source missing", Cause: "x"}}

	saved.FinalizeDiagnostics()

	if err := saved.Validate(); err != nil {
		t.Fatal(err)
	}

	initial, err := saved.Query(Request{View: ViewDiagnostics, Limit: new(int64(1)), Details: true})
	if err != nil {
		t.Fatal(err)
	}

	initialView, ok := ContentOf[ViewResponse[DiagnosticView]](initial)
	if !ok {
		t.Fatal("missing real diagnostic view")
	}

	payload, err := EncodePretty(initialView)
	if err != nil {
		t.Fatal(err)
	}

	return saved, len(payload)
}
