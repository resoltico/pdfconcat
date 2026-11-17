// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdffixture

import (
	"testing"

	packet "github.com/resoltico/pdfconcat/internal/core"
)

// Plan admits a complete valid declaration for adapter tests.
func Plan(t testing.TB, d packet.Declaration) packet.Plan {
	t.Helper()
	p, ds := packet.AdmitDeclaration(d)
	if len(ds) > 0 {
		t.Fatalf("invalid test declaration: %+v", ds)
	}
	return p
}
