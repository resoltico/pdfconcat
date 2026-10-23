// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan

import (
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
)

// DirectItem is the deliberately small source/blank declaration language.
type DirectItem struct {
	Path, Reference string
	Blank           bool
}

// Direct shares effective defaults, paths and authored budgets with JSON plans.
// It does not impose JSON syntax or a synthetic serialization byte limit.
func Direct(items []DirectItem, base string) (packet.Job, []diag.Diagnostic) {
	var j packet.Job
	var ds []diag.Diagnostic
	entries, generated := 0, 0
	for _, item := range items {
		count := 0
		if item.Blank {
			count = 1
		}
		if message := charge(&entries, &generated, count); message != "" {
			ds = append(ds, diag.New(diag.PlanLimit, message, item.Reference))
			break
		}
		p := packet.Part{Pointer: item.Reference, Count: 1}
		if item.Blank {
			style := packet.Defaults()
			p.Page = &style
		} else {
			path, err := packet.Path(base, item.Path)
			if err != nil {
				ds = append(ds, diag.New(diag.PathInvalid, err.Error(), item.Reference))
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
	if *entries > packet.MaxEntries {
		return "Plan exceeds 100000 authored items."
	}
	*generated += count
	if *generated > packet.MaxGeneratedPages {
		return "Plan generates more than 100000 pages before contents and padding."
	}
	return ""
}
