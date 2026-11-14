// SPDX-License-Identifier: AGPL-3.0-only
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

//go:embed AGPL-3.0-only.txt
var projectLicense string

const header = `Copyright (c) 2026 Ervins Strauhmanis.
PDFConcat original code is licensed under AGPL-3.0-only.
You may copy, modify and convey it under that license. There is no warranty.
https://www.gnu.org/licenses/agpl-3.0.html
Source code: https://github.com/resoltico/pdfconcat
Qualified binary releases include SOURCE.tar.gz with project and module source:
https://github.com/resoltico/pdfconcat/releases
Licensing and contribution terms:
https://github.com/resoltico/pdfconcat/blob/main/PROJECT-NOTICE.md
Separate commercial terms are pending; no exception is granted:
https://github.com/resoltico/pdfconcat/blob/main/LICENSE-COMMERCIAL.md

`

// Text returns PDFConcat's license notice, the third-party inventory and every
// bundled license text, in file-name order.
func Text() (string, error) {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("==> LICENSE <==\n\n")
	b.WriteString(projectLicense)
	b.WriteString("\n\n")
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
