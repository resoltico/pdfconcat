// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const stringsBuilderFixture = "src/strings/builder.go"

func TestInfallibleMethodPremisesRejectChangedContracts(t *testing.T) {
	t.Parallel()

	entries := []*repopolicy.Entry{{
		ID: "lint-buffer-contract", Tool: repopolicy.ToolLint,
		Setting: "linters.settings.errcheck.exclude-functions", Values: []string{"(*strings.Builder).WriteString"},
	}}
	source := "package strings\ntype Builder struct {}\nfunc (b *Builder) WriteString(s string) (int,error) { return len(s), nil }\n"

	read := sourceFrom(map[string]string{stringsBuilderFixture: source})
	if problems := repopolicy.InfallibleMethodPremiseIssues(entries, read); len(problems) != 0 {
		t.Fatal(problems)
	}

	changed := strings.Replace(source, "nil", "fail()", 1)
	problems := repopolicy.InfallibleMethodPremiseIssues(entries, sourceFrom(map[string]string{stringsBuilderFixture: changed}))
	requireContains(t, problems, "no longer has a verified nil-error implementation")

	entries[0].Values = []string{"(io.Writer).Write"}
	requireContains(t, repopolicy.InfallibleMethodPremiseIssues(entries, read), "no independently reviewed infallible method")
	entries[0].Values = []string{"(*strings.Builder).WriteString"}
	requireContains(t, repopolicy.InfallibleMethodPremiseIssues(entries, sourceFrom(nil)), "read Go SDK contract")
	requireContains(
		t,
		repopolicy.InfallibleMethodPremiseIssues(entries, sourceFrom(map[string]string{stringsBuilderFixture: "package broken{"})),
		"parse Go SDK contract",
	)
}
