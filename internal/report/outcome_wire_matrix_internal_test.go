// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestOutcomeWireMatrix(t *testing.T) {
	compiler := jsonschema.NewCompiler()
	document, err := jsonschema.UnmarshalJSON(strings.NewReader(Schema()))
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.AddResource("https://example.test/report.json", document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("https://example.test/report.json")
	if err != nil {
		t.Fatal(err)
	}

	states := []PhaseState{PhaseNotRun, PhaseIncomplete, PhaseComplete}
	supported := map[Phases]bool{
		{PhaseNotRun, PhaseNotRun, PhaseNotRun, PhaseNotRun}:           true,
		{PhaseIncomplete, PhaseNotRun, PhaseNotRun, PhaseNotRun}:       true,
		{PhaseComplete, PhaseNotRun, PhaseNotRun, PhaseNotRun}:         true,
		{PhaseComplete, PhaseIncomplete, PhaseNotRun, PhaseNotRun}:     true,
		{PhaseComplete, PhaseComplete, PhaseNotRun, PhaseNotRun}:       true,
		{PhaseComplete, PhaseComplete, PhaseIncomplete, PhaseNotRun}:   true,
		{PhaseComplete, PhaseComplete, PhaseComplete, PhaseNotRun}:     true,
		{PhaseComplete, PhaseComplete, PhaseComplete, PhaseIncomplete}: true,
		{PhaseComplete, PhaseComplete, PhaseComplete, PhaseComplete}:   true,
	}
	total, accepted := 0, 0
	for _, command := range []string{commandBuild, commandCheck} {
		for _, status := range []Status{StatusOK, StatusInvalid, StatusFailed, StatusInterrupted} {
			for _, published := range []bool{false, true} {
				for _, reportState := range []WriteState{ReportNotRequested, ReportWritten, ReportFailed} {
					for _, recovery := range []bool{false, true} {
						for _, instructions := range states {
							for _, inspection := range states {
								for _, layout := range states {
									for _, verification := range states {
										phases := Phases{instructions, inspection, layout, verification}
										allowed := supported[phases]
										if command == commandCheck && verification != PhaseNotRun {
											allowed = false
										}
										if published && (command != commandBuild || (status != StatusOK && status != StatusFailed) || verification != PhaseComplete) {
											allowed = false
										}
										if status == StatusOK && (layout != PhaseComplete || reportState == ReportFailed || (command == commandBuild && (!published || verification != PhaseComplete))) {
											allowed = false
										}
										if recovery && (!published || reportState != ReportFailed) {
											allowed = false
										}
										built := matrixReport(command, status, published, reportState, recovery, phases)
										total++
										var wire bytes.Buffer
										_, writeErr := Write(&wire, built, MaxReportBytes)
										if (writeErr == nil) != allowed {
											t.Fatalf("state command=%s status=%s published=%t report=%s recovery=%t phases=%+v accepted=%t expected=%t error=%v", command, status, published, reportState, recovery, phases, writeErr == nil, allowed, writeErr)
										}
										savedProjection := *built
										if savedProjection.Sources == nil {
											savedProjection.Sources = []Source{}
										}
										if savedProjection.Styles == nil {
											savedProjection.Styles = []Style{}
										}
										if savedProjection.Fonts == nil {
											savedProjection.Fonts = []Font{}
										}
										if savedProjection.Diagnostics == nil {
											savedProjection.Diagnostics = []Diagnostic{}
										}
										raw, err := json.Marshal(&savedProjection)
										if err != nil {
											t.Fatal(err)
										}
										value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
										if err != nil {
											t.Fatal(err)
										}
										if err := schema.Validate(value); (err == nil) != allowed {
											t.Fatalf("schema matrix state command=%s status=%s published=%t report=%s recovery=%t phases=%+v accepted=%t expected=%t: %v", command, status, published, reportState, recovery, phases, err == nil, allowed, err)
										}
										_, savedErr := Decode(DecoderTestContext(t.Context(), t), "matrix-saved.json", ReaderRequiringStorage(bytes.NewReader(raw)))
										if (savedErr == nil) != allowed {
											t.Fatalf("saved matrix state accepted=%t expected=%t: %v", savedErr == nil, allowed, savedErr)
										}
										if !allowed {
											continue
										}
										accepted++
										decoded, decodeErr := Decode(DecoderTestContext(t.Context(), t), "matrix.json", ReaderRequiringStorage(bytes.NewReader(wire.Bytes())))
										if decodeErr != nil {
											t.Fatalf("produced state cannot decode: %v", decodeErr)
										}
										if _, queryErr := decoded.Query(Request{View: ViewParts, Limit: new(int64(1))}); queryErr != nil {
											t.Fatalf("decoded state cannot query: %v", queryErr)
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if total != 7776 || accepted == 0 {
		t.Fatalf("matrix discovery total=%d accepted=%d", total, accepted)
	}
	t.Logf("wire states=%d accepted=%d rejected=%d", total, accepted, total-accepted)
}

func matrixReport(command string, status Status, published bool, reportState WriteState, recovery bool, phases Phases) *Report {
	builder := NewBuilder(command)
	builder.SetPhases(phases)
	publication := Publication{Output: "/output.pdf", Published: published, ReportStatus: reportState}
	if reportState != ReportNotRequested {
		publication.ReportPath = "/report.json"
	}
	if recovery {
		publication.RecoveryReport = "/recovery.json"
		publication.RecoveryState = RecoveryCurrent
	}
	builder.SetPublication(publication)
	if status != StatusOK {
		builder.AddDiagnostic(0, Diagnostic{Stage: Stage("publish"), Code: "matrix_failure", Message: fmt.Sprint(status)})
	}
	part := Part{ID: "argv:0", Kind: PartBlank, Origin: Position{File: "argv"}, Pages: new(int64(1))}
	if phases.Layout == PhaseComplete {
		builder.SetCounts(Counts{SourcePages: new(int64), GeneratedPages: new(int64(1)), TotalPages: new(int64(1))})
		style := builder.Style(&Style{Background: BackgroundNone, Size: PageSize{Origin: SizeExplicit, Width: 100, Height: 100}})
		part.Style = &style
		part.Range = &PageRange{Start: 1, End: 1}
	}
	builder.AddPart(part)
	return builder.Build(status)
}
