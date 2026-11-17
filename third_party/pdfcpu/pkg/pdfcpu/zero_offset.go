// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdfcpu

import (
	"context"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// prepareZeroOffsetGate runs after effective xref precedence has been resolved.
// Exceptional hybrid/stream files are refused: those readers can parse objects
// before the combined table is known. Ordinary inputs take their existing path.
func prepareZeroOffsetGate(c context.Context, ctx *model.Context) error {
	if len(ctx.ZeroOffsetEntries) == 0 {
		return nil
	}
	if ctx.Read.Hybrid || ctx.Read.UsingXRefStreams {
		return fmt.Errorf("zero-offset exception requires exclusively classic xref revisions")
	}
	for _, entry := range ctx.Table {
		if entry.Compressed {
			return fmt.Errorf("zero-offset exception cannot establish original compressed-object references")
		}
	}
	return guardZeroOffsetReferences(c, ctx, ctx.OriginalTrailer, 0)
}

// guardZeroOffsetReferences observes parsed syntax BEFORE resolveObject can
// dereference stream Length/filters or decrypt/normalize dictionary values.
// Every effective live object is parsed by dereferenceObjects, even unreachable
// objects. Strings, comments and stream payload are never interpreted as refs.
func guardZeroOffsetReferences(c context.Context, ctx *model.Context, o types.Object, owner int) error {
	if len(ctx.ZeroOffsetEntries) == 0 {
		return nil
	}
	var walk func(types.Object, int) error
	walk = func(value types.Object, depth int) error {
		if err := c.Err(); err != nil {
			return err
		}
		if err := model.CheckRecursionDepth("zero-offset references", depth, recursionLimit(ctx)); err != nil {
			return err
		}
		switch value := value.(type) {
		case types.IndirectRef:
			if ctx.ZeroOffsetObjects[int(value.ObjectNumber)] {
				return model.WithValidationErrorObject(fmt.Errorf("original reference to zero-offset object %d (generation %d)", value.ObjectNumber, value.GenerationNumber), owner)
			}
		case types.Dict:
			for _, child := range value {
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
		case types.Array:
			for _, child := range value {
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(o, 0)
}

// The exception never adopts decodeSubsection's unrelated generation repair.
func literalZeroOffsetEntry(fields []string, generation int) bool {
	if len(fields) != 3 || fields[0] != "0000000000" || len(fields[1]) != 5 || generation < 0 || generation > 65535 {
		return false
	}
	for _, digit := range fields[1] {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
