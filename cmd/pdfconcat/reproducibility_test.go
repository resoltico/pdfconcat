// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestPDFBytesAcrossProcessesAndEffectiveInputs(t *testing.T) {
	tools := oracle.RequireTools(t)
	binary := buildBinary(t)
	font, err := filepath.Abs("../../internal/core/testdata/fonts/source-serif/SourceSerif4-Regular.otf")
	if err != nil {
		t.Fatal(err)
	}
	plans := map[string]string{
		"generated":     `{"version":5,"fit_to":"A4","items":[{"page":{"text":{"value":"A report"}}}]}`,
		"cff":           `{"version":5,"fit_to":"A4","items":[{"page":{"text":{"value":"Rīgas office report","font":{"file":` + strconv.Quote(font) + `}}}}]}`,
		"source":        `{"version":5,"items":[{"source":"SOURCE"}]}`,
		"tagged-fitted": `{"version":5,"fit_to":"A4","metadata":{"title":"A title","language":"en"},"page_numbers":{"start":9,"mode":"consecutive","position":"top-right"},"items":[{"source":"SOURCE"},{"source":"SOURCE"},{"page":{"text":{"value":"After the annex"}}}]}`,
	}
	env := []string{}
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "SOURCE_DATE_EPOCH=") && !strings.HasPrefix(value, "GOMAXPROCS=") && !strings.HasPrefix(value, "TZ=") {
			env = append(env, value)
		}
	}
	run := func(dir, plan string, workers int) string {
		t.Helper()
		command := exec.CommandContext(t.Context(), binary, "build", "--plan", "-", "-o", "output.pdf", "--report", "report.json", "--json")
		command.Dir = dir
		command.Stdin = strings.NewReader(plan)
		timezone := "Pacific/Honolulu"
		if workers != 1 {
			timezone = "Europe/Riga"
		}
		command.Env = append(append([]string{}, env...), "GOMAXPROCS="+strconv.Itoa(workers), "TZ="+timezone)
		var stderr bytes.Buffer
		command.Stderr = &stderr
		raw, err := command.Output()
		if err != nil || stderr.Len() != 0 {
			t.Fatalf("build process: %v %s stderr %s", err, raw, stderr.String())
		}
		var result core.Result
		if err = json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "output.pdf")
		if err = tools.Check(path); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		hash := hex.EncodeToString(digest[:])
		if result.Output.SHA256 != hash {
			t.Fatal("report hash differs from actual bytes")
		}
		return hash
	}
	for name, plan := range plans {
		t.Run(name, func(t *testing.T) {
			dirs := []string{t.TempDir(), t.TempDir()}
			var hashes []string
			for index, dir := range dirs {
				filename := "input.pdf"
				if index == 1 {
					filename = "equivalent-name.pdf"
				}
				if err = os.WriteFile(filepath.Join(dir, filename), pdffixture.Tagged("visible source").Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.Mkdir(filepath.Join(dir, "sub"), 0700); err != nil {
					t.Fatal(err)
				}
				spelling := filename
				if index == 1 {
					spelling = "sub/../" + filename
				}
				effective := strings.ReplaceAll(plan, "SOURCE", spelling)
				hashes = append(hashes, run(dir, effective, 1+index*2))
				if index == 0 {
					time.Sleep(1100 * time.Millisecond)
				}
			}
			if hashes[0] != hashes[1] {
				t.Fatal("clock, worker, path or directory affected bytes")
			}
			// A visible/metadata change is the negative control, rather than ignored bytes.
			changed := strings.Replace(plan, `"items":`, `"metadata":{"title":"Different authored identity"},"items":`, 1)
			if strings.Contains(plan, `"metadata"`) {
				changed = strings.Replace(plan, "A title", "Changed title", 1)
			}
			if err = os.Remove(filepath.Join(dirs[0], "output.pdf")); err != nil {
				t.Fatal(err)
			}
			if err = os.Remove(filepath.Join(dirs[0], "report.json")); err != nil {
				t.Fatal(err)
			}
			if run(dirs[0], strings.ReplaceAll(changed, "SOURCE", "input.pdf"), 2) == hashes[0] {
				t.Fatal("authored metadata change did not affect bytes")
			}
			if err = os.Remove(filepath.Join(dirs[0], "output.pdf")); err != nil {
				t.Fatal(err)
			}
			if err = os.Remove(filepath.Join(dirs[0], "report.json")); err != nil {
				t.Fatal(err)
			}
			visible := plan
			if strings.Contains(plan, "SOURCE") {
				if err = os.WriteFile(filepath.Join(dirs[0], "input.pdf"), pdffixture.Tagged("changed visible source").Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				visible = strings.Replace(plan, "report", "changed report", 1)
			}
			if run(dirs[0], strings.ReplaceAll(visible, "SOURCE", "input.pdf"), 3) == hashes[0] {
				t.Fatal("visible content change did not affect bytes")
			}

		})
	}
}
