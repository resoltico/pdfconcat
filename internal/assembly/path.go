// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"
)

type (
	// PathStyle is a family of path syntax rules. Classification is pure string work, so the rules of
	// every style are testable on every host; only HostPathStyle ever decides a real resolution.
	PathStyle uint8

	// PathClass says how a path is anchored.
	PathClass uint8
)

const (
	// PathStylePOSIX is the syntax of Linux and macOS: only "/" separates, and only a leading "/" anchors.
	PathStylePOSIX PathStyle = 1
	// PathStyleWindows is the syntax of Windows: "/" and "\" separate; a drive letter and colon, or two
	// leading separators (UNC, verbatim, and device paths), anchor.
	PathStyleWindows PathStyle = 2

	// PathRelative is anchored to a base directory.
	PathRelative PathClass = 1
	// PathAbsolute is anchored to the filesystem root: a POSIX "/x", a Windows "C:\x" or "C:/x", or a UNC
	// "\\server\share\x".
	PathAbsolute PathClass = 2
	// PathDriveRelative is a Windows "C:x": relative to the current directory of drive C, which depends on
	// process state outside the job.
	PathDriveRelative PathClass = 3
	// PathRootedWithoutDrive is a Windows "\x" or "/x": rooted on the current drive, which depends on
	// process state outside the job.
	PathRootedWithoutDrive PathClass = 4

	driveColonLength = 2 // a drive letter and its colon
)

// ErrBaseNotAbsolute reports a base directory that is not an absolute path.
var ErrBaseNotAbsolute = errors.New("base directory is not absolute")

// CheckPathText rejects paths that JSON cannot represent losslessly or that no filesystem can hold.
func CheckPathText(path string) error {
	if !utf8.ValidString(path) {
		return fmt.Errorf("%w: a path must be valid UTF-8", ErrInvalidPath)
	}

	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("%w: a path must not contain a NUL character", ErrInvalidPath)
	}

	return nil
}

// JoinDir resolves name against the base directory dir lexically: an absolute name stands alone, an
// empty name is dir itself, and otherwise the two are joined with "/". No filesystem is consulted.
func JoinDir(dir, name string) string {
	switch {
	case dir == "":
		return name
	case name == "":
		return dir
	case filepath.IsAbs(name):
		return name
	default:
		return dir + "/" + name
	}
}

// HostPathStyle returns the style of the running operating system.
func HostPathStyle() PathStyle {
	if runtime.GOOS == "windows" {
		return PathStyleWindows
	}

	return PathStylePOSIX
}

func isSeparator(char byte) bool {
	return char == '/' || char == '\\'
}

// ClassifyPath says how path is anchored under the syntax rules of style. It reads the text only; it
// never consults a filesystem, so it makes no claim that a path means the same thing on another host.
func ClassifyPath(style PathStyle, path string) PathClass {
	if style != PathStyleWindows {
		if strings.HasPrefix(path, "/") {
			return PathAbsolute
		}

		return PathRelative
	}

	return classifyWindowsPath(path)
}

func classifyWindowsPath(path string) PathClass {
	switch {
	case len(path) >= 2 && isSeparator(path[0]) && isSeparator(path[1]):
		return PathAbsolute
	case path != "" && isSeparator(path[0]):
		return PathRootedWithoutDrive
	case len(path) >= driveColonLength && isASCIILetter(path[0]) && path[1] == ':':
		if len(path) > driveColonLength && isSeparator(path[2]) {
			return PathAbsolute
		}

		return PathDriveRelative
	default:
		return PathRelative
	}
}

func isASCIILetter(char byte) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
}

// pathForm checks that path, written in the field called what, is not anchored to process state.
func pathForm(style PathStyle, what, path string) (Code, error) {
	class := ClassifyPath(style, path)

	if class == PathDriveRelative {
		return CodePathDriveRelative, fmt.Errorf(
			"%w: %s %q names a drive without a folder, so it depends on the current folder of that drive; "+
				`write the full path with a separator after the colon (such as "C:\folder\file") or a path relative to the plan`,
			ErrInvalidPath, what, path)
	}

	if class == PathRootedWithoutDrive {
		return CodePathRootedWithoutDrive, fmt.Errorf(
			"%w: %s %q has a root but no drive, so it depends on the current drive; "+
				`write the full path with its drive (such as "C:\folder\file"), a UNC path, or a path relative to the plan`,
			ErrInvalidPath, what, path)
	}

	return "", nil
}

// ResolvePath resolves name against the absolute base directory base, lexically and under the rules of
// the host: an absolute name stands alone, and the result is cleaned. The text is a literal path, not
// a pattern or a URL, and no filesystem is consulted, so ".." is collapsed lexically. A name that depends
// on process state (a Windows "C:x" or "\x") is rejected, and what names the field in the message.
func ResolvePath(base, what, name string) (string, error) {
	_, path, err := resolvePathAs(HostPathStyle(), base, what, name)

	return path, err
}

// resolvePathAs is ResolvePath under the rules of style; it also returns the diagnostic code of a rejection.
func resolvePathAs(style PathStyle, base, what, name string) (Code, string, error) {
	err := CheckPathText(name)
	if err != nil {
		return CodeInvalidJob, "", fmt.Errorf("%s: %w", what, err)
	}

	code, err := pathForm(style, what, name)
	if err != nil {
		return code, "", err
	}

	if baseErr := CheckPathText(base); baseErr != nil {
		return CodeInvalidJob, "", fmt.Errorf("base directory: %w", baseErr)
	}

	if !filepath.IsAbs(base) {
		return CodeBaseNotAbsolute, "", fmt.Errorf("%w: %q must be an absolute path to resolve %s %q", ErrBaseNotAbsolute, base, what, name)
	}

	return "", filepath.Clean(JoinDir(base, name)), nil
}
