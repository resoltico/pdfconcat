// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package legal embeds the license notices that accompany every executable, so
// an installed program carries them even without its release archive.
package legal

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed THIRD_PARTY_NOTICES.md licenses/*.txt
var files embed.FS

const header = `PDFConcat is licensed under the Mozilla Public License 2.0:
https://mozilla.org/MPL/2.0/
Source code: https://github.com/resoltico/pdfconcat

`

// Text returns PDFConcat's license notice, the third-party inventory and every
// bundled license text, in file-name order.
func Text() (string, error) {
	var b strings.Builder
	b.WriteString(header)
	notices, err := files.ReadFile("THIRD_PARTY_NOTICES.md")
	if err != nil {
		return "", err
	}
	b.Write(notices)
	entries, err := fs.ReadDir(files, "licenses")
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		data, err := files.ReadFile("licenses/" + entry.Name())
		if err != nil {
			return "", err
		}
		b.WriteString("\n\n==> licenses/" + entry.Name() + " <==\n\n")
		b.Write(data)
	}
	return b.String(), nil
}
