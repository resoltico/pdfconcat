// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/plan"
)

// job is a fully resolved assembly request: absolute paths, effective blank defaults.
type job struct {
	output   string
	defaults assembly.BlankStyle
	sequence assembly.Sequence
}

// resolve turns a parsed request into a job, reading the plan file if one is named.
func (r *Runner) resolve(req *cli.Request) (job, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return job{}, fmt.Errorf("determine working directory: %w", err)
	}

	resolved := job{defaults: req.Blank}
	if req.Output != "" {
		resolved.output = absolutePath(workingDir, req.Output)
	}

	if req.PlanPath == "" {
		resolved.sequence = absoluteSequence(&req.Sequence, workingDir)
	} else {
		err = r.resolveFromPlan(req, workingDir, &resolved)
		if err != nil {
			return job{}, err
		}
	}

	if resolved.output == "" {
		return job{}, &cli.UsageError{Message: "no output path: pass -o FILE or set \"output\" in the plan"}
	}

	err = resolved.sequence.Validate()
	if err != nil {
		return job{}, err
	}

	if !req.Blank.IsZero() && !resolved.sequence.HasBlank() {
		return job{}, &cli.UsageError{Message: "--blank-* options were given but the sequence contains no blank page"}
	}

	return resolved, nil
}

// resolveFromPlan fills resolved from the plan named by the request. Command-line
// blank defaults win over the plan's, and -o wins over the plan's output.
func (r *Runner) resolveFromPlan(req *cli.Request, workingDir string, resolved *job) error {
	document, planPath, err := r.loadPlan(req.PlanPath, workingDir)
	if err != nil {
		return err
	}

	resolved.sequence = document.Sequence
	resolved.defaults = req.Blank.Over(document.Blank)

	if resolved.output == "" {
		resolved.output = document.Output
	}

	return rejectOutputAsPlan(planPath, resolved.output)
}

// loadPlan reads the plan from a file or, for "-", from standard input. It returns
// the plan's absolute path, or "" for standard input.
func (r *Runner) loadPlan(planArg, workingDir string) (plan.Document, string, error) {
	if planArg == cli.StdinPlan {
		document, err := plan.Decode(r.streams.Stdin, workingDir)
		if err != nil {
			return plan.Document{}, "", fmt.Errorf("plan from standard input: %w", err)
		}

		return document, "", nil
	}

	planPath := absolutePath(workingDir, planArg)

	file, err := os.Open(planPath)
	if err != nil {
		return plan.Document{}, "", fmt.Errorf("open plan: %w", err)
	}

	document, decodeErr := plan.Decode(file, filepath.Dir(planPath))
	closeErr := file.Close()

	if decodeErr != nil {
		return plan.Document{}, "", fmt.Errorf("plan %q: %w", planPath, decodeErr)
	}

	if closeErr != nil {
		return plan.Document{}, "", fmt.Errorf("close plan %q: %w", planPath, closeErr)
	}

	return document, planPath, nil
}

func absolutePath(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}

	return filepath.Join(base, path)
}

// absoluteSequence resolves relative PDF paths of a direct sequence against base.
func absoluteSequence(sequence *assembly.Sequence, base string) assembly.Sequence {
	items := make([]assembly.Item, len(sequence.Items))
	copy(items, sequence.Items)

	for index := range items {
		if items[index].Kind == assembly.PDF {
			items[index].Path = absolutePath(base, items[index].Path)
		}
	}

	return assembly.Sequence{Items: items}
}

func rejectOutputAsPlan(planPath, output string) error {
	if planPath == "" || output == "" {
		return nil
	}

	same, err := samePath(planPath, output)
	if err != nil {
		return err
	}

	if same {
		return fmt.Errorf("output %q is also the plan file", output)
	}

	return nil
}

func rejectOutputAsInput(sequence *assembly.Sequence, output string) error {
	for _, path := range sequence.DistinctPDFPaths() {
		same, err := samePath(path, output)
		if err != nil {
			return err
		}

		if same {
			return fmt.Errorf("output %q is also an input PDF", output)
		}
	}

	return nil
}

// samePath reports whether two paths name the same file, comparing by identity when both exist.
func samePath(first, second string) (bool, error) {
	if filepath.Clean(first) == filepath.Clean(second) {
		return true, nil
	}

	firstInfo, firstExists, err := statIfExists(first)
	if err != nil {
		return false, err
	}

	secondInfo, secondExists, err := statIfExists(second)
	if err != nil {
		return false, err
	}

	return firstExists && secondExists && os.SameFile(firstInfo, secondInfo), nil
}

// statIfExists stats path, treating a missing file as a normal outcome rather than an error.
func statIfExists(path string) (os.FileInfo, bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, fmt.Errorf("inspect path: %w", err)
	}

	return info, true, nil
}
