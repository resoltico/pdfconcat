// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
)

var markdownLink = regexp.MustCompile(`\]\(([^)#\s]+)(#[^)]*)?\)`)

// TestMarkdownLinksResolve fails for a relative link whose target file does not exist.
func TestMarkdownLinksResolve(t *testing.T) {
	t.Parallel()

	tree, err := os.OpenRoot(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		closeErr := tree.Close()
		if closeErr != nil {
			t.Error(closeErr)
		}
	})

	checked := 0

	err = fs.WalkDir(tree.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == ".tools" || entry.Name() == "third_party" {
				return fs.SkipDir
			}

			return nil
		}

		if strings.HasSuffix(name, ".md") {
			checked += checkLinks(t, tree, name)
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

// checkLinks reports each broken relative link of one Markdown file and returns how many it checked.
func checkLinks(t *testing.T, tree *os.Root, name string) int {
	t.Helper()

	content, err := tree.ReadFile(name)
	if err != nil {
		t.Error(err)

		return 0
	}

	checked := 0

	for _, match := range markdownLink.FindAllStringSubmatch(string(content), -1) {
		if strings.Contains(match[1], "://") {
			continue
		}

		checked++

		_, statErr := tree.Stat(path.Join(path.Dir(name), match[1]))
		if statErr != nil {
			t.Errorf("%s: broken link %q", name, match[1])
		}
	}

	return checked
}
