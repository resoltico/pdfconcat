// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy

import (
	"bufio"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
)

type (
	// Block is one statement block of a Go coverage profile.
	Block struct {
		File      string
		StartLine int
		StartCol  int
		EndLine   int
		EndCol    int
		Stmts     int
		Count     int
	}

	// CoverageProfile is a merged text-format coverage profile.
	CoverageProfile struct {
		Mode   string
		Blocks []Block
	}

	// PackageCoverage is the statement coverage of one package.
	PackageCoverage struct {
		Package string
		Total   int
		Covered int
	}

	// CoverageResult is the outcome of applying the registry to a merged profile.
	CoverageResult struct {
		// Packages lists the adjusted coverage of each package, ordered by name.
		Packages []PackageCoverage
		// Problems lists registry entries that no longer describe the profile and other gate failures.
		Problems []string
		// NotEvaluated lists entries skipped because they apply to another operating system.
		NotEvaluated []string
		// Raw is the coverage of every statement, before any exclusion.
		Raw PackageCoverage
		// Adjusted is the coverage of the reachable statements: the statements left after exclusions.
		Adjusted PackageCoverage
		// Excluded is the number of statements removed by registry entries.
		Excluded int
	}

	// CoverageInput is everything EvaluateCoverage judges.
	CoverageInput struct {
		// Profile is the merged unit and executable coverage.
		Profile *CoverageProfile
		// Read returns the repository's source files.
		Read SourceReader
		// Module is the module path that prefixes the profile's file names.
		Module string
		// GOOS is the operating system the profile was measured on.
		GOOS string
		// Entries are the registry entries to apply; entries of other tools are ignored.
		Entries []*Entry
		// Threshold is the minimum reachable coverage in percent.
		Threshold float64
	}

	// SourceReader returns the content of a repository file given its slash-separated path.
	SourceReader func(file string) ([]byte, error)

	// coverageScope is what a coverage evaluation applies the registry to.
	coverageScope struct {
		profile *CoverageProfile
		read    SourceReader
		module  string
		goos    string
	}
)

const (
	// percentOf is the scale of a coverage percentage.
	percentOf = 100.0
	// profileFields is the number of space-separated fields after the file name in a profile line.
	profileFields = 3
	// scannerBuffer is the initial size of the profile scanner's buffer.
	scannerBuffer = 1 << 16
	// scannerMaxLine bounds one profile line.
	scannerMaxLine = 1 << 24
)

// ErrProfile reports a malformed coverage profile or an unmergeable pair of profiles.
var ErrProfile = errors.New("coverage profile")

// ParseCoverageProfile reads a text-format coverage profile as written by `go test -coverprofile`
// or `go tool covdata textfmt`.
func ParseCoverageProfile(reader io.Reader) (*CoverageProfile, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, scannerBuffer), scannerMaxLine)

	var profile CoverageProfile

	for scanner.Scan() {
		line := scanner.Text()

		mode, isMode := strings.CutPrefix(line, "mode: ")
		if isMode {
			profile.Mode = mode

			continue
		}

		if strings.TrimSpace(line) == "" {
			continue
		}

		block, err := parseBlock(line)
		if err != nil {
			return nil, err
		}

		profile.Blocks = append(profile.Blocks, block)
	}

	err := scanner.Err()
	if err != nil {
		return nil, fmt.Errorf("%w: read: %w", ErrProfile, err)
	}

	if profile.Mode == "" {
		return nil, fmt.Errorf("%w: no mode line", ErrProfile)
	}

	return &profile, nil
}

// parseBlock parses "file:startLine.startCol,endLine.endCol statements count".
func parseBlock(line string) (Block, error) {
	file, rest, found := strings.CutLast(line, ":")
	if !found {
		return Block{}, fmt.Errorf("%w: malformed line %q", ErrProfile, line)
	}

	fields := strings.Fields(rest)
	if len(fields) != profileFields {
		return Block{}, fmt.Errorf("%w: malformed line %q", ErrProfile, line)
	}

	start, end, hasEnd := strings.Cut(fields[0], ",")
	if !hasEnd {
		return Block{}, fmt.Errorf("%w: malformed position in %q", ErrProfile, line)
	}

	block := Block{File: file}

	var parseErrs [4]error

	block.StartLine, block.StartCol, parseErrs[0] = parsePosition(start)
	block.EndLine, block.EndCol, parseErrs[1] = parsePosition(end)
	block.Stmts, parseErrs[2] = strconv.Atoi(fields[1])
	block.Count, parseErrs[3] = strconv.Atoi(fields[2])

	err := errors.Join(parseErrs[:]...)
	if err != nil {
		return Block{}, fmt.Errorf("%w: malformed line %q: %w", ErrProfile, line, err)
	}

	return block, nil
}

func parsePosition(text string) (int, int, error) {
	lineText, colText, found := strings.Cut(text, ".")
	if !found {
		return 0, 0, fmt.Errorf("%w: position %q has no column", ErrProfile, text)
	}

	line, err := strconv.Atoi(lineText)
	if err != nil {
		return 0, 0, fmt.Errorf("line of %q: %w", text, err)
	}

	col, err := strconv.Atoi(colText)
	if err != nil {
		return 0, 0, fmt.Errorf("column of %q: %w", text, err)
	}

	return line, col, nil
}

// MergeProfiles sums execution counts of identical blocks across profiles that share one mode
// and one set of instrumented packages, as unit-test and executable-subprocess profiles do.
func MergeProfiles(profiles ...*CoverageProfile) (*CoverageProfile, error) {
	merged := &CoverageProfile{}
	index := map[Block]int{}

	for _, profile := range profiles {
		if merged.Mode == "" {
			merged.Mode = profile.Mode
		}

		if profile.Mode != merged.Mode {
			return nil, fmt.Errorf("%w: cannot merge modes %q and %q", ErrProfile, merged.Mode, profile.Mode)
		}

		for _, block := range profile.Blocks {
			key := block
			key.Count = 0

			position, seen := index[key]
			if !seen {
				index[key] = len(merged.Blocks)
				merged.Blocks = append(merged.Blocks, block)

				continue
			}

			merged.Blocks[position].Count += block.Count
		}
	}

	return merged, nil
}

// Write renders the profile in text format.
func (p *CoverageProfile) Write(writer io.Writer) error {
	_, err := fmt.Fprintf(writer, "mode: %s\n", p.Mode)
	if err != nil {
		return fmt.Errorf("write coverage profile: %w", err)
	}

	for _, block := range p.Blocks {
		_, err = fmt.Fprintf(writer, "%s:%d.%d,%d.%d %d %d\n",
			block.File, block.StartLine, block.StartCol, block.EndLine, block.EndCol, block.Stmts, block.Count)
		if err != nil {
			return fmt.Errorf("write coverage profile: %w", err)
		}
	}

	return nil
}

// Percent returns covered statements as a percentage; an empty package counts as fully covered.
func (c PackageCoverage) Percent() float64 {
	if c.Total == 0 {
		return percentOf
	}

	return percentOf * float64(c.Covered) / float64(c.Total)
}

// EvaluateCoverage applies the coverage entries to the profile and checks the threshold. An entry is
// stale, and reported as a problem, when it matches no block or when a block it excludes was in fact
// executed: an exception must describe code the tests cannot reach.
func EvaluateCoverage(input CoverageInput) CoverageResult {
	scope := coverageScope{profile: input.Profile, read: input.Read, module: input.Module, goos: input.GOOS}
	excluded, problems, notEvaluated := scope.apply(input.Entries)
	result := scope.tally(excluded)

	result.Problems = problems
	result.NotEvaluated = notEvaluated

	switch {
	case result.Adjusted.Total == 0:
		result.Problems = append(result.Problems, "the profile contains no statements; nothing was measured")
	case result.Adjusted.Percent() < input.Threshold:
		result.Problems = append(result.Problems,
			fmt.Sprintf("coverage %.2f%% is below the threshold %.2f%%", result.Adjusted.Percent(), input.Threshold))
	default:
	}

	return result
}

// apply resolves each coverage entry to the blocks it excludes.
func (s coverageScope) apply(entries []*Entry) (map[int]bool, []string, []string) {
	excluded := map[int]bool{}

	var problems, notEvaluated []string

	for _, entry := range entries {
		if entry.Tool != ToolCoverage {
			continue
		}

		if entry.GOOS != "" && entry.GOOS != s.goos {
			notEvaluated = append(notEvaluated, entry.ID+" ("+entry.GOOS+" only)")

			continue
		}

		matched, problem := s.match(entry)
		if problem != "" {
			problems = append(problems, entry.ID+": "+problem)

			continue
		}

		for _, position := range matched {
			excluded[position] = true
		}
	}

	return excluded, problems, notEvaluated
}

// tally counts statements per package, separating raw from reachable ones.
func (s coverageScope) tally(excluded map[int]bool) CoverageResult {
	var result CoverageResult

	byPackage := map[string]*PackageCoverage{}

	for position, block := range s.profile.Blocks {
		covered := 0
		if block.Count > 0 {
			covered = block.Stmts
		}

		result.Raw.Total += block.Stmts
		result.Raw.Covered += covered

		if excluded[position] {
			result.Excluded += block.Stmts

			continue
		}

		result.Adjusted.Total += block.Stmts
		result.Adjusted.Covered += covered

		name := strings.TrimPrefix(path.Dir(block.File), s.module+"/")
		if byPackage[name] == nil {
			byPackage[name] = &PackageCoverage{Package: name}
		}

		byPackage[name].Total += block.Stmts
		byPackage[name].Covered += covered
	}

	for _, name := range slices.Sorted(maps.Keys(byPackage)) {
		result.Packages = append(result.Packages, *byPackage[name])
	}

	return result
}

// match returns the indexes of the blocks an entry excludes, or a problem when the entry is stale.
func (s coverageScope) match(entry *Entry) ([]int, string) {
	content, err := s.read(entry.Path)
	if err != nil {
		return nil, fmt.Sprintf("cannot read %s: %v", entry.Path, err)
	}

	first, last, err := functionLines(entry.Path, content, entry.Function)
	if err != nil {
		return nil, err.Error()
	}

	lines := strings.Split(string(content), "\n")

	var matched []int

	for position, block := range s.profile.Blocks {
		if block.File != s.module+"/"+entry.Path || block.StartLine < first || block.EndLine > last {
			continue
		}

		if strings.TrimSpace(lines[block.StartLine-1]) == entry.Anchor {
			matched = append(matched, position)
		}
	}

	if len(matched) == 0 {
		return nil, fmt.Sprintf(
			"no block of %s in %s starts at the line %q; the exception is stale",
			entry.Function,
			entry.Path,
			entry.Anchor,
		)
	}

	for _, position := range matched {
		if s.profile.Blocks[position].Count > 0 {
			return nil, fmt.Sprintf("the block at %s:%d is executed by the tests; delete the exception",
				entry.Path, s.profile.Blocks[position].StartLine)
		}
	}

	return matched, ""
}

// functionLines returns the first and last source line of the named function or method in a file.
func functionLines(file string, content []byte, name string) (int, int, error) {
	fset := token.NewFileSet()

	parsed, err := parser.ParseFile(fset, file, content, parser.SkipObjectResolution)
	if err != nil {
		return 0, 0, fmt.Errorf("parse %s: %w", file, err)
	}

	for _, declaration := range parsed.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if isFunction && functionName(function) == name {
			return fset.Position(function.Pos()).Line, fset.Position(function.End()).Line, nil
		}
	}

	return 0, 0, fmt.Errorf("%w: %s has no function %s", ErrProfile, file, name)
}

// functionName renders a declaration as Name or Receiver.Name, without pointer stars or type parameters.
func functionName(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return function.Name.Name
	}

	receiver := function.Recv.List[0].Type

	for {
		switch typed := receiver.(type) {
		case *ast.StarExpr:
			receiver = typed.X
		case *ast.IndexExpr:
			receiver = typed.X
		case *ast.IndexListExpr:
			receiver = typed.X
		case *ast.Ident:
			return typed.Name + "." + function.Name.Name
		default:
			return function.Name.Name
		}
	}
}
