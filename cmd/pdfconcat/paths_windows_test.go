// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main_test

import "testing"

func TestDriveRelativePathsAreRejectedWithTheirLocation(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")

	for name, plan := range map[string]string{
		"source":  planJSON(t, itemsOf(`C:a.pdf`)),
		keyOutput: planJSON(t, obj{keyVersion: 1, keyOutput: `C:out.pdf`, keyItems: []any{fileA}}),
		"rooted":  planJSON(t, itemsOf(`\a.pdf`)),
	} {
		res := run(t, dir, "", commandCheck, inlinePlanFlag, plan)
		requireExit(t, res, 2)

		parsed := summaryOf(t, res)
		if parsed.Diagnostics[0].Location == nil || parsed.Diagnostics[0].Location.Pointer == "" {
			t.Errorf(namedDiagnosticFormat, name, parsed.Diagnostics[0])
		}
	}
}
