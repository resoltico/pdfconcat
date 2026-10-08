// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/resoltico/pdfconcat/internal/report"
)

const recoveryDirectoryMember = "recovery_directory_from"

func TestSummaryJSONLinePreservesExactlyFittingForeignCause(t *testing.T) {
	t.Parallel()

	const wireLimit = 2048

	saved := mustDecode(t, failedCheck)
	saved.Diagnostics[0].Cause = "x"

	initial, err := report.Encode(saved.Summary())
	if err != nil {
		t.Fatal(err)
	}

	for _, wireSize := range []int{wireLimit, wireLimit + 1} {
		cause := strings.Repeat("x", wireSize-1-len(initial)+1)
		saved.Diagnostics[0].Cause = cause
		restored := mustDecode(t, encodeReport(t, saved))
		summary := restored.Summary()

		payload, encodeErr := report.Encode(summary)
		if encodeErr != nil || len(payload)+1 > wireLimit {
			t.Fatalf("summary line is %d bytes: %v", len(payload)+1, encodeErr)
		}

		if wireSize == wireLimit && (len(payload)+1 != wireLimit || summary.Diagnostics[0].Cause != cause) {
			t.Fatal("exactly fitting cause must be preserved without optional loss")
		}

		if restored.Diagnostics[0].Cause != cause {
			t.Fatal("summary changed complete report evidence")
		}
	}
}

func TestSummaryMessageCutFlagsDescribeBothPreviewLimits(t *testing.T) {
	t.Parallel()

	for _, message := range []string{strings.Repeat("m", 1000), strings.Repeat("😀", 160)} {
		saved := mustDecode(t, failedCheck)
		saved.Diagnostics[0].Message = message
		saved.Diagnostics[0].Cause = strings.Repeat("cause ", 1000)
		saved = mustDecode(t, encodeReport(t, saved))

		summary := saved.Summary()
		if !summary.Diagnostics[0].MessageTruncated || summary.Diagnostics[0].Message == message ||
			saved.Diagnostics[0].Message != message {
			t.Fatal("lost message bytes must carry a cut flag and preserve complete evidence")
		}

		var human strings.Builder
		if err := summary.RenderText(&human); err != nil {
			t.Fatal(err)
		}

		if !strings.Contains(human.String(), " [message cut]") {
			t.Fatal("human message preview concealed truncation")
		}
	}
}

func summaryFailureWithProtectedRecovery(t *testing.T) *report.Report {
	t.Helper()

	saved := mustDecode(t, failedCheck)
	saved.Publication.Output = strings.Repeat("o", 96)
	saved.Publication.ReportPath = strings.Repeat("r", 96)
	saved.Publication.ReportStatus = report.ReportFailed

	saved.Diagnostics = make([]report.Diagnostic, 2)
	for i := range saved.Diagnostics {
		saved.Diagnostics[i] = report.Diagnostic{
			Stage: report.Stage(strings.Repeat("s", 64)), Code: report.Code(strings.Repeat("c", 64)),
			Message: strings.Repeat("m", 256), Path: strings.Repeat("p", 64), Cause: strings.Repeat("a", 64),
			Location: &report.Location{File: strings.Repeat("f", 64), Pointer: strings.Repeat("/", 64)},
		}
	}

	saved.Diagnostics[1].Recovery = &report.Recovery{Action: "choose_new_report", ReportFrom: "unused_report_target"}

	encoded := encodeReport(t, saved)
	if schemaVerdict(t, reportSchema(t), []byte(encoded)) != schemaAccept {
		t.Fatal("fixture violates complete report schema")
	}

	return mustDecode(t, encoded)
}

func TestSummaryFallbackStopsLosingDetailAsSoonAsItFits(t *testing.T) {
	t.Parallel()

	saved := summaryFailureWithProtectedRecovery(t)
	summary := saved.Summary()

	payload, err := report.Encode(summary)
	if err != nil || len(payload)+1 > 2048 || len(summary.Diagnostics) != 2 {
		t.Fatalf("protected summary budget: %d bytes, %v", len(payload)+1, err)
	}

	first, second := summary.Diagnostics[0], summary.Diagnostics[1]
	requireSummaryDetail(t, first, saved.Diagnostics[0], "")
	requireSummaryDetail(t, second, saved.Diagnostics[1], saved.Diagnostics[1].Cause)

	if len(summary.TruncatedFields) != 1 || summary.TruncatedFields[0] != "diagnostics/0/cause" {
		t.Fatalf("loss labels describe %v, want only omitted primary cause", summary.TruncatedFields)
	}

	if saved.Diagnostics[0].Cause == "" || saved.Diagnostics[0].Location == nil {
		t.Fatal("fallback changed complete evidence")
	}
}

func TestSummaryTextBudgetCoversQuotedRecoveryAndForeignDetails(t *testing.T) {
	t.Parallel()

	for _, character := range []string{"\x7f", "\u0080", "\u200b", "\u2060", "\ufeff"} {
		saved := summaryFailureWithProtectedRecovery(t)
		saved.Publication.Published = true
		saved.Publication.RecoveryReport = strings.Repeat("a/", 100) + strings.Repeat(character, 96/len(character))
		saved.Publication.RecoveryState = report.RecoveryCurrent
		saved = mustDecode(t, encodeReport(t, saved))
		summary := saved.Summary()
		summary.BindContinuation(programName, strings.Repeat("report/", 1000), originalReportReference)
		encoded, err := report.Encode(summary)

		var human strings.Builder
		if textErr := summary.RenderText(&human); textErr != nil {
			t.Fatal(textErr)
		}

		if err != nil || len(encoded)+1 > 2048 || human.Len() > 2048 {
			t.Fatalf("quoted recovery exceeds summary transport: JSON%d text%d error%v", len(encoded)+1, human.Len(), err)
		}

		if !summary.NextOmitted || summary.NextReference == nil {
			t.Fatal("oversized continuation lost reconstruction authority")
		}

		if saved.Publication.RecoveryReport == "" {
			t.Fatal("summary erased complete recovery reference")
		}
	}
}

func requireSummaryDetail(t *testing.T, got report.DiagnosticView, original report.Diagnostic, cause string) {
	t.Helper()

	if got.Cause != cause || got.Path != original.Path || !reflect.DeepEqual(got.Location, original.Location) ||
		!reflect.DeepEqual(got.Recovery, original.Recovery) {
		t.Fatalf("summary optional detail changed: %+v; source %+v wanted cause %q", got, original, cause)
	}
}

func summaryWithQuotedRecoveryBasename(t *testing.T) *report.Report {
	t.Helper()
	saved := mustDecode(t, failedCheck)
	saved.Command = strings.Repeat("c", 32)
	saved.Counts = report.Counts{SourcePages: new(int64(1<<63 - 1)), GeneratedPages: new(int64(0)), TotalPages: new(int64(1<<63 - 1))}
	saved.Publication = report.Publication{
		Published:    true,
		Output:       strings.Repeat("o", 96),
		ReportStatus: report.ReportFailed,
		ReportPath: strings.Repeat(
			"r",
			96,
		),
		RecoveryReport: strings.Repeat("a/", 2000) + strings.Repeat("\x7f", 96),
		RecoveryState:  report.RecoveryCurrent,
	}
	saved.Diagnostics = []report.Diagnostic{{
		Stage: report.Stage(strings.Repeat("s", 64)), Code: report.Code(strings.Repeat("c", 64)),
		Message: strings.Repeat("m", 160), Cause: strings.Repeat("a", 96),
		Location: &report.Location{File: strings.Repeat("f", 96), Pointer: strings.Repeat("/", 96)},
	}}

	encoded := encodeReport(t, saved)
	if schemaVerdict(t, reportSchema(t), []byte(encoded)) != schemaAccept {
		t.Fatal("fixture violates complete report schema")
	}

	return mustDecode(t, encoded)
}

func summaryEncodedAndHumanSizes(t *testing.T, summary *report.Summary) (int, int) {
	t.Helper()

	encoded, err := report.Encode(summary)
	if err != nil {
		t.Fatal(err)
	}

	if schemaVerdict(t, compileSchema(t, report.ResponseSchema(), responseSchemaURL), encoded) != schemaAccept {
		t.Fatalf("summary violates response schema: %s", encoded)
	}

	var human strings.Builder
	if err = summary.RenderText(&human); err != nil {
		t.Fatal(err)
	}

	return len(encoded) + 1, human.Len()
}

func TestSummaryHumanBudgetKeepsExactlyFittingContinuation(t *testing.T) {
	t.Parallel()
	saved := summaryWithQuotedRecoveryBasename(t)
	operand := strings.Repeat("x", 322)
	summary := saved.Summary()
	summary.BindContinuation(programName, operand, originalReportReference)

	wire, human := summaryEncodedAndHumanSizes(t, summary)
	if wire != 2030 || human != 2048 {
		t.Fatalf("exact-fit dimensions: JSON%d text%d", wire, human)
	}

	if summary.Next == nil || summary.NextOmitted {
		t.Fatal("exactly fitting human argv must remain usable")
	}

	if got := (*summary.Next)[2]; got != operand {
		t.Fatal("exact continuation argument changed")
	}
}

func TestSummaryHumanBudgetOmitsJSONFittingOversizedContinuation(t *testing.T) {
	t.Parallel()
	saved := summaryWithQuotedRecoveryBasename(t)
	accepted := saved.Summary()
	accepted.BindContinuation(programName, strings.Repeat("x", 322), originalReportReference)

	if accepted.Next == nil {
		t.Fatal("exactly fitting seed lost its continuation")
	}
	// Extend only the captured exact argv of an actual fitting summary, independently
	// measuring the caller's candidate before BindContinuation can apply its budget.
	argv := append([]string{}, (*accepted.Next)...)
	argv[2] += "x"
	accepted.Next = &argv

	wire, human := summaryEncodedAndHumanSizes(t, accepted)
	if wire != 2031 || human != 2049 {
		t.Fatalf("rejected candidate dimensions: JSON%d text%d", wire, human)
	}

	bounded := saved.Summary()
	bounded.BindContinuation(programName, argv[2], originalReportReference)

	wire, human = summaryEncodedAndHumanSizes(t, bounded)
	if wire > 2048 || human > 2048 {
		t.Fatalf("bounded continuation exceeded transport: JSON%d text%d", wire, human)
	}

	if bounded.Next != nil || !bounded.NextOmitted || bounded.NextReference == nil {
		t.Fatal("oversized human argv must retain only reconstruction authority")
	}
}

func TestSummarySchemaRejectsMissingOrContradictoryRecoveryAuthority(t *testing.T) {
	t.Parallel()

	saved := summaryWithQuotedRecoveryBasename(t)
	summary := saved.Summary()

	valid, err := report.Encode(summary)
	if err != nil {
		t.Fatal(err)
	}

	compiled := compileSchema(t, report.ResponseSchema(), responseSchemaURL)
	if schemaVerdict(t, compiled, valid) != schemaAccept {
		t.Fatal("actual basename projection rejected")
	}

	for _, edit := range []func(map[string]any, map[string]any){
		func(root, _ map[string]any) { delete(root, recoveryDirectoryMember) },
		func(root, _ map[string]any) { delete(root, "recovery_basename") },
		func(root, _ map[string]any) { root[recoveryDirectoryMember] = "other_directory" },
		func(_, publication map[string]any) { publication["recovery_state"] = "stale" },
		func(_, publication map[string]any) { delete(publication, "recovery_state") },
		func(root, _ map[string]any) {
			delete(root, recoveryDirectoryMember)
			delete(root, "recovery_basename")
		},
		func(_, publication map[string]any) { publication["recovery_report"] = "/retained.json" },
		func(_, publication map[string]any) { publication["published"] = false },
		func(_, publication map[string]any) { delete(publication, "output") },
		func(_, publication map[string]any) { publication["report_status"] = "written" },
		func(_, publication map[string]any) { publication["report_status"] = "not_requested" },
	} {
		decoder := json.NewDecoder(strings.NewReader(string(valid)))
		decoder.UseNumber()

		var root map[string]any
		if decodeErr := decoder.Decode(&root); decodeErr != nil {
			t.Fatal(decodeErr)
		}

		publication, ok := root["publication"].(map[string]any)
		if !ok {
			t.Fatal("fixture lacks publication object")
		}

		edit(root, publication)

		altered, marshalErr := json.Marshal(root)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}

		if schemaVerdict(t, compiled, altered) != schemaReject {
			t.Fatalf("contradictory/missing recovery authority accepted: %s", altered)
		}
	}
}

func TestSummarySchemaAcceptsCompleteLocationForAnUnboundedBasename(t *testing.T) {
	t.Parallel()
	saved := summaryWithQuotedRecoveryBasename(t)
	compiled := compileSchema(t, report.ResponseSchema(), responseSchemaURL)
	saved.Publication.RecoveryReport = strings.Repeat("a/", 2000) + strings.Repeat("basename", 100)
	saved = mustDecode(t, encodeReport(t, saved))

	longBase := saved.Summary()
	if longBase.RecoveryBasename != "" || longBase.RecoveryDirectoryFrom != "complete_report.publication.recovery_report" {
		t.Fatal("unbounded basename must reference complete recovery location")
	}

	encoded, encodeErr := report.Encode(longBase)
	if encodeErr != nil || schemaVerdict(t, compiled, encoded) != schemaAccept {
		t.Fatal("actual complete-location projection rejected")
	}
}

func TestCompleteSchemaRetainsLiteralRecoveryReferenceRequirement(t *testing.T) {
	t.Parallel()
	saved := summaryWithQuotedRecoveryBasename(t)
	saved.Publication.RecoveryReport = ""

	invalid, encodeErr := json.Marshal(saved)
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}

	if schemaVerdict(t, reportSchema(t), invalid) != schemaReject {
		t.Fatal("complete publication must still require literal recovery reference")
	}

	if errorCode(saved.Validate()) != report.CodeInvalidValue {
		t.Fatal("complete decoder/domain relationship weakened")
	}
}

func TestHistoricalSummarySchemaRequiresCopiedQueryAuthority(t *testing.T) {
	t.Parallel()

	saved := saveEnvelopeFixture(t, summaryWithQuotedRecoveryBasename(t))
	queried := report.QueryResultOf(saved, queryOK(t, saved, report.Request{}), programName, fixtureReportPath)

	valid, err := report.Encode(queried)
	if err != nil {
		t.Fatal(err)
	}

	compiled := compileSchema(t, report.ResponseSchema(), responseSchemaURL)
	if schemaVerdict(t, compiled, valid) != schemaAccept {
		t.Fatal("actual historical query rejected")
	}

	summarySchema := standaloneSummarySchema(t)

	_, summaryMembers := decodeQuerySummaryDocument(t, valid)
	if schemaVerdict(t, summarySchema, envelopeJSON(t, summaryMembers)) != schemaAccept {
		t.Fatal("actual standalone historical summary rejected")
	}

	for _, field := range []string{recoveryDirectoryMember, "publication_context"} {
		envelope, summary := decodeQuerySummaryDocument(t, valid)

		if field == recoveryDirectoryMember {
			summary[field] = json.RawMessage(`"original_report_argument"`)
		} else {
			delete(summary, field)
		}

		nested := envelopeJSON(t, summary)
		envelope["result"] = nested
		altered := envelopeJSON(t, envelope)

		if schemaVerdict(t, compiled, altered) != schemaReject {
			t.Fatalf("historical query accepted contradictory/missing %s", field)
		}

		if field == recoveryDirectoryMember && schemaVerdict(t, summarySchema, nested) != schemaReject {
			t.Fatal("standalone historical summary accepted current-argument directory authority")
		}
	}
}

func standaloneSummarySchema(t *testing.T) *jsonschema.Schema {
	t.Helper()

	var document map[string]json.RawMessage
	if err := json.Unmarshal([]byte(report.ResponseSchema()), &document); err != nil {
		t.Fatal(err)
	}

	delete(document, "oneOf")
	document["$ref"] = json.RawMessage(`"#/$defs/summary"`)

	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	return compileSchema(t, string(encoded), responseSchemaURL)
}

func decodeQuerySummaryDocument(t *testing.T, encoded []byte) (map[string]json.RawMessage, map[string]json.RawMessage) {
	t.Helper()

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatal(err)
	}

	var summary map[string]json.RawMessage
	if err := json.Unmarshal(envelope["result"], &summary); err != nil {
		t.Fatal(err)
	}

	return envelope, summary
}
