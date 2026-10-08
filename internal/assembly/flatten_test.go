// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

func sizeStyle(width, height float64) assembly.BlankStyle {
	dim := assembly.PageDim{Width: assembly.Length(width), Height: assembly.Length(height)}

	return assembly.BlankStyle{Size: assembly.Set(assembly.PageSize{Dim: dim}, itemOrigin())}
}

func textStyle(text string) assembly.BlankStyle {
	return assembly.BlankStyle{Text: assembly.TextStyle{Value: assembly.Set(text, itemOrigin())}}
}

func mustFlatten(tb testing.TB, job *assembly.Job) *assembly.Flattened {
	tb.Helper()

	flat, err := assembly.Flatten(job)
	if err != nil {
		tb.Fatalf("Flatten: %v", err)
	}

	return flat
}

func codesOf(err error) []assembly.Code {
	diagnostics := assembly.Diagnostics(err)
	codes := make([]assembly.Code, 0, len(diagnostics))

	for _, diagnostic := range diagnostics {
		codes = append(codes, diagnostic.Code)
	}

	return codes
}

func TestFlattenOrderPathsAndTables(t *testing.T) {
	t.Parallel()

	blank := mustBlankItem(t, 4, &assembly.BlankStyle{}, 3)
	group := assembly.NewGroupItem(assembly.Origin{Ref: 5}, "chapters", []assembly.Item{
		pdfAt(6, "c1.pdf"),
		assembly.NewGroupItem(assembly.Origin{Ref: 7}, "../shared", []assembly.Item{pdfAt(8, "s.pdf")}),
		pdfAt(9, hostPath("/abs/x.pdf")),
	})

	job := argumentJob(pdfAt(1, aPath), blank, pdfAt(2, aPath), group, mustBlankItem(t, 10, &assembly.BlankStyle{}, 2))
	job.Base = hostPath(workingDirectory)
	job.Dir = assembly.Set("top", defaultsOrigin())

	flat := mustFlatten(t, job)

	checkContributionPaths(t, flat)
	checkSharedFile(t, flat)
	checkSharedStyle(t, flat)

	blankCounts := flat.Contributions[1].Count == 3 && flat.Contributions[1].File == -1
	if flat.GeneratedPages != 5 || !blankCounts || flat.Contributions[0].Count != 1 {
		t.Errorf("counts %d %+v", flat.GeneratedPages, flat.Contributions)
	}
}

// checkContributionPaths verifies the order and resolved path of every contribution.
func checkContributionPaths(t *testing.T, flat *assembly.Flattened) {
	t.Helper()

	got := make([]string, 0, len(flat.Contributions))

	for index, contribution := range flat.Contributions {
		if contribution.Index != index {
			t.Errorf("Index %d at %d", contribution.Index, index)
		}

		got = append(got, contribution.Kind.String()+":"+filepath.ToSlash(contribution.Path))
	}

	want := []string{
		"pdf:/cwd/top/a.pdf", "blank:", "pdf:/cwd/top/a.pdf", "pdf:/cwd/top/chapters/c1.pdf",
		"pdf:/cwd/top/shared/s.pdf", "pdf:/abs/x.pdf", "blank:",
	}

	if len(got) != len(want) {
		t.Fatalf("contributions %v, want %v", got, want)
	}

	for index := range want {
		wantPath := want[index]
		if path, isPDF := strings.CutPrefix(wantPath, "pdf:"); isPDF {
			wantPath = "pdf:" + filepath.ToSlash(hostPath(path))
		}

		if got[index] != wantPath {
			t.Errorf("contribution %d = %q, want %q", index, got[index], wantPath)
		}
	}
}

func checkSharedFile(t *testing.T, flat *assembly.Flattened) {
	t.Helper()

	sharedOnce := flat.Contributions[0].File == 0 && flat.Contributions[2].File == 0
	if len(flat.Files) != 4 || flat.Files[0].Uses != 2 || !sharedOnce {
		t.Errorf("repeated source must be one file with two uses: %+v", flat.Files)
	}

	if flat.Files[0].FirstUse != (assembly.Origin{Ref: 1}) {
		t.Errorf("first use %+v", flat.Files[0].FirstUse)
	}
}

func checkSharedStyle(t *testing.T, flat *assembly.Flattened) {
	t.Helper()

	sharedOnce := flat.Contributions[1].Style == 0 && flat.Contributions[6].Style == 0
	if len(flat.Styles) != 1 || !sharedOnce || flat.Contributions[0].Style != -1 {
		t.Errorf("identical appearances must share one style entry: %+v", flat.Styles)
	}
}

func TestFlattenLayersDefaultsAndFonts(t *testing.T) {
	t.Parallel()

	rootFont, err := assembly.FontFile("fonts/root.ttf", hostPath(workingDirectory))
	if err != nil {
		t.Fatal(err)
	}

	groupFont, err := assembly.FontFile("g.ttf", hostPath("/cwd/grp"))
	if err != nil {
		t.Fatal(err)
	}

	withFont := assembly.BlankStyle{Text: assembly.TextStyle{Font: assembly.Set(groupFont, itemOrigin())}}
	inGroup := assembly.NewGroupItem(assembly.Origin{Ref: 5}, "grp", []assembly.Item{
		mustBlankItem(t, 6, &withFont, 1),
		mustBlankItem(t, 7, &assembly.BlankStyle{}, 1),
	})

	job := argumentJob(mustBlankItem(t, 1, &assembly.BlankStyle{}, 1), inGroup, mustBlankItem(t, 3, &withFont, 1))
	job.Base = hostPath(workingDirectory)
	job.Defaults = assembly.BlankStyle{
		Text: assembly.TextStyle{Font: assembly.Set(rootFont, defaultsOrigin()), Value: assembly.Set("hi", defaultsOrigin())},
	}

	flat := mustFlatten(t, job)
	if job.Defaults.Text.Font.Value != rootFont {
		t.Fatal("font resolution mutated the retained default declaration")
	}

	if len(flat.Styles) != 2 {
		t.Fatalf("styles %+v", flat.Styles)
	}

	rootAbs, groupAbs := filepath.Clean(hostPath("/cwd/fonts/root.ttf")), filepath.Clean(hostPath("/cwd/grp/g.ttf"))

	checkLayeredStyles(t, flat, rootAbs, groupAbs)

	// Contribution 1 and 3 share the group font style, the item in group uses it too: three blanks, two styles.
	if flat.Contributions[1].Style != 1 || flat.Contributions[2].Style != 0 || flat.Contributions[3].Style != 1 {
		t.Errorf("styles %+v", flat.Contributions)
	}

	if len(flat.Fonts) != 2 || flat.Fonts[0].Path != rootAbs || flat.Fonts[1].Path != groupAbs {
		t.Errorf("fonts in first-use order: %+v", flat.Fonts)
	}
}

func checkLayeredStyles(t *testing.T, flat *assembly.Flattened, rootAbs, groupAbs string) {
	t.Helper()

	first, second := flat.Styles[0].Style, flat.Styles[1].Style
	if first.Text.Font.Value != (assembly.Font{File: rootAbs}) || second.Text.Font.Value != (assembly.Font{File: groupAbs}) {
		t.Errorf("fonts keep the base of the scope that declared them: %+v %+v", first.Text.Font, second.Text.Font)
	}

	if first.Text.Value.Value != "hi" || second.Text.Value.Value != "hi" || first.Text.Value.Origin != defaultsOrigin() {
		t.Errorf("defaults layer under the item, declaration origins survive: %+v", first.Text.Value)
	}
}

func TestFlattenProblemsAreAllReported(t *testing.T) {
	t.Parallel()

	badDefaults := assembly.BlankStyle{Text: assembly.TextStyle{Leading: assembly.Set(0.5, defaultsOrigin())}}
	driveFont := assembly.Font{File: "C:f.ttf", Base: workingDirectory}
	fontStyle := assembly.BlankStyle{Text: assembly.TextStyle{Font: assembly.Set(driveFont, itemOrigin())}}

	badDir := assembly.NewGroupItem(assembly.Origin{Ref: 4}, `C:grp`, []assembly.Item{pdfAt(5, "skipped.pdf")})
	job := argumentJob(
		pdfAt(1, "C:a.pdf"), pdfAt(2, `\b.pdf`), pdfAt(3, "ok.pdf"), badDir,
		mustBlankItem(t, 6, &fontStyle, 1), mustBlankItem(t, 7, &fontStyle, 1), mustBlankItem(t, 8, &assembly.BlankStyle{}, 1),
	)
	job.Defaults = badDefaults

	_, err := assembly.FlattenWithStyle(job, assembly.PathStyleWindows)
	got := codesOf(err)
	want := []assembly.Code{
		assembly.CodePathDriveRelative, assembly.CodePathRootedWithoutDrive, assembly.CodePathDriveRelative,
		assembly.CodePathDriveRelative, assembly.CodeStyleInvalid,
	}

	if len(got) != len(want) {
		t.Fatalf("codes %v, want %v (%v)", got, want, err)
	}

	for index := range want {
		if got[index] != want[index] {
			t.Errorf("code %d = %s, want %s", index, got[index], want[index])
		}
	}

	list := assembly.Diagnostics(err)
	if list[0].Location.Pointer != "argv:1" || list[0].Stage != assembly.StagePath || !errors.Is(err, assembly.ErrInvalidPath) {
		t.Errorf(detailedValueFormat, list[0])
	}

	if list[2].Location.Pointer != "argv:4" || list[3].Location.Pointer != argumentThree || list[4].Location.Pointer != "argv:8" {
		t.Errorf("locations %v %v %v", list[2].Location, list[3].Location, list[4].Location)
	}

	if list[4].Stage != assembly.StageStyle || list[0].Affected != 1 {
		t.Errorf(detailedValueFormat, list[4])
	}
}

func TestFlattenRejectsStructureAndBase(t *testing.T) {
	t.Parallel()

	_, err := assembly.Flatten(&assembly.Job{Items: []assembly.Item{pdfAt(1, aPath)}})
	if !errors.Is(err, assembly.ErrInvalidJob) {
		t.Errorf("no source: %v", err)
	}

	_, err = assembly.Flatten(argumentJob())
	if got := codesOf(err); len(got) != 1 || got[0] != assembly.CodeInvalidJob || !errors.Is(err, assembly.ErrInvalidJob) {
		t.Errorf("empty job: %v", err)
	}

	relative := argumentJob(pdfAt(1, aPath))
	relative.Base = "rel"
	relative.Origin = assembly.Origin{Ref: 0}

	_, err = assembly.Flatten(relative)
	if got := codesOf(err); len(got) != 1 || got[0] != assembly.CodeBaseNotAbsolute || !errors.Is(err, assembly.ErrBaseNotAbsolute) {
		t.Errorf("relative base: %v", err)
	}

	drive := argumentJob(pdfAt(1, aPath))
	drive.Dir = assembly.Set(driveRelativePath, defaultsOrigin())

	_, err = assembly.FlattenWithStyle(drive, assembly.PathStyleWindows)
	if got := codesOf(err); len(got) != 1 || got[0] != assembly.CodePathDriveRelative {
		t.Errorf("drive-relative job dir: %v", err)
	}
}

func TestDiagnostics(t *testing.T) {
	t.Parallel()

	if assembly.Diagnostics(nil) != nil {
		t.Error("nil error has diagnostics")
	}

	plain := assembly.Diagnostics(errBoom)
	if len(plain) != 1 || plain[0].Message != "boom" || plain[0].Error() == "" {
		t.Errorf(detailedValueFormat, plain)
	}

	one := assembly.Errors{{Message: "m", Code: "c", Location: assembly.Location{Source: "s"}}}
	if one.Error() != "s: m [c]" || (assembly.Errors{}).Error() != "no diagnostics" {
		t.Errorf("%q", one.Error())
	}

	two := assembly.Errors{one[0], one[0]}
	if two.Error() != "s: m [c] (and 1 more problems)" || len(two.Unwrap()) != 2 {
		t.Errorf("%q", two.Error())
	}

	if !errors.Is(error(two), two[1]) {
		t.Error("errors.Is must see every diagnostic")
	}
}

func TestNewOperandJob(t *testing.T) {
	t.Parallel()

	job, err := assembly.NewOperandJob(hostPath(workingDirectory), []assembly.Operand{
		{Position: 0, Path: aPath},
		{Position: 1, Blank: true},
		{Position: 3, Path: blankOperand},
		{Position: 4, Path: hostPath("/x/y.pdf")},
	})
	if err != nil {
		t.Fatal(err)
	}

	flat := mustFlatten(t, job)

	if len(flat.Contributions) != 4 || flat.Contributions[1].Kind != assembly.ItemBlank || flat.Contributions[1].Count != 1 {
		t.Fatalf(detailedValueFormat, flat.Contributions)
	}

	if want := filepath.Join(hostPath(workingDirectory), blankOperand); flat.Contributions[2].Path != want {
		t.Errorf("literal directive-shaped name: %q, want %q", flat.Contributions[2].Path, want)
	}

	location := assembly.Locate(job.Source, flat.Contributions[2].Origin, "")
	if location.Pointer != "argv:3" || location.Source != "argv" {
		t.Errorf(detailedValueFormat, location)
	}

	for _, position := range []int{-1, 1 << 32} {
		_, err = assembly.NewOperandJob("/c", []assembly.Operand{{Position: position, Path: aPath}})
		if !errors.Is(err, assembly.ErrOutOfRange) {
			t.Errorf("position %d: %v", position, err)
		}
	}

	if _, err = assembly.NewOperandJob("/c", nil); !errors.Is(err, assembly.ErrInvalidJob) {
		t.Errorf("no operands: %v", err)
	}
}
