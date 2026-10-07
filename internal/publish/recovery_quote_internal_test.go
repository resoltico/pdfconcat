// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package publish

import (
	"strings"
	"testing"
)

func TestWindowsRecoveryQuotesLiteralNamesInPowerShellAndNeverOverwrites(t *testing.T) {
	t.Parallel()

	source := `C:\Ģimene\100% %VAR% & ! it's.partial`
	target := `C:\Ģimene\target%PATH%! & one's.json`
	got := recoveryInstruction("windows", source, target)

	want := `[System.IO.File]::Copy('C:\Ģimene\100% %VAR% & ! it''s.partial', 'C:\Ģimene\target%PATH%! & one''s.json', $false)`
	if !strings.Contains(got, want) || !strings.Contains(got, "PowerShell") || !strings.Contains(got, "distinct unused report target") {
		t.Fatalf("unsafe/ambiguous recovery instruction: %s", got)
	}

	if strings.Contains(got, "copy /-Y") || strings.Contains(got, "$true") {
		t.Fatal("unsafe Windows recovery command remains")
	}
}
