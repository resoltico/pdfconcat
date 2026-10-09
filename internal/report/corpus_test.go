// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/resoltico/pdfconcat/internal/report"
)

type (
	verdict string

	refusingLoader struct{}

	// corpusCase edits one of the two hand-written base documents and states the decoder's and the
	// schema's verdicts. A non-empty Gap says why the two may differ: the rule is one JSON Schema cannot state.
	corpusCase struct {
		Name   string
		Base   string
		Old    string
		New    string
		Code   report.Code // "" means the decoder accepts
		Schema verdict
		Gap    string
	}
)

const (
	schemaAccept  verdict = "accept"
	schemaReject  verdict = "reject"
	schemaNotJSON verdict = "not-json"

	baseFailed = "failed"

	schemaURL     = "https://github.com/resoltico/pdfconcat/blob/main/internal/report/report.schema.json"
	metaSchemaURL = "https://json-schema.org/draft/2020-12/schema"

	gapReferences = "table references are cross-document rules"
	gapRelations  = "relations between members are not expressible in the schema"
	gapOrder      = "range order and contiguity are cross-record rules"
	gapLexical    = "duplicate members and lexical rules belong to the parser"
	gapInteger    = "the decoder reads integers written without fraction or exponent; the schema accepts 1.0 and 1e0"
)

var errLoadRefused = errors.New("load refused")

func (refusingLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("%w: %s", errLoadRefused, url)
}

func compileSchema(tb testing.TB, schema, id string) *jsonschema.Schema {
	tb.Helper()

	document, err := jsonschema.UnmarshalJSON(strings.NewReader(schema))
	if err != nil {
		tb.Fatalf("parse schema: %v", err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(refusingLoader{})
	compiler.DefaultDraft(jsonschema.Draft2020)

	err = compiler.AddResource(id, document)
	if err != nil {
		tb.Fatalf("add schema: %v", err)
	}

	compiled, err := compiler.Compile(id)
	if err != nil {
		tb.Fatalf("compile schema: %v", err)
	}

	return compiled
}

func schemaVerdict(tb testing.TB, compiled *jsonschema.Schema, document []byte) verdict {
	tb.Helper()

	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(bytes.TrimPrefix(document, []byte("\xef\xbb\xbf"))))
	if err != nil {
		return schemaNotJSON
	}

	if compiled.Validate(instance) != nil {
		return schemaReject
	}

	return schemaAccept
}

func reportSchema(tb testing.TB) *jsonschema.Schema {
	tb.Helper()

	return compileSchema(tb, report.Schema(), schemaURL)
}

func bases() map[string]string {
	return map[string]string{"ok": completeCheck, baseFailed: failedCheck}
}

func corpus() []corpusCase {
	var all []corpusCase

	for _, group := range [][]corpusCase{
		corpusVersionAndMembers(), corpusEnvelopeValues(), corpusCountsAndOutcome(), corpusPublication(),
		corpusSyntax(), corpusNumbersAndTypes(), corpusDiagnostics(), corpusSourcesAndFonts(),
		corpusPartIdentity(), corpusPartReferences(), corpusPartExtent(), corpusPartLayout(),
		corpusPageStyles(), corpusTextStyles(),
	} {
		all = append(all, group...)
	}

	return all
}

func corpusVersionAndMembers() []corpusCase {
	return []corpusCase{
		{Name: "complete check", Base: "ok", Schema: schemaAccept},
		{Name: "failed check with nulls", Base: baseFailed, Schema: schemaAccept},
		{
			Name:   "unsupported format 3",
			Base:   "ok",
			Old:    memberVersion,
			New:    `"format_version":3`,
			Code:   report.CodeUnsupportedVersion,
			Schema: schemaReject,
		},
		{
			Name:   "noncanonical format 2.0",
			Base:   "ok",
			Old:    memberVersion,
			New:    `"format_version":2.0`,
			Code:   report.CodeUnsupportedVersion,
			Schema: schemaAccept,
			Gap:    gapInteger,
		},
		{
			Name:   "noncanonical format 2e0",
			Base:   "ok",
			Old:    memberVersion,
			New:    `"format_version":2e0`,
			Code:   report.CodeUnsupportedVersion,
			Schema: schemaAccept,
			Gap:    gapInteger,
		},
		{
			Name:   "version string",
			Base:   "ok",
			Old:    memberVersion,
			New:    `"format_version":"2"`,
			Code:   report.CodeWrongType,
			Schema: schemaReject,
		},
		{
			Name:   "summary is not a report",
			Base:   "ok",
			Old:    `"kind":"report"`,
			New:    `"kind":"summary"`,
			Code:   report.CodeWrongKind,
			Schema: schemaReject,
		},
		{
			Name:   "unknown member",
			Base:   "ok",
			Old:    `"diagnostics":[],`,
			New:    `"diagnostics":[],"extra":1,`,
			Code:   report.CodeUnknownMember,
			Schema: schemaReject,
		},
	}
}

func corpusEnvelopeValues() []corpusCase {
	return []corpusCase{
		{
			Name:   "wrong-case member",
			Base:   "ok",
			Old:    memberStatusOK,
			New:    `"Status":"ok"`,
			Code:   report.CodeMissingMember,
			Schema: schemaReject,
		},
		{Name: "missing member", Base: "ok", Old: memberKind, Code: report.CodeMissingMember, Schema: schemaReject},
		{Name: "null where required", Base: "ok", Old: memberStatusOK, New: memberStatusNull, Code: report.CodeNull, Schema: schemaReject},
		{
			Name:   "null in array",
			Base:   "ok",
			Old:    memberEmptyDiagnostics,
			New:    `"diagnostics":[null]`,
			Code:   report.CodeNull,
			Schema: schemaReject,
		},
		{
			Name:   "status unknown",
			Base:   "ok",
			Old:    memberStatusOK,
			New:    `"status":"fine"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "command uppercase",
			Base:   "ok",
			Old:    `"command":"check"`,
			New:    `"command":"Check"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "phase state unknown",
			Base:   "ok",
			Old:    `"output_verification":"not_run"`,
			New:    `"output_verification":"done"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "layout complete after incomplete inspection",
			Base:   "ok",
			Old:    `"input_inspection":"complete"`,
			New:    `"input_inspection":"incomplete"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaAccept,
			Gap:    gapRelations,
		},
	}
}

func corpusCountsAndOutcome() []corpusCase {
	return []corpusCase{
		{
			Name:   "negative count",
			Base:   "ok",
			Old:    `"source_pages":3`,
			New:    `"source_pages":-3`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "counts do not add up",
			Base:   "ok",
			Old:    `"total_pages":5`,
			New:    `"total_pages":6`,
			Code:   report.CodeInvalidValue,
			Schema: schemaAccept,
			Gap:    gapRelations,
		},
		{
			Name:   "ok with a diagnostic",
			Base:   "ok",
			Old:    memberEmptyDiagnostics,
			New:    `"diagnostics":[{"severity":"error","stage":"x","code":"y","message":"m"}]`,
			Code:   report.CodeInvalidValue,
			Schema: schemaAccept,
			Gap:    gapRelations,
		},
		{
			Name:   "failure without diagnostics",
			Base:   baseFailed,
			Old:    failedDiagnostics,
			New:    `[]`,
			Code:   report.CodeInvalidValue,
			Schema: schemaAccept,
			Gap:    gapRelations,
		},
		{
			Name:   "output published without path",
			Base:   "ok",
			Old:    `"published":false`,
			New:    `"published":true`,
			Code:   report.CodeInvalidValue,
			Schema: schemaAccept,
			Gap:    gapRelations,
		},
		{Name: "report status unknown", Base: "ok", Old: `"written"`, New: `"saved"`, Code: report.CodeInvalidValue, Schema: schemaReject},
	}
}

func corpusPublication() []corpusCase {
	return []corpusCase{
		{
			Name:   "report written without path",
			Base:   "ok",
			Old:    `,"report_path":"/w/r.json"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "path without report",
			Base:   "ok",
			Old:    `"written"`,
			New:    `"not_requested"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaAccept,
			Gap:    gapRelations,
		},
		{
			Name:   "recovery report without failure",
			Base:   "ok",
			Old:    `"published":false`,
			New:    `"published":false,"recovery_report":"/w/rec.json","recovery_state":"current"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaAccept,
			Gap:    gapRelations,
		},
	}
}

func corpusSyntax() []corpusCase {
	return []corpusCase{
		{
			Name:   "duplicate member",
			Base:   "ok",
			Old:    memberKind,
			New:    duplicatedKindMembers,
			Code:   report.CodeDuplicateMember,
			Schema: schemaAccept,
			Gap:    gapLexical,
		},
		{
			Name:   "duplicate through escape",
			Base:   "ok",
			Old:    memberKind,
			New:    duplicatedKindMembers,
			Code:   report.CodeDuplicateMember,
			Schema: schemaAccept,
			Gap:    gapLexical,
		},
		{
			Name:   "invalid UTF-8",
			Base:   "ok",
			Old:    memberFontName,
			New:    "\"name\":\"Noto\xffSans\"",
			Code:   report.CodeSyntax,
			Schema: schemaAccept,
			Gap:    gapLexical,
		},
		{
			Name:   "comment",
			Base:   "ok",
			Old:    memberKind,
			New:    `"kind":"report",/*c*/`,
			Code:   report.CodeSyntax,
			Schema: schemaNotJSON,
		},
		{
			Name:   "trailing comma",
			Base:   "ok",
			Old:    `"fonts":[{"digest":"` + digestB + `","name":"NotoSans-Regular"}]`,
			New:    `"fonts":[],`,
			Code:   report.CodeSyntax,
			Schema: schemaNotJSON,
		},
		{
			Name:   "unpaired surrogate",
			Base:   "ok",
			Old:    `"path":"/w/a.pdf"`,
			New:    `"path":"\ud800"`,
			Code:   report.CodeSyntax,
			Schema: schemaAccept,
			Gap:    gapLexical,
		},
	}
}

func corpusNumbersAndTypes() []corpusCase {
	return []corpusCase{
		{
			Name:   "integer with fraction",
			Base:   "ok",
			Old:    memberBytes,
			New:    `"bytes":1234.5`,
			Code:   report.CodeWrongType,
			Schema: schemaReject,
		},
		{
			Name:   "integer overflow",
			Base:   "ok",
			Old:    memberBytes,
			New:    `"bytes":9223372036854775808`,
			Code:   report.CodeWrongType,
			Schema: schemaAccept,
			Gap:    "the decoder reads 64-bit integers; the schema has no bound",
		},
		{Name: "wrong type", Base: "ok", Old: memberBytes, New: `"bytes":"1234"`, Code: report.CodeWrongType, Schema: schemaReject},
	}
}

func corpusDiagnostics() []corpusCase {
	return []corpusCase{
		{
			Name:   "diagnostic stage uppercase",
			Base:   baseFailed,
			Old:    `"stage":"inspect"`,
			New:    `"stage":"Inspect"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "diagnostic without message",
			Base:   baseFailed,
			Old:    `,"message":"cannot read"`,
			Code:   report.CodeMissingMember,
			Schema: schemaReject,
		},
		{
			Name:   "diagnostic empty message",
			Base:   baseFailed,
			Old:    memberCannotRead,
			New:    `"message":""`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "location without file",
			Base:   baseFailed,
			Old:    `"file":"argv","argv_index":2`,
			New:    memberArgvIndex,
			Code:   report.CodeMissingMember,
			Schema: schemaReject,
		},
		{
			Name:   "offset without line",
			Base:   baseFailed,
			Old:    `"offset":30,"line":2,"column":3`,
			New:    `"offset":30`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "argv index and pointer",
			Base:   baseFailed,
			Old:    memberArgvIndex,
			New:    `"argv_index":2,"pointer":"/items/0"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
	}
}

func corpusSourcesAndFonts() []corpusCase {
	return []corpusCase{
		{
			Name:   "negative argv index",
			Base:   baseFailed,
			Old:    memberArgvIndex,
			New:    `"argv_index":-2`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "source without digest is allowed",
			Base:   baseFailed,
			Old:    `"sources":[{"path":"/w/a.pdf","bytes":null}]`,
			New:    `"sources":[{"path":"/w/a.pdf","bytes":7}]`,
			Schema: schemaAccept,
		},
		{
			Name:   "digest uppercase",
			Base:   "ok",
			Old:    digestA,
			New:    strings.ToUpper(digestA),
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "source empty path",
			Base:   "ok",
			Old:    `"path":"/w/a.pdf"`,
			New:    `"path":""`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "source negative bytes",
			Base:   "ok",
			Old:    memberBytes,
			New:    `"bytes":-1`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{Name: "font digest short", Base: "ok", Old: digestB, New: "bb", Code: report.CodeInvalidValue, Schema: schemaReject},
		{
			Name:   "font empty name",
			Base:   "ok",
			Old:    memberFontName,
			New:    `"name":""`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "font file path",
			Base:   "ok",
			Old:    memberFontName,
			New:    `"name":"NotoSans-Regular","file":"/w/fonts/N.ttf"`,
			Schema: schemaAccept,
		},
	}
}

func corpusPartIdentity() []corpusCase {
	return []corpusCase{
		{
			Name:   "duplicate part id",
			Base:   "ok",
			Old:    memberItemOneID,
			New:    `"id":"/items/0"`,
			Code:   report.CodeDuplicateID,
			Schema: schemaAccept,
			Gap:    gapOrder,
		},
		{
			Name:   "bad part id",
			Base:   "ok",
			Old:    memberItemOneID,
			New:    `"id":"/item/1"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{Name: "nested part id", Base: "ok", Old: memberItemOneID, New: `"id":"/items/42/items/3"`, Schema: schemaAccept},
		{
			Name:   "leading zero id",
			Base:   "ok",
			Old:    memberItemOneID,
			New:    `"id":"/items/01"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "part kind unknown",
			Base:   "ok",
			Old:    `"kind":"blank"`,
			New:    `"kind":"gap"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "pdf with style",
			Base:   "ok",
			Old:    `"source":0}`,
			New:    `"source":0,"style":0}`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{Name: "pdf without source", Base: "ok", Old: `,"source":0}`, New: `}`, Code: report.CodeInvalidValue, Schema: schemaReject},
		{
			Name:   "blank with source",
			Base:   "ok",
			Old:    `"style":0}`,
			New:    `"style":0,"source":0}`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
	}
}

func corpusPartReferences() []corpusCase {
	return []corpusCase{
		{
			Name:   "blank without style when layout complete",
			Base:   "ok",
			Old:    `,"style":0}`,
			New:    `}`,
			Code:   report.CodeInvalidValue,
			Schema: schemaAccept,
			Gap:    gapRelations,
		},
		{
			Name:   "dangling source",
			Base:   "ok",
			Old:    `"source":0}`,
			New:    `"source":3}`,
			Code:   report.CodeDanglingReference,
			Schema: schemaAccept,
			Gap:    gapReferences,
		},
		{
			Name:   "dangling style",
			Base:   "ok",
			Old:    `"style":0}`,
			New:    `"style":3}`,
			Code:   report.CodeDanglingReference,
			Schema: schemaAccept,
			Gap:    gapReferences,
		},
		{
			Name:   "dangling font",
			Base:   "ok",
			Old:    memberFontZeroEnd,
			New:    `"font":1}`,
			Code:   report.CodeDanglingReference,
			Schema: schemaAccept,
			Gap:    gapReferences,
		},
		{
			Name:   "negative reference",
			Base:   "ok",
			Old:    memberFontZeroEnd,
			New:    `"font":-1}`,
			Code:   report.CodeDanglingReference,
			Schema: schemaReject,
		},
		{
			Name:   "part origin without file",
			Base:   "ok",
			Old:    `"origin":{"file":"/w/job.json","offset":50,"line":3,"column":3}`,
			New:    `"origin":{}`,
			Code:   report.CodeMissingMember,
			Schema: schemaReject,
		},
	}
}

func corpusPartExtent() []corpusCase {
	return []corpusCase{
		{Name: "zero pages", Base: "ok", Old: memberPagesOf3, New: `"pages":0`, Code: report.CodeInvalidValue, Schema: schemaReject},
		{
			Name:   "blank over a million pages",
			Base:   "ok",
			Old:    `"range":{"start":4,"end":5},"pages":2`,
			New:    `"range":{"start":4,"end":1000004},"pages":1000001`,
			Code:   report.CodeInvalidValue,
			Schema: schemaAccept,
			Gap:    gapRelations,
		},
		{
			Name:   "range overlaps",
			Base:   "ok",
			Old:    `"start":4`,
			New:    `"start":3`,
			Code:   report.CodeInvalidRange,
			Schema: schemaAccept,
			Gap:    gapOrder,
		},
		{
			Name:   "range gap when layout is complete",
			Base:   "ok",
			Old:    `"start":4,"end":5`,
			New:    `"start":5,"end":6`,
			Code:   report.CodeInvalidRange,
			Schema: schemaAccept,
			Gap:    gapOrder,
		},
		{
			Name:   "range out of order",
			Base:   "ok",
			Old:    `"range":{"start":1,"end":3},"pages":3`,
			New:    `"range":{"start":7,"end":9},"pages":3`,
			Code:   report.CodeInvalidRange,
			Schema: schemaAccept,
			Gap:    gapOrder,
		},
		{
			Name:   "range inverted",
			Base:   "ok",
			Old:    `"start":1,"end":3`,
			New:    `"start":3,"end":1`,
			Code:   report.CodeInvalidRange,
			Schema: schemaAccept,
			Gap:    gapOrder,
		},
	}
}

func corpusPartLayout() []corpusCase {
	return []corpusCase{
		{
			Name:   "range starts at zero",
			Base:   "ok",
			Old:    `"start":1,"end":3`,
			New:    `"start":0,"end":2`,
			Code:   report.CodeInvalidRange,
			Schema: schemaReject,
		},
		{
			Name:   "range disagrees with pages",
			Base:   "ok",
			Old:    memberPagesOf3,
			New:    `"pages":4`,
			Code:   report.CodeInvalidRange,
			Schema: schemaAccept,
			Gap:    gapOrder,
		},
		{
			Name:   "range with unknown pages",
			Base:   "ok",
			Old:    memberPagesOf3,
			New:    `"pages":null`,
			Code:   report.CodeInvalidRange,
			Schema: schemaAccept,
			Gap:    gapOrder,
		},
		{
			Name:   "part without range when layout is complete",
			Base:   "ok",
			Old:    `"range":{"start":1,"end":3},"pages":3`,
			New:    `"range":null,"pages":null`,
			Code:   report.CodeInvalidRange,
			Schema: schemaAccept,
			Gap:    gapOrder,
		},
		{
			Name:   "counts disagree with ranges",
			Base:   "ok",
			Old:    `"generated_pages":2,"total_pages":5`,
			New:    `"generated_pages":1,"total_pages":4`,
			Code:   report.CodeInvalidRange,
			Schema: schemaAccept,
			Gap:    gapOrder,
		},
		{
			Name:   "counts unknown when layout is complete",
			Base:   "ok",
			Old:    `"total_pages":5`,
			New:    `"total_pages":null`,
			Code:   report.CodeInvalidValue,
			Schema: schemaAccept,
			Gap:    gapRelations,
		},
	}
}

func corpusPageStyles() []corpusCase {
	return []corpusCase{
		{Name: "background none", Base: "ok", Old: `"background":"#ffffff"`, New: `"background":"none"`, Schema: schemaAccept},
		{
			Name:   "background short hex",
			Base:   "ok",
			Old:    `"background":"#ffffff"`,
			New:    `"background":"#fff"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "size origin unknown",
			Base:   "ok",
			Old:    `"origin":"explicit"`,
			New:    `"origin":"guessed"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{Name: "inherited size", Base: "ok", Old: `"origin":"explicit"`, New: `"origin":"following_source"`, Schema: schemaAccept},
		{Name: "page side zero", Base: "ok", Old: `"width":595,`, New: `"width":0,`, Code: report.CodeInvalidValue, Schema: schemaReject},
		{
			Name:   "page side too large",
			Base:   "ok",
			Old:    `"height":842}`,
			New:    `"height":14401}`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "text color bad",
			Base:   "ok",
			Old:    `"color":"#000000"`,
			New:    `"color":"black"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "anchor unknown",
			Base:   "ok",
			Old:    `"anchor":"center"`,
			New:    `"anchor":"middle"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "align unknown",
			Base:   "ok",
			Old:    `"align":"center"`,
			New:    `"align":"middle"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{Name: "overflow allow", Base: "ok", Old: `"overflow":"error"`, New: `"overflow":"allow"`, Schema: schemaAccept},
	}
}

func corpusTextStyles() []corpusCase {
	return []corpusCase{
		{
			Name:   "overflow unknown",
			Base:   "ok",
			Old:    `"overflow":"error"`,
			New:    `"overflow":"clip"`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{Name: "font size zero", Base: "ok", Old: `"size":12`, New: `"size":0`, Code: report.CodeInvalidValue, Schema: schemaReject},
		{
			Name:   "offset out of range",
			Base:   "ok",
			Old:    `"x":0,"y":0`,
			New:    `"x":14401,"y":0`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{Name: "wrap width zero", Base: "ok", Old: `"width":523`, New: `"width":0`, Code: report.CodeInvalidValue, Schema: schemaReject},
		{
			Name:   "leading below one",
			Base:   "ok",
			Old:    `"leading":1.2`,
			New:    `"leading":0.5`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{
			Name:   "bounds with negative size",
			Base:   "ok",
			Old:    `"width":60,"height":14`,
			New:    `"width":-60,"height":14`,
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
		{Name: "bounds off the page", Base: "ok", Old: `"x":250,"y":420`, New: `"x":-250,"y":-420`, Schema: schemaAccept},
		{
			Name:   "style without text",
			Base:   "ok",
			Old:    `,"text":{"value":"Chapter ā"`,
			New:    `,"x_text":{"value":"Chapter ā"`,
			Code:   report.CodeUnknownMember,
			Schema: schemaReject,
		},
		{Name: "text of 10000 characters", Base: "ok", Old: "Chapter ā", New: strings.Repeat("ā", 10000), Schema: schemaAccept},
		{
			Name:   "text of 10001 characters",
			Base:   "ok",
			Old:    "Chapter ā",
			New:    strings.Repeat("ā", 10001),
			Code:   report.CodeInvalidValue,
			Schema: schemaReject,
		},
	}
}

// documentOf builds the row's document from its base.
func documentOf(tb testing.TB, row *corpusCase) string {
	tb.Helper()

	base := bases()[row.Base]
	if row.Old == "" {
		return base
	}

	return replaceOnce(tb, base, row.Old, row.New)
}

func TestCorpusAgainstSchemaAndDecoder(t *testing.T) {
	t.Parallel()

	compiled := reportSchema(t)

	for _, row := range corpus() {
		t.Run(row.Name, func(t *testing.T) {
			t.Parallel()

			doc := documentOf(t, &row)

			got := schemaVerdict(t, compiled, []byte(doc))
			if got != row.Schema {
				t.Errorf("schema verdict %s, corpus says %s", got, row.Schema)
			}

			decoded, err := decodeText(t, doc)
			checkDecoderVerdict(t, &row, decoded, err)
			checkGap(t, &row, err == nil, got)
		})
	}
}

// checkDecoderVerdict requires the decoder to accept the row's document, or to reject it with the row's code.
func checkDecoderVerdict(t *testing.T, row *corpusCase, decoded *report.Report, err error) {
	t.Helper()

	if row.Code != "" {
		if errorCode(err) != row.Code {
			t.Fatalf("decoder: %v, want code %s", err, row.Code)
		}

		return
	}

	if err != nil {
		t.Fatalf("decoder rejected: %v", err)
	}

	if decoded == nil {
		t.Fatal("no report")
	}
}

// checkGap requires the decoder and the schema to disagree exactly where the row documents a gap.
func checkGap(t *testing.T, row *corpusCase, decoderAccepts bool, got verdict) {
	t.Helper()

	schemaAccepts := got == schemaAccept

	switch {
	case row.Gap != "" && decoderAccepts == schemaAccepts && got != schemaNotJSON:
		t.Error("documented as a gap, but the decoder and the schema agree")
	case row.Gap == "" && decoderAccepts != schemaAccepts:
		t.Errorf("undocumented divergence: decoder accepts=%v schema=%s", decoderAccepts, got)
	default:
	}
}

// TestGapsAreOnlyWhatSchemasCannotExpress keeps the gap list honest: a gap row is one the decoder rejects.
func TestGapsAreOnlyWhatSchemasCannotExpress(t *testing.T) {
	t.Parallel()

	for _, row := range corpus() {
		if row.Gap != "" && (row.Code == "" || row.Schema != schemaAccept) {
			t.Errorf("%s: a gap row is accepted by the schema and rejected by the decoder", row.Name)
		}
	}
}

func TestBrokenSchemasAreRejected(t *testing.T) {
	t.Parallel()

	for name, broken := range map[string]string{
		"bad type":     `{"$schema":"` + metaSchemaURL + `","type":"strng"}`,
		"dangling ref": `{"$schema":"` + metaSchemaURL + `","$ref":"#/$defs/missing"}`,
	} {
		document, err := jsonschema.UnmarshalJSON(strings.NewReader(broken))
		if err != nil {
			t.Fatal(err)
		}

		compiler := jsonschema.NewCompiler()
		compiler.UseLoader(refusingLoader{})

		if compiler.AddResource("https://example.test/"+name, document) != nil {
			continue
		}

		if _, err = compiler.Compile("https://example.test/" + name); err == nil {
			t.Errorf("%s: compiled", name)
		}
	}
}

func TestSchemaNeverFetches(t *testing.T) {
	t.Parallel()

	document, err := jsonschema.UnmarshalJSON(
		strings.NewReader(`{"$schema":"` + metaSchemaURL + `","$ref":"https://example.com/other.json"}`),
	)
	if err != nil {
		t.Fatal(err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(refusingLoader{})

	if err = compiler.AddResource("https://example.test/remote.json", document); err != nil {
		t.Fatal(err)
	}

	if _, err = compiler.Compile("https://example.test/remote.json"); err == nil || !strings.Contains(err.Error(), errLoadRefused.Error()) {
		t.Fatalf("a remote $ref must fail rather than fetch: %v", err)
	}
}

func TestSchemaDeclaresItsIdentityAndMatchesTheDomainConstants(t *testing.T) {
	t.Parallel()

	for _, want := range []string{`"$schema": "` + metaSchemaURL + `"`, `"$id": "` + schemaURL + `"`, `"maxLength": 10000`} {
		if !strings.Contains(report.Schema(), want) {
			t.Errorf("schema lacks %s", want)
		}
	}

	if report.MaxTextRunes != 10000 {
		t.Errorf("MaxTextRunes = %d", report.MaxTextRunes)
	}
}
