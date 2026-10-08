// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly_test

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

// hostPath writes a slash-separated absolute path in the form of the running host.
func hostPath(slashed string) string {
	if runtime.GOOS == windowsName {
		return filepath.FromSlash("C:" + slashed)
	}

	return slashed
}

func TestClassifyPath(t *testing.T) {
	t.Parallel()

	const (
		posix   = assembly.PathStylePOSIX
		windows = assembly.PathStyleWindows
	)

	rows := []struct {
		name  string
		path  string
		style assembly.PathStyle
		want  assembly.PathClass
	}{
		{"posix absolute", "/usr/a.pdf", posix, assembly.PathAbsolute},
		{"posix root", "/", posix, assembly.PathAbsolute},
		{"posix relative", "a/b.pdf", posix, assembly.PathRelative},
		{"posix empty", "", posix, assembly.PathRelative},
		{"posix parent segment", "../a.pdf", posix, assembly.PathRelative},
		{"posix backslash is a name character", `\a.pdf`, posix, assembly.PathRelative},
		{"posix drive-shaped is a relative name", `C:\a.pdf`, posix, assembly.PathRelative},
		{"posix colon name", "C:a.pdf", posix, assembly.PathRelative},
		{"posix directive shaped", blankOperand, posix, assembly.PathRelative},
		{"posix unicode", "Ābols/ļ.pdf", posix, assembly.PathRelative},
		{"posix spaces and metacharacters", "a b/$HOME*?[x].pdf", posix, assembly.PathRelative},
		{"posix unc-shaped", "//server/share/a.pdf", posix, assembly.PathAbsolute},
		{"windows drive backslash", `C:\x`, windows, assembly.PathAbsolute},
		{"windows drive slash", "C:/x", windows, assembly.PathAbsolute},
		{"windows lower-case drive", `d:\x`, windows, assembly.PathAbsolute},
		{"windows unc", `\\server\share\x`, windows, assembly.PathAbsolute},
		{"windows unc slashes", "//server/share/x", windows, assembly.PathAbsolute},
		{"windows verbatim", `\\?\C:\x`, windows, assembly.PathAbsolute},
		{"windows drive relative", driveRelativePath, windows, assembly.PathDriveRelative},
		{"windows drive only", "C:", windows, assembly.PathDriveRelative},
		{"windows drive parent segment", `C:..\x`, windows, assembly.PathDriveRelative},
		{"windows rooted backslash", `\x`, windows, assembly.PathRootedWithoutDrive},
		{"windows rooted slash", "/x", windows, assembly.PathRootedWithoutDrive},
		{"windows relative", `a\b.pdf`, windows, assembly.PathRelative},
		{"windows relative slash", "a/b.pdf", windows, assembly.PathRelative},
		{"windows empty", "", windows, assembly.PathRelative},
		{"windows single character", "a", windows, assembly.PathRelative},
		{"windows digit colon", "1:x", windows, assembly.PathRelative},
		{"windows colon after two characters", "ab:c", windows, assembly.PathRelative},
		{"windows single-letter name is a drive", "a:b", windows, assembly.PathDriveRelative},
		{"windows unicode", "Ābols/ļ.pdf", windows, assembly.PathRelative},
		{"windows directive shaped", blankOperand, windows, assembly.PathRelative},
		{"windows dot slash directive shaped", `.\--blank`, windows, assembly.PathRelative},
	}

	for _, row := range rows {
		if got := assembly.ClassifyPath(row.style, row.path); got != row.want {
			t.Errorf("%s: ClassifyPath(%q) = %d, want %d", row.name, row.path, got, row.want)
		}
	}
}

func TestHostPathStyle(t *testing.T) {
	t.Parallel()

	want := assembly.PathStylePOSIX
	if runtime.GOOS == "windows" {
		want = assembly.PathStyleWindows
	}

	if got := assembly.HostPathStyle(); got != want {
		t.Errorf("HostPathStyle() = %d", got)
	}
}

func TestWindowsRulesRejectAmbiguousPathsOnEveryHost(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name, path string
		code       assembly.Code
	}{
		{"drive relative", "C:x.pdf", assembly.CodePathDriveRelative},
		{"drive only", "C:", assembly.CodePathDriveRelative},
		{"rooted", `\x.pdf`, assembly.CodePathRootedWithoutDrive},
		{"rooted slash", "/x.pdf", assembly.CodePathRootedWithoutDrive},
	}

	for _, row := range rows {
		code, got, err := assembly.ResolvePathWithStyle(assembly.PathStyleWindows, baseDirectory, pdfPathLabel, row.path)
		if code != row.code || got != "" || !errors.Is(err, assembly.ErrInvalidPath) {
			t.Errorf("%s: code %q path %q err %v", row.name, code, got, err)

			continue
		}

		if msg := err.Error(); !contains(msg, pdfPathLabel) || !contains(msg, row.path) || !contains(msg, `C:\folder\file`) {
			t.Errorf("%s: message does not name the field, the value, and the fix: %q", row.name, msg)
		}
	}
}

func TestResolvePath(t *testing.T) {
	t.Parallel()

	base := hostPath("/work/plan")

	rows := []struct {
		name, in, want string
	}{
		{"relative", aPath, "/work/plan/a.pdf"},
		{"nested relative", "x/y/a.pdf", "/work/plan/x/y/a.pdf"},
		{"parent segment", "../a.pdf", "/work/a.pdf"},
		{"parent segment inside", "x/../a.pdf", "/work/plan/a.pdf"},
		{"parent segment above the root", "../../../../a.pdf", "/a.pdf"},
		{"unicode", "Ābols ļ/ž.pdf", "/work/plan/Ābols ļ/ž.pdf"},
		{"spaces", "a b  c.pdf", "/work/plan/a b  c.pdf"},
		{"metacharacters are literal", "$HOME/*?[a]~%PATH%.pdf", "/work/plan/$HOME/*?[a]~%PATH%.pdf"},
		{"directive shaped", blankOperand, "/work/plan/--blank"},
		{"dot slash directive shaped", "./--blank", "/work/plan/--blank"},
		{"url-like is a path", "file:///a.pdf", "/work/plan/file:/a.pdf"},
		{"empty is the base", "", "/work/plan"},
		{"redundant separators", "x//y/./a.pdf", "/work/plan/x/y/a.pdf"},
		{"absolute stands alone", hostPath("/abs/a.pdf"), "/abs/a.pdf"},
	}

	for _, row := range rows {
		got, err := assembly.ResolvePath(base, pdfPathLabel, row.in)
		want := filepath.FromSlash(row.want)

		if runtime.GOOS == "windows" {
			want = filepath.FromSlash("C:" + row.want)
		}

		if err != nil || got != want {
			t.Errorf("%s: ResolvePath(%q) = %q, %v; want %q", row.name, row.in, got, err, want)
		}
	}
}

func TestResolvePathRejectsBadInput(t *testing.T) {
	t.Parallel()

	_, err := assembly.ResolvePath(hostPath("/b"), pdfPathLabel, "a\x00.pdf")
	if !errors.Is(err, assembly.ErrInvalidPath) {
		t.Errorf("NUL: %v", err)
	}

	for _, base := range []string{"", "rel/dir", "."} {
		_, err = assembly.ResolvePath(base, pdfPathLabel, aPath)
		if !errors.Is(err, assembly.ErrBaseNotAbsolute) {
			t.Errorf("base %q: %v", base, err)
		}
	}
}
