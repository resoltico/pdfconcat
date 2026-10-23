// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/oracle"
)

type corpusProfile struct {
	ID       string
	Factors  [8]bool
	Sequence string
	Sections [][3]int64
	Outlines []oracle.Outline
}
type corpusRecord struct {
	ID, Boundary, ArtifactDir string
	Factors                   *[8]bool         `json:",omitempty"`
	ExpectedSequence          string           `json:",omitempty"`
	ExpectedOutlines          []oracle.Outline `json:",omitempty"`
}

func corpusJSON(t testing.TB, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
func corpusCopy(t testing.TB, source, target string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func corpusStage(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.ArtifactDir()
	for _, name := range names {
		corpusCopy(t, filepath.Join("testdata/corpus", filepath.FromSlash(name)), filepath.Join(dir, filepath.FromSlash(name)))
	}
	return dir
}
func corpusBinary(t *testing.T, name, path string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), name+".exe")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-o", binary, path)
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build current %s: %s %v", name, raw, err)
	}
	return binary
}
func corpusArtifact(t *testing.T, r core.Result) {
	t.Helper()
	schemaValid(t, r)
	if r.Output == nil || !r.Output.Published || r.Output.Bytes == nil {
		t.Fatalf("not published: %+v", r)
	}
	data, err := os.ReadFile(r.Output.Path)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(data)) != *r.Output.Bytes || fmt.Sprintf("%x", sha256.Sum256(data)) != r.Output.SHA256 {
		t.Fatal("actual artifact identity differs from outcome")
	}
}
func corpusPage(value, font string) map[string]any {
	text := map[string]any{"value": value}
	if font != "" {
		text["font"] = map[string]any{"file": font}
	}
	return map[string]any{"page": map[string]any{"text": text}}
}
func profilePlan(p corpusProfile) map[string]any {
	source := map[string]any{"source": "business-report.pdf"}
	if p.Factors[0] {
		source["source"] = "business-report-object-streams.pdf"
	}
	if p.Factors[1] {
		source["pages"] = [][2]int{{1, 1}, {3, 3}}
	}
	cover, spacer, card := corpusPage("", ""), corpusPage("", ""), corpusPage("", "")
	if p.Factors[7] {
		cover = corpusPage("Packet Ā Ελληνικά", "")
		spacer = corpusPage("Italic AV italic", "fonts/Go-Italic.ttf")
		card = corpusPage("Mono iii WWW 0123456789", "fonts/Go-Mono.ttf")
	}
	innerItems := []any{}
	if p.Factors[2] {
		innerItems = append(innerItems, source)
	}
	innerItems = append(innerItems, card)
	start := "any"
	if p.Factors[6] {
		start = "odd"
	}
	inner := map[string]any{"start_on": start, "items": innerItems}
	outer := map[string]any{"start_on": start, "items": []any{source, spacer, inner}}
	items := []any{cover}
	if p.Factors[5] {
		items = append(items, map[string]any{"contents": map[string]any{}})
		outer["title"] = "Evidence"
		outer["divider"] = map[string]any{}
		inner["title"] = "Appendix"
		inner["divider"] = map[string]any{}
	}
	items = append(items, outer)
	plan := map[string]any{"version": 4, "output": "packet.pdf", "page_defaults": map[string]any{"size": "A4"}, "items": items}
	if p.Factors[3] {
		plan["fit_to"] = "A5"
	}
	if p.Factors[4] {
		plan["page_numbers"] = map[string]any{}
	}
	return plan
}
func profileCoverage(profiles []corpusProfile) error {
	seen := map[string]bool{}
	for _, p := range profiles {
		if seen[p.ID] {
			return fmt.Errorf("duplicate profile %s", p.ID)
		}
		seen[p.ID] = true
	}
	for a := range 8 {
		for b := a + 1; b < 8; b++ {
			var tuples [4]bool
			for _, p := range profiles {
				i := 0
				if p.Factors[a] {
					i += 2
				}
				if p.Factors[b] {
					i++
				}
				tuples[i] = true
			}
			for tuple, ok := range tuples {
				if !ok {
					return fmt.Errorf("missing factor pair %d/%d tuple %d", a, b, tuple)
				}
			}
		}
	}
	return nil
}
func actualProfileSequence(r core.Result) string {
	var sequence []string
	occurrence := 0
	for _, p := range r.Parts {
		if p.Kind == "source" {
			occurrence++
			if p.Spans == nil {
				return "missing spans"
			}
			for _, span := range *p.Spans {
				for n := span.Source[0]; n <= span.Source[1]; n++ {
					sequence = append(sequence, fmt.Sprintf("source:%d:%d", occurrence, n))
				}
			}
		} else if p.Pages != nil {
			for range *p.Pages {
				sequence = append(sequence, p.Kind.String())
			}
		}
	}
	return strings.Join(sequence, " ")
}
func checkProfileFacts(t *testing.T, p corpusProfile, r core.Result) {
	t.Helper()
	if r.Status != "ok" || r.Pages == nil || r.Pages.Total == nil || *r.Pages.Total != int64(len(strings.Fields(p.Sequence))) || actualProfileSequence(r) != p.Sequence {
		t.Fatalf("profile literal itinerary: %+v expected %s", r, p.Sequence)
	}
	if len(r.Sections) != len(p.Sections) {
		t.Fatal("section count", r.Sections)
	}
	for n, want := range p.Sections {
		s := r.Sections[n]
		if s.TargetPage == nil || s.OutputPages == nil || *s.TargetPage != want[0] || s.OutputPages[0] != want[1] || s.OutputPages[1] != want[2] {
			t.Fatalf("literal section %d: %+v want %v", n, s, want)
		}
	}
	next := int64(1)
	for _, part := range r.Parts {
		if part.OutputPages == nil || part.OutputPages[0] != next {
			t.Fatal("leaf partition", r.Parts)
		}
		next = part.OutputPages[1] + 1
	}
	if next != *r.Pages.Total+1 {
		t.Fatal("leaf partition end")
	}
}
func profileOracle(t *testing.T, p corpusProfile, r core.Result) {
	t.Helper()
	tools := oracle.RequireTools(t)
	if err := tools.Check(r.Output.Path); err != nil {
		t.Fatal(err)
	}
	doc, err := oracle.Load(tools, r.Output.Path)
	if err != nil {
		t.Fatal(err)
	}
	texts, err := doc.RawTexts()
	if err != nil {
		t.Fatal(err)
	}
	sequence := strings.Fields(p.Sequence)
	if len(texts) != len(sequence) {
		t.Fatal("independent page count")
	}
	for n, kind := range sequence {
		text := texts[n]
		if strings.HasPrefix(kind, "source:") {
			fields := strings.Split(kind, ":")
			page, _ := strconv.Atoi(fields[2])
			marker := []string{"Project evidence report", "Measured comparison", "Supporting notes"}[page-1]
			if !strings.Contains(text, marker) {
				t.Fatalf("page %d missing source marker %q: %q", n+1, marker, text)
			}
		}
		if kind == "padding" && strings.TrimSpace(text) != "" {
			t.Fatalf("padding painted inherited text/footer: %q", text)
		}
		if p.Factors[3] {
			w, h, err := doc.RenderedSize(n + 1)
			if err != nil || math.Abs(float64(w)-420) > 1 || math.Abs(float64(h)-596) > 1 {
				t.Fatalf("A5 physical fit: %d %d %v", w, h, err)
			}
		}
	}
	checkCorpusFooters(t, p, tools, r.Output.Path)
	if err := doc.CheckOutlineTree(p.Outlines); err != nil {
		t.Fatal("literal authored/native outline tree:", err)
	}
	if p.Factors[5] {
		targets, err := doc.ActionTargets(2)
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(targets)
		want := []int{int(p.Sections[0][0]), int(p.Sections[1][0])}
		slices.Sort(want)
		if !slices.Equal(targets, want) {
			t.Fatalf("contents links %v want %v", targets, want)
		}

	}
	corpusJSON(t, filepath.Join(t.ArtifactDir(), "oracle-facts.json"), map[string]any{"texts": texts, "pages": doc.PageCount()})
}
func TestCorpus(t *testing.T) {
	cli := corpusBinary(t, "pdfconcat", "../../cmd/pdfconcat")
	client := corpusBinary(t, "core-client", "./testdata/client")
	if err := profileCoverage(corpusProfiles); err != nil {
		t.Fatal(err)
	}
	var matrix []corpusRecord
	for _, p := range corpusProfiles {
		t.Run(p.ID, func(t *testing.T) {
			dir := corpusStage(t, "business-report.pdf", "business-report-object-streams.pdf", "fonts/Go-Italic.ttf", "fonts/Go-Mono.ttf")
			plan := filepath.Join(dir, "plan.json")
			corpusJSON(t, plan, profilePlan(p))
			matrix = append(matrix, corpusRecord{ID: p.ID, Boundary: "core check/build", ArtifactDir: dir, Factors: &p.Factors, ExpectedSequence: p.Sequence, ExpectedOutlines: p.Outlines})
			check := core.Execute(t.Context(), core.NewRequest(core.Check, core.FromPlan(core.FilePlan(plan)), core.Options{WorkingDir: dir, Report: filepath.Join(dir, "check.json")}))
			checkProfileFacts(t, p, check)
			if check.Output.Published {
				t.Fatal("check published output")
			}
			build := core.Execute(t.Context(), core.NewRequest(core.Build, core.FromPlan(core.FilePlan(plan)), core.Options{WorkingDir: dir, Report: filepath.Join(dir, "build.json")}))
			checkProfileFacts(t, p, build)
			corpusArtifact(t, build)
			schemaValid(t, build)
			corpusJSON(t, filepath.Join(dir, "complete.json"), build)
			t.Run("oracle", func(t *testing.T) { profileOracle(t, p, build) })
			if p.ID == "all-profiles" {
				t.Run("transports", func(t *testing.T) { corpusTransports(t, p, cli, client) })
			}
		})
	}
	t.Run("producers", corpusProducers)
	t.Run("cli-contracts", func(t *testing.T) { corpusCLIContracts(t, cli) })
	t.Run("concurrent-publication", func(t *testing.T) { corpusCLIConcurrentPublication(t, cli) })
	t.Run("blank-neighbors", func(t *testing.T) { corpusBlankNeighbors(t, cli) })
	t.Run("workflows", func(t *testing.T) { corpusWorkflows(t, cli) })
	t.Run("hidden-priority", func(t *testing.T) { corpusHiddenPriority(t, cli) })
	t.Run("matrix-rejects", func(t *testing.T) {
		if profileCoverage(nil) == nil {
			t.Fatal("empty factor table accepted")
		}
		bad := append([]corpusProfile{}, corpusProfiles...)
		bad = append(bad, bad[0])
		if profileCoverage(bad) == nil {
			t.Fatal("duplicate IDs accepted")
		}
		var uncovered []corpusProfile
		for _, p := range corpusProfiles {
			if !p.Factors[0] {
				uncovered = append(uncovered, p)
			}
		}
		if profileCoverage(uncovered) == nil {
			t.Fatal("uncovered pair accepted")
		}
	})
	corpusJSON(t, filepath.Join(t.ArtifactDir(), "matrix.json"), matrix)
}
func corpusTransports(t *testing.T, p corpusProfile, cli, client string) {
	var first core.Result
	for _, mode := range []string{"file", "bytes", "loader", "document", "cli-json", "cli-text", "cli-stdin"} {
		t.Run(mode, func(t *testing.T) {
			dir := corpusStage(t, "business-report.pdf", "business-report-object-streams.pdf", "fonts/Go-Italic.ttf", "fonts/Go-Mono.ttf")
			plan := filepath.Join(dir, "plan.json")
			corpusJSON(t, plan, profilePlan(p))
			output, report := filepath.Join(dir, "packet.pdf"), filepath.Join(dir, "report.json")
			cmd := exec.CommandContext(t.Context(), client, mode, plan, dir, output, report)
			if strings.HasPrefix(mode, "cli-") {
				args := []string{"build", "--plan", plan, "--output", output, "--report", report}
				if mode == "cli-text" {
					args = append(args, "--format", "text")
				}
				if mode == "cli-stdin" {
					args[2] = "-"
					args = append(args, "--base-dir", dir)
				}
				cmd = exec.CommandContext(t.Context(), cli, args...)
				if mode == "cli-stdin" {
					data, err := os.ReadFile(plan)
					if err != nil {
						t.Fatal(err)
					}
					cmd.Stdin = strings.NewReader(string(data))
				}
			}
			cmd.Dir = dir
			raw, err := cmd.Output()
			if err != nil {
				t.Fatalf("%s: %s %v", mode, raw, err)
			}
			if err = os.WriteFile(filepath.Join(dir, "stdout"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(report)
			if err != nil {
				t.Fatal(err)
			}
			var r core.Result
			if err = json.Unmarshal(data, &r); err != nil {
				t.Fatal(err)
			}
			checkProfileFacts(t, p, r)
			corpusArtifact(t, r)
			schemaValid(t, r)
			// These runs have different copied-input identities/targets, not different
			// packet intent. Check actual hashes/bytes above before normalizing paths.
			r.Output = nil
			r.Report = nil
			for n := range r.Parts {
				r.Parts[n].Path = ""
				if r.Parts[n].Text != nil {
					r.Parts[n].Text.Font = filepath.Base(r.Parts[n].Text.Font)
				}
			}
			for n := range r.Diagnostics {
				r.Diagnostics[n].File = ""
			}
			if mode == "file" {
				first = r
			} else if !reflect.DeepEqual(first, r) {
				t.Fatalf("%s complete common facts differ", mode)
			}
		})
	}
}

// Footer bounds are independently observed Poppler coordinates for default 9pt
// Noto numbers: centered, in the bottom 18–34pt typographic band. Body/source
// digits elsewhere on the page cannot satisfy this witness. Two-digit ActualText
// may expose the first-cell bbox, so allow one cell around the physical center.
func corpusFooterWords(page corpusBBoxPage) []corpusWord {
	var found []corpusWord
	for _, word := range page.Words {
		if word.YMin >= page.Height-34 && word.YMax <= page.Height-18 && math.Abs((word.XMin+word.XMax)/2-page.Width/2) <= 6 {
			found = append(found, word)
		}
	}
	return found
}

func corpusFooterError(p corpusProfile, pages []corpusBBoxPage) error {
	sequence := strings.Fields(p.Sequence)
	if len(pages) != len(sequence) {
		return errors.New("footer page geometry count")
	}
	for n, page := range pages {
		words := corpusFooterWords(page)
		enabled := p.Factors[4] && sequence[n] != "padding"
		if enabled {
			if len(words) != 1 || words[0].Text != strconv.Itoa(n+1) {
				return fmt.Errorf("physical footer %d missing/wrong in independent bottom-center band: %v", n+1, words)
			}
		} else if len(words) != 0 {
			return fmt.Errorf("disabled/padding page %d has a footer: %v", n+1, words)
		}
	}
	return nil
}
func checkCorpusFooters(t *testing.T, p corpusProfile, tools oracle.Tools, path string) {
	t.Helper()
	if err := corpusFooterError(p, corpusTextGeometry(t, tools, path)); err != nil {
		t.Fatal(err)
	}
}
