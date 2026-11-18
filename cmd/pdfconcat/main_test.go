// SPDX-License-Identifier: AGPL-3.0-only
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

	exchange "github.com/resoltico/pdfconcat/internal/wire"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func invokeJSON(t *testing.T, args []string, stdin string) (exchange.Result, int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	exit := run(jsonArguments(args), strings.NewReader(stdin), &stdout, &stderr)
	if stderr.Len() > 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	var r exchange.Result
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
	r, exit, _ := invokeJSON(t, args, "")
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
	var full exchange.Result
	if err = json.Unmarshal(data, &full); err != nil {
		t.Fatal(err)
	}
	if len(full.Parts) != 2 || full.Parts[0].SHA256 == "" || full.Parts[1].OutputPages == nil || *full.Parts[1].OutputPages != [2]int64{2, 2} {
		t.Fatalf("full report %+v", full.Parts)
	}
	args[0] = "build"
	args = append(args, "--overwrite")
	r, exit, _ = invokeJSON(t, args, "")
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
	if code := run([]string{"check", source}, strings.NewReader(""), &stdout, &stderr); code != 0 || !strings.Contains(stderr.String(), "source_structure_partial") || !strings.Contains(stdout.String(), "check ok") {
		t.Fatalf("text result %d %q %q", code, stdout.String(), stderr.String())
	}
}

func TestEveryUsageClassAndNoReportAfterArgvFailure(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "report.json")
	for _, args := range [][]string{{"build", "--unknown"}, {"build", "--plan"}, {"build", "--json=true"}, {"build", "--plan=x", "a"}, {"build"}, {"build", "missing.pdf"}, {"version", "--unknown"}} {
		r, exit, _ := invokeJSON(t, args, "")
		if exit != 2 || r.Status != "invalid" {
			t.Fatalf("usage %v: %+v %d", args, r, exit)
		}
	}
	invokeJSON(t, []string{"build", "--report", report, "--unknown"}, "")
	if _, err := os.Stat(report); !os.IsNotExist(err) {
		t.Fatalf("argv failure wrote report: %v", err)
	}
}

func TestVersionAndSchemas(t *testing.T) {
	for _, args := range [][]string{{"version", "--json"}, {"--version", "--json"}, {"schema", "plan"}, {"schema", "result"}} {
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
	output, err := exec.CommandContext(t.Context(), path, "version", "--json").Output()
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
	output, err := exec.CommandContext(t.Context(), binary, "build", "--json", "-o", out, source).Output()
	if err != nil {
		t.Fatal(err)
	}
	validateResult(t, output)
	data, err := os.ReadFile(out)
	if err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("binary did not publish PDF %v", err)
	}
}

func hasDiagnostic(r exchange.Result, code string) bool {
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
					if err := os.WriteFile(planPath, []byte(`{"version":8,"items":[`), 0600); err != nil {
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
				r, exit, _ := invokeJSON(t, args, "")
				expectedExit := 2

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
					if overwrite && !unreadable {
						var stdout, stderr bytes.Buffer
						code := run(args, strings.NewReader(""), &stdout, &stderr)
						if code != 2 || !strings.Contains(stderr.String(), "Choose a new report path") ||
							strings.Contains(stderr.String(), "--overwrite") {
							t.Fatal(stderr.String())
						}
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
	for _, command := range []string{"check", "build"} {
		r, exit, _ := invokeJSON(t, []string{command, "-o", badOutput, source, "--report", report, "--overwrite"}, "")
		if exit != 2 || r.Output.Published || !r.Report.Written || !hasDiagnostic(r, "output_parent_invalid") {
			t.Fatalf("invalid output parent: %+v %d", r, exit)
		}
		data, err := os.ReadFile(report)
		if err != nil {
			t.Fatal(err)
		}
		validateResult(t, data)
	}
	out := filepath.Join(dir, "output.pdf")
	badReport := filepath.Join(dir, "missing-parent", "report.json")
	r, exit, _ := invokeJSON(t, []string{"build", "-o", out, source, "--report", badReport}, "")
	if exit != 2 || r.Output.Published || r.Report.Written || !hasDiagnostic(r, "report_parent_invalid") {
		t.Fatalf("invalid report parent: %+v %d", r, exit)
	}
}

func TestInitiallyAbsentReportAliasRefusedBeforePublication(t *testing.T) {
	dir := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	source := filepath.Join(dir, "source.pdf")
	if err := pdffixture.Plain("source").WriteFile(source); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "packet.pdf")
	report := filepath.Join(alias, "packet.pdf")
	for _, command := range []string{"check", "build"} {
		r, exit, _ := invokeJSON(t, []string{command, "-o", out, source, "--report", report, "--overwrite"}, "")
		if exit != 2 || r.Output.Published || r.Report.Written || len(r.Diagnostics) != 1 || !hasDiagnostic(r, "report_is_output") {
			t.Fatalf("%s collision: %+v %d", command, r, exit)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Fatalf("%s created the protected name: %v", command, err)
		}
	}
}

func TestTextPolicyPrecedesPDFReadsAndTinyPagesAreExplicit(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ text, code string }{{"漢", "text_unsupported_character"}, {"😀", "text_glyph_missing"}} {
		plan := `{"version":8,"items":[{"source":"missing.pdf"},{"page":{"size":"A4","text":{"value":"` + tc.text + `"}}}]}`
		r, exit, _ := invokeJSON(t, []string{"check", "--plan", "-", "--base-dir", dir}, plan)
		if exit != 2 || !hasDiagnostic(r, tc.code) || hasDiagnostic(r, "source_input_invalid") {
			t.Fatalf("font/text ordering: %+v %d", r, exit)
		}
	}
	for _, text := range []string{"", "A"} {
		out := filepath.Join(dir, "tiny-"+text+".pdf")
		plan := `{"version":8,"items":[{"page":{"size":"1x100pt","text":{"value":"` + text + `","overflow":"allow"}}}]}`
		r, exit, _ := invokeJSON(t, []string{"build", "--plan", "-", "-o", out}, plan)
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
	r, exit, _ := invokeJSON(t, []string{"check", "--plan", dir}, "")
	if exit != 2 || !hasDiagnostic(r, "plan_input_invalid") {
		t.Fatalf("directory plan: %+v %d", r, exit)
	}
	r, exit, _ = invokeJSON(t, []string{"check", dir}, "")
	if exit != 2 || !hasDiagnostic(r, "source_input_invalid") {
		t.Fatalf("directory source: %+v %d", r, exit)
	}
	fontPlan := func(path string) string {
		data, err := json.Marshal(map[string]any{"version": 8, "items": []any{map[string]any{"source": "missing.pdf"}, map[string]any{"page": map[string]any{"size": "A4", "text": map[string]any{"font": map[string]any{"file": path}, "value": "A"}}}}})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	r, exit, _ = invokeJSON(t, []string{"check", "--plan", "-", "--base-dir", dir}, fontPlan(dir))
	if exit != 2 || !hasDiagnostic(r, "font_input_invalid") || hasDiagnostic(r, "source_input_invalid") {
		t.Fatalf("directory font stage: %+v %d", r, exit)
	}
	data, err := os.ReadFile("../../internal/typeset/fontdata/NotoSans-Regular.ttf")
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
	r, exit, _ = invokeJSON(t, []string{"check", "--plan", "-", "--base-dir", dir}, fontPlan(fontPath))
	if exit != 2 || !hasDiagnostic(r, "font_not_embeddable") || hasDiagnostic(r, "source_input_invalid") {
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
		r, exit, _ = invokeJSON(t, []string{"check", link}, "")
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
	r, exit, _ := invokeJSON(t, []string{"check", source, "--report", report}, "")
	if exit != 2 || !r.Report.Written {
		t.Fatalf("refused source outcome %+v %d", r, exit)
	}
	saved, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	validateResult(t, saved)
	var full exchange.Result
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
	plan := `{"version":8,"page_defaults":{"size":"A4","text":{"font":{"file":"missing.ttf"},"value":""}},"items":[{"page":{}}]}`
	r, exit, _ := invokeJSON(t, []string{"check", "--plan", "-", "--base-dir", dir}, plan)
	if exit != 2 || !hasDiagnostic(r, "font_input_invalid") {
		t.Fatalf("empty text bypassed selected-font validation: %+v %d", r, exit)
	}
}

func TestTextPlanLocationsAndInputContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Rīga-Ελλάδα-Київ.json")
	input := "{\n  \"version\":8,\n  \"items\":[null]\n}\n"
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := run([]string{"check", "--plan", path}, strings.NewReader(""), &stdout, &stderr)
	if exit != 2 || stderr.Len() == 0 || !strings.Contains(stderr.String(), strconv.Quote(path)+":3:12") || !strings.Contains(stderr.String(), `[pointer "/items/0"]`) {
		t.Fatalf("located plan error: %d %q %q", exit, stdout.String(), stderr.String())
	}
	for _, tc := range []struct{ input, code, file, item string }{
		{`{"version":8,"items":[{"source":"missing.pdf"}]}`, "source_input_invalid", filepath.Join(dir, "missing.pdf"), "/items/0"},
		{`{"version":8,"items":[{"page":{"size":"A4","text":{"value":"A","font":{"file":"missing.ttf"}}}}]}`, "font_input_invalid", filepath.Join(dir, "missing.ttf"), "/items/0"},
		{`{"version":8,"items":[{"page":{"size":"10x10pt","text":{"value":"A"}}}]}`, "text_overflow", "", "/items/0"},
	} {
		stdout.Reset()
		stderr.Reset()
		exit = run([]string{"check", "--plan", "-", "--base-dir", dir}, strings.NewReader(tc.input), &stdout, &stderr)
		if exit == 0 || stderr.Len() == 0 || !strings.Contains(stderr.String(), tc.code) || !strings.Contains(stderr.String(), tc.item) || tc.file != "" && !strings.Contains(stderr.String(), strconv.Quote(tc.file)) {
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
	if exit := run([]string{"check", source}, strings.NewReader(""), &stdout, &stderr); exit != 0 || !strings.Contains(stderr.String(), "source_structure_partial") {
		t.Fatalf("legal filename rejected: %d %q %q", exit, stdout.String(), stderr.String())
	}
	// A refusal must identify the same legal path without printing its controls.
	if err := os.WriteFile(source, []byte("not a PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	exit := run([]string{"check", source}, strings.NewReader(""), &stdout, &stderr)
	if exit != 2 || !strings.Contains(stdout.String(), strconv.Quote(source)) || strings.ContainsAny(stdout.String(), "\r\t\x1b") || strings.Count(stdout.String(), "\n") != 2 || strings.Count(stderr.String(), "\n") != 1 || !strings.Contains(stderr.String(), strconv.Quote(source)) || strings.ContainsAny(stderr.String(), "\r\t\x1b") {
		t.Fatalf("unsafe filename diagnostic: %d %q %q", exit, stdout.String(), stderr.String())
	}
	r, code, _ := invokeJSON(t, []string{"check", source}, "")
	if code != 2 || len(r.Diagnostics) != 1 || r.Diagnostics[0].File != source {
		t.Fatalf("JSON path changed: %d %+v", code, r)
	}
}

func resultSchemaBytes(t *testing.T) []byte {
	t.Helper()
	data, err := app.Schema(app.ResultSchema)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func jsonArguments(args []string) []string {
	result := append([]string(nil), args...)
	if len(result) == 0 {
		return result
	}
	switch result[0] {
	case "build", "check", "version", "--version":
	default:
		return result
	}
	for i, arg := range result[1:] {
		if arg == "--json" {
			return result
		}
		if arg == "--" {
			index := i + 1
			result = append(result, "")
			copy(result[index+1:], result[index:])
			result[index] = "--json"
			return result
		}
	}
	return append(result, "--json")
}

func TestLanguageSyntaxRefusedBeforePublication(t *testing.T) {
	for _, tag := range []string{"en-a-foo-a-bar", "en-abcde-abcde", "en-Latn-Latn"} {
		dir := t.TempDir()
		output := filepath.Join(dir, "out.pdf")
		plan := `{"version":8,"metadata":{"language":"` + tag + `"},"items":[{"page":{"size":"A4","text":{"value":"Text"}}}]}`
		for _, command := range []string{"check", "build"} {
			r, exit, _ := invokeJSON(t, []string{command, "--plan", "-", "-o", output}, plan)
			if exit != 2 || r.Status != "invalid" || !hasDiagnostic(r, "plan_value") {
				t.Fatalf("invalid language accepted: %s %+v", tag, r)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatalf("invalid language staged/published output: %v", err)
			}
		}
	}
}

func TestInitialPlanSyntaxNamedFileAndStdin(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		empty       bool
		offset      int64
	}{
		{"empty", "", true, 0}, {"whitespace", " \r\n\t", true, 0},
		{"BOM", "\ufeff", true, 3}, {"BOM whitespace", "\ufeff\t \n", true, 3},
		{"truncated", `{"version":8,"items":[`, false, 22}, {"NBSP", "\u00a0", false, 0},
	} {
		for _, stdin := range []bool{false, true} {
			t.Run(tc.name+strconv.FormatBool(stdin), func(t *testing.T) {
				dir := t.TempDir()
				label := filepath.Join(dir, "plan.json")
				input := ""
				if stdin {
					label = "<stdin>"
					input = tc.input
				} else if err := os.WriteFile(label, []byte(tc.input), 0600); err != nil {
					t.Fatal(err)
				}
				argument := label
				if stdin {
					argument = "-"
				}
				args := []string{"check", "--plan", argument}
				if stdin {
					args = append(args, "--base-dir", dir)
				}
				result, exit, _ := invokeJSON(t, args, input)
				if exit != 2 || result.Status != "invalid" || len(result.Diagnostics) != 1 {
					t.Fatalf("outcome: %d %+v", exit, result)
				}
				d := result.Diagnostics[0]
				if d.Code.String() != "plan_syntax" || d.File != label || d.Location == nil || d.Location.File != label || d.Location.Offset != tc.offset {
					t.Fatalf("attribution/classification: %+v", d)
				}
				emptyExplanation := d.Message == "Plan is empty; provide a JSON plan object."
				if emptyExplanation != tc.empty || d.Message == "" {
					t.Fatalf("explanation: %+v", d)
				}
				if tc.empty && (d.Location.Ref != "" || d.Location.Line != 1 || d.Location.Column != int(tc.offset)+1) {
					t.Fatalf("root location: %+v", d)
				}
				var stdout, stderr bytes.Buffer
				if code := run(args, strings.NewReader(input), &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "plan_syntax: "+d.Message) || !strings.Contains(stderr.String(), strconv.Quote(label)) {
					t.Fatalf("text outcome: %d %q %q", code, stdout.String(), stderr.String())
				}
			})
		}
	}
}

func TestLayoutDiagnosticsLocateEffectiveAuthoredValues(t *testing.T) {
	input := "\ufeff{\r\n" +
		"  \"version\": 8,\r\n" +
		"  \"page_defaults\": {\"size\": \"A4\", \"text\": {\"width\": 40}},\r\n" +
		"  \"items\": [\r\n" +
		"    {\"page\": {\"text\": {\"value\": \"āRepeatedTokenWithoutSpaces\"}}},\r\n" +
		"    {\"page\": {\"text\": {\"value\": \"āRepeatedTokenWithoutSpaces\", \"width\": 20}}},\r\n" +
		"    {\"page\": {\"text\": {\"value\": \"A\", \"width\": 60, \"y\": 800}}}\r\n" +
		"  ]\r\n}"
	file := filepath.Join(t.TempDir(), "authored.json")
	if err := os.WriteFile(file, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	for _, planPath := range []string{file, "-"} {
		r, exit, _ := invokeJSON(t, []string{"check", "--plan", planPath}, input)
		if exit != 2 || len(r.Diagnostics) != 3 {
			t.Fatalf("%s: %+v", planPath, r.Diagnostics)
		}
		label := file
		if planPath == "-" {
			label = "<stdin>"
		}
		for i, want := range []struct {
			ref, token   string
			line, column int
		}{
			{"/page_defaults/text/width", "40}", 3, 53},
			{"/items/1/page/text/width", "20}", 6, 74},
		} {
			d := r.Diagnostics[i]
			loc := d.Location
			if d.Code.String() != "text_overflow" || d.File != label || loc == nil || loc.File != label ||
				loc.Ref != want.ref || loc.Line != want.line || loc.Column != want.column ||
				loc.Offset != int64(strings.Index(input, want.token)) {
				t.Errorf("%s diagnostic %d: %+v, location %+v", planPath, i, d, loc)
			}
		}
		if d := r.Diagnostics[2]; d.Location != nil || strings.Contains(d.Message, "using width") {
			t.Errorf("unrelated vertical overflow attributed to width: %+v", d)
		}
	}
}
