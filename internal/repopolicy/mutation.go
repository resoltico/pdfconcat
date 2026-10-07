// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
)

type (
	// Mutant is one mutation gremlins generated.
	Mutant struct {
		File               string
		Type               string
		Status             string
		DiscoveryStatus    string
		ExecutionReference string
		Line               int
		Column             int
	}

	// MutationReport is a parsed gremlins JSON report.
	MutationReport struct {
		TerminationReason string
		Mutants           []Mutant
		Complete          bool
	}

	// MutationResult is the outcome of judging a report against the registry.
	MutationResult struct {
		// Counts maps each status to its number of mutants, including excluded and other-platform ones.
		Counts map[string]int
		// Problems lists the failures: survivors, timeouts, uncovered mutants, stale entries.
		Problems []string
		// Evaluated is the number of mutants judged: those in files compiled for the host platform.
		Evaluated int
		// OtherPlatform is the number of mutants in files not compiled for the host platform, which
		// only that platform's native run judges.
		OtherPlatform int
		// Accepted is the number of unfavourable mutants removed by registry entries.
		Accepted int
	}

	// mutationFile is one file of gremlins' JSON report.
	mutationFile struct {
		Name      string             `json:"file_name"`
		Mutations []mutationEntryRaw `json:"mutations"`
	}

	// mutationEntryRaw is one mutation of gremlins' JSON report.
	mutationEntryRaw struct {
		Type               string `json:"type"`
		Status             string `json:"status"`
		DiscoveryStatus    string `json:"discovery_status"`
		ExecutionReference string `json:"execution_reference"`
		Line               int    `json:"line"`
		Column             int    `json:"column"`
	}

	// mutationJSON is gremlins' JSON report.
	mutationJSON struct {
		TerminationReason string         `json:"termination_reason"`
		Complete          *bool          `json:"complete"`
		Files             []mutationFile `json:"files"`
	}
)

const (
	// Mutant statuses reported by gremlins.
	statusKilled     = "KILLED"
	statusLived      = "LIVED"
	statusNotCovered = "NOT COVERED"
	statusNotViable  = "NOT VIABLE"
	statusTimedOut   = "TIMED OUT"

	// summaryExtraLines is the number of summary lines after the per-status counts.
	summaryExtraLines = 2
)

// ErrMutation reports an unreadable mutation report.
var ErrMutation = errors.New("mutation report")

// MutationOperators lists every operator gremlins v0.6.0 offers; the gate enables all of them.
func MutationOperators() []string {
	return []string{
		"ARITHMETIC_BASE", "CONDITIONALS_BOUNDARY", "CONDITIONALS_NEGATION", "INCREMENT_DECREMENT",
		"INVERT_ASSIGNMENTS", "INVERT_BITWISE", "INVERT_BWASSIGN", "INVERT_LOGICAL", "INVERT_LOOPCTRL",
		"INVERT_NEGATIVES", "REMOVE_SELF_ASSIGNMENTS",
	}
}

func acceptableMutantStatuses() []string {
	return []string{"lived", "timed-out"}
}

// registryStatus maps a gremlins status to the registry's spelling.
func registryStatus(status string) string {
	switch status {
	case statusLived:
		return "lived"
	case statusTimedOut:
		return "timed-out"
	default:
		return ""
	}
}

// ParseMutationReport reads the JSON written by `gremlins unleash --output`.
func ParseMutationReport(data []byte) (*MutationReport, error) {
	var raw mutationJSON

	err := json.Unmarshal(data, &raw)
	if err != nil {
		return nil, fmt.Errorf("%w: decode: %w", ErrMutation, err)
	}

	report := MutationReport{TerminationReason: raw.TerminationReason}
	if raw.Complete != nil {
		report.Complete = *raw.Complete
	}

	for _, file := range raw.Files {
		for _, mutation := range file.Mutations {
			report.Mutants = append(report.Mutants, Mutant{
				File: file.Name, Type: mutation.Type, Status: mutation.Status, DiscoveryStatus: mutation.DiscoveryStatus,
				ExecutionReference: mutation.ExecutionReference, Line: mutation.Line, Column: mutation.Column,
			})
		}
	}

	return &report, nil
}

// EvaluateMutation fails every mutant that was not killed and is not rejected as not viable, unless a
// registry entry accepts a judged outcome exactly, and fails every entry that accepts nothing.
// Final NOT COVERED outcomes are incomplete judgments and can never be excepted. Mutants in files outside
// hostFiles are not judged on this platform. read returns a repository file's content.
func EvaluateMutation(report *MutationReport, entries []*Entry, hostFiles map[string]bool, read SourceReader) MutationResult {
	result := MutationResult{Counts: map[string]int{}}
	used := map[string]bool{}
	seenFiles := map[string]bool{}

	for _, mutant := range report.Mutants {
		result.Counts[mutant.Status]++
		seenFiles[mutant.File] = true

		if !hostFiles[mutant.File] {
			if _, err := read(mutant.File); err != nil {
				result.Problems = append(result.Problems, "unknown mutation file: "+mutant.File)
			}

			result.OtherPlatform++

			continue
		}

		result.Evaluated++
		result.judge(mutant, entries, used, read)
	}

	for file := range hostFiles {
		if !seenFiles[file] {
			result.Problems = append(result.Problems, "report missing host file: "+file)
		}
	}

	if result.Evaluated == 0 {
		result.Problems = append(result.Problems, "the report contains no mutants for this platform; nothing was measured")
	}

	for _, entry := range entries {
		if entry.Tool == ToolMutation && hostFiles[entry.Path] && !used[entry.ID] {
			result.Problems = append(result.Problems, entry.ID+": accepts no surviving mutant; delete the exception")
		}
	}

	slices.Sort(result.Problems)

	return result
}

// SummaryLines renders the status counts in a stable order for the gate's output.
func (r *MutationResult) SummaryLines() []string {
	lines := make([]string, 0, len(r.Counts)+summaryExtraLines)

	for _, status := range slices.Sorted(maps.Keys(r.Counts)) {
		lines = append(lines, fmt.Sprintf("%-12s %d", status, r.Counts[status]))
	}

	return append(lines,
		fmt.Sprintf("judged on this platform: %d mutants; other-platform files (not judged): %d", r.Evaluated, r.OtherPlatform),
		fmt.Sprintf("accepted by registry entries: %d", r.Accepted),
	)
}

// judge classifies one mutant of a host-platform file.
func (r *MutationResult) judge(mutant Mutant, entries []*Entry, used map[string]bool, read SourceReader) {
	switch mutant.Status {
	case statusKilled, statusNotViable:
		return
	case statusLived, statusTimedOut, statusNotCovered:
	default:
		r.Problems = append(r.Problems, fmt.Sprintf("%s:%d: unexpected mutant status %q", mutant.File, mutant.Line, mutant.Status))

		return
	}

	id := ""
	if mutant.Status != statusNotCovered {
		id = acceptedBy(mutant, entries, read)
	}

	if id != "" {
		used[id] = true
		r.Accepted++

		return
	}

	r.Problems = append(r.Problems, fmt.Sprintf("%s:%d:%d: %s mutant %s not killed",
		mutant.File, mutant.Line, mutant.Column, mutant.Type, strings.ToLower(mutant.Status)))
}

// acceptedBy returns the id of the entry accepting the mutant: same file, operator, accepted status,
// and the mutant's source line equal to the entry's anchor. It returns "" when none does.
func acceptedBy(mutant Mutant, entries []*Entry, read SourceReader) string {
	for _, entry := range entries {
		if entry.Tool != ToolMutation || entry.Path != mutant.File || entry.Operator != mutant.Type || entry.Column == nil ||
			*entry.Column != mutant.Column {
			continue
		}

		if !slices.Contains(entry.Statuses, registryStatus(mutant.Status)) {
			continue
		}

		content, err := read(mutant.File)
		if err != nil {
			continue
		}

		line, targetErr := mutationAnchorLine(entry, content)
		if targetErr == nil && line == mutant.Line {
			return entry.ID
		}
	}

	return ""
}

// MutationDiscoveryIssues compares the campaign with all positions discovered by a separate,
// all-operator dry run on the same snapshot. A fragment cannot certify a completed campaign.
func MutationDiscoveryIssues(discovery, campaign *MutationReport, hostFiles map[string]bool) []string {
	expected, problems := discoveredMutations(discovery, hostFiles)
	if !campaign.Complete {
		problems = append(problems, "mutation campaign lacks complete:true: "+campaign.TerminationReason)
	} else if campaign.TerminationReason != "" {
		problems = append(problems, "completed mutation campaign carries a termination reason: "+campaign.TerminationReason)
	}

	actual := map[Mutant]int{}
	references := map[string]bool{}

	for _, mutant := range campaign.Mutants {
		if !validJudgedStatus(mutant.Status) {
			problems = append(problems, fmt.Sprintf("unfinished or unknown mutation outcome: %+v", mutant))
		}

		if validJudgedStatus(mutant.Status) && !validExecutionReference(mutant.ExecutionReference) {
			problems = append(problems, fmt.Sprintf("missing or invalid mutation execution reference: %+v", mutant))
		}

		if mutant.ExecutionReference != "" {
			if references[mutant.ExecutionReference] {
				problems = append(problems, "duplicate mutation execution reference: "+mutant.ExecutionReference)
			}

			references[mutant.ExecutionReference] = true
		}

		if !validDiscoveryStatus(mutant.DiscoveryStatus) {
			problems = append(problems, fmt.Sprintf("missing or invalid original discovery status: %+v", mutant))
		}

		mutant.Status, mutant.ExecutionReference = "", ""
		actual[mutant]++
	}

	problems = append(problems, mutationDifference(expected, actual)...)
	slices.Sort(problems)

	return problems
}

func discoveredMutations(discovery *MutationReport, hostFiles map[string]bool) (map[Mutant]int, []string) {
	expected := map[Mutant]int{}

	var problems []string
	if !discovery.Complete {
		problems = append(problems, "mutation discovery lacks complete:true: "+discovery.TerminationReason)
	} else if discovery.TerminationReason != "" {
		problems = append(problems, "completed mutation discovery carries a termination reason: "+discovery.TerminationReason)
	}

	for _, mutant := range discovery.Mutants {
		if !hostFiles[mutant.File] {
			problems = append(problems, "discovery contains file outside host runtime scope: "+mutant.File)
		}

		if !validMutantPosition(mutant) || !validDiscoveryStatus(mutant.Status) {
			problems = append(problems, fmt.Sprintf("invalid discovered mutation: %+v", mutant))
		}

		if mutant.DiscoveryStatus != mutant.Status {
			problems = append(problems, fmt.Sprintf("discovery status differs from immutable original: %+v", mutant))
		}

		mutant.Status, mutant.ExecutionReference = "", ""
		expected[mutant]++
	}

	if len(expected) == 0 {
		problems = append(problems, "mutation discovery is empty")
	}

	return expected, problems
}

func validJudgedStatus(status string) bool {
	return slices.Contains([]string{statusKilled, statusLived, statusNotViable, statusTimedOut}, status)
}

func validExecutionReference(reference string) bool {
	if len(reference) != 64 || strings.ToLower(reference) != reference {
		return false
	}

	_, err := hex.DecodeString(reference)

	return err == nil
}

func validDiscoveryStatus(status string) bool {
	return status == "RUNNABLE" || status == statusNotCovered
}

func validMutantPosition(mutant Mutant) bool {
	return path.Clean(mutant.File) == mutant.File && !strings.HasPrefix(mutant.File, "../") && !strings.HasPrefix(mutant.File, "/") &&
		mutant.Line >= 1 && mutant.Column >= 1 && slices.Contains(MutationOperators(), mutant.Type)
}

func mutationDifference(expected, actual map[Mutant]int) []string {
	var problems []string

	for mutant, count := range expected {
		if actual[mutant] != count {
			problems = append(problems, fmt.Sprintf("missing mutation outcome: %s:%d:%d %s (want %d, got %d)",
				mutant.File, mutant.Line, mutant.Column, mutant.Type, count, actual[mutant]))
		}
	}

	for mutant := range actual {
		if expected[mutant] == 0 {
			problems = append(problems, fmt.Sprintf("unexpected mutation outcome: %+v", mutant))
		}
	}

	return problems
}
