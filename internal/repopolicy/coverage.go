// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"bufio"
	"errors"
	"fmt"
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

	// CoverageResult reports raw statement counts and per-package measurements.
	CoverageResult struct {
		Packages []PackageCoverage
		Problems []string
		Raw      PackageCoverage
	}
	// CoverageInput supplies the merged profile and its module prefix.
	CoverageInput struct {
		Profile *CoverageProfile
		Module  string
	}
	// SourceReader returns one owned repository source file.
	SourceReader func(file string) ([]byte, error)
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

// EvaluateCoverage counts every measured statement without exclusions or a global threshold.
func EvaluateCoverage(input CoverageInput) CoverageResult {
	var result CoverageResult
	byPackage := map[string]*PackageCoverage{}
	for _, block := range input.Profile.Blocks {
		covered := 0
		if block.Count > 0 {
			covered = block.Stmts
		}
		result.Raw.Total += block.Stmts
		result.Raw.Covered += covered
		name := strings.TrimPrefix(path.Dir(block.File), input.Module+"/")
		if byPackage[name] == nil {
			byPackage[name] = &PackageCoverage{Package: name}
		}
		byPackage[name].Total += block.Stmts
		byPackage[name].Covered += covered
	}
	for _, name := range slices.Sorted(maps.Keys(byPackage)) {
		result.Packages = append(result.Packages, *byPackage[name])
	}
	if result.Raw.Total == 0 {
		result.Problems = append(result.Problems, "the profile contains no statements; nothing was measured")
	}
	return result
}
