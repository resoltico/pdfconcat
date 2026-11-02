// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const noticesDir = "../../internal/legal"

var releaseTargets = []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"}

type linkedPackage struct {
	ImportPath string
	Dir        string
	EmbedFiles []string
	Module     *struct{ Path, Dir string }
}

// linkedThirdPartyPackages lists the non-PDFConcat packages linked into the
// executable for every release target.
func linkedThirdPartyPackages(t *testing.T) map[string]linkedPackage {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	packages := map[string]linkedPackage{}
	for _, target := range releaseTargets {
		goos, goarch, _ := strings.Cut(target, "/")
		command := exec.CommandContext(t.Context(), "go", "list", "-deps", "-json=ImportPath,Dir,EmbedFiles,Module", "./cmd/pdfconcat")
		command.Dir = root
		command.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
		output, err := command.Output()
		if err != nil {
			t.Fatalf("target %s packages: %v", target, err)
		}
		decoder := jsontext.NewDecoder(bytes.NewReader(output))
		for {
			var p linkedPackage
			if err := json.UnmarshalDecode(decoder, &p); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				t.Fatal(err)
			}
			if p.Module != nil && p.Module.Path != "github.com/resoltico/pdfconcat" {
				packages[p.ImportPath] = p
			}
		}
	}
	return packages
}

func readNotices(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(noticesDir, "THIRD_PARTY_NOTICES.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestNoticesMatchLinkedTargetModules(t *testing.T) {
	documented := map[string]bool{}
	for line := range strings.SplitSeq(readNotices(t), "\n") {
		if strings.HasPrefix(line, "| `") {
			columns := strings.Split(line, "|")
			name := strings.Trim(strings.TrimSpace(columns[1]), "`")
			if documented[name] {
				t.Fatalf("duplicate notice %s", name)
			}
			documented[name] = true
		}
	}
	linked := map[string]bool{}
	for _, p := range linkedThirdPartyPackages(t) {
		linked[p.Module.Path] = true
	}
	if len(linked) != len(documented) {
		t.Fatalf("linked/noticed module count %d/%d", len(linked), len(documented))
	}
	for module := range linked {
		if !documented[module] {
			t.Fatalf("missing notice %s", module)
		}
	}
}

// A linked package can carry its own license in a subdirectory of its module,
// as go-text's harfbuzz port and pdfcpu's pkcs7 package do. Each such file
// must be bundled byte for byte.
func TestNoticesBundleNestedPackageLicenses(t *testing.T) {
	bundled := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(noticesDir, "licenses"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(noticesDir, "licenses", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		bundled[string(data)] = true
	}
	checked := map[string]bool{}
	for _, p := range linkedThirdPartyPackages(t) {
		for dir := p.Dir; ; dir = filepath.Dir(dir) {
			files, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range files {
				if f.IsDir() || !isLicenseFile(f.Name()) {
					continue
				}
				path := filepath.Join(dir, f.Name())
				if checked[path] {
					continue
				}
				checked[path] = true
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bundled[string(data)] {
					t.Errorf("license file of linked package %s is not bundled: %s", p.ImportPath, path)
				}
			}
			if dir == p.Module.Dir || filepath.Dir(dir) == dir {
				break
			}
		}
	}
	if len(checked) == 0 {
		t.Fatal("found no dependency license files; the module cache layout is unexpected")
	}
}

func isLicenseFile(name string) bool {
	name = strings.ToUpper(name)
	for _, prefix := range []string{"LICENSE", "LICENCE", "COPYING", "NOTICE"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// Assets that dependencies embed, such as pdfcpu's Roboto font, ship inside the
// executable and must be named in the inventory.
func TestNoticesNameDependencyEmbeddedAssets(t *testing.T) {
	notices := readNotices(t)
	for _, p := range linkedThirdPartyPackages(t) {
		for _, file := range p.EmbedFiles {
			if file == "resources/config.yml" && p.ImportPath == "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model" {
				continue // pdfcpu's own Apache-2.0 configuration defaults, not a separate work
			}
			stem := strings.SplitN(filepath.Base(file), "-", 2)[0]
			stem = strings.TrimSuffix(stem, filepath.Ext(stem))
			if !strings.Contains(notices, stem) {
				t.Errorf("%s embeds %s, which THIRD_PARTY_NOTICES.md does not name", p.ImportPath, file)
			}
		}
	}
}
