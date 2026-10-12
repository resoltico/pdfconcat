// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"fmt"
	"os/exec"
	"testing"
)

// Create an actual junction. Its traversal oracle is the independent native reference open.
func nativeDirectoryLink(t *testing.T, target, link string) error {
	output, err := exec.CommandContext(t.Context(), "cmd.exe", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("create native junction: %w: %s", err, output)
	}
	return nil
}
