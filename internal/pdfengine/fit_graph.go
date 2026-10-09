// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type (
	fitDestinationKey struct {
		name   string
		legacy bool
	}
	fitGraphWalk struct {
		references map[types.IndirectRef]bool
		names      map[fitDestinationKey]bool
		visits     int
	}
)

// maxFitGraphVisits bounds expanded nodes and edges, including repeatedly shared DAG branches.
// It complements the source-byte/object limits and depth guard; no action is executed or rewritten.
const maxFitGraphVisits = 1 << 16

func newFitGraphWalk() *fitGraphWalk {
	return &fitGraphWalk{references: map[types.IndirectRef]bool{}, names: map[fitDestinationKey]bool{}}
}

func (w *fitGraphWalk) visit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("fit graph canceled: %w", err)
	}

	if w.visits >= maxFitGraphVisits {
		return fmt.Errorf("%w: graph exceeds %d expanded nodes/edges", errFitUnsupported, maxFitGraphVisits)
	}

	w.visits++

	return nil
}

func (w *fitGraphWalk) enter(ctx context.Context, object types.Object, depth int) error {
	if err := w.visit(ctx); err != nil {
		return err
	}

	if err := fitGraphGuard(ctx, object, w.references, depth); err != nil {
		return err
	}

	if ref, ok := object.(types.IndirectRef); ok {
		w.references[ref] = true
	}

	return nil
}

func (w *fitGraphWalk) leave(object types.Object) {
	if ref, ok := object.(types.IndirectRef); ok {
		delete(w.references, ref)
	}
}
