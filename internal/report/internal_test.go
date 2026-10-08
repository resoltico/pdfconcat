// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report

import (
	"bytes"
	"encoding/json/jsontext"
	"io"
	"maps"
	"math"
	"strings"
	"testing"
)

const (
	internalDigest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	internalFile   = "/w/job.json"

	// Values of the single-feature samples.
	featureFile    = "/f"
	featureArgv    = "argv"
	featurePointer = "/items/0"
	featureID      = "argv:0"
)

// nodeSamples are reports that between them use every optional member the writer counts.
func nodeSamples() []*Report {
	offset := int64(7)
	argv := 3
	count := int64(1)
	rng := &PageRange{Start: 1, End: 1}
	text := &Text{
		Color: "#000000", Anchor: "top", Align: "left", Overflow: "allow", Size: 1, Width: 1, Leading: 1,
	}
	pointerLocation := &Location{File: internalFile, Offset: &offset, Line: 1, Column: 8, Pointer: "/items/0"}
	argvLocation := &Location{File: "argv", ArgvIndex: &argv}

	full := &Report{
		FormatVersion: Version, AttemptID: "AAAAAAAAAAAAAAAAAAAAAAAAAA", Kind: KindReport, Status: StatusFailed, Command: "build",
		Phases: Phases{PhaseComplete, PhaseComplete, PhaseComplete, PhaseComplete},
		Counts: Counts{SourcePages: &count, GeneratedPages: &count, TotalPages: new(int64(2))},
		Publication: Publication{
			Output:         "/o.pdf",
			ReportStatus:   ReportFailed,
			ReportPath:     "/r.json",
			RecoveryReport: "/rec.json",
			RecoveryState:  RecoveryCurrent,
			Published:      true,
		},
		Diagnostics: []Diagnostic{
			{Stage: "a", Code: "b", Message: "m", Path: "/p", Location: pointerLocation},
			{Stage: "a", Code: "b", Message: "m", Location: argvLocation},
			{Stage: "a", Code: "b", Message: "m"},
		},
		Parts: []Part{
			{
				ID:     "/items/0",
				Kind:   PartPDF,
				Origin: Position{File: internalFile, Offset: &offset, Line: 1, Column: 8},
				Range:  rng,
				Pages:  &count,
				Source: new(int),
			},
			{
				ID:     "/items/1",
				Kind:   PartBlank,
				Origin: Position{File: internalFile},
				Range:  &PageRange{Start: 2, End: 2},
				Pages:  &count,
				Style:  new(int),
			},
		},
		Sources: []Source{{Path: "/a.pdf", Digest: internalDigest, Bytes: &count}, {Path: "/b.pdf"}},
		Fonts:   []Font{{Digest: internalDigest, Name: "n", File: "/f.ttf"}, {Digest: internalDigest, Name: "n"}},
		Styles: []Style{
			{Background: "none", Size: PageSize{Origin: SizeExplicit, Width: 1, Height: 1}, Text: text},
			{Background: "none", Size: PageSize{Origin: SizeExplicit, Width: 1, Height: 1}},
		},
	}

	return []*Report{
		full,
		failedReportFixture("report", StatusInvalid, Diagnostic{Stage: "usage", Code: "x", Message: "m"}),
		NewBuilder(commandCheck).Build(StatusInvalid),
	}
}

// TestNodeCountMatchesTheDecoder checks the writer's structural count against the decoder's token count:
// a report decodes with exactly nodes() allowed and not with one fewer.
func TestNodeCountMatchesTheDecoder(t *testing.T) {
	t.Parallel()

	for index, r := range nodeSamples() {
		if len(r.Diagnostics) == 0 {
			r.Diagnostics = []Diagnostic{{Stage: "a", Code: "b", Message: "m"}}
		}

		var out bytes.Buffer

		_, err := Write(&out, r, MaxReportBytes)
		if err != nil {
			t.Fatalf("sample %d: %v", index, err)
		}

		nodes := r.nodes()
		limits := DefaultLimits()
		limits.MaxNodes = nodes

		_, err = DecodeLimited(DecoderTestContext(t.Context(), t), "s", ReaderRequiringStorage(bytes.NewReader(out.Bytes())), limits)
		if err != nil {
			t.Errorf("sample %d: %d nodes should be accepted: %v", index, nodes, err)
		}

		limits.MaxNodes = nodes - 1

		_, err = DecodeLimited(DecoderTestContext(t.Context(), t), "s", ReaderRequiringStorage(bytes.NewReader(out.Bytes())), limits)

		if found, ok := AsError(err); !ok || found.Diagnostic.Code != CodeLimitNodes {
			t.Errorf("sample %d: %d nodes should be refused: %v", index, nodes-1, err)
		}
	}
}

func TestPageOfReportsAnUnencodableRecord(t *testing.T) {
	t.Parallel()

	_, err := pageOf("x", 1, Request{}, func(int) float64 { return math.NaN() })
	if found, ok := AsError(err); !ok || found.Diagnostic.Code != CodeWriteFailed {
		t.Errorf("got %v", err)
	}
}

func TestPreviewKeepsWholeScalars(t *testing.T) {
	t.Parallel()

	text := strings.Repeat("ā", PreviewRunes)

	if cut, truncated := preview(text); truncated || cut != text {
		t.Error("exactly PreviewRunes characters must not be cut")
	}

	cut, truncated := preview(text + "x")
	if !truncated || cut != text {
		t.Errorf("one over: %q %v", cut, truncated)
	}
}

// encoderExpectingAMemberName is an encoder inside an object, so that a value written next is a grammar error.
func encoderExpectingAMemberName(t *testing.T) *jsontext.Encoder {
	t.Helper()

	enc := jsontext.NewEncoder(io.Discard)

	err := enc.WriteToken(jsontext.BeginObject)
	if err != nil {
		t.Fatal(err)
	}

	return enc
}

func TestResponseReportsAnEncoderThatCannotTakeIt(t *testing.T) {
	t.Parallel()

	err := NewResponse(&PartResponse{}).MarshalJSONTo(encoderExpectingAMemberName(t))
	if err == nil || !strings.Contains(err.Error(), "write response") {
		t.Errorf("a response where a member name belongs: %v", err)
	}
}

// singleFeatureReport is a valid report with one failure diagnostic and nothing else; each sample of
// singleFeatureSamples adds one optional feature to it, so every counting condition of the writer is
// evaluated exactly once per sample and a miscount cannot be cancelled by an opposite one.
func singleFeatureReport(edit func(r *Report)) *Report {
	built := NewBuilder(commandCheck)
	built.AddDiagnostic(0, Diagnostic{Stage: "a", Code: "b", Message: "m"})

	r := built.Build(StatusFailed)
	edit(r)

	return r
}

func diagnosticFeatureSamples() map[string]*Report {
	offset, argv := int64(5), 2

	withDiagnostic := func(edit func(d *Diagnostic)) *Report {
		return singleFeatureReport(func(r *Report) { edit(&r.Diagnostics[0]) })
	}

	return map[string]*Report{
		"bare": singleFeatureReport(func(*Report) {}),
		"consumer relation": singleFeatureReport(func(r *Report) {
			r.Parts = []Part{{ID: featureID, Kind: PartBlank, Origin: Position{File: featureFile}, Pages: new(int64(1))}}
			r.Diagnostics[0].Consumers = []string{featureID}
		}),
		"diagnostic cause": withDiagnostic(func(d *Diagnostic) { d.Cause = "original IO fault" }),
		"diagnostic recovery": withDiagnostic(func(d *Diagnostic) {
			d.Recovery = &Recovery{
				Action:       "edit_input",
				Command:      commandCheck,
				LocationFrom: "original_argv",
				Replacement:  "--blank",
				ReportFrom:   "unused_report_target",
				RecoveryFrom: "publication.recovery_report",
				Location:     &Location{File: featureArgv, ArgvIndex: &argv},
			}
		}),
		"diagnostic path": withDiagnostic(func(d *Diagnostic) { d.Path = featureFile }),
		"pointer location": withDiagnostic(func(d *Diagnostic) {
			d.Location = &Location{File: internalFile, Offset: &offset, Line: 1, Column: 6, Pointer: featurePointer}
		}),
		"argv location":      withDiagnostic(func(d *Diagnostic) { d.Location = &Location{File: featureArgv, ArgvIndex: &argv} }),
		"file-only location": withDiagnostic(func(d *Diagnostic) { d.Location = &Location{File: internalFile} }),
	}
}

func publicationFeatureSamples() map[string]*Report {
	publish := func(p Publication) *Report {
		return singleFeatureReport(func(r *Report) { r.Publication = p })
	}

	return map[string]*Report{
		"producer": singleFeatureReport(func(r *Report) {
			r.Producer = &Producer{
				Tool:     "pdfconcat",
				Version:  "devel",
				Commit:   unknownMetadataValue,
				Go:       "go1.27.1",
				Platform: "darwin/arm64",
			}
		}),
		"report target observation": publish(
			Publication{ReportStatus: ReportNotRequested, ReportTargetObservation: "absent_when_observed"},
		),
		"report write": publish(Publication{ReportStatus: ReportWritten, ReportPath: featureFile, ReportWrite: "written"}),
		"output":       publish(Publication{ReportStatus: ReportNotRequested, Output: featureFile}),
		"unresolved report argument": publish(Publication{
			ReportStatus:            ReportFailed,
			ReportFrom:              jobReportReference,
			ReportWrite:             attemptNotWritten,
			ReportTargetObservation: unknownMetadataValue,
		}),
		"report path": publish(Publication{ReportStatus: ReportWritten, ReportPath: featureFile}),
		"recovery report": publish(Publication{
			Output:         featureFile,
			Published:      true,
			ReportStatus:   ReportFailed,
			ReportPath:     featureFile,
			RecoveryReport: featureFile,
			RecoveryState:  RecoveryCurrent,
		}),
	}
}

func partFeatureSamples() map[string]*Report {
	offset := int64(5)
	spot := Position{File: internalFile, Offset: &offset, Line: 1, Column: 6}
	plain := Position{File: featureArgv}
	firstPage := &PageRange{Start: 1, End: 1}
	one := new(int64(1))
	size := PageSize{Origin: SizeExplicit, Width: 1, Height: 1}

	withParts := func(edit func(r *Report), parts ...Part) *Report {
		return singleFeatureReport(func(r *Report) {
			edit(r)

			r.Parts = parts
		})
	}
	withSource := func(r *Report) { r.Sources = []Source{{Path: featureFile}} }
	withStyle := func(r *Report) { r.Styles = []Style{{Background: BackgroundNone, Size: size}} }
	nothing := func(*Report) {}

	return map[string]*Report{
		"pdf part": withParts(withSource, Part{ID: featureID, Kind: PartPDF, Origin: spot, Pages: one, Source: new(int)}),
		"ranged pdf part": withParts(
			withSource, Part{ID: featureID, Kind: PartPDF, Origin: plain, Range: firstPage, Pages: one, Source: new(int)},
		),
		"styled blank part": withParts(withStyle, Part{ID: featureID, Kind: PartBlank, Origin: spot, Pages: one, Style: new(int)}),
		"ranged blank part": withParts(nothing, Part{ID: featureID, Kind: PartBlank, Origin: plain, Range: firstPage, Pages: one}),
	}
}

func tableFeatureSamples() map[string]*Report {
	size := PageSize{Origin: SizeExplicit, Width: 1, Height: 1}
	font := Font{Digest: internalDigest, Name: "n"}
	text := &Text{Color: "#000000", Anchor: "top", Align: "left", Overflow: "allow", Size: 1, Width: 1, Leading: 1}

	return map[string]*Report{
		"producer identity": singleFeatureReport(func(r *Report) {
			r.Producer = &Producer{
				Tool:     "pdfconcat",
				Version:  "devel",
				Commit:   unknownMetadataValue,
				Go:       "go1.27.1",
				Platform: "darwin/arm64",
			}
		}),
		"output digest": singleFeatureReport(func(r *Report) { r.Publication.OutputDigest = internalDigest }),
		"source digest": singleFeatureReport(func(r *Report) {
			r.Sources = []Source{{Path: featureFile, Digest: internalDigest, Bytes: new(int64(1))}}
		}),
		"font file": singleFeatureReport(
			func(r *Report) { r.Fonts = []Font{{Digest: internalDigest, Name: "n", File: featureFile}} },
		),
		"font without file": singleFeatureReport(func(r *Report) { r.Fonts = []Font{font} }),
		"style with findings": singleFeatureReport(func(r *Report) {
			r.Fonts = []Font{font}
			copyText := *text
			copyText.Findings = []TextFinding{{Kind: "outside-page-horizontal", Line: -1, Detail: "off page"}}
			r.Styles = []Style{{Background: BackgroundNone, Size: size, Text: &copyText}}
		}),
		"style with ink bounds": singleFeatureReport(func(r *Report) {
			r.Fonts = []Font{font}
			copyText := *text
			copyText.InkBounds = &Rect{X: 1, Y: 1, Width: 2, Height: 3}
			r.Styles = []Style{{Background: BackgroundNone, Size: size, Text: &copyText}}
		}),
		"style with text": singleFeatureReport(func(r *Report) {
			r.Fonts = []Font{font}
			r.Styles = []Style{{Background: BackgroundNone, Size: size, Text: text}}
		}),
		"style without text": singleFeatureReport(func(r *Report) { r.Styles = []Style{{Background: BackgroundNone, Size: size}} }),
	}
}

func singleFeatureSamples() map[string]*Report {
	samples := map[string]*Report{}

	for _, group := range []map[string]*Report{
		diagnosticFeatureSamples(), publicationFeatureSamples(), partFeatureSamples(), tableFeatureSamples(),
	} {
		maps.Copy(samples, group)
	}

	return samples
}

// TestNodeCountOfEachFeatureMatchesTheDecoder checks the writer's count against the decoder's for reports
// that each hold one optional feature: the count is exact, so the decoder accepts the report with that many
// nodes allowed and refuses it with one fewer.
func TestNodeCountOfEachFeatureMatchesTheDecoder(t *testing.T) {
	t.Parallel()

	for name, r := range singleFeatureSamples() {
		var out bytes.Buffer

		written, err := Write(&out, r, MaxReportBytes)
		if err != nil || written != int64(out.Len()) {
			t.Errorf("%s: wrote %d bytes of %d: %v", name, written, out.Len(), err)

			continue
		}

		nodes := r.nodes()
		limits := DefaultLimits()
		limits.MaxNodes = nodes

		_, err = DecodeLimited(DecoderTestContext(t.Context(), t), name, ReaderRequiringStorage(bytes.NewReader(out.Bytes())), limits)
		if err != nil {
			t.Errorf("%s: %d nodes should be accepted: %v", name, nodes, err)
		}

		limits.MaxNodes = nodes - 1

		_, err = DecodeLimited(DecoderTestContext(t.Context(), t), name, ReaderRequiringStorage(bytes.NewReader(out.Bytes())), limits)
		if found, ok := AsError(err); !ok || found.Diagnostic.Code != CodeLimitNodes {
			t.Errorf("%s: %d nodes should be refused: %v", name, nodes-1, err)
		}
	}
}

func failedReportFixture(command string, status Status, diagnostics ...Diagnostic) *Report {
	builder := NewBuilder(command)
	builder.SetPhases(Phases{
		Instructions: PhaseIncomplete, InputInspection: PhaseNotRun, Layout: PhaseNotRun, OutputVerification: PhaseNotRun,
	})

	for index, diagnostic := range diagnostics {
		builder.AddDiagnostic(index, diagnostic)
	}

	return builder.Build(status)
}
