// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"fmt"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestDiagnosticConsumersPreserveFullRelationsAndStayOutOfPreviews(t *testing.T) {
	t.Parallel()
	saved := mustDecode(t, completeCheck)
	saved.Status = report.StatusInvalid
	saved.Diagnostics = []report.Diagnostic{
		{
			Stage:     report.Stage(fixtureLayoutStage),
			Code:      fixtureOverflowCode,
			Message:   "outside page",
			Consumers: []string{firstItemPointer, pointerItemOne},
		},
	}

	decoded := mustDecode(t, encodeReport(t, saved))
	if len(decoded.Diagnostics[0].Consumers) != 2 {
		t.Fatal("complete affected consumer relation was lost")
	}

	if len(decoded.Summary().Diagnostics[0].Consumers) != 0 {
		t.Fatal("default summary expanded affected consumers")
	}

	brief := queryAs[report.ViewResponse[report.DiagnosticView]](t, decoded, report.Request{View: report.ViewDiagnostics})

	full := queryAs[report.ViewResponse[report.DiagnosticView]](t, decoded, report.Request{View: report.ViewDiagnostics, Details: true})
	if len(brief.Records[0].Consumers) != 0 || len(full.Records[0].Consumers) != 2 {
		t.Fatal("diagnostic detail/preview consumer contracts differ")
	}

	for _, consumers := range [][]string{{"missing"}, {firstItemPointer, firstItemPointer}} {
		decoded.Diagnostics[0].Consumers = consumers
		if err := decoded.Validate(); err == nil {
			t.Fatalf("invalid consumer references accepted: %v", consumers)
		}
	}
}

func TestLargeConsumerRelationIsCompleteOnlyInExplicitDetailedRecord(t *testing.T) {
	t.Parallel()

	const count = 20000

	saved := failedReportFixture(
		report.Diagnostic{Stage: "s", Code: "c", Message: "m"}, report.Diagnostic{Stage: "s", Code: "c", Message: "m"})

	for index := range count {
		id := fmt.Sprintf("/items/%d", index)
		saved.Parts = append(
			saved.Parts,
			report.Part{ID: id, Kind: report.PartBlank, Pages: new(int64(1)), Origin: report.Position{File: argvFile}},
		)
		saved.Diagnostics[0].Consumers = append(saved.Diagnostics[0].Consumers, id)
	}

	if err := saved.Validate(); err != nil {
		t.Fatal(err)
	}

	decoded := mustDecode(t, encodeReport(t, saved))

	full := queryAs[report.ViewResponse[report.DiagnosticView]](t, decoded, report.Request{View: report.ViewDiagnostics, Details: true})
	if !full.OversizedRecord || full.NextOffset == nil || *full.NextOffset != 1 || len(full.Records) != 1 ||
		len(full.Records[0].Consumers) != count {
		t.Fatal("large complete consumer relation was truncated or did not advance paging")
	}

	brief := queryAs[report.ViewResponse[report.DiagnosticView]](t, decoded, report.Request{View: report.ViewDiagnostics})
	if len(brief.Records) != 2 || len(brief.Records[0].Consumers) != 0 {
		t.Fatal("brief diagnostics expanded consumer lists")
	}

	if len(jsonText(t, decoded.Summary())) > report.SummaryBytes {
		t.Fatal("consumer lists exceeded summary byte budget")
	}
}

func TestBuilderOwnsCapturedConsumerReferences(t *testing.T) {
	t.Parallel()

	builder := report.NewBuilder(testCommandCheck)
	consumers := []string{firstItemPointer}
	builder.AddDiagnostic(
		0,
		report.Diagnostic{
			Stage:     report.Stage(fixtureLayoutStage),
			Code:      fixtureOverflowCode,
			Message:   "outside page",
			Consumers: consumers,
		},
	)
	consumers[0] = fixtureMutation

	built := builder.Build(report.StatusInvalid)
	if built.Diagnostics[0].Consumers[0] != firstItemPointer {
		t.Fatal("caller changed captured consumers")
	}

	built.Diagnostics[0].Consumers[0] = fixtureMutationAgain
	if builder.Build(report.StatusInvalid).Diagnostics[0].Consumers[0] != firstItemPointer {
		t.Fatal("report changed builder consumers")
	}
}
