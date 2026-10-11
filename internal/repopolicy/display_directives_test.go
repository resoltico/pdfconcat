// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func TestOwnedSourceRejectsCompilerDisplayLaundering(t *testing.T) {
	for _, source := range []string{
		"//line /other/internal/app/allowed.go:9\npackage assembly\n",
		"//line :100\npackage assembly\n",
		"/*line /other/multiline\ninternal/app/allowed.go:100*/package assembly\n",
		"package assembly\n/*line :100:10*/var Value=1\n",
		"package assembly\n/*line /other/source.go:99:1*/var Value=1\n",
	} {
		findings, err := repopolicy.DirectivesIn("internal/assembly/actual.go", []byte(source))
		if err != nil || len(findings) != 1 || findings[0].Kind != "compiler display" || findings[0].File != "internal/assembly/actual.go" {
			t.Fatalf("laundered ownership for %q: %+v %v", source, findings, err)
		}
	}
	// Reproduction fixtures keep directive text in strings; build/embed/compiler controls remain usable.
	safe := "package assembly\n\t//line /ignored.go:100\nconst Fixture=`//line /display.go:70`\n// ordinary prose mentioning //line is not a directive\n"
	findings, err := repopolicy.DirectivesIn("fixture.go", []byte(safe))
	if err != nil || len(findings) != 0 {
		t.Fatalf("literal fixture rejected: %+v %v", findings, err)
	}
}
