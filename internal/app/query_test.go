// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type (
	// recordID is the identifier of a part in a query result.
	recordID struct {
		ID string `json:"id"`
	}

	// answer is the part of a saved-report query result the tests read.
	answer struct {
		View       string     `json:"view"`
		Kind       string     `json:"kind"`
		Part       recordID   `json:"part"`
		Records    []recordID `json:"records"`
		Total      int        `json:"total"`
		Returned   int        `json:"returned"`
		Offset     int        `json:"offset"`
		NextOffset int        `json:"next_offset"`
		Page       int        `json:"page"`
	}
)

// Words of queries and of the identifiers they answer with.
const (
	viewParts = "parts"
	argvBlank = "argv:4"
)

// queryAnswer runs a report query and decodes its answer.
func queryAnswer(tb testing.TB, dir string, args ...string) answer {
	tb.Helper()

	res := execute(tb.Context(), tb, appOf(newFake(tb)), dir, append([]string{commandReport}, args...)...)
	if res.code != 0 {
		tb.Fatalf(exitFailureFormat, res.code, res.stdout)
	}

	var parsed answer

	err := json.Unmarshal([]byte(res.stdout), &parsed)
	if err != nil {
		tb.Fatal(err)
	}

	return parsed
}

func TestReportQueriesHonorEverySelector(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)
	writePDF(t, dir, sourceB)

	execute(t.Context(), t, appOf(newFake(t)), dir, commandCheck, reportFlag, reportFile, sourceA, "--blank", sourceB).
		requireCode(t, 0, "")

	cases := map[string]struct {
		args []string
		want answer
	}{
		"a window of one part from offset 1": {
			[]string{"--view", viewParts, "--limit", "1", "--offset", "1"},
			answer{Kind: "view", View: viewParts, Total: 3, Returned: 1, Offset: 1, NextOffset: 2, Records: []recordID{{argvBlank}}},
		},
		// An offset of zero is a selector too: the limit alone then decides the window.
		"the first two parts": {
			[]string{"--view", viewParts, "--offset", "0", "--limit", "2"},
			answer{Kind: "view", View: viewParts, Total: 3, Returned: 2, NextOffset: 2, Records: []recordID{{"argv:3"}, {argvBlank}}},
		},
		"one part":            {[]string{"--part", "argv:5"}, answer{Kind: "part", Part: recordID{"argv:5"}}},
		"the page of a blank": {[]string{"--page", "2"}, answer{Kind: "page", Page: 2, Part: recordID{argvBlank}}},
	}

	for name, test := range cases {
		got := queryAnswer(t, dir, append([]string{reportFile}, test.args...)...)
		if fmt.Sprintf("%+v", got) != fmt.Sprintf("%+v", test.want) {
			t.Errorf("%s: %+v, want %+v", name, got, test.want)
		}
	}
}

func TestAReportThatCannotBeReadIsAReadFailure(t *testing.T) {
	t.Parallel()

	dir := workDir(t)

	err := os.Mkdir(filepath.Join(dir, reportDirectoryPath), 0o700)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct{ file, message string }{
		"missing":   {missingPlanPath, "cannot open the saved report: "},
		"directory": {reportDirectoryPath, "cannot read the saved report: "},
	}

	for name, test := range cases {
		res := execute(t.Context(), t, appOf(newFake(t)), dir, commandReport, test.file)
		parsed := res.requireCode(t, 1, "report_read_failed")

		found := parsed.Diagnostics[0]
		if !strings.HasPrefix(found.Message, test.message) || found.Path != filepath.Join(dir, test.file) {
			t.Errorf(namedFailureFormat, name, found)
		}
	}

	res := execute(t.Context(), t, appOf(newFake(t)), dir, commandReport, reportDirectoryPath)
	if message := res.summary(t).Diagnostics[0].Message; !strings.Contains(message, "a saved report must be a regular file") {
		t.Errorf("message %q", message)
	}
}
