// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func invoke(t *testing.T, args []string, stdin string) (core.Result, int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	exit := run(args, strings.NewReader(stdin), &stdout, &stderr)
	if stderr.Len() > 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	var r core.Result
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("stdout is not exactly one result: %v %q", err, stdout.String())
	}
	validateResult(t, stdout.Bytes())
	return r, exit, stdout.String()
}

func validateResult(t *testing.T, data []byte) {
	t.Helper()
	var document, value any
	if err := json.Unmarshal(resultSchemaBytes(t), &document); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("result.json", document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("result.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = schema.Validate(value); err != nil {
		t.Fatalf("result schema: %v\n%s", err, data)
	}
}

func TestBuildCheckJSONTextAndReport(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pdf")
	out := filepath.Join(dir, "output.pdf")
	report := filepath.Join(dir, "report.json")
	if err := pdffixture.Plain("source").WriteFile(source); err != nil {
		t.Fatal(err)
	}
	args := []string{"check", "-o", out, source, "--blank", "--report", report}
	r, exit, _ := invoke(t, args, "")
	if exit != 0 || r.Status != "ok" || r.Output.Published || r.Pages == nil || *r.Pages.Total != 2 || !r.Report.Written {
		t.Fatalf("check %+v exit %d", r, exit)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("check wrote a PDF: %v", err)
	}
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	validateResult(t, data)
	var full core.Result
	if err = json.Unmarshal(data, &full); err != nil {
		t.Fatal(err)
	}
	if len(full.Parts) != 2 || full.Parts[0].SHA256 == "" || full.Parts[1].OutputPages == nil || *full.Parts[1].OutputPages != [2]int64{2, 2} {
		t.Fatalf("full report %+v", full.Parts)
	}
	args[0] = "build"
	args = append(args, "--overwrite")
	r, exit, _ = invoke(t, args, "")
	if exit != 0 || !r.Output.Published || !r.Report.Written || r.Output.Bytes == nil {
		t.Fatalf("build %+v exit %d", r, exit)
	}
	pdfBytes, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(pdfBytes)
	if r.Output.SHA256 != hex.EncodeToString(digest[:]) || *r.Output.Bytes != int64(len(pdfBytes)) {
		t.Fatal("published digest/size mismatch")
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check", "--format=text", source}, strings.NewReader(""), &stdout, &stderr); code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "check ok") {
		t.Fatalf("text result %d %q %q", code, stdout.String(), stderr.String())
	}
}

func TestEveryUsageClassAndNoReportAfterArgvFailure(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "report.json")
	for _, args := range [][]string{{"unknown"}, {"build", "--unknown"}, {"build", "--plan"}, {"build", "--format=xml"}, {"build", "--plan=x", "a"}, {"build"}, {"build", "missing.pdf"}, {"schema", "wrong"}, {"version", "--unknown"}} {
		r, exit, _ := invoke(t, args, "")
		if exit != 2 || r.Status != "invalid" {
			t.Fatalf("usage %v: %+v %d", args, r, exit)
		}
	}
	invoke(t, []string{"build", "--report", report, "--unknown"}, "")
	if _, err := os.Stat(report); !os.IsNotExist(err) {
		t.Fatalf("argv failure wrote report: %v", err)
	}
}

func TestVersionAndSchemas(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}, {"schema", "plan"}, {"schema", "result"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, strings.NewReader(""), &stdout, &stderr); code != 0 || stderr.Len() > 0 {
			t.Fatalf("%v: %d %s", args, code, stderr.String())
		}
		var value any
		if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if args[0] != "schema" {
			validateResult(t, stdout.Bytes())
		}
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	name := "pdfconcat"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(t.TempDir(), name)
	command := exec.CommandContext(t.Context(), "go", "build", "-o", path, ".")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v %s", err, output)
	}
	return path
}
func TestVersionBinarySmoke(t *testing.T) {
	path := buildBinary(t)
	output, err := exec.CommandContext(t.Context(), path, "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	validateResult(t, output)
}
func TestBuildBinarySmoke(t *testing.T) {
	binary := buildBinary(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pdf")
	out := filepath.Join(dir, "output.pdf")
	if err := pdffixture.Plain("smoke").WriteFile(source); err != nil {
		t.Fatal(err)
	}
	output, err := exec.CommandContext(t.Context(), binary, "build", "-o", out, source).Output()
	if err != nil {
		t.Fatal(err)
	}
	validateResult(t, output)
	data, err := os.ReadFile(out)
	if err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("binary did not publish PDF %v", err)
	}
}

func hasDiagnostic(r core.Result, code string) bool {
	for _, d := range r.Diagnostics {
		if d.Code.String() == code {
			return true
		}
	}
	return false
}

func TestFailedAndRefusedReportsPreservePDFBytes(t *testing.T) {
	for _, unreadable := range []bool{false, true} {
		for _, existing := range []bool{false, true} {
			for _, overwrite := range []bool{false, true} {
				dir := t.TempDir()
				planPath := filepath.Join(dir, "plan.json")
				report := filepath.Join(dir, "report.json")
				if unreadable {
					report = filepath.Join(t.TempDir(), "report.json")
				}
				if !unreadable {
					if err := os.WriteFile(planPath, []byte(`{"version":4,"items":[`), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if existing {
					if err := os.WriteFile(report, []byte("keep existing"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				args := []string{"check", "--plan", planPath, "--report", report}
				if overwrite {
					args = append(args, "--overwrite")
				}
				r, exit, _ := invoke(t, args, "")
				expectedExit := 2
				if unreadable {
					expectedExit = 1
				}
				if exit != expectedExit || r.Report == nil || r.Report.Written != !existing {
					t.Fatalf("unreadable=%v existing=%v overwrite=%v: %+v exit=%d", unreadable, existing, overwrite, r, exit)
				}
				if existing {
					data, err := os.ReadFile(report)
					if err != nil || string(data) != "keep existing" {
						t.Fatalf("unsafe report replacement: %q %v", data, err)
					}
					if !hasDiagnostic(r, "report_exists") || hasDiagnostic(r, "report_write_failed") {
						t.Fatalf("refusal misclassified: %+v", r.Diagnostics)
					}
				} else {
					data, err := os.ReadFile(report)
					if err != nil {
						t.Fatal(err)
					}
					validateResult(t, data)
				}
			}
		}
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pdf")
	if err := pdffixture.Plain("source").WriteFile(source); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(dir, "safe-report.json")
	badOutput := filepath.Join(dir, "missing-parent", "output.pdf")
	r, exit, _ := invoke(t, []string{"build", "-o", badOutput, source, "--report", report}, "")
	if exit != 1 || r.Output.Published || !r.Report.Written || !hasDiagnostic(r, "output_write_failed") {
		t.Fatalf("failed build report: %+v %d", r, exit)
	}
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	validateResult(t, data)
	out := filepath.Join(dir, "output.pdf")
	badReport := filepath.Join(dir, "missing-parent", "report.json")
	r, exit, _ = invoke(t, []string{"build", "-o", out, source, "--report", badReport}, "")
	if exit != 1 || !r.Output.Published || r.Report.Written || !hasDiagnostic(r, "report_write_failed") {
		t.Fatalf("late report failure: %+v %d", r, exit)
	}
	pdfBytes, err := os.ReadFile(out)
	if err != nil || !bytes.HasPrefix(pdfBytes, []byte("%PDF-")) {
		t.Fatalf("late report failure lost PDF: %v", err)
	}
}

func TestReportAliasAfterPublicationCannotReplacePDF(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "realDir")
	alias := filepath.Join(dir, "alias")
	if err := os.Mkdir(realDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	source := filepath.Join(dir, "source.pdf")
	if err := pdffixture.Plain("source").WriteFile(source); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(realDir, "packet.pdf")
	report := filepath.Join(alias, "packet.pdf")
	r, exit, _ := invoke(t, []string{"build", "-o", out, source, "--report", report, "--overwrite"}, "")
	if exit != 2 || !r.Output.Published || r.Report.Written || !hasDiagnostic(r, "report_is_output") || hasDiagnostic(r, "report_write_failed") {
		t.Fatalf("late alias outcome: %+v %d", r, exit)
	}
	data, err := os.ReadFile(out)
	if err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("report overwrote PDF: %v", err)
	}
}

func TestTextPolicyPrecedesPDFReadsAndTinyPagesAreExplicit(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ text, code string }{{"漢", "text_unsupported_character"}, {"😀", "text_glyph_missing"}} {
		plan := `{"version":4,"items":[{"source":"missing.pdf"},{"page":{"size":"A4","text":{"value":"` + tc.text + `"}}}]}`
		r, exit, _ := invoke(t, []string{"check", "--plan", "-", "--base-dir", dir}, plan)
		if exit != 2 || !hasDiagnostic(r, tc.code) || hasDiagnostic(r, "source_unreadable") {
			t.Fatalf("font/text ordering: %+v %d", r, exit)
		}
	}
	for _, text := range []string{"", "A"} {
		out := filepath.Join(dir, "tiny-"+text+".pdf")
		plan := `{"version":4,"items":[{"page":{"size":"1x100pt","text":{"value":"` + text + `","overflow":"allow"}}}]}`
		r, exit, _ := invoke(t, []string{"build", "--plan", "-", "-o", out}, plan)
		if text == "" {
			if exit != 0 || !r.Output.Published {
				t.Fatalf("plain tiny page: %+v %d", r, exit)
			}
		} else {
			if exit != 2 || !hasDiagnostic(r, "text_overflow") {
				t.Fatalf("unusable derived width: %+v %d", r, exit)
			}
		}
	}
}

func TestNamedInputRolesAndFontEmbeddingRefusal(t *testing.T) {
	dir := t.TempDir()
	r, exit, _ := invoke(t, []string{"check", "--plan", dir}, "")
	if exit != 1 || !hasDiagnostic(r, "plan_unreadable") {
		t.Fatalf("directory plan: %+v %d", r, exit)
	}
	r, exit, _ = invoke(t, []string{"check", dir}, "")
	if exit != 1 || !hasDiagnostic(r, "source_unreadable") {
		t.Fatalf("directory source: %+v %d", r, exit)
	}
	fontPlan := func(path string) string {
		data, err := json.Marshal(map[string]any{"version": 4, "items": []any{map[string]any{"source": "missing.pdf"}, map[string]any{"page": map[string]any{"size": "A4", "text": map[string]any{"font": map[string]any{"file": path}, "value": "A"}}}}})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	r, exit, _ = invoke(t, []string{"check", "--plan", "-", "--base-dir", dir}, fontPlan(dir))
	if exit != 1 || !hasDiagnostic(r, "font_unreadable") || hasDiagnostic(r, "source_unreadable") {
		t.Fatalf("directory font stage: %+v %d", r, exit)
	}
	data, err := os.ReadFile("../../internal/core/internal/typeset/fontdata/NotoSans-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	count := int(binary.BigEndian.Uint16(data[4:]))
	found := false
	for i := range count {
		record := data[12+16*i:]
		if string(record[:4]) == "OS/2" {
			offset := int(binary.BigEndian.Uint32(record[8:]))
			binary.BigEndian.PutUint16(data[offset+8:], 0x0200)
			found = true
			break
		}
	}
	if !found {
		t.Fatal("font asset lacks OS/2")
	}
	fontPath := filepath.Join(dir, "bitmap-only.ttf")
	if err = os.WriteFile(fontPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	r, exit, _ = invoke(t, []string{"check", "--plan", "-", "--base-dir", dir}, fontPlan(fontPath))
	if exit != 2 || !hasDiagnostic(r, "font_not_embeddable") || hasDiagnostic(r, "source_unreadable") {
		t.Fatalf("bitmap-only font: %+v %d", r, exit)
	}
	source := filepath.Join(dir, "source.pdf")
	link := filepath.Join(dir, "source-link.pdf")
	if err = pdffixture.Plain("symlink").WriteFile(source); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(source, link); err != nil {
		t.Logf("input symlink unavailable: %v", err)
	} else {
		r, exit, _ = invoke(t, []string{"check", link}, "")
		if exit != 0 || r.Pages == nil || *r.Pages.Total != 1 {
			t.Fatalf("regular source symlink: %+v %d", r, exit)
		}
	}
}

func TestRefusedReadableSourceRetainsInspectedByteFacts(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "invalid.pdf")
	report := filepath.Join(dir, "report.json")
	data := []byte("%PDF-1.7\nnot a complete PDF")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	r, exit, _ := invoke(t, []string{"check", source, "--report", report}, "")
	if exit != 2 || !r.Report.Written {
		t.Fatalf("refused source outcome %+v %d", r, exit)
	}
	saved, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	validateResult(t, saved)
	var full core.Result
	if err = json.Unmarshal(saved, &full); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if len(full.Parts) != 1 || full.Parts[0].SHA256 != hex.EncodeToString(digest[:]) || full.Parts[0].Bytes == nil || *full.Parts[0].Bytes != int64(len(data)) || full.Parts[0].Pages != nil {
		t.Fatalf("known byte facts were lost or pages guessed: %+v", full.Parts)
	}
}

func TestEmptyTextStillValidatesSelectedFont(t *testing.T) {
	dir := t.TempDir()
	plan := `{"version":4,"page_defaults":{"size":"A4","text":{"font":{"file":"missing.ttf"},"value":""}},"items":[{"page":{}}]}`
	r, exit, _ := invoke(t, []string{"check", "--plan", "-", "--base-dir", dir}, plan)
	if exit != 1 || !hasDiagnostic(r, "font_unreadable") {
		t.Fatalf("empty text bypassed selected-font validation: %+v %d", r, exit)
	}
}

func TestTextPlanLocationsAndInputContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Rīga-Ελλάδα-Київ.json")
	input := "{\n  \"version\":4,\n  \"items\":[null]\n}\n"
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := run([]string{"check", "--format", "text", "--plan", path}, strings.NewReader(""), &stdout, &stderr)
	if exit != 2 || stderr.Len() != 0 || !strings.Contains(stdout.String(), strconv.Quote(path)+":3:12") || !strings.Contains(stdout.String(), `[pointer "/items/0"]`) {
		t.Fatalf("located plan error: %d %q %q", exit, stdout.String(), stderr.String())
	}
	for _, tc := range []struct{ input, code, file, item string }{
		{`{"version":4,"items":[{"source":"missing.pdf"}]}`, "source_unreadable", filepath.Join(dir, "missing.pdf"), "/items/0"},
		{`{"version":4,"items":[{"page":{"size":"A4","text":{"value":"A","font":{"file":"missing.ttf"}}}}]}`, "font_unreadable", filepath.Join(dir, "missing.ttf"), "/items/0"},
		{`{"version":4,"items":[{"page":{"size":"10x10pt","text":{"value":"A"}}}]}`, "text_overflow", "", "/items/0"},
	} {
		stdout.Reset()
		stderr.Reset()
		exit = run([]string{"check", "--format", "text", "--plan", "-", "--base-dir", dir}, strings.NewReader(tc.input), &stdout, &stderr)
		if exit == 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), tc.code) || !strings.Contains(stdout.String(), tc.item) || tc.file != "" && !strings.Contains(stdout.String(), strconv.Quote(tc.file)) {
			t.Fatalf("%s context: %d %q %q", tc.code, exit, stdout.String(), stderr.String())
		}
	}
}

func TestTextLegalControlCharacterSourcePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows filenames forbid the tested control characters")
	}
	source := filepath.Join(t.TempDir(), "Rīga-Ελλάδα-Київ\n\r\t\x1b.pdf")
	if err := pdffixture.Plain("safe path").WriteFile(source); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if exit := run([]string{"check", "--format", "text", source}, strings.NewReader(""), &stdout, &stderr); exit != 0 || stderr.Len() != 0 {
		t.Fatalf("legal filename rejected: %d %q %q", exit, stdout.String(), stderr.String())
	}
	// A refusal must identify the same legal path without printing its controls.
	if err := os.WriteFile(source, []byte("not a PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	exit := run([]string{"check", "--format", "text", source}, strings.NewReader(""), &stdout, &stderr)
	if exit != 2 || !strings.Contains(stdout.String(), strconv.Quote(source)) || strings.ContainsAny(stdout.String(), "\r\t\x1b") || strings.Count(stdout.String(), "\n") != 3 {
		t.Fatalf("unsafe filename diagnostic: %d %q %q", exit, stdout.String(), stderr.String())
	}
	r, code, _ := invoke(t, []string{"check", source}, "")
	if code != 2 || len(r.Diagnostics) != 1 || r.Diagnostics[0].File != source {
		t.Fatalf("JSON path changed: %d %+v", code, r)
	}
}

func resultSchemaBytes(t *testing.T) []byte {
	t.Helper()
	data, err := core.Schema(core.ResultSchema)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
