// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/report"
)

func TestInvalidReportRequestsNeverOpenArtifacts(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		code    report.Code
		command cli.Command
	}{
		{"zero limit", report.CodeInvalidPaging, cli.Command{View: report.ViewParts, HasLimit: true}},
		{"excess limit", report.CodeInvalidPaging, cli.Command{View: report.ViewParts, HasLimit: true, Limit: 101}},
		{"negative offset", report.CodeInvalidPaging, cli.Command{View: report.ViewParts, HasOffset: true, Offset: -1}},
		{"explicit page zero", report.CodeInvalidNumber, cli.Command{HasPage: true}},
		{"negative page", report.CodeInvalidNumber, cli.Command{HasPage: true, Page: -1}},
		{"unknown view", report.CodeUnknownView, cli.Command{View: "unknown"}},
		{"conflicting selectors", report.CodeSelectionConflict, cli.Command{Part: "argv:1", HasPage: true, Page: 1}},
		{"details without selection", report.CodeDetailsNeedSelect, cli.Command{Details: true}},
		{"paging without view", report.CodePagingNeedsView, cli.Command{HasOffset: true}},
		{"malformed expectation", report.CodeInvalidExpectation, cli.Command{ExpectAttempt: "bad"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			command := test.command

			command.Name, command.ReportFile = cli.NameReport, "query-artifact.json"
			opens := 0

			var output bytes.Buffer

			code := runQueryWith(t.Context(), &command, Env{Stdout: &output}, func(context.Context, string) (*os.File, error) {
				opens++
				return nil, os.ErrNotExist
			})

			var result report.CommandError
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatal(err)
			}

			if code != 2 || opens != 0 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != test.code {
				t.Fatalf("code=%d opens=%d result=%+v", code, opens, result)
			}
		})
	}
}

func TestValidReportRequestStillOpensArtifact(t *testing.T) {
	t.Parallel()

	command := cli.Command{Name: cli.NameReport, ReportFile: "query-artifact.json", View: report.ViewParts, HasLimit: true, Limit: 1}
	opens := 0

	var output bytes.Buffer

	code := runQueryWith(t.Context(), &command, Env{Stdout: &output}, func(context.Context, string) (*os.File, error) {
		opens++
		return nil, os.ErrNotExist
	})
	if code != 1 || opens != 1 {
		t.Fatalf("code=%d opens=%d output=%s", code, opens, output.String())
	}
}
