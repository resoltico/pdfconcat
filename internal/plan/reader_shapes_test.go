// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan_test

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/resoltico/pdfconcat/internal/plan"
)

type (
	// readShape is a way an input delivers the same bytes: all at once, in single bytes, with its end of input
	// attached to the last bytes, and so on.
	readShape struct {
		open func(text string) io.Reader
		name string
	}

	// chunkedReader returns at most size bytes per Read.
	chunkedReader struct {
		reader *strings.Reader
		size   int
	}

	// emptyFirstReader answers its first Read with no bytes and no error, which [io.Reader] permits.
	emptyFirstReader struct {
		reader *strings.Reader
		asked  bool
	}

	// shapeDocument is an input and what decoding it must give.
	shapeDocument struct {
		name string
		text string
		// code is the failure the input must cause; empty means the plan is accepted.
		code plan.Code
	}

	// parserOnlyContext reports cancellation to everything except the reader of the input, so that only the
	// decoder's own checks between tokens can notice it.
	parserOnlyContext struct{}
)

const (
	chunkOfBOMLength = 3
	chunkOfTwo       = 2
	abcText          = "abc"
	// readerFrame names the input reader in a stack; a context check made from it is a read-time check.
	readerFrame = "(*chunkReader).Read"
	// tokensPerCancelCheck is the number of tokens between the decoder's own context checks.
	tokensPerCancelCheck = 4096
)

func (c chunkedReader) Read(buffer []byte) (int, error) {
	length, err := c.reader.Read(buffer[:min(len(buffer), c.size)])
	if err != nil {
		return length, io.EOF // a strings.Reader fails only at the end of its data
	}

	return length, nil
}

func (e *emptyFirstReader) Read(buffer []byte) (int, error) {
	if !e.asked {
		e.asked = true

		return 0, nil
	}

	length, err := e.reader.Read(buffer)
	if err != nil {
		return length, io.EOF // a strings.Reader fails only at the end of its data
	}

	return length, nil
}

func readShapes() []readShape {
	return []readShape{
		{name: "whole", open: func(text string) io.Reader { return strings.NewReader(text) }},
		{name: "one byte", open: func(text string) io.Reader { return iotest.OneByteReader(strings.NewReader(text)) }},
		{name: "half", open: func(text string) io.Reader { return iotest.HalfReader(strings.NewReader(text)) }},
		{name: "end attached to data", open: func(text string) io.Reader { return iotest.DataErrReader(strings.NewReader(text)) }},
		{name: "three byte chunks", open: func(text string) io.Reader {
			return chunkedReader{reader: strings.NewReader(text), size: chunkOfBOMLength}
		}},
		{name: "two byte chunks", open: func(text string) io.Reader {
			return chunkedReader{reader: strings.NewReader(text), size: chunkOfTwo}
		}},
		{name: "empty first", open: func(text string) io.Reader { return &emptyFirstReader{reader: strings.NewReader(text)} }},
	}
}

func shapeDocuments() []shapeDocument {
	return []shapeDocument{
		{name: "plan", text: minimalPlan},
		{name: "plan with byte order mark", text: byteOrderMark + minimalPlan},
		{name: "plan with trailing space", text: minimalPlan + " \n "},
		{name: "plan with byte order mark and trailing space", text: byteOrderMark + minimalPlan + "\n"},
		{name: "two byte object", text: "{}", code: plan.CodeMissingMember},
		{name: "empty", text: "", code: plan.CodeEmpty},
		{name: "byte order mark only", text: byteOrderMark, code: plan.CodeEmpty},
		{name: "byte order mark prefix", text: byteOrderMark[:chunkOfTwo], code: plan.CodeSyntax},
		{name: "trailing data", text: minimalPlan + " x", code: plan.CodeTrailingData},
		{name: "trailing data after byte order mark", text: byteOrderMark + minimalPlan + "x", code: plan.CodeTrailingData},
		{name: "second byte order mark", text: byteOrderMark + byteOrderMark + minimalPlan, code: plan.CodeSyntax},
	}
}

// TestDecodingDoesNotDependOnHowTheInputIsDelivered decodes small inputs delivered in every shape, and each must
// give the same verdict, located at the same place, as the verdict the input is known to deserve.
func TestDecodingDoesNotDependOnHowTheInputIsDelivered(t *testing.T) {
	t.Parallel()

	for _, document := range shapeDocuments() {
		for _, shape := range readShapes() {
			t.Run(document.name+"/"+shape.name, func(t *testing.T) {
				t.Parallel()

				checkShapedDecode(t, &document, &shape)
			})
		}
	}
}

func checkShapedDecode(t *testing.T, document *shapeDocument, shape *readShape) {
	t.Helper()

	job, err := plan.Decode(context.Background(), inputFor(), shape.open(document.text))
	if document.code == "" {
		if err != nil || job == nil {
			t.Fatalf("decode: %v", err)
		}

		return
	}

	got := codedError(t, err, document.code)
	want := codedError(t, errorOf(decodeString(t, document.text)), document.code)

	if *got != *want {
		t.Errorf("%+v, want the location of a whole read %+v", got, want)
	}
}

// TestSourceDeliversThePlanWithoutItsByteOrderMark reads the decoder's input stream to its end.
func TestSourceDeliversThePlanWithoutItsByteOrderMark(t *testing.T) {
	t.Parallel()

	rows := []struct{ name, text, want string }{
		{"empty", "", ""},
		{"one byte", "a", "a"},
		{"two bytes", "ab", "ab"},
		{"three bytes", abcText, abcText},
		{"four bytes", "abcd", "abcd"},
		{"byte order mark only", byteOrderMark, ""},
		{"byte order mark and one byte", byteOrderMark + "a", "a"},
		{"byte order mark prefix", byteOrderMark[:chunkOfTwo], byteOrderMark[:chunkOfTwo]},
		{"byte order mark prefix and a byte", byteOrderMark[:chunkOfTwo] + "a", byteOrderMark[:chunkOfTwo] + "a"},
		{"second byte order mark stays", byteOrderMark + byteOrderMark, byteOrderMark},
		{"long", byteOrderMark + strings.Repeat("xyz", 5000), strings.Repeat("xyz", 5000)},
	}

	for _, row := range rows {
		for _, shape := range readShapes() {
			t.Run(row.name+"/"+shape.name, func(t *testing.T) {
				t.Parallel()

				source := plan.LimitedSourceOver(context.Background(), inputName, shape.open(row.text))

				got, err := io.ReadAll(source)
				if err != nil || string(got) != row.want {
					t.Errorf("read %q, %v; want %q", got, err, row.want)
				}
			})
		}
	}
}

// TestSourceFirstReadReturnsBytes pins that a source with bytes answers its first Read with some of them, so
// that a reader that stops at an empty answer still makes progress.
func TestSourceFirstReadReturnsBytes(t *testing.T) {
	t.Parallel()

	for _, text := range []string{abcText, "abcdefgh", byteOrderMark + "abcdefgh"} {
		source := plan.LimitedSourceOver(context.Background(), inputName, strings.NewReader(text))
		buffer := make([]byte, len(text))

		length, err := source.Read(buffer)
		if length == 0 || err != nil {
			t.Errorf("first Read of %q returned %d bytes and %v", text, length, err)
		}
	}
}

func (parserOnlyContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (parserOnlyContext) Done() <-chan struct{} { return nil }

func (parserOnlyContext) Value(any) any { return nil }

func (parserOnlyContext) Err() error {
	pcs := make([]uintptr, 32)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs)])

	for {
		frame, more := frames.Next()
		if strings.HasSuffix(frame.Function, readerFrame) {
			return nil
		}

		if !more {
			return context.Canceled
		}
	}
}

// TestDecoderChecksTheContextWhileParsing cancels a context that the reader never sees as canceled, so only the
// decoder's own checks between tokens can notice. They must, once the plan has more tokens than one check
// interval.
func TestDecoderChecksTheContextWhileParsing(t *testing.T) {
	t.Parallel()

	ctx := parserOnlyContext{}
	document := repeated(sourceStringJSON, 2*tokensPerCancelCheck)

	_, err := plan.Decode(ctx, inputFor(), strings.NewReader(document))
	planErr := codedError(t, err, plan.CodeInterrupted)

	if !errors.Is(err, context.Canceled) || planErr.Stage != plan.StageRead {
		t.Errorf(diagnosticFormat, planErr)
	}

	_, err = plan.Decode(ctx, inputFor(), strings.NewReader(repeated(sourceStringJSON, tokensPerCancelCheck/4)))
	if err != nil {
		t.Errorf("a plan with fewer tokens than one check interval needs no check: %v", err)
	}
}

func TestExponentSaturates(t *testing.T) {
	t.Parallel()

	const saturated = int64(1) << 40 // 1099511627776

	rows := []struct {
		tail string
		want int64
	}{
		{"", 0},
		{"0", 0},
		{"12", 12},
		{"+5", 5},
		{"-5", -5},
		{"1099511627775", saturated - 1},
		{"1099511627776", saturated},
		{"10995116277760", saturated},
		{"-10995116277760", -saturated},
		{"9999999999999", 9999999999999},
		{"99999999999999999999", 9999999999999},
	}

	for _, row := range rows {
		if got := plan.SaturatedExponent(row.tail); got != row.want {
			t.Errorf("exponent %q = %d, want %d", row.tail, got, row.want)
		}
	}
}
