// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// agentSession is the state an agent carries through its check-and-fix loop: the directory and the plan it keeps editing.
type agentSession struct {
	plan  obj
	dir   string
	items []any
}

// TestAgentWorkflowFromGeneratedPlanToQueriedPublication follows the loop an agent runs: generate a plan,
// check and save the report, fetch exactly the failing locations, edit the plan, check again, build, and
// read the final publication state back from the saved report. Every step prints one bounded JSON line.
func TestAgentWorkflowFromGeneratedPlanToQueriedPublication(t *testing.T) {
	t.Parallel()

	session := newAgentSession(t)

	session.fixMissingSources(t)
	session.fixOverflow(t)
	session.buildAndQuery(t)
}

// newAgentSession writes the sources and a plan with two mistakes: sources that are not there, and text that cannot fit.
func newAgentSession(tb testing.TB) *agentSession {
	tb.Helper()

	dir := tempDir(tb)
	writePDFs(tb, dir, 2, "doc0", "doc1", "doc2", "doc3", "doc4", "doc5", "doc6", "doc7")

	items := make([]any, 0, 10)

	for index := range 8 {
		items = append(items, fmt.Sprintf("doc%d.pdf", index))
	}

	items[2], items[5] = "docs/missing-a.pdf", "docs/missing-b.pdf"
	items = append(items,
		obj{keyBlank: obj{keyText: obj{keyValue: strings.Repeat("W", 60), keyWidth: 50}}},
		obj{keyBlank: obj{keyText: obj{keyValue: shortText}}, "count": 3},
	)

	session := &agentSession{dir: dir, items: items, plan: obj{keyVersion: 1, keyOutput: "bundle.pdf", keyItems: items}}
	session.save(tb)

	return session
}

// save writes the plan as the agent currently has it.
func (s *agentSession) save(tb testing.TB) {
	tb.Helper()

	writeFile(tb, s.dir, planPath, planJSON(tb, s.plan))
}

// fixMissingSources checks and saves the report, fetches the failing locations, and edits the plan at exactly those.
func (s *agentSession) fixMissingSources(t *testing.T) {
	t.Helper()

	// The summary is bounded and says where the rest is.
	first := run(t, s.dir, "", commandCheck, flagPlan, planPath, flagReport, planReportPath)
	requireExit(t, first, 1)

	summary := summaryOf(t, first)
	if summary.DiagnosticCount != 2 || summary.Publication.ReportStatus != reportWritten || len(summary.Next) == 0 {
		t.Fatalf("first summary: %+v", summary)
	}

	if len(first.stdout) > 4096 {
		t.Errorf("the failure summary is %d bytes", len(first.stdout))
	}

	// Fetch the failing locations, nothing else.
	failing := pointersOf(t, run(t, s.dir, "", summary.Next[1:]...))
	if strings.Join(failing, ",") != "/items/2,/items/5" {
		t.Fatalf("failing locations: %v", failing)
	}

	// Edit the plan at exactly those locations.
	for _, pointer := range failing {
		index, err := strconv.Atoi(strings.TrimPrefix(pointer, "/items/"))
		ensure(t, err)

		s.items[index] = fmt.Sprintf("doc%d.pdf", index)
	}

	s.save(t)
}

// fixOverflow checks again, which now fails in layout with its own located diagnostic, and repairs the text.
func (s *agentSession) fixOverflow(t *testing.T) {
	t.Helper()

	second := run(t, s.dir, "", commandCheck, flagPlan, planPath, flagReport, planReportPath, flagOverwrite)
	requireExit(t, second, 2)

	summary := summaryOf(t, second)
	if summary.Phases["input_inspection"] != "complete" || summary.Phases["layout"] != phaseIncomplete {
		t.Fatalf("second summary: %+v", summary)
	}

	failing := pointersOf(t, run(t, s.dir, "", summary.Next[1:]...))
	if len(failing) != 1 || failing[0] != "/items/8/blank/text/width" {
		t.Fatalf("overflow location: %v", failing)
	}

	// The failing part is one call away, with its text and settings.
	part := generic(t, run(t, s.dir, "", commandReport, planReportPath, partFlag, "/items/8", flagDetails).stdout)
	if textAt(t, part, partField, "id") != "/items/8" {
		t.Errorf("part: %v", part)
	}

	s.items[8] = obj{keyBlank: obj{keyText: obj{keyValue: strings.Repeat("W", 60), keyWidth: 500, keySize: 8}}}
	s.save(t)
}

// buildAndQuery checks and builds, both succeeding, then answers questions from the saved build report.
func (s *agentSession) buildAndQuery(t *testing.T) {
	t.Helper()

	requireExit(t, run(t, s.dir, "", commandCheck, flagPlan, planPath), 0)

	built := run(t, s.dir, "", commandBuild, flagPlan, planPath, flagReport, buildReportPath)
	requireExit(t, built, 0)

	summary := summaryOf(t, built)
	bundle := filepath.Join(s.dir, "bundle.pdf")

	if !summary.Publication.Published || *summary.Counts.Total != 8*2+1+3 || summary.Publication.Output != bundle {
		t.Fatalf("build summary: %+v", summary)
	}

	// Query the final publication state without any PDF.
	final := generic(t, run(t, s.dir, "", commandReport, buildReportPath).stdout)
	published := flagAt(t, final, "publication", "published")

	if final["status"] != "ok" || !published || textAt(t, final, "publication", keyOutput) != bundle {
		t.Errorf("final state: %v", final)
	}

	page := generic(t, run(t, s.dir, "", commandReport, buildReportPath, flagPage, "19").stdout)
	if textAt(t, page, partField, "id") != "/items/9" || page["page_in_part"] != float64(2) {
		t.Errorf("page 19: %v", page)
	}

	verifyPages(t, bundle,
		"doc0 p1", "doc0 p2", "doc1 p1", "doc1 p2", "doc2 p1", "doc2 p2", "doc3 p1", "doc3 p2",
		"doc4 p1", "doc4 p2", "doc5 p1", "doc5 p2", "doc6 p1", "doc6 p2", "doc7 p1", "doc7 p2",
		strings.Repeat("W", 60), shortText, shortText, shortText)
}

// pointersOf runs a diagnostics query and returns the JSON pointers of the diagnostics, in order.
func pointersOf(tb testing.TB, res result) []string {
	tb.Helper()

	requireExit(tb, res, 0)

	var view struct {
		Records []diagnostic `json:"records"`
	}

	ensure(tb, json.Unmarshal([]byte(res.stdout), &view))

	pointers := make([]string, len(view.Records))
	for index, record := range view.Records {
		pointers[index] = record.Location.Pointer
	}

	return pointers
}

// TestOutputStaysBoundedOnLargeJobs checks the agent-facing economy on a 10,000-entry job: the success
// summary is under 2 KiB, a failure summary does not grow with the number of problems, and paged
// queries stay within their byte bound.
func TestOutputStaysBoundedOnLargeJobs(t *testing.T) {
	t.Parallel()

	const sources = 5000

	dir := tempDir(t)

	for index := range sources {
		writePDFsAt(t, filepath.Join(dir, "src", fmt.Sprintf("%05d.pdf", index)), fmt.Sprintf("s%d", index))
	}

	good := make([]any, 0, 2*sources)
	bad := make([]any, 0, 2*sources)

	for index := range sources {
		blank := obj{keyBlank: obj{keyText: obj{keyValue: fmt.Sprintf("Section %d", index)}}}
		good = append(good, fmt.Sprintf("src/%05d.pdf", index), blank)
		bad = append(bad, fmt.Sprintf("absent/%05d.pdf", index), blank)
	}

	writeFile(t, dir, "good.json", planJSON(t, itemsOf(good...)))
	writeFile(t, dir, fileBadPlan, planJSON(t, itemsOf(bad...)))

	checked := run(t, dir, "", commandCheck, flagPlan, "good.json", flagReport, "good.report.json")
	requireExit(t, checked, 0)

	if len(checked.stdout) >= 2048 {
		t.Errorf("the success summary of a 10,000-entry job is %d bytes, want under 2048: %s", len(checked.stdout), checked.stdout)
	}

	t.Logf("success summary of a 10,000-entry job: %d bytes", len(checked.stdout))

	if parsed := summaryOf(t, checked); parsed.PartCount != 2*sources || *parsed.Counts.Total != 2*sources {
		t.Errorf(summaryFailureFormat, parsed)
	}

	failed := run(t, dir, "", commandCheck, flagPlan, fileBadPlan, flagReport, "bad.report.json", jobsFlag, "4")
	requireExit(t, failed, 1)

	parsed := summaryOf(t, failed)
	if parsed.DiagnosticCount != sources || len(parsed.Diagnostics) > 5 || len(failed.stdout) > 2048 {
		t.Errorf("failure summary: %d diagnostics, %d shown, %d bytes", parsed.DiagnosticCount, len(parsed.Diagnostics), len(failed.stdout))
	}

	if seen := pagedDiagnostics(t, dir, "bad.report.json"); seen != sources {
		t.Errorf("paging saw %d of %d diagnostics", seen, sources)
	}

	last := generic(t, run(t, dir, "", commandReport, "good.report.json", flagPage, strconv.Itoa(2*sources)).stdout)
	if textAt(t, last, partField, "id") != fmt.Sprintf(itemIDFormat, 2*sources-1) {
		t.Errorf("last page: %v", last)
	}
}

// pagedDiagnostics reads every diagnostic of a saved report in pages of 100, checks each page against the byte
// bound, and returns how many it saw.
func pagedDiagnostics(tb testing.TB, dir, reportFile string) int {
	tb.Helper()

	offset, seen := 0, 0

	for offset >= 0 {
		page := run(tb, dir, "", commandReport, reportFile, flagView, diagnosticsView, offsetFlag, strconv.Itoa(offset), limitFlag, "100")
		requireExit(tb, page, 0)

		if len(page.stdout) > 128<<10 {
			tb.Fatalf("a page of diagnostics is %d bytes", len(page.stdout))
		}

		var view struct {
			NextOffset *int `json:"next_offset"`
			Returned   int  `json:"returned"`
		}

		decodeLine(tb, page.stdout, &view)

		seen += view.Returned
		offset = -1

		if view.NextOffset != nil {
			offset = *view.NextOffset
		}
	}

	return seen
}
