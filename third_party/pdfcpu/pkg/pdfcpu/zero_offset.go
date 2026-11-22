// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdfcpu

import (
	"context"
	"fmt"
	"maps"
	"slices"

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
	audit, _ := c.Value(zeroOffsetStructureAuditKey{}).(*zeroOffsetStructureAudit)
	if audit != nil {
		// Loading a stream dependency can reparse an effective object. A
		// repeated read is not a second inbound edge.
		if audit.seen[owner] {
			return nil
		}
		audit.seen[owner] = true
	}
	return walkOriginalZeroOffsetRefs(c, ctx, o, func(value types.IndirectRef, depth int, slot string) error {
		if !ctx.ZeroOffsetObjects[value.ObjectNumber.Value()] {
			return nil
		}
		if audit != nil && owner > 0 && depth == 1 && (slot == "ParentTree" || slot == "IDTree") {
			entry := audit.entries[value.ObjectNumber.Value()]
			if entry.Generation == value.GenerationNumber.Value() && len(audit.candidates) < 2 {
				audit.candidates = append(audit.candidates, zeroOffsetCandidate{owner, slot, entry})
				return nil
			}
		}
		return zeroOffsetReferenceError(owner, value)
	})
}

func walkOriginalZeroOffsetRefs(c context.Context, ctx *model.Context, o types.Object, visit func(types.IndirectRef, int, string) error) error {
	var walk func(types.Object, int, string) error
	walk = func(value types.Object, depth int, slot string) error {
		if err := c.Err(); err != nil {
			return err
		}
		if err := model.CheckRecursionDepth("zero-offset references", depth, recursionLimit(ctx)); err != nil {
			return err
		}
		switch value := value.(type) {
		case types.IndirectRef:
			return visit(value, depth, slot)
		case types.Dict:
			for _, key := range slices.Sorted(maps.Keys(value)) {
				if err := walk(value[key], depth+1, key); err != nil {
					return err
				}
			}
		case types.Array:
			for _, child := range value {
				if err := walk(child, depth+1, ""); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(o, 0, "")
}

func zeroOffsetReferenceError(owner int, ref types.IndirectRef) error {
	return model.WithValidationErrorObject(fmt.Errorf("original reference to zero-offset object %d (generation %d)", ref.ObjectNumber, ref.GenerationNumber), owner)
}

type zeroOffsetStructureAuditKey struct{}

type zeroOffsetCandidate struct {
	owner int
	slot  string
	entry model.ZeroOffsetEntry
}

type zeroOffsetStructureAudit struct {
	seen       types.IntSet
	entries    map[int]model.ZeroOffsetEntry
	candidates []zeroOffsetCandidate
}

func newZeroOffsetStructureAudit(ctx *model.Context) *zeroOffsetStructureAudit {
	audit := &zeroOffsetStructureAudit{seen: types.IntSet{}, entries: map[int]model.ZeroOffsetEntry{}}
	for _, entry := range ctx.ZeroOffsetEntries {
		audit.entries[entry.Object] = entry
	}
	return audit
}

func originalZeroOffsetLive(ctx *model.Context, ref types.IndirectRef) bool {
	entry := ctx.Table[ref.ObjectNumber.Value()]
	_, mismatch := ctx.Read.MismatchedObjectHeaders[ref]
	return !mismatch && entry != nil && !entry.Free && entry.Generation != nil && *entry.Generation == ref.GenerationNumber.Value()
}

func (a *zeroOffsetStructureAudit) prove(c context.Context, ctx *model.Context) error {
	if len(a.candidates) == 0 {
		return nil
	}
	first := a.candidates[0]
	refusal := func(candidate zeroOffsetCandidate) error {
		return zeroOffsetReferenceError(candidate.owner, *types.NewIndirectRef(candidate.entry.Object, candidate.entry.Generation))
	}
	for number, entry := range ctx.Table {
		if !entry.Free && !a.seen[number] {
			return refusal(first)
		}
	}
	if ctx.Root == nil || !originalZeroOffsetLive(ctx, *ctx.Root) {
		return refusal(first)
	}
	catalog, err := ctx.Catalog()
	if err != nil {
		return err
	}
	root, ok := catalog["StructTreeRoot"].(types.IndirectRef)
	if !ok || !originalZeroOffsetLive(ctx, root) {
		return refusal(first)
	}
	body, ok := ctx.Table[root.ObjectNumber.Value()].Object.(types.Dict)
	if !ok || body["Type"] != types.Name("StructTreeRoot") {
		return refusal(first)
	}
	var proved []model.ZeroOffsetStructureReference
	for _, candidate := range a.candidates {
		target := *types.NewIndirectRef(candidate.entry.Object, candidate.entry.Generation)
		if candidate.owner != root.ObjectNumber.Value() || body[candidate.slot] != target {
			return refusal(candidate)
		}
		proved = append(proved, model.ZeroOffsetStructureReference{Root: root, Slot: candidate.slot, Entry: candidate.entry})
	}
	owned, ok, err := zeroOffsetStructureOwners(c, ctx, root, body["K"])
	if err != nil {
		return err
	}
	if !ok {
		return refusal(first)
	}
	if err := proveExclusiveZeroOffsetOwners(c, ctx, root, owned, refusal(first)); err != nil {
		return err
	}
	ctx.Read.DiscardedZeroOffsetReferences = proved
	return nil
}

// The discarded hierarchy is defined by K, not by all objects reachable from
// the root: Pg, Stm and Obj can point to retained artwork or annotations.
func zeroOffsetStructureOwners(c context.Context, ctx *model.Context, root types.IndirectRef, kids types.Object) (types.IntSet, bool, error) {
	owned := types.IntSet{root.ObjectNumber.Value(): true}
	var walk func(types.Object, int) (bool, error)
	walk = func(value types.Object, depth int) (bool, error) {
		if err := c.Err(); err != nil {
			return false, err
		}
		if err := model.CheckRecursionDepth("discarded structure hierarchy", depth, recursionLimit(ctx)); err != nil {
			return false, err
		}
		if ref, ok := value.(types.IndirectRef); ok {
			if !originalZeroOffsetLive(ctx, ref) {
				return false, nil
			}
			number := ref.ObjectNumber.Value()
			if owned[number] {
				return true, nil
			}
			owned[number] = true
			value = ctx.Table[number].Object
		}
		switch v := value.(type) {
		case types.Array:
			for _, child := range v {
				if valid, err := walk(child, depth+1); !valid || err != nil {
					return valid, err
				}
			}
		case types.Dict:
			switch v["Type"] {
			case types.Name("MCR"), types.Name("OBJR"):
			case types.Name("StructElem"), nil:
				if v["S"] == nil {
					return false, nil
				}
				return walk(v["K"], depth+1)
			default:
				return false, nil
			}
		case nil, types.Integer:
		default:
			return false, nil
		}
		return true, nil
	}
	ok, err := walk(kids, 0)
	return owned, ok, err
}

// Only a source with deferred candidates needs this second original-syntax
// pass. Reuse the effective table and shared object-read budget. Parsed
// stream dictionaries are scanned before resolveObject can rewrite them.
func proveExclusiveZeroOffsetOwners(c context.Context, ctx *model.Context, root types.IndirectRef, owned types.IntSet, refusal error) error {
	check := func(raw types.Object, owner int) error {
		return walkOriginalZeroOffsetRefs(c, ctx, raw, func(ref types.IndirectRef, depth int, slot string) error {
			if !owned[ref.ObjectNumber.Value()] {
				return nil
			}
			if owner == ctx.Root.ObjectNumber.Value() && depth == 1 && slot == "StructTreeRoot" && ref == root {
				return nil
			}
			if owned[owner] && originalZeroOffsetLive(ctx, ref) {
				return nil
			}
			return refusal
		})
	}
	if err := check(ctx.OriginalTrailer, 0); err != nil {
		return err
	}
	for _, number := range slices.Sorted(maps.Keys(ctx.Table)) {
		if err := c.Err(); err != nil {
			return err
		}
		entry := ctx.Table[number]
		if entry.Free {
			continue
		}
		if entry.Offset == nil || entry.Generation == nil {
			return refusal
		}
		raw, _, _, _, err := object(c, ctx, *entry.Offset, number, *entry.Generation)
		if err != nil {
			return err
		}
		if err := check(raw, number); err != nil {
			return err
		}
	}
	return nil
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
