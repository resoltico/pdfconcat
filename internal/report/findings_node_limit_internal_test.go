// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report

import (
	"bytes"
	"testing"
)

func TestComputedFindingsWriterNodeLimitMatchesDecoder(t *testing.T) {
	t.Parallel()

	value := nodeSamples()[0]
	value.Styles[0].Text.Findings = []TextFinding{
		{Kind: "word-wider-than-box", Line: 0, Detail: "wide word"},
		{Kind: "outside-page-horizontal", Line: -1, Detail: "ink leaves page"},
		{Kind: "outside-page-vertical", Line: -1, Detail: "mark above page"},
	}

	var output bytes.Buffer

	written, err := Write(&output, value, MaxReportBytes)
	if err != nil || written != int64(output.Len()) || written == 0 {
		t.Fatalf("valid findings report write failed: bytes=%d error=%v", written, err)
	}

	limits := DefaultLimits()
	limits.MaxNodes = value.nodes()

	decoded, err := DecodeLimited(
		DecoderTestContext(t.Context(), t),
		"findings.report.json",
		ReaderRequiringStorage(bytes.NewReader(output.Bytes())),
		limits,
	)
	if err != nil || decoded == nil || len(decoded.Styles[0].Text.Findings) != 3 {
		t.Fatalf("writer node budget must admit every encoded finding: %v", err)
	}

	limits.MaxNodes--

	_, err = DecodeLimited(
		DecoderTestContext(t.Context(), t),
		"findings.report.json",
		ReaderRequiringStorage(bytes.NewReader(output.Bytes())),
		limits,
	)
	if found, ok := AsError(err); !ok || found.Diagnostic.Code != CodeLimitNodes {
		t.Fatalf("one fewer node must refuse the actual report: %v", err)
	}
}
