// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
package repopolicy_test

import (
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	registryFile = ".quality-exceptions.yml"
	golangciFile = ".golangci.yml"
)

func TestParseLinterInfoRejectsEmptyAndMalformed(t *testing.T) {
	t.Parallel()

	for _, input := range []string{`{}`, `not json`} {
		_, err := repopolicy.ParseLinterInfo([]byte(input))
		if err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestParseIssuesRejectsGarbage(t *testing.T) {
	t.Parallel()

	_, err := repopolicy.ParseIssues([]byte("not json"), "/work/repo")
	if err == nil {
		t.Fatal("garbage accepted as a lint report")
	}
}
