// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package legal

import (
	"bytes"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

// Every bundled license is linked from the inventory, and every link resolves,
// so the archive and `pdfconcat licenses` carry the same complete set.
func TestInventoryLinksEveryBundledLicense(t *testing.T) {
	notices, err := files.ReadFile("THIRD_PARTY_NOTICES.md")
	if err != nil {
		t.Fatal(err)
	}
	linked := map[string]bool{}
	for _, match := range regexp.MustCompile(`\]\((licenses/[^)]+)\)`).FindAllSubmatch(notices, -1) {
		linked[string(match[1])] = true
	}
	entries, err := fs.ReadDir(files, "licenses")
	if err != nil {
		t.Fatal(err)
	}
	bundled := map[string]bool{}
	for _, entry := range entries {
		bundled["licenses/"+entry.Name()] = true
	}
	for name := range linked {
		if !bundled[name] {
			t.Errorf("inventory links missing file %s", name)
		}
	}
	for name := range bundled {
		if !linked[name] {
			t.Errorf("bundled license %s is not linked from the inventory", name)
		}
	}
}

func TestTextContainsEveryLicense(t *testing.T) {
	text, err := Text()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text, "Copyright (c) 2026 Ervins Strauhmanis.\nPDFConcat original code is licensed under AGPL-3.0-only.") {
		t.Fatalf("missing PDFConcat notice: %.80q", text)
	}
	entries, err := fs.ReadDir(files, "licenses")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := files.ReadFile("licenses/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, "==> licenses/"+entry.Name()+" <==\n\n"+string(data)) {
			t.Errorf("Text omits %s", entry.Name())
		}
	}
}

// The font's own directory keeps the OFL beside the asset; both copies must agree.
func TestNotoLicenseMatchesFontAsset(t *testing.T) {
	beside, err := os.ReadFile("../core/internal/typeset/fontdata/OFL.txt")
	if err != nil {
		t.Fatal(err)
	}
	bundled, err := files.ReadFile("licenses/NotoSans-OFL-1.1.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beside, bundled) {
		t.Fatal("fontdata/OFL.txt and licenses/NotoSans-OFL-1.1.txt differ")
	}
}

// Installed single-file programs must carry the same full license as archives.
func TestProjectLicenseMatchesArchive(t *testing.T) {
	data, err := os.ReadFile("../../LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	if projectLicense != string(data) {
		t.Fatal("embedded project license differs from root LICENSE")
	}
	text, err := Text()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "==> LICENSE <==\n\n"+string(data)) {
		t.Fatal("license command omits full project license")
	}
}
