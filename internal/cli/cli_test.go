// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package cli_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/diag"
)

func TestInterleavedOperandsAndDelimiter(t *testing.T) {
	c, failure := cli.Parse([]string{"build", "a.pdf", "--blank", "-o", "out.pdf", "b.pdf", "--report=report.json", "--", "--blank", "-h"})
	if failure != nil || c.Output != "out.pdf" || c.Report != "report.json" || len(c.Operands) != 5 {
		t.Fatalf("command %+v diagnostics %+v", c, failure)
	}
	for i, want := range []struct {
		path  string
		blank bool
		index int
	}{{"a.pdf", false, 1}, {"", true, 2}, {"b.pdf", false, 5}, {"--blank", false, 8}, {"-h", false, 9}} {
		got := c.Operands[i]
		if got.Path != want.path || got.Blank != want.blank || got.Index != want.index {
			t.Fatalf("operand %d: %+v", i, got)
		}
	}
	c, failure = cli.Parse([]string{"check", "--plan=-", "--base-dir=here", "--fit-to", "297x210mm", "--overwrite", "--format=text"})
	if failure != nil || c.Plan != "-" || c.BaseDir != "here" || c.Format != "text" || !c.Overwrite {
		t.Fatalf("options %+v %+v", c, failure)
	}
}

func TestUsageDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code diag.Code
	}{
		{[]string{"other"}, "usage_unknown_command"}, {[]string{"report"}, "usage_unknown_command"},
		{[]string{"build", "--jobs", "2"}, "usage_unknown_option"}, {[]string{"build", "--progress"}, "usage_unknown_option"},
		{[]string{"build", "--plan"}, "usage_missing_value"}, {[]string{"build", "--format=xml"}, "usage_invalid_value"},
		{[]string{"build", "--fit-to=inherit", "a"}, "usage_invalid_value"}, {[]string{"build", "--overwrite=true", "a"}, "usage_invalid_value"},
		{[]string{"build", "--plan=x", "a"}, "usage_conflict"}, {[]string{"build", "--plan=x", "--blank"}, "usage_conflict"},
		{[]string{"check", "--base-dir=x", "a"}, "usage_conflict"}, {[]string{"build", "-o", "a", "--output=b", "c"}, "usage_conflict"},
		{[]string{"build"}, "usage_missing_input"}, {[]string{"schema"}, "usage_invalid_value"}, {[]string{"schema", "other"}, "usage_invalid_value"},
		{[]string{"version", "--report=x"}, "usage_unknown_option"}, {[]string{"version", "x"}, "usage_conflict"},
		{[]string{"help", "unknown"}, "usage_unknown_command"}, {[]string{"build", string([]byte{0xff})}, "usage_invalid_value"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			_, failure := cli.Parse(tc.args)
			if failure == nil || len(failure.Diagnostics) != 1 || failure.Diagnostics[0].Code != tc.code {
				t.Fatalf("want %s got %+v", tc.code, failure)
			}
		})
	}
}

func TestHelpAndVersionForms(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"build", "--help"}, {"check", "-h"}, {"help", "version"}, {"schema", "--help"}} {
		c, failure := cli.Parse(args)
		if failure != nil || !c.Help || cli.Help(c.Name) == "" {
			t.Fatalf("help %v: %+v %+v", args, c, failure)
		}
	}
	for _, args := range [][]string{{"version"}, {"--version"}, {"version", "--format=text"}} {
		c, failure := cli.Parse(args)
		if failure != nil || c.Name != "version" {
			t.Fatalf("version %v: %+v %+v", args, c, failure)
		}
	}
	help := cli.Help("build")
	for _, option := range []string{"--plan", "--output", "--base-dir", "--fit-to", "--report", "--overwrite", "--format", "--help"} {
		if !strings.Contains(help, option) {
			t.Fatalf("help omits %s", option)
		}
	}
	for _, removed := range []string{"--jobs", "--progress", "--plan-json"} {
		if strings.Contains(help, removed) {
			t.Fatalf("help advertises removed option %s", removed)
		}
	}
}

func TestUsageErrorFormatAndFirstFailure(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		format string
		code   diag.Code
		item   string
	}{
		{[]string{"check", "--bad", "--format", "text"}, "text", "usage_unknown_option", "argv:1"},
		{[]string{"check", "--format", "text", "--bad"}, "text", "usage_unknown_option", "argv:3"},
		{[]string{"check", "--report=", "--format=text"}, "text", "usage_invalid_value", "argv:1"},
		{[]string{"check", "--format=text", "--report="}, "text", "usage_invalid_value", "argv:2"},
		{[]string{"check", "--report", "", "--format", "text"}, "text", "usage_invalid_value", "argv:2"},
		{[]string{"check", "--report", "--format", "text"}, "text", "usage_missing_value", "argv:1"},
		{[]string{"check", "--format", "text", "--report"}, "text", "usage_missing_value", "argv:3"},
		{[]string{"check", "--overwrite=yes", "--format=text"}, "text", "usage_invalid_value", "argv:1"},
		{[]string{"check", "--bad=--format", "text"}, "json", "usage_unknown_option", "argv:1"},
		{[]string{"check", "--bad", "--report=--format", "text"}, "json", "usage_unknown_option", "argv:1"},
		{[]string{"check", "--bad", "--report", "--", "--format=text"}, "text", "usage_unknown_option", "argv:1"},
		{[]string{"check", "--bad", "--", "--format=text"}, "json", "usage_unknown_option", "argv:1"},
		{[]string{"check", "--bad", "--format", "xml"}, "json", "usage_unknown_option", "argv:1"},
		{[]string{"check", "--bad", "--format="}, "json", "usage_unknown_option", "argv:1"},
		{[]string{"check", "--bad", "--format"}, "json", "usage_unknown_option", "argv:1"},
		{[]string{"check", "--bad", "--format=text", "--format=text"}, "json", "usage_unknown_option", "argv:1"},
		{[]string{"check", "--format=text", "--format=json"}, "json", "usage_conflict", "argv:2"},
		{[]string{"check", "--format=json", "--format=text"}, "json", "usage_conflict", "argv:2"},
		{[]string{"check", "--format=text", "--format"}, "json", "usage_conflict", "argv:2"},
		{[]string{"check", "--format", "--report", "x", "--format=text"}, "json", "usage_missing_value", "argv:1"},
		{[]string{"check", "--report=x", "--report", "--", "--format=text"}, "text", "usage_conflict", "argv:2"},
		{[]string{"version", "--report", "--", "--format=text"}, "json", "usage_unknown_option", "argv:1"},
		{[]string{"version", "--bad", "--format=text"}, "text", "usage_unknown_option", "argv:1"},
		{[]string{"--version", "--bad", "--format=text"}, "text", "usage_unknown_option", "argv:1"},
		{[]string{"schema", "--bad", "--format=text"}, "json", "usage_invalid_value", "argv:1"},
		{[]string{"unknown", "--format=text"}, "json", "usage_unknown_command", "argv:0"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			c, failure := cli.Parse(tc.args)
			if c.Format != tc.format || failure == nil || len(failure.Diagnostics) != 1 || failure.Diagnostics[0].Code != tc.code || len(failure.Diagnostics[0].Items) != 1 || failure.Diagnostics[0].Items[0] != tc.item {
				t.Fatalf("want %s/%s/%s, got %+v %+v", tc.format, tc.code, tc.item, c, failure)
			}
		})
	}
}
