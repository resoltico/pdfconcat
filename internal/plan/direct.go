// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan

import (
	"github.com/resoltico/pdfconcat/internal/diag"
	"github.com/resoltico/pdfconcat/internal/job"
)

// DirectItem is the deliberately small source/blank declaration language.
type DirectItem struct {
	Path, Reference string
	Blank           bool
}

// Direct shares effective defaults, paths and authored budgets with JSON plans.
// It does not impose JSON syntax or a synthetic serialization byte limit.
func Direct(items []DirectItem, base string) (job.Job, []diag.Diagnostic) {
	var j job.Job
	var ds []diag.Diagnostic
	entries, generated := 0, 0
	for _, item := range items {
		count := 0
		if item.Blank {
			count = 1
		}
		if message := charge(&entries, &generated, count); message != "" {
			ds = append(ds, diag.New("plan_limit", message, item.Reference))
			break
		}
		p := job.Part{Pointer: item.Reference, Count: 1}
		if item.Blank {
			style := job.Defaults()
			p.Page = &style
		} else {
			path, err := job.Path(base, item.Path)
			if err != nil {
				ds = append(ds, diag.New("path_invalid", err.Error(), item.Reference))
				continue
			}
			p.Source = path
		}
		j.Parts = append(j.Parts, p)
	}
	return j, ds
}
func charge(entries, generated *int, count int) string {
	*entries++
	if *entries > job.MaxEntries {
		return "Plan exceeds 100000 authored items."
	}
	*generated += count
	if *generated > job.MaxGeneratedPages {
		return "Plan generates more than 100000 pages before contents and padding."
	}
	return ""
}
