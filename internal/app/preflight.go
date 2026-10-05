// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"strings"
	"sync"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

// maxReportedInputFailures bounds how many failing inputs one error lists.
const maxReportedInputFailures = 20

// inspectAll validates every distinct source PDF concurrently. All failures are
// collected, so one run reveals every bad file in a large sequence.
func (r *Runner) inspectAll(ctx context.Context, paths []string) (map[string]assembly.DocumentInfo, error) {
	infos := make([]assembly.DocumentInfo, len(paths))
	failures := make([]error, len(paths))

	progress := newProgress(r.streams.Progress, "Validating PDFs", len(paths))
	defer progress.finish()

	jobs := make(chan int)

	var workers sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(paths)) {
		workers.Go(func() {
			for index := range jobs {
				infos[index], failures[index] = r.engine.Inspect(ctx, paths[index])
				if failures[index] != nil {
					failures[index] = annotateInputFailure(paths[index], failures[index])
				}

				progress.advance()
			}
		})
	}

feed:
	for index := range paths {
		select {
		case jobs <- index:
		case <-ctx.Done():
			break feed
		}
	}

	close(jobs)
	workers.Wait()

	err := ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("validate PDFs: %w", err)
	}

	err = summarizeFailures(failures)
	if err != nil {
		return nil, err
	}

	documents := make(map[string]assembly.DocumentInfo, len(paths))
	for index, path := range paths {
		documents[path] = infos[index]
	}

	return documents, nil
}

// annotateInputFailure explains the common wildcard mistake on shells that do not expand globs.
func annotateInputFailure(path string, err error) error {
	if errors.Is(err, fs.ErrNotExist) && strings.ContainsAny(path, "*?[") {
		return fmt.Errorf("%w (PDFConcat does not expand wildcards; if your shell did not either, list the files or use a plan)", err)
	}

	return err
}

func summarizeFailures(failures []error) error {
	var failed []error

	for _, failure := range failures {
		if failure != nil {
			failed = append(failed, failure)
		}
	}

	if len(failed) == 0 {
		return nil
	}

	shown := failed[:min(len(failed), maxReportedInputFailures)]

	message := fmt.Sprintf("%d of %d input PDFs failed validation", len(failed), len(failures))
	if len(failed) > len(shown) {
		return fmt.Errorf("%s (showing the first %d):\n%w", message, len(shown), errors.Join(shown...))
	}

	return fmt.Errorf("%s:\n%w", message, errors.Join(shown...))
}
