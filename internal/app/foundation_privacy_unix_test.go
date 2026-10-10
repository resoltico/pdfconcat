// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
//go:build unix

package app_test

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestPublishedPDFIsPrivateUnderPermissiveUmask(t *testing.T) {
	for _, mask := range []int{0022, 0000} {
		t.Run(fmt.Sprintf("%04o", mask), func(t *testing.T) {
			dir := workDir(t)
			writePDF(t, dir, "source.pdf")
			old := syscall.Umask(mask)
			defer syscall.Umask(old)
			for _, overwrite := range []bool{false, true} {
				target := filepath.Join(dir, "output.pdf")
				if overwrite {
					if err := os.Chmod(target, 0600); err != nil {
						t.Fatal(err)
					}
				}
				args := []string{"build", "-o", target, "source.pdf"}
				if overwrite {
					args = append(args, "--overwrite")
				}
				execute(t.Context(), t, appOf(newFake(t)), dir, args...).requireCode(t, 0, "")
				info, err := os.Stat(target)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0600 {
					t.Errorf("overwrite=%t published mode=%o", overwrite, info.Mode().Perm())
				}
			}
		})
	}
}

func TestArtifactParentPreservesLiteralBackslashDirectory(t *testing.T) {
	dir := workDir(t)
	parent := filepath.Join(dir, `literal\`)
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	writePDF(t, dir, "source.pdf")
	output := parent + "/output.pdf"
	execute(t.Context(), t, appOf(newFake(t)), dir, "build", "-o", output, "source.pdf").requireCode(t, 0, "")
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("literal native parent lost: %v", err)
	}
}
