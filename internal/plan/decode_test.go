// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package plan_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/plan"
)

type (
	// oneByteReader returns one byte per Read.
	oneByteReader struct{ reader *bytes.Reader }

	// failingReader fails every Read with err.
	failingReader struct{ err error }

	// flippingContext is a context that reports cancellation once Err has been called flipAfter times, which
	// lets a sweep land a cancellation at every kind of checkpoint the decoder has.
	flippingContext struct {
		calls     atomic.Int64
		flipAfter int64
	}
)

var errBoom = errors.New("boom")

func checkAccepted(tb testing.TB, row *corpusCase, job *assembly.Job) {
	tb.Helper()

	err := job.Validate()
	if err != nil {
		tb.Fatalf("%s: accepted job violates the domain invariants: %v", row.Name, err)
	}
}

func checkRejected(tb testing.TB, row *corpusCase, err error) {
	tb.Helper()

	planErr := codedError(tb, err, row.Code)

	if row.Pointer != "" && planErr.Location.Pointer != row.Pointer {
		tb.Fatalf("%s: pointer = %q, want %q", row.Name, planErr.Location.Pointer, row.Pointer)
	}

	if planErr.Stage == plan.StageRead || planErr.Location.Line < 1 || planErr.Location.Column < 1 {
		tb.Fatalf("%s: diagnostic is not located: %+v", row.Name, planErr.Location)
	}

	if planErr.Location.Source != inputName || planErr.Message == "" || planErr.Error() == "" || planErr.Unwrap() != nil {
		tb.Fatalf("%s: malformed diagnostic %+v", row.Name, planErr)
	}
}

func TestCorpusDecoder(t *testing.T) {
	t.Parallel()

	rows := corpus()
	if len(rows) < 80 {
		t.Fatalf("the shared corpus has %d cases; want at least 80", len(rows))
	}

	for _, row := range rows {
		t.Run(row.Name, func(t *testing.T) {
			t.Parallel()

			job, err := decodeString(t, row.Doc)
			if row.Code == "" {
				if err != nil {
					t.Fatalf("want accept, got %v", err)
				}

				checkAccepted(t, &row, job)

				return
			}

			checkRejected(t, &row, err)
		})
	}
}

// Read returns one byte per call, to prove that no verdict or location depends on chunking.
func (o oneByteReader) Read(buffer []byte) (int, error) {
	length, err := o.reader.Read(buffer[:min(1, len(buffer))])
	if err != nil {
		return length, io.EOF // a bytes.Reader fails only at the end of its data
	}

	return length, nil
}

func TestVerdictsDoNotDependOnReadChunking(t *testing.T) {
	t.Parallel()

	for _, row := range corpus() {
		_, whole := decodeString(t, row.Doc)
		_, split := plan.Decode(context.Background(), inputFor(), oneByteReader{bytes.NewReader([]byte(row.Doc))})

		if (whole == nil) != (split == nil) {
			t.Fatalf("%s: %v vs %v", row.Name, whole, split)
		}

		if whole == nil {
			continue
		}

		wholeErr, wholeOK := errors.AsType[*plan.Error](whole)
		splitErr, splitOK := errors.AsType[*plan.Error](split)

		if !wholeOK || !splitOK || *wholeErr != *splitErr {
			t.Fatalf("%s: %+v vs %+v", row.Name, whole, split)
		}
	}
}

func TestBOMEdgeCases(t *testing.T) {
	t.Parallel()

	for document, want := range map[string]plan.Code{
		"\xef\xbb\xbf":          plan.CodeEmpty,
		"\xef\xbb":              plan.CodeSyntax,
		"\xef\xbb\xbf \n":       plan.CodeEmpty,
		"\xef\xbb\xbf[1]":       plan.CodeNotObject,
		"\xef\xbb\xbf{\"x\":1}": plan.CodeUnknownMember,
		"\xef":                  plan.CodeSyntax,
	} {
		_, err := decodeString(t, document)
		wantCode(t, err, want)
	}
}

func TestTrailingDataIsLocatedAfterABOM(t *testing.T) {
	t.Parallel()

	for _, tail := range []string{" xyz", ` "`, ` }`, " {}", "\n\n  x", "\x00", "// comment", ","} {
		prefix := "\xef\xbb\xbf" + minimalPlan + "\n"
		_, err := decodeString(t, prefix+tail)
		planErr := codedError(t, err, plan.CodeTrailingData)

		want := int64(len(prefix) + len(tail) - len(strings.TrimLeft(tail, " \n")))
		if planErr.Location.Offset != want {
			t.Fatalf("%q: offset %d, want %d", tail, planErr.Location.Offset, want)
		}
	}
}

func TestDuplicateMemberLocation(t *testing.T) {
	t.Parallel()

	_, err := decodeString(t, "\xef\xbb\xbf{\"version\":1,\n \"items\":[\"a\"],\n \"vers\\u0069on\":1}")
	planErr := codedError(t, err, plan.CodeDuplicateMember)

	if planErr.Location.Line != 3 || planErr.Location.Pointer != "/version" || planErr.Stage != plan.StageSyntax {
		t.Fatalf(diagnosticFormat, planErr)
	}
}

func TestSyntaxErrorLocationAndStableMessage(t *testing.T) {
	t.Parallel()

	_, err := decodeString(t, "{\n\"version\":1,\n\"items\":[\"a\",]}")
	planErr := codedError(t, err, plan.CodeSyntax)

	if planErr.Location.Line != 3 || strings.Contains(planErr.Message, "`") {
		t.Fatalf(diagnosticFormat, planErr)
	}
}

func TestUnknownMemberHintsCase(t *testing.T) {
	t.Parallel()

	_, err := decodeString(t, `{"VERSION":1}`)
	planErr := codedError(t, err, plan.CodeUnknownMember)

	if !strings.Contains(planErr.Message, `did you mean "version"`) {
		t.Fatal(planErr.Message)
	}
}

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

func TestDecodeReportsReadFailures(t *testing.T) {
	t.Parallel()

	partial := io.MultiReader(strings.NewReader(`{"version":1,`), failingReader{errBoom})

	_, err := plan.Decode(context.Background(), plan.Input{Name: "r"}, partial)
	planErr := codedError(t, err, plan.CodeReadFailed)

	if !errors.Is(err, errBoom) || planErr.Stage != plan.StageRead {
		t.Fatalf(diagnosticFormat, planErr)
	}

	_, err = plan.Decode(context.Background(), plan.Input{Name: "r"}, failingReader{errBoom})
	wantCode(t, err, plan.CodeReadFailed)

	// A failure while scanning for trailing data is a read failure too.
	_, err = plan.Decode(
		context.Background(),
		plan.Input{Name: "r"},
		io.MultiReader(strings.NewReader(minimalPlan+"  "), failingReader{errBoom}),
	)
	wantCode(t, err, plan.CodeReadFailed)
}

func TestCancelledContextStopsDecoding(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := plan.Decode(ctx, plan.Input{Name: "c"}, strings.NewReader(minimalPlan))
	wantCode(t, err, plan.CodeInterrupted)

	if !errors.Is(err, context.Canceled) {
		t.Fatal("the cause is lost")
	}
}

// everyMemberPlan uses every member the format has.
func everyMemberPlan() string {
	return `{"$schema":"s","version":1,"output":"o.pdf","dir":"d","blank":{"size":"A4","background":"#fff","text":{"value":"v",` +
		`"font":{"file":"f.ttf"},"size":1,"color":"#000","anchor":"top","x":1,"y":2,"width":3,` +
		`"align":"left","leading":1.5,"overflow":"allow"}},` +
		`"items":["a",{"blank":{"text":{"font":"default"}},"count":2},{"dir":"g","items":["b"]}]}`
}

// TestEveryTruncationIsRejected decodes every proper prefix of a document that uses every member: none may be
// accepted, and none may panic. This walks the end-of-input path of every reader in the decoder.
func TestEveryTruncationIsRejected(t *testing.T) {
	t.Parallel()

	document := everyMemberPlan()
	mustDecode(t, document)

	for length := range len(document) {
		_, err := decodeString(t, document[:length])

		planErr, ok := errors.AsType[*plan.Error](err)
		if !ok || planErr == nil {
			t.Fatalf("prefix of %d bytes: %v is not a *plan.Error", length, err)
		}
	}
}

func (*flippingContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (*flippingContext) Done() <-chan struct{} { return nil }

func (*flippingContext) Value(any) any { return nil }

func (f *flippingContext) Err() error {
	if f.calls.Add(1) > f.flipAfter {
		return context.Canceled
	}

	return nil
}

func TestCancellationAtEveryCheckpointIsReported(t *testing.T) {
	t.Parallel()

	document := repeated(sourceStringJSON, 20000)

	interrupted, completed := 0, 0

	for flipAfter := range int64(60) {
		_, err := plan.Decode(&flippingContext{flipAfter: flipAfter}, plan.Input{Name: "c"}, strings.NewReader(document))
		if err == nil {
			completed++

			continue
		}

		wantCode(t, err, plan.CodeInterrupted)

		interrupted++
	}

	if interrupted == 0 || completed == 0 {
		t.Fatalf("the sweep interrupted %d and completed %d decodes; it must cross the end of the work", interrupted, completed)
	}
}
