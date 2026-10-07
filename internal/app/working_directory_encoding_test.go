// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app_test

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/app"
)

func TestPathCommandsRejectNonUnicodeWorkingDirectoryEnvironment(t *testing.T) {
	t.Parallel()
	actual := workDir(t)
	writePDF(t, actual, sourceA)

	for _, command := range []string{commandBuild, commandCheck, commandReport} {
		args := []string{command, sourceA}
		if command == commandBuild {
			args = append(args, outputFlag, outputFile)
		}

		env := app.Env{WorkingDir: filepath.Join(actual, string([]byte{0xff}))}
		result := executeWith(t.Context(), t, appOf(newFake(t)), env, args...)
		result.requireCode(t, 1, "working_directory_invalid_utf8")

		if !utf8.ValidString(result.stdout) || strings.ContainsRune(result.stdout, '�') {
			t.Fatal("invalid CWD escaped lossily")
		}
	}

	requireMissing(t, filepath.Join(actual, outputFile))
}
