// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type (
	// LinkedModule is a third-party module linked into a release binary.
	LinkedModule struct {
		Path    string
		Version string
		Dir     string
	}

	// noticeRow is one row of the THIRD_PARTY_NOTICES.md table.
	noticeRow struct {
		component string
		version   string
		copies    []string
	}
)

const (
	standardLibraryName = "Go standard library"
	// listFields is the number of fields in a line of the module listing: path, version, directory.
	listFields = 3
	// licenseDir is where the shipped license texts live, relative to the repository root.
	licenseDir = "third_party/licenses"
)

var (
	noticeRowPattern = regexp.MustCompile("(?m)^\\| (?:`([^`]+)`|(Go standard library)) \\| ([^|]*) \\|[^|]*\\|[^|]*\\| ([^|]*) \\|$")
	licenseCopyPath  = regexp.MustCompile("`(third_party/licenses/[^`]+)`")
	licenseFileName  = regexp.MustCompile(`(?i)^(licen[cs]e|copying|notice)([-._].*)?$`)
	goDirective      = regexp.MustCompile(`(?m)^go (\S+)$`)
)

// releaseGOOS lists the operating systems whose release binaries the notices must cover.
func releaseGOOS() []string {
	return []string{"darwin", "linux", "windows"}
}

// LinkedModules returns the third-party modules linked into ./cmd/pdfconcat for each release
// operating system, ordered by path.
func LinkedModules(ctx context.Context, root, ownModule string) ([]LinkedModule, error) {
	found := map[string]LinkedModule{}

	for _, goos := range releaseGOOS() {
		format := "{{with .Module}}{{.Path}} {{.Version}} {{.Dir}}{{end}}"
		command := exec.CommandContext(ctx, "go", "list", "-deps", "-f", format, "./cmd/pdfconcat")
		command.Dir = root
		command.Env = append(command.Environ(), "GOOS="+goos, "GOFLAGS=-mod=readonly")

		output, err := command.Output()
		if err != nil {
			return nil, fmt.Errorf("go list for %s: %w", goos, err)
		}

		for line := range strings.SplitSeq(string(output), "\n") {
			fields := strings.SplitN(strings.TrimSpace(line), " ", listFields)
			if len(fields) == listFields && fields[0] != ownModule {
				found[fields[0]] = LinkedModule{Path: fields[0], Version: fields[1], Dir: fields[2]}
			}
		}
	}

	modules := make([]LinkedModule, 0, len(found))
	for _, module := range found {
		modules = append(modules, module)
	}

	slices.SortFunc(modules, func(a, b LinkedModule) int { return strings.Compare(a.Path, b.Path) })

	return modules, nil
}

// parseNoticeRows reads the component table; the component is the module path or standardLibraryName.
func parseNoticeRows(notices string) []noticeRow {
	matches := noticeRowPattern.FindAllStringSubmatch(notices, -1)
	rows := make([]noticeRow, 0, len(matches))

	for _, match := range matches {
		component := match[1]
		if component == "" {
			component = match[2]
		}

		row := noticeRow{component: component, version: strings.TrimSpace(match[3])}
		for _, copied := range licenseCopyPath.FindAllStringSubmatch(match[4], -1) {
			row.copies = append(row.copies, copied[1])
		}

		rows = append(rows, row)
	}

	return rows
}

// NoticeIssues verifies THIRD_PARTY_NOTICES.md against the modules actually linked into the release
// binaries: one row per linked module and no others, the version in each row, the existence of each
// listed license copy, and that every license file the module ships is reproduced byte-for-byte
// (up to whitespace) by a copy listed in its row. goroot is the Go toolchain root, whose LICENSE
// the standard-library row must reproduce when the toolchain ships one.
func NoticeIssues(root, goroot string, modules []LinkedModule) ([]string, error) {
	var problems []string

	err := withRoot(root, func(tree *os.Root) error {
		var checkErr error

		problems, checkErr = noticeProblems(tree, goroot, modules)

		return checkErr
	})
	if err != nil {
		return nil, err
	}

	return problems, nil
}

// noticeProblems implements NoticeIssues over an open repository root.
func noticeProblems(tree *os.Root, goroot string, modules []LinkedModule) ([]string, error) {
	noticeText, err := tree.ReadFile("THIRD_PARTY_NOTICES.md")
	if err != nil {
		return nil, fmt.Errorf("read notices: %w", err)
	}

	goMod, err := tree.ReadFile("go.mod")
	if err != nil {
		return nil, fmt.Errorf("read go.mod: %w", err)
	}

	rows := parseNoticeRows(string(noticeText))
	byComponent := map[string]noticeRow{}
	linked := map[string]bool{standardLibraryName: true}

	for _, row := range rows {
		byComponent[row.component] = row
	}

	var problems []string

	for _, module := range modules {
		linked[module.Path] = true
		problems = append(problems, moduleProblems(tree, module, byComponent)...)
	}

	for _, row := range rows {
		if !linked[row.component] {
			problems = append(problems, fmt.Sprintf("THIRD_PARTY_NOTICES.md lists %s, which is not linked into any release binary",
				row.component))
		}
	}

	problems = append(problems, standardLibraryProblems(tree, goroot, goMod, byComponent)...)

	return append(problems, orphanLicenseProblems(tree, string(noticeText))...), nil
}

// moduleProblems checks one linked module's row: presence, version and license texts.
func moduleProblems(tree *os.Root, module LinkedModule, rows map[string]noticeRow) []string {
	row, listed := rows[module.Path]
	if !listed {
		return []string{fmt.Sprintf("module %s is linked into a release binary but has no row in THIRD_PARTY_NOTICES.md", module.Path)}
	}

	var problems []string

	if row.version != module.Version {
		problems = append(problems, fmt.Sprintf("%s: notices say %s, go.mod selects %s", module.Path, row.version, module.Version))
	}

	return append(problems, licenseProblems(tree, module.Path, module.Dir, row)...)
}

// standardLibraryProblems checks the standard-library row against go.mod's Go version and the toolchain's LICENSE.
func standardLibraryProblems(tree *os.Root, goroot string, goMod []byte, rows map[string]noticeRow) []string {
	standard, hasStandard := rows[standardLibraryName]
	if !hasStandard {
		return []string{"THIRD_PARTY_NOTICES.md has no Go standard library row"}
	}

	var problems []string

	match := goDirective.FindSubmatch(goMod)
	if len(match) < 2 || !strings.Contains(standard.version, string(match[1])) {
		problems = append(problems, fmt.Sprintf("standard library row says %q but go.mod does not require that Go version",
			standard.version))
	}

	// The official Go distributions ship LICENSE in GOROOT; some packagers move it one directory up and
	// others drop it. A toolchain without the file cannot be compared, which is not a defect in the
	// notices, so the check is skipped there and enforced wherever an official toolchain runs (CI).
	for _, dir := range []string{goroot, filepath.Dir(goroot)} {
		_, err := os.Stat(filepath.Join(dir, "LICENSE"))
		if err == nil {
			return append(problems, licenseProblems(tree, standardLibraryName, dir, standard)...)
		}
	}

	return problems
}

// licenseProblems checks that the row's copies exist and cover the license files in dir.
func licenseProblems(tree *os.Root, name, dir string, row noticeRow) []string {
	var problems []string

	copies := make([]string, 0, len(row.copies))

	for _, copied := range row.copies {
		content, err := tree.ReadFile(filepath.FromSlash(copied))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: license copy %s: %v", name, copied, err))

			continue
		}

		copies = append(copies, normalizeLicense(content))
	}

	err := withRoot(dir, func(module *os.Root) error {
		problems = append(problems, shippedLicenseProblems(module, name, copies)...)

		return nil
	})
	if err != nil {
		problems = append(problems, fmt.Sprintf("%s: cannot read module directory: %v", name, err))
	}

	return problems
}

// shippedLicenseProblems requires each license file in a module directory to be reproduced by one of the copies.
func shippedLicenseProblems(module *os.Root, name string, copies []string) []string {
	entries, err := fs.ReadDir(module.FS(), ".")
	if err != nil {
		return []string{fmt.Sprintf("%s: cannot read module directory: %v", name, err)}
	}

	var problems []string

	shipped := 0

	for _, entry := range entries {
		if entry.IsDir() || !licenseFileName.MatchString(entry.Name()) {
			continue
		}

		shipped++

		content, readErr := module.ReadFile(entry.Name())
		if readErr != nil {
			problems = append(problems, fmt.Sprintf("%s: read %s: %v", name, entry.Name(), readErr))

			continue
		}

		if !slices.Contains(copies, normalizeLicense(content)) {
			problems = append(problems, fmt.Sprintf("%s: no listed license copy reproduces the module's %s", name, entry.Name()))
		}
	}

	if shipped == 0 {
		problems = append(problems, name+": the module ships no license file to compare the notices with")
	}

	return problems
}

// orphanLicenseProblems reports license texts under third_party that no row refers to.
func orphanLicenseProblems(tree *os.Root, notices string) []string {
	entries, err := fs.ReadDir(tree.FS(), licenseDir)
	if err != nil {
		return []string{fmt.Sprintf("read %s: %v", licenseDir, err)}
	}

	var problems []string

	for _, entry := range entries {
		if !strings.Contains(notices, licenseDir+"/"+entry.Name()) {
			problems = append(problems, licenseDir+"/"+entry.Name()+" is not referenced by THIRD_PARTY_NOTICES.md")
		}
	}

	return problems
}

// normalizeLicense makes license text comparable across line endings and trailing whitespace.
func normalizeLicense(content []byte) string {
	text := strings.ReplaceAll(string(bytes.TrimSpace(content)), "\r\n", "\n")

	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimRight(line, " \t")
	}

	return strings.Join(lines, "\n")
}
