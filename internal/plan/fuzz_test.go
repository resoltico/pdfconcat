// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/plan"
)

const fuzzName = "fuzz.json"

// dumpJob renders everything a decoded job holds except provenance as text, so two decodes can be compared.
func dumpJob(job *assembly.Job) string {
	var builder strings.Builder

	fmt.Fprintf(&builder, "base=%q output=%v dir=%v defaults=%v\n", job.Base, job.Output.Value, job.Dir.Value, dumpStyle(&job.Defaults))
	dumpItems(&builder, job.Items, 0)

	return builder.String()
}

func dumpStyle(style *assembly.BlankStyle) string {
	text := &style.Text

	return fmt.Sprint(style.Size, style.Background, text.Value, text.Font, text.Size, text.Color, text.Anchor, text.X, text.Y, text.Width,
		text.Align, text.Leading, text.Overflow)
}

func dumpItems(builder *strings.Builder, items []assembly.Item, depth int) {
	for index := range items {
		item := &items[index]

		fmt.Fprintf(
			builder,
			"%*s%v ref=%d off=%d path=%q dir=%q",
			depth,
			"",
			item.Kind,
			item.Origin.Ref,
			item.Origin.Offset,
			item.Path,
			item.Dir.Value,
		)

		if item.Blank != nil {
			fmt.Fprintf(builder, " count=%d style=%s", item.Blank.Count, dumpStyle(&item.Blank.Style))
		}

		builder.WriteString("\n")
		dumpItems(builder, item.Items, depth+1)
	}
}

func knownCodes() []plan.Code {
	return []plan.Code{
		plan.CodeInterrupted,
		plan.CodeReadFailed,
		plan.CodeTooLarge,
		plan.CodeEmpty,
		plan.CodeSyntax,
		plan.CodeDuplicateMember,
		plan.CodeTrailingData,
		plan.CodeNotObject,
		plan.CodeUnknownMember,
		plan.CodeNull,
		plan.CodeWrongType,
		plan.CodeOutOfRange,
		plan.CodeNotInteger,
		plan.CodeBadValue,
		plan.CodeMissingMember,
		plan.CodeAmbiguousItem,
		plan.CodeUnsupportedVer,
		plan.CodeEmptyItems,
		plan.CodeLimitNodes,
		plan.CodeLimitDepth,
		plan.CodeLimitContribs,
		plan.CodeLimitPages,
	}
}

// FuzzDecode feeds arbitrary bytes to the decoder. It must never panic or hang, must decide
// deterministically, must locate every failure with a stable code, and must only accept documents that an
// independent JSON parser and the JSON Schema also accept and whose job satisfies the domain invariants.
func FuzzDecode(f *testing.F) {
	for _, row := range corpus() {
		f.Add([]byte(row.Doc))
	}

	f.Add([]byte(minimalPlan + "\n  "))
	f.Add([]byte(nestedGroups(assembly.MaxGroupDepth)))
	f.Add([]byte(nestedGroups(assembly.MaxGroupDepth + 1)))
	f.Add([]byte(everyMemberPlan()))

	compiled := planSchema(f)

	f.Fuzz(func(t *testing.T, data []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		input := plan.Input{Name: fuzzName, BaseDir: baseDir}

		job, err := plan.Decode(ctx, input, bytes.NewReader(data))
		again, errAgain := plan.Decode(ctx, input, bytes.NewReader(data))

		if (err == nil) != (errAgain == nil) {
			t.Fatalf("not deterministic: %v vs %v", err, errAgain)
		}

		if err != nil {
			checkFailure(t, err, errAgain, len(data))

			return
		}

		if dumpJob(job) != dumpJob(again) {
			t.Fatal("decoded jobs differ between runs")
		}

		checkSuccess(t, data, job, compiled)
	})
}

func checkFailure(t *testing.T, err, again error, size int) {
	t.Helper()

	first := wantAnyCode(t, err)
	second := wantAnyCode(t, again)

	if *first != *second {
		t.Fatalf("diagnostics differ: %+v vs %+v", first, second)
	}

	location := first.Location
	located := location.Source == fuzzName && location.Offset >= 0 && location.Offset <= int64(size)
	positioned := location.Line >= 1 && location.Column >= 1

	if first.Message == "" || !located || !positioned {
		t.Fatalf("malformed diagnostic %+v for %d bytes", first, size)
	}

	if location.Pointer != "" && location.Pointer[0] != '/' {
		t.Fatalf("pointer %q is not a JSON pointer", location.Pointer)
	}
}

// wantAnyCode asserts that err is a *plan.Error with one of the documented codes.
func wantAnyCode(t *testing.T, err error) *plan.Error {
	t.Helper()

	planErr, ok := errors.AsType[*plan.Error](err)
	if ok && slices.Contains(knownCodes(), planErr.Code) {
		return planErr
	}

	t.Fatalf("%v is not a *plan.Error with a documented code", err)

	return nil
}

func checkSuccess(t *testing.T, data []byte, job *assembly.Job, compiled *jsonschema.Schema) {
	t.Helper()

	err := job.Validate()
	if err != nil {
		t.Fatalf("accepted job violates the domain invariants: %v", err)
	}

	var anyValue any

	err = json.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &anyValue)
	if err != nil {
		t.Fatalf("accepted a document that encoding/json rejects: %v", err)
	}

	if got := schemaVerdict(t, compiled, data); got != schemaAccept {
		t.Fatalf("accepted a document the schema says is %s", got)
	}

	_, err = plan.Decode(context.Background(), plan.Input{Name: fuzzName}, bytes.NewReader(append(bytes.Clone(data), " x"...)))
	if err == nil {
		t.Fatal("trailing data after an accepted document was accepted")
	}

	_, err = plan.Decode(context.Background(), plan.Input{Name: fuzzName}, bytes.NewReader(append(bytes.Clone(data), " \n\t\r"...)))
	if err != nil {
		t.Fatalf("trailing whitespace after an accepted document was rejected: %v", err)
	}
}
