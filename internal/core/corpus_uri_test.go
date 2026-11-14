// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func corpusURICanary(t *testing.T, binary string) {
	tools := oracle.RequireTools(t)
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	target := server.URL + "/retained-link"
	// A positive control prevents an unreachable/dead canary from proving nothing.
	client := http.Client{Timeout: 2 * time.Second}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if err = response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent || requests.Load() != 1 {
		t.Fatal("HTTP canary did not observe the positive control")
	}
	requests.Store(0)
	dir := t.ArtifactDir()
	source := filepath.Join(dir, "uri.pdf")
	if err := pdffixture.URILink("OFFLINE-URI", target, "", false).WriteFile(source); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"check", "build"} {
		t.Run(operation, func(t *testing.T) {
			output := filepath.Join(t.ArtifactDir(), "packet.pdf")
			args := []string{operation, source, "--output", output, "--json"}
			command := exec.CommandContext(t.Context(), binary, args...)
			var stderr bytes.Buffer
			command.Stderr = &stderr
			raw, err := command.Output()
			if err != nil || stderr.Len() != 0 {
				t.Fatalf("CLI URI workflow: %v %s %s", err, raw, stderr.String())
			}
			var result core.Result
			if err = json.Unmarshal(raw, &result); err != nil || result.Status != "ok" {
				t.Fatalf("URI result: %v %s", err, raw)
			}
			schemaValid(t, result)
			if requests.Load() != 0 {
				t.Fatal("PDFConcat requested a retained URI")
			}
			if operation == "check" {
				if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatalf("check published a PDF: %v", err)
				}
				return
			}
			if result.Output == nil || !result.Output.Published {
				t.Fatal("build did not publish")
			}
			if err := tools.Check(output); err != nil {
				t.Fatal(err)
			}
			graph, err := oracle.QPDFJSON(tools, output)
			if err != nil {
				t.Fatal(err)
			}
			uris := corpusFirstPageURIs(t, graph)
			if len(uris) != 1 || uris[0] != "u:"+target {
				t.Fatalf("retained URI changed or disappeared: %v", uris)
			}
			if requests.Load() != 0 {
				t.Fatal("CLI or independent PDF checks requested the URI")
			}
		})
	}
}

// corpusFirstPageURIs follows page membership, rather than accepting an unrelated
// URI action elsewhere in the serialized object graph as proof of retention.
func corpusFirstPageURIs(t *testing.T, raw []byte) []string {
	t.Helper()
	var dump struct {
		Pages []struct {
			Object string `json:"object"`
		} `json:"pages"`
		QPDF []jsontext.Value `json:"qpdf"`
	}
	if err := json.Unmarshal(raw, &dump); err != nil {
		t.Fatal(err)
	}
	if len(dump.Pages) == 0 || len(dump.QPDF) != 2 {
		t.Fatal("missing qpdf page/object graph")
	}
	var objects map[string]struct {
		Value jsontext.Value `json:"value"`
	}
	if err := json.Unmarshal(dump.QPDF[1], &objects); err != nil {
		t.Fatal(err)
	}
	resolve := func(value jsontext.Value) jsontext.Value {
		for range 20 {
			var ref string
			if err := json.Unmarshal(value, &ref); err != nil {
				return value
			}
			fields := strings.Fields(ref)
			if len(fields) != 3 || fields[2] != "R" {
				return value
			}
			if _, err := strconv.Atoi(fields[0]); err != nil {
				return value
			}
			if _, err := strconv.Atoi(fields[1]); err != nil {
				return value
			}
			object, exists := objects["obj:"+ref]
			if !exists {
				t.Fatalf("missing qpdf indirect object %s", ref)
			}
			value = object.Value
		}
		t.Fatal("cyclic qpdf indirect references")
		return nil
	}
	dictionary := func(value jsontext.Value) map[string]jsontext.Value {
		t.Helper()
		var result map[string]jsontext.Value
		if err := json.Unmarshal(resolve(value), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	page := dictionary(objects["obj:"+dump.Pages[0].Object].Value)
	if page["/Annots"] == nil {
		return nil
	}
	var annotations []jsontext.Value
	if err := json.Unmarshal(resolve(page["/Annots"]), &annotations); err != nil {
		t.Fatal(err)
	}
	var uris []string
	for _, value := range annotations {
		annotation := dictionary(value)
		var subtype string
		if err := json.Unmarshal(annotation["/Subtype"], &subtype); err != nil {
			t.Fatal(err)
		}
		if subtype != "/Link" || annotation["/A"] == nil {
			continue
		}
		action := dictionary(annotation["/A"])
		var kind string
		if err := json.Unmarshal(action["/S"], &kind); err != nil {
			t.Fatal(err)
		}
		if kind != "/URI" {
			continue
		}
		var uri string
		if err := json.Unmarshal(resolve(action["/URI"]), &uri); err != nil {
			t.Fatal(err)
		}
		uris = append(uris, uri)
	}
	return uris
}

func TestCorpusURIOracleRequiresPageAnnotationMembership(t *testing.T) {
	// The action remains reachable through catalog OpenAction in the negative
	// control. Mere presence of its target must not prove page-link retention.
	const graph = `{
      "pages":[{"object":"3 0 R"}],
      "qpdf":[{}, {
        "trailer":{"value":{"/Root":"1 0 R"}},
        "obj:1 0 R":{"value":{"/Type":"/Catalog","/Pages":"2 0 R","/OpenAction":"6 0 R"}},
        "obj:2 0 R":{"value":{"/Type":"/Pages","/Count":1,"/Kids":["3 0 R"]}},
        "obj:3 0 R":{"value":{"/Annots":"4 0 R"}},
        "obj:4 0 R":{"value":["5 0 R"]},
        "obj:5 0 R":{"value":{"/Subtype":"/Link","/A":"6 0 R"}},
        "obj:6 0 R":{"value":{"/S":"/URI","/URI":"u:http://127.0.0.1/retained-link"}}
      }]
    }`
	uris := corpusFirstPageURIs(t, []byte(graph))
	if len(uris) != 1 || uris[0] != "u:http://127.0.0.1/retained-link" {
		t.Fatal(uris)
	}
	withoutLink := bytes.Replace([]byte(graph), []byte(`"/Annots":"4 0 R"`), []byte(`"/Annots":[]`), 1)
	if uris := corpusFirstPageURIs(t, withoutLink); len(uris) != 0 {
		t.Fatalf("non-page URI counted as a retained page link: %v", uris)
	}
}
