// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/plan"
)

func readText(t *testing.T, root string, parts ...string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}

	return string(content)
}

// TestMarkdownLinksResolve fails for a relative link whose target file does not exist.
func TestMarkdownLinksResolve(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	link := regexp.MustCompile(`\]\(([^)#\s]+)(#[^)]*)?\)`)
	checked := 0

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "third_party") {
			return filepath.SkipDir
		}

		if entry.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		for _, match := range link.FindAllStringSubmatch(string(content), -1) {
			if strings.Contains(match[1], "://") {
				continue
			}

			checked++

			_, statErr := os.Stat(filepath.Join(filepath.Dir(path), match[1]))
			if statErr != nil {
				relative, _ := filepath.Rel(root, path)
				t.Errorf("%s: broken link %q", relative, match[1])
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if checked == 0 {
		t.Fatal("no relative links found; the link pattern has drifted from the documents")
	}
}

// TestHelpOptionsAreDocumented fails for an option in --help that the contracts never mention.
func TestHelpOptionsAreDocumented(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	documented := readText(t, root, "docs", "CLI.md") + readText(t, root, "docs", "BLANK_PAGES.md")

	// Options are listed at the start of a help line, optionally after a short form.
	listed := regexp.MustCompile(`(?m)^\s+(?:-[a-z], )?(--[a-z][a-z-]*)`).FindAllStringSubmatch(cli.HelpText(), -1)
	if len(listed) < 10 {
		t.Fatalf("found only %d options in help text; the pattern has drifted", len(listed))
	}

	for _, match := range listed {
		option := match[1]
		if !strings.Contains(documented, option) {
			t.Errorf("option %s appears in --help but not in docs/CLI.md or docs/BLANK_PAGES.md", option)
		}
	}
}

// TestSchemaEnumsMatchDomainAndDocs fails if the schema's font or anchor lists drift from the code or docs.
func TestSchemaEnumsMatchDomainAndDocs(t *testing.T) {
	t.Parallel()

	var schema struct {
		Defs struct {
			TextStyle struct {
				Properties map[string]struct {
					Enum []string `json:"enum"`
				} `json:"properties"`
			} `json:"textStyle"`
		} `json:"$defs"`
	}

	err := json.Unmarshal([]byte(plan.Schema()), &schema)
	if err != nil {
		t.Fatal(err)
	}

	blankPages := readText(t, moduleRoot(t), "docs", "BLANK_PAGES.md")
	properties := schema.Defs.TextStyle.Properties

	if len(properties["font"].Enum) != 12 {
		t.Fatalf("schema lists %d fonts, want 12", len(properties["font"].Enum))
	}

	for _, name := range properties["font"].Enum {
		_, parseErr := assembly.ParseFont(name)
		if parseErr != nil || !strings.Contains(blankPages, "`"+name+"`") {
			t.Errorf("font %q is not a supported, documented font (%v)", name, parseErr)
		}
	}

	for _, name := range properties["anchor"].Enum {
		_, parseErr := assembly.ParseAnchor(name)
		if parseErr != nil || !strings.Contains(blankPages, name) {
			t.Errorf("anchor %q is not a supported, documented anchor (%v)", name, parseErr)
		}
	}

	for _, name := range properties["align"].Enum {
		_, parseErr := assembly.ParseTextAlign(name)
		if parseErr != nil {
			t.Errorf("alignment %q is not supported: %v", name, parseErr)
		}
	}
}
