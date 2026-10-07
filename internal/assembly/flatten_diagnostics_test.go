// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly_test

import (
	"strconv"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

// memberSource is a Source whose pointers name the member too, so a test can see which field a
// diagnostic was attached to.
type memberSource struct{ assembly.ArgumentSource }

func (memberSource) Pointer(ref assembly.Ref, member string) string {
	return "node" + strconv.FormatUint(uint64(ref), 10) + member
}

func TestFlattenLocatesAnUnusableBaseAtTheJobAndAnUnusableDirAtItsDeclaration(t *testing.T) {
	t.Parallel()

	jobOrigin := assembly.Origin{Ref: 2, Offset: 5}
	dirOrigin := assembly.Origin{Ref: 7, Offset: 10}

	relativeBase := &assembly.Job{
		Source: memberSource{}, Base: "relative", Items: []assembly.Item{pdfAt(1, aPath)}, Origin: jobOrigin,
		Dir: assembly.Set("sub", dirOrigin),
	}
	driveDir := &assembly.Job{
		Source: memberSource{}, Base: "/work", Items: []assembly.Item{pdfAt(1, aPath)}, Origin: jobOrigin,
		Dir: assembly.Set("C:sub", dirOrigin),
	}

	rows := []struct {
		name        string
		job         *assembly.Job
		wantCode    assembly.Code
		wantPointer string
		wantOffset  int64
	}{
		{"relative base is the job's problem", relativeBase, assembly.CodeBaseNotAbsolute, "node2", jobOrigin.Offset},
		{"unusable directory is the declaration's problem", driveDir, assembly.CodePathDriveRelative, "node7/dir", dirOrigin.Offset},
	}

	for _, row := range rows {
		_, err := assembly.FlattenWithStyle(row.job, assembly.PathStyleWindows)

		diagnostics := assembly.Diagnostics(err)
		if len(diagnostics) != 1 {
			t.Errorf(namedFailureFormat, row.name, err)

			continue
		}

		got := diagnostics[0]
		if got.Code != row.wantCode || got.Location.Pointer != row.wantPointer || got.Location.Offset != row.wantOffset {
			t.Errorf("%s: code %q at %+v", row.name, got.Code, got.Location)
		}
	}
}

func TestFlattenTreatsAnExplicitBuiltInFontAsNoFontFile(t *testing.T) {
	t.Parallel()

	style := assembly.BlankStyle{Text: assembly.TextStyle{Font: assembly.Set(assembly.Font{}, itemOrigin())}}

	flat, err := assembly.Flatten(argumentJob(mustBlankItem(t, 1, &style, 1)))
	if err != nil {
		t.Fatalf("Flatten: %v", err)
	}

	if len(flat.Fonts) != 0 {
		t.Errorf("fonts %+v, want none", flat.Fonts)
	}
}
