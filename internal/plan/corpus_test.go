// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan_test

import (
	"strings"

	"github.com/resoltico/pdfconcat/internal/plan"
)

type (
	// verdict is how the JSON Schema validator sees a document.
	verdict string

	// corpusCase is one document with the decoder's and the schema's expected verdicts. A non-empty Gap
	// documents why the schema validator accepts something the decoder rejects (or the reverse): the rule is
	// lexical, structural beyond the schema language, or a unit-converted range.
	corpusCase struct {
		Name    string
		Doc     string
		Code    plan.Code // "" means the decoder accepts
		Schema  verdict
		Gap     string
		Pointer string // expected decoder pointer when set
	}
)

const (
	schemaAccept  verdict = "accept"
	schemaReject  verdict = "reject"
	schemaNotJSON verdict = "not-json" // the lenient instance parser used before validation refuses it

	// minimalPlan is the smallest valid plan.
	minimalPlan = `{"version":1,"items":["a.pdf"]}`

	// pathRuleGap explains the schema gap for NUL characters in paths.
	pathRuleGap = "a NUL in a path is a filesystem rule, not a schema rule"
)

// corpus is the shared accept/reject corpus: every case runs against both the decoder and the schema.
func corpus() []corpusCase {
	var all []corpusCase

	for _, group := range [][]corpusCase{
		corpusVersion(),
		corpusRootMembers(),
		corpusCounts(),
		corpusItemShapes(),
		corpusFontNames(),
		corpusFontObjects(),
		corpusOverflowAndBackground(),
		corpusSizesAndText(),
		corpusLengthGrammar(),
		corpusLengthUnitsAndLeading(),
		corpusLeadingBounds(),
		corpusDefaultsAndPaths(),
		corpusMemberShapes(),
		corpusEnumerationsAndEncoding(),
		corpusLexicalStructure(),
	} {
		all = append(all, group...)
	}

	return all
}

// corpusVersion lists the cases for the version member.
func corpusVersion() []corpusCase {
	return []corpusCase{
		{Name: "minimal", Doc: minimalPlan, Schema: schemaAccept},
		{Name: "version 1.0", Doc: `{"version":1.0,"items":["a.pdf"]}`, Schema: schemaAccept},
		{Name: "version 1e0", Doc: `{"version":1e0,"items":["a.pdf"]}`, Schema: schemaAccept},
		{Name: "version 10e-1", Doc: `{"version":10e-1,"items":["a.pdf"]}`, Schema: schemaAccept},
		{Name: "version 0.1E+1", Doc: `{"version":0.1E+1,"items":["a.pdf"]}`, Schema: schemaAccept},
		{Name: "version 2", Doc: `{"version":2,"items":["a.pdf"]}`, Code: plan.CodeUnsupportedVer, Schema: schemaReject},
		{Name: "version 1.5", Doc: `{"version":1.5,"items":["a.pdf"]}`, Code: plan.CodeNotInteger, Schema: schemaReject},
		{
			Name:   "version 2^53+1",
			Doc:    `{"version":9007199254740993,"items":["a.pdf"]}`,
			Code:   plan.CodeUnsupportedVer,
			Schema: schemaReject,
		},
		{Name: "version 1e400", Doc: `{"version":1e400,"items":["a.pdf"]}`, Code: plan.CodeOutOfRange, Schema: schemaReject},
		{Name: "version 2^63", Doc: `{"version":9223372036854775808,"items":["a.pdf"]}`, Code: plan.CodeOutOfRange, Schema: schemaReject},
		{Name: "version string", Doc: `{"version":"1","items":["a.pdf"]}`, Code: plan.CodeWrongType, Schema: schemaReject},
		{Name: "version null", Doc: `{"version":null,"items":["a.pdf"]}`, Code: plan.CodeNull, Schema: schemaReject},
		{Name: "version missing", Doc: `{"items":["a.pdf"]}`, Code: plan.CodeMissingMember, Schema: schemaReject},
	}
}

// corpusRootMembers lists the cases for required and unknown root members.
func corpusRootMembers() []corpusCase {
	return []corpusCase{
		{Name: "items missing", Doc: `{"version":1}`, Code: plan.CodeMissingMember, Schema: schemaReject},
		{Name: "items empty", Doc: `{"version":1,"items":[]}`, Code: plan.CodeEmptyItems, Schema: schemaReject},
		{Name: "items null", Doc: `{"version":1,"items":null}`, Code: plan.CodeNull, Schema: schemaReject},
		{Name: "items object", Doc: `{"version":1,"items":{}}`, Code: plan.CodeWrongType, Schema: schemaReject},
		{
			Name:   "duplicate version",
			Doc:    `{"version":1,"version":1,"items":["a.pdf"]}`,
			Code:   plan.CodeDuplicateMember,
			Schema: schemaAccept,
			Gap:    "JSON Schema validates the instance after a lenient parse that keeps the last duplicate",
		},
		{
			Name:   "duplicate version by escape",
			Doc:    `{"version":1,"vers\u0069on":1,"items":["a.pdf"]}`,
			Code:   plan.CodeDuplicateMember,
			Schema: schemaAccept,
			Gap:    "names identical only after unescaping; invisible to a schema",
		},
		{
			Name:   "duplicate nested member",
			Doc:    `{"version":1,"items":[{"blank":{"size":"A4","size":"A5"}}]}`,
			Code:   plan.CodeDuplicateMember,
			Schema: schemaAccept,
			Gap:    "duplicates at depth",
		},
		{
			Name:   "wrong case VERSION",
			Doc:    `{"VERSION":1,"version":1,"items":["a.pdf"]}`,
			Code:   plan.CodeUnknownMember,
			Schema: schemaReject,
		},
		{Name: "unknown member", Doc: `{"version":1,"items":["a.pdf"],"extra":1}`, Code: plan.CodeUnknownMember, Schema: schemaReject},
		{
			Name:   "unknown text member",
			Doc:    `{"version":1,"blank":{"text":{"colour":"#000"}},"items":["a.pdf"]}`,
			Code:   plan.CodeUnknownMember,
			Schema: schemaReject,
		},
		{Name: "$schema string", Doc: `{"$schema":"https://x/y.json","version":1,"items":["a.pdf"]}`, Schema: schemaAccept},
		{Name: "$schema number", Doc: `{"$schema":1,"version":1,"items":["a.pdf"]}`, Code: plan.CodeWrongType, Schema: schemaReject},
		{Name: "output empty", Doc: `{"version":1,"output":"","items":["a.pdf"]}`, Code: plan.CodeBadValue, Schema: schemaReject},
		{Name: "output null", Doc: `{"version":1,"output":null,"items":["a.pdf"]}`, Code: plan.CodeNull, Schema: schemaReject},
	}
}

// corpusCounts lists the cases for blank counts.
func corpusCounts() []corpusCase {
	return []corpusCase{
		{Name: "count 1.0", Doc: `{"version":1,"items":[{"blank":{},"count":1.0}]}`, Schema: schemaAccept},
		{Name: "count 1e0", Doc: `{"version":1,"items":[{"blank":{},"count":1e0}]}`, Schema: schemaAccept},
		{Name: "count 1e6 (max)", Doc: `{"version":1,"items":[{"blank":{},"count":1e6}]}`, Schema: schemaAccept},
		{
			Name:    "count 1.5",
			Doc:     `{"version":1,"items":[{"blank":{},"count":1.5}]}`,
			Code:    plan.CodeNotInteger,
			Schema:  schemaReject,
			Pointer: "/items/0/count",
		},
		{Name: "count 0", Doc: `{"version":1,"items":[{"blank":{},"count":0}]}`, Code: plan.CodeOutOfRange, Schema: schemaReject},
		{Name: "count -0", Doc: `{"version":1,"items":[{"blank":{},"count":-0}]}`, Code: plan.CodeOutOfRange, Schema: schemaReject},
		{
			Name:   "count 1000001",
			Doc:    `{"version":1,"items":[{"blank":{},"count":1000001}]}`,
			Code:   plan.CodeOutOfRange,
			Schema: schemaReject,
		},
		{
			Name:   "count 2^53+1",
			Doc:    `{"version":1,"items":[{"blank":{},"count":9007199254740993}]}`,
			Code:   plan.CodeOutOfRange,
			Schema: schemaReject,
		},
		{Name: "count 1e400", Doc: `{"version":1,"items":[{"blank":{},"count":1e400}]}`, Code: plan.CodeOutOfRange, Schema: schemaReject},
		{Name: "count string", Doc: `{"version":1,"items":[{"blank":{},"count":"2"}]}`, Code: plan.CodeWrongType, Schema: schemaReject},
		{Name: "count null", Doc: `{"version":1,"items":[{"blank":{},"count":null}]}`, Code: plan.CodeNull, Schema: schemaReject},
	}
}

// corpusItemShapes lists the cases for item shapes and groups.
func corpusItemShapes() []corpusCase {
	return []corpusCase{
		{Name: "count without blank", Doc: `{"version":1,"items":[{"count":2}]}`, Code: plan.CodeAmbiguousItem, Schema: schemaReject},
		{
			Name:   "blank and dir",
			Doc:    `{"version":1,"items":[{"blank":{},"dir":"d","items":["a"]}]}`,
			Code:   plan.CodeAmbiguousItem,
			Schema: schemaReject,
		},
		{Name: "group without items", Doc: `{"version":1,"items":[{"dir":"d"}]}`, Code: plan.CodeMissingMember, Schema: schemaReject},
		{
			Name:   "group with empty items",
			Doc:    `{"version":1,"items":[{"dir":"d","items":[]}]}`,
			Code:   plan.CodeEmptyItems,
			Schema: schemaReject,
		},
		{
			Name:   "nested groups",
			Doc:    `{"version":1,"items":[{"dir":"a","items":[{"dir":"b","items":["c.pdf",{"blank":{}}]}]}]}`,
			Schema: schemaAccept,
		},
		{Name: "item number", Doc: `{"version":1,"items":[1]}`, Code: plan.CodeWrongType, Schema: schemaReject, Pointer: "/items/0"},
		{Name: "item null", Doc: `{"version":1,"items":["a",null]}`, Code: plan.CodeNull, Schema: schemaReject, Pointer: "/items/1"},
		{Name: "item array", Doc: `{"version":1,"items":[["a"]]}`, Code: plan.CodeWrongType, Schema: schemaReject},
		{Name: "item empty string", Doc: `{"version":1,"items":[""]}`, Code: plan.CodeBadValue, Schema: schemaReject},
	}
}

// corpusFontNames lists the cases for built-in font names.
func corpusFontNames() []corpusCase {
	return []corpusCase{
		{
			Name: "full blank style",
			Doc: `{"version":1,"blank":{"size":"A4","background":"#F4EFE6",` +
				`"text":{"value":"x\ny","font":{"file":"fonts/X.ttf"},"size":"14pt","color":"#555","anchor":"top-left",` +
				`"x":"25mm","y":-30,"width":300,"align":"justify","leading":1.2}},"items":["a"]}`,
			Schema: schemaAccept,
		},
		{Name: "blank null", Doc: `{"version":1,"blank":null,"items":["a"]}`, Code: plan.CodeNull, Schema: schemaReject},
		{
			Name:   "text value null",
			Doc:    `{"version":1,"blank":{"text":{"value":null}},"items":["a"]}`,
			Code:   plan.CodeNull,
			Schema: schemaReject,
		},
		{Name: "text value empty", Doc: `{"version":1,"blank":{"text":{"value":""}},"items":["a"]}`, Schema: schemaAccept},
		{
			Name:   "font unknown name",
			Doc:    `{"version":1,"blank":{"text":{"font":"Helvetica"}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{Name: "font default", Doc: `{"version":1,"blank":{"text":{"font":"default"}},"items":["a"]}`, Schema: schemaAccept},
		{
			Name:   "font default wrong case",
			Doc:    `{"version":1,"blank":{"text":{"font":"Default"}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{Name: "font file", Doc: `{"version":1,"blank":{"text":{"font":{"file":"f/A.ttf"}}},"items":["a"]}`, Schema: schemaAccept},
	}
}

// corpusFontObjects lists the cases for file font objects.
func corpusFontObjects() []corpusCase {
	return []corpusCase{
		{
			Name:   "font file empty",
			Doc:    `{"version":1,"blank":{"text":{"font":{"file":""}}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{
			Name:   "font file missing",
			Doc:    `{"version":1,"blank":{"text":{"font":{}}},"items":["a"]}`,
			Code:   plan.CodeMissingMember,
			Schema: schemaReject,
		},
		{
			Name:   "font file number",
			Doc:    `{"version":1,"blank":{"text":{"font":{"file":1}}},"items":["a"]}`,
			Code:   plan.CodeWrongType,
			Schema: schemaReject,
		},
		{
			Name:   "font file null",
			Doc:    `{"version":1,"blank":{"text":{"font":{"file":null}}},"items":["a"]}`,
			Code:   plan.CodeNull,
			Schema: schemaReject,
		},
		{
			Name:   "font object extra member",
			Doc:    `{"version":1,"blank":{"text":{"font":{"file":"a.ttf","size":1}}},"items":["a"]}`,
			Code:   plan.CodeUnknownMember,
			Schema: schemaReject,
		},
		{
			Name:   "font file NUL",
			Doc:    `{"version":1,"blank":{"text":{"font":{"file":"a\u0000.ttf"}}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaAccept,
			Gap:    pathRuleGap,
		},
		{Name: "font null", Doc: `{"version":1,"blank":{"text":{"font":null}},"items":["a"]}`, Code: plan.CodeNull, Schema: schemaReject},
		{
			Name:   "font array",
			Doc:    `{"version":1,"blank":{"text":{"font":["a"]}},"items":["a"]}`,
			Code:   plan.CodeWrongType,
			Schema: schemaReject,
		},
	}
}

// corpusOverflowAndBackground lists the cases for overflow policies and backgrounds.
func corpusOverflowAndBackground() []corpusCase {
	return []corpusCase{
		{Name: "overflow allow", Doc: `{"version":1,"blank":{"text":{"overflow":"allow"}},"items":["a"]}`, Schema: schemaAccept},
		{Name: "overflow error", Doc: `{"version":1,"blank":{"text":{"overflow":"error"}},"items":["a"]}`, Schema: schemaAccept},
		{
			Name:   "overflow unknown",
			Doc:    `{"version":1,"blank":{"text":{"overflow":"clip"}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{
			Name:   "overflow bool",
			Doc:    `{"version":1,"blank":{"text":{"overflow":true}},"items":["a"]}`,
			Code:   plan.CodeWrongType,
			Schema: schemaReject,
		},
		{Name: "background none", Doc: `{"version":1,"blank":{"background":"none"},"items":["a"]}`, Schema: schemaAccept},
		{
			Name:   "background None",
			Doc:    `{"version":1,"blank":{"background":"None"},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{
			Name:   "background null",
			Doc:    `{"version":1,"blank":{"background":null},"items":["a"]}`,
			Code:   plan.CodeNull,
			Schema: schemaReject,
		},
	}
}

// corpusSizesAndText lists the cases for page sizes and text values.
func corpusSizesAndText() []corpusCase {
	return []corpusCase{
		{Name: "size inherit", Doc: `{"version":1,"blank":{"size":"inherit"},"items":["a"]}`, Schema: schemaAccept},
		{Name: "size named", Doc: `{"version":1,"blank":{"size":"Letter"},"items":["a"]}`, Schema: schemaAccept},
		{Name: "size dimensions", Doc: `{"version":1,"blank":{"size":"210x297mm"},"items":["a"]}`, Schema: schemaAccept},
		{
			Name:   "size lower case name",
			Doc:    `{"version":1,"blank":{"size":"a4"},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{
			Name:   "size too small",
			Doc:    `{"version":1,"blank":{"size":"0.5x10"},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaAccept,
			Gap:    "per-side range of a parsed size is a decoder rule",
		},
		{
			Name:   "size too large",
			Doc:    `{"version":1,"blank":{"size":"14401x100"},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaAccept,
			Gap:    "per-side range of a parsed size is a decoder rule",
		},
		{Name: "size negative", Doc: `{"version":1,"blank":{"size":"-5x10"},"items":["a"]}`, Code: plan.CodeBadValue, Schema: schemaReject},
		{Name: "size number", Doc: `{"version":1,"blank":{"size":5},"items":["a"]}`, Code: plan.CodeWrongType, Schema: schemaReject},
		{
			Name:   "text value too long",
			Doc:    `{"version":1,"blank":{"text":{"value":"` + strings.Repeat("x", 10001) + `"}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{
			Name:   "text value at the limit",
			Doc:    `{"version":1,"blank":{"text":{"value":"` + strings.Repeat("ā", 10000) + `"}},"items":["a"]}`,
			Schema: schemaAccept,
		},
	}
}

// corpusLengthGrammar lists the cases for the length grammar.
func corpusLengthGrammar() []corpusCase {
	return []corpusCase{
		{
			Name:   "length too large string",
			Doc:    `{"version":1,"blank":{"text":{"x":"99999mm"}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaAccept,
			Gap:    "unit-converted length bounds are a decoder rule",
		},
		{
			Name:   "length too large number",
			Doc:    `{"version":1,"blank":{"text":{"x":14400.5}},"items":["a"]}`,
			Code:   plan.CodeOutOfRange,
			Schema: schemaReject,
		},
		{Name: "length at the limit", Doc: `{"version":1,"blank":{"text":{"x":"5080mm","y":-14400}},"items":["a"]}`, Schema: schemaAccept},
		{
			Name:   "length exponent string",
			Doc:    `{"version":1,"blank":{"text":{"x":"1e3"}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{Name: "length exponent number", Doc: `{"version":1,"blank":{"text":{"x":1e2}},"items":["a"]}`, Schema: schemaAccept},
		{
			Name:   "length trailing dot",
			Doc:    `{"version":1,"blank":{"text":{"x":"5."}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{Name: "length leading dot", Doc: `{"version":1,"blank":{"text":{"x":".5mm"}},"items":["a"]}`, Schema: schemaAccept},
		{
			Name:   "length hex float",
			Doc:    `{"version":1,"blank":{"text":{"x":"0x1p3"}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{
			Name:   "length inf word",
			Doc:    `{"version":1,"blank":{"text":{"x":"Inf"}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{
			Name:   "length empty string",
			Doc:    `{"version":1,"blank":{"text":{"x":""}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{
			Name:   "length with space",
			Doc:    `{"version":1,"blank":{"text":{"x":"10 mm"}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
	}
}

// corpusLengthUnitsAndLeading lists the cases for length units and leading.
func corpusLengthUnitsAndLeading() []corpusCase {
	return []corpusCase{
		{Name: "length unit string", Doc: `{"version":1,"blank":{"text":{"size":"10.5mm"}},"items":["a"]}`, Schema: schemaAccept},
		{
			Name:   "length padded string",
			Doc:    `{"version":1,"blank":{"text":{"size":" 10.5mm "}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{
			Name:   "length bad unit",
			Doc:    `{"version":1,"blank":{"text":{"size":"10km"}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{
			Name:   "length NBSP",
			Doc:    `{"version":1,"blank":{"text":{"size":" 10mm"}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{
			Name:   "length bool",
			Doc:    `{"version":1,"blank":{"text":{"size":true}},"items":["a"]}`,
			Code:   plan.CodeWrongType,
			Schema: schemaReject,
		},
		{
			Name:   "length 1e400",
			Doc:    `{"version":1,"blank":{"text":{"x":1e400}},"items":["a"]}`,
			Code:   plan.CodeOutOfRange,
			Schema: schemaReject,
		},
		{
			Name:   "leading 11",
			Doc:    `{"version":1,"blank":{"text":{"leading":11}},"items":["a"]}`,
			Code:   plan.CodeOutOfRange,
			Schema: schemaReject,
		},
		{Name: "leading 1", Doc: `{"version":1,"blank":{"text":{"leading":1}},"items":["a"]}`, Schema: schemaAccept},
		{
			Name:   "leading NaN-like string",
			Doc:    `{"version":1,"blank":{"text":{"leading":"1.2"}},"items":["a"]}`,
			Code:   plan.CodeWrongType,
			Schema: schemaReject,
		},
		{
			Name:   "leading 0.5",
			Doc:    `{"version":1,"blank":{"text":{"leading":0.5}},"items":["a"]}`,
			Code:   plan.CodeOutOfRange,
			Schema: schemaReject,
		},
		{
			Name:   "leading 1e400",
			Doc:    `{"version":1,"blank":{"text":{"leading":1e400}},"items":["a"]}`,
			Code:   plan.CodeOutOfRange,
			Schema: schemaReject,
		},
	}
}

// corpusDefaultsAndPaths lists the cases for defaults, output, and directory paths.
func corpusDefaultsAndPaths() []corpusCase {
	return []corpusCase{
		{Name: "defaults only blank", Doc: `{"version":1,"blank":{"text":{"value":"x"}},"items":[{"blank":{}}]}`, Schema: schemaAccept},
		{Name: "output and dir", Doc: `{"version":1,"output":"o.pdf","dir":"src","items":["a"]}`, Schema: schemaAccept},
		{
			Name:   "output NUL",
			Doc:    `{"version":1,"output":"a\u0000b","items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaAccept,
			Gap:    pathRuleGap,
		},
		{
			Name:   "dir NUL",
			Doc:    `{"version":1,"dir":"a\u0000b","items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaAccept,
			Gap:    pathRuleGap,
		},
		{
			Name:   "group dir NUL",
			Doc:    `{"version":1,"items":[{"dir":"a\u0000b","items":["a"]}]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaAccept,
			Gap:    pathRuleGap,
		},
		{Name: "dir null", Doc: `{"version":1,"dir":null,"items":["a"]}`, Code: plan.CodeNull, Schema: schemaReject},
		{Name: "dir empty string", Doc: `{"version":1,"dir":"","items":["a"]}`, Schema: schemaAccept},
		{
			Name:   "item path NUL",
			Doc:    `{"version":1,"items":["a\u0000b"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaAccept,
			Gap:    pathRuleGap,
		},
	}
}

// corpusMemberShapes lists the cases for member shapes and types.
func corpusMemberShapes() []corpusCase {
	return []corpusCase{
		{Name: "blank string", Doc: `{"version":1,"blank":"x","items":["a"]}`, Code: plan.CodeWrongType, Schema: schemaReject},
		{Name: "text array", Doc: `{"version":1,"blank":{"text":[]},"items":["a"]}`, Code: plan.CodeWrongType, Schema: schemaReject},
		{Name: "text null", Doc: `{"version":1,"blank":{"text":null},"items":["a"]}`, Code: plan.CodeNull, Schema: schemaReject},
		{
			Name:   "align unknown",
			Doc:    `{"version":1,"blank":{"text":{"align":"start"}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{Name: "escape-spelled member names", Doc: `{"vers\u0069on":1,"items":["a"]}`, Schema: schemaAccept},
		{Name: "group dir null", Doc: `{"version":1,"items":[{"dir":null,"items":["a"]}]}`, Code: plan.CodeNull, Schema: schemaReject},
		{Name: "group items null", Doc: `{"version":1,"items":[{"dir":"d","items":null}]}`, Code: plan.CodeNull, Schema: schemaReject},
		{
			Name:   "item unknown member",
			Doc:    `{"version":1,"items":[{"blank":{},"cnt":2}]}`,
			Code:   plan.CodeUnknownMember,
			Schema: schemaReject,
		},
		{
			Name:   "item duplicate blank",
			Doc:    `{"version":1,"items":[{"blank":{},"blank":{}}]}`,
			Code:   plan.CodeDuplicateMember,
			Schema: schemaAccept,
			Gap:    "duplicates at depth",
		},
		{Name: "empty item object", Doc: `{"version":1,"items":[{}]}`, Code: plan.CodeAmbiguousItem, Schema: schemaReject},
		{Name: "item boolean", Doc: `{"version":1,"items":[true]}`, Code: plan.CodeWrongType, Schema: schemaReject},
		{
			Name:   "generated pages over the total",
			Doc:    `{"version":1,"items":[{"blank":{},"count":600000},{"blank":{},"count":600000}]}`,
			Code:   plan.CodeLimitPages,
			Schema: schemaAccept,
			Gap:    "aggregate limits are a decoder rule",
		},
		{
			Name:   "ambiguous dir without items but with count",
			Doc:    `{"version":1,"items":[{"dir":"d","count":2}]}`,
			Code:   plan.CodeAmbiguousItem,
			Schema: schemaReject,
		},
	}
}

// corpusEnumerationsAndEncoding lists the cases for enumerations, surrogates, and encodings.
func corpusEnumerationsAndEncoding() []corpusCase {
	return []corpusCase{
		{
			Name:   "anchor wrong case",
			Doc:    `{"version":1,"blank":{"text":{"anchor":"Top"}},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{
			Name:   "color 2 digits",
			Doc:    `{"version":1,"blank":{"background":"#12"},"items":["a"]}`,
			Code:   plan.CodeBadValue,
			Schema: schemaReject,
		},
		{Name: "surrogate pair escape", Doc: `{"version":1,"items":["😀.pdf"]}`, Schema: schemaAccept},
		{
			Name: "lone high surrogate escape", Doc: `{"version":1,"items":["\ud800.pdf"]}`, Code: plan.CodeSyntax, Schema: schemaAccept,
			Gap: "lexical Unicode rule; a lenient parser substitutes U+FFFD before validation",
		},
		{
			Name:   "lone low surrogate escape",
			Doc:    `{"version":1,"items":["\ude00.pdf"]}`,
			Code:   plan.CodeSyntax,
			Schema: schemaAccept,
			Gap:    "lexical Unicode rule",
		},
		{
			Name:   "high surrogate then non-surrogate",
			Doc:    `{"version":1,"items":["\ud800A"]}`,
			Code:   plan.CodeSyntax,
			Schema: schemaAccept,
			Gap:    "lexical Unicode rule",
		},
		{
			Name:   "invalid UTF-8 byte",
			Doc:    "{\"version\":1,\"items\":[\"a\xffb.pdf\"]}",
			Code:   plan.CodeSyntax,
			Schema: schemaAccept,
			Gap:    "lexical UTF-8 rule",
		},
		{Name: "BOM then valid", Doc: "\xef\xbb\xbf" + minimalPlan, Schema: schemaAccept},
		{Name: "two BOMs", Doc: "\xef\xbb\xbf\xef\xbb\xbf" + minimalPlan, Code: plan.CodeSyntax, Schema: schemaNotJSON},
	}
}

// corpusLexicalStructure lists the cases for comments, commas, trailing data, and truncation.
func corpusLexicalStructure() []corpusCase {
	return []corpusCase{
		{Name: "comment after", Doc: minimalPlan + " // c", Code: plan.CodeTrailingData, Schema: schemaNotJSON},
		{Name: "block comment inside", Doc: `{"version":1,/*x*/"items":["a"]}`, Code: plan.CodeSyntax, Schema: schemaNotJSON},
		{Name: "trailing comma object", Doc: `{"version":1,"items":["a"],}`, Code: plan.CodeSyntax, Schema: schemaNotJSON},
		{Name: "trailing comma array", Doc: `{"version":1,"items":["a",]}`, Code: plan.CodeSyntax, Schema: schemaNotJSON},
		{Name: "NaN", Doc: `{"version":NaN,"items":["a"]}`, Code: plan.CodeSyntax, Schema: schemaNotJSON},
		{
			Name:   "Infinity",
			Doc:    `{"version":1,"blank":{"text":{"x":Infinity}},"items":["a"]}`,
			Code:   plan.CodeSyntax,
			Schema: schemaNotJSON,
		},
		{
			Name:   "leading zero",
			Doc:    `{"version":1,"blank":{"text":{"width":01}},"items":["a"]}`,
			Code:   plan.CodeSyntax,
			Schema: schemaNotJSON,
		},
		{Name: "single quotes", Doc: `{'version':1,'items':['a']}`, Code: plan.CodeSyntax, Schema: schemaNotJSON},
		{Name: "trailing garbage word", Doc: minimalPlan + " xyz", Code: plan.CodeTrailingData, Schema: schemaNotJSON},
		{Name: "trailing quote", Doc: minimalPlan + ` "`, Code: plan.CodeTrailingData, Schema: schemaNotJSON},
		{Name: "trailing brace", Doc: minimalPlan + ` }`, Code: plan.CodeTrailingData, Schema: schemaNotJSON},
		{Name: "second object", Doc: minimalPlan + ` {}`, Code: plan.CodeTrailingData, Schema: schemaNotJSON},
		{Name: "second value no space", Doc: minimalPlan + `{"x":1}`, Code: plan.CodeTrailingData, Schema: schemaNotJSON},
		{Name: "trailing whitespace ok", Doc: minimalPlan + " \t\r\n\n  ", Schema: schemaAccept},
		{Name: "trailing NUL", Doc: minimalPlan + "\x00", Code: plan.CodeTrailingData, Schema: schemaNotJSON},
		{Name: "array document", Doc: `[]`, Code: plan.CodeNotObject, Schema: schemaReject},
		{Name: "string document", Doc: `"x"`, Code: plan.CodeNotObject, Schema: schemaReject},
		{Name: "empty document", Doc: ``, Code: plan.CodeEmpty, Schema: schemaNotJSON},
		{Name: "whitespace document", Doc: " \n ", Code: plan.CodeEmpty, Schema: schemaNotJSON},
		{Name: "truncated", Doc: `{"version":1,"items":["a"`, Code: plan.CodeSyntax, Schema: schemaNotJSON},
	}
}

// corpusLeadingBounds lists the cases at and around both ends of the leading range.
func corpusLeadingBounds() []corpusCase {
	return []corpusCase{
		{Name: "leading 10", Doc: `{"version":1,"blank":{"text":{"leading":10}},"items":["a"]}`, Schema: schemaAccept},
		{Name: "leading 10.0", Doc: `{"version":1,"blank":{"text":{"leading":10.0}},"items":["a"]}`, Schema: schemaAccept},
		{
			Name:   "leading just above 10",
			Doc:    `{"version":1,"blank":{"text":{"leading":10.000000000000002}},"items":["a"]}`,
			Code:   plan.CodeOutOfRange,
			Schema: schemaReject,
		},
		{
			Name:   "leading just below 1",
			Doc:    `{"version":1,"blank":{"text":{"leading":0.9999999999999999}},"items":["a"]}`,
			Code:   plan.CodeOutOfRange,
			Schema: schemaReject,
		},
	}
}
