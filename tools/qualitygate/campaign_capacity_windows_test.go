// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"path/filepath"
	"testing"
)

func TestCampaignCapacityDirectoryPreservesNativeSpelling(t *testing.T) {
	t.Parallel()
	for _, item := range []struct{ name, path, want string }{
		{"empty", "", ""},
		{"drive", `C:\workspace`, `C:\workspace\`},
		{"terminated", `C:\workspace\`, `C:\workspace\`},
		{"UNC", `\\server\share`, `\\server\share\`},
		{"UNC-directory", `\\server\share\directory`, `\\server\share\directory\`},
		{"extended-UNC", `\\?\UNC\server\share`, `\\?\UNC\server\share\`},
		{"extended-drive", `\\?\C:\directory`, `\\?\C:\directory\`},
		{"native-parent-traversal", `C:\logical\link\..`, `C:\logical\link\..\`},
		{"forward-separators", `C:/directory/`, `C:/directory/\`},
	} {
		t.Run(item.name, func(t *testing.T) {
			if got := campaignCapacityDirectory(item.path); got != item.want {
				t.Fatalf("native capacity spelling changed: %q, want %q", got, item.want)
			}
		})
	}
}

func TestCampaignCapacityNativeDirectoryTerminationAndInvalidInputs(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	for _, path := range []string{directory, directory + `\`} {
		if available, err := availableCampaignBytes(path); err != nil || available == 0 {
			t.Fatalf("native directory query %q: %d/%v", path, available, err)
		}
	}
	for _, path := range []string{"", "invalid\x00directory", filepath.Join(directory, "missing")} {
		if available, err := availableCampaignBytes(path); err == nil {
			t.Fatalf("invalid native capacity path %q accepted: %d", path, available)
		}
	}
}
