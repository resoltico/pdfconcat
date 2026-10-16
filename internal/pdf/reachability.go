// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// freeUnreachable gives the native writer a complete xref free list. Otherwise
// unreachable live objects at the end of a composed context can inflate Size
// beyond the highest xref entry the pinned writer actually emits.
func freeUnreachable(c context.Context, s *model.Context) error {
	pending := []types.Object{*s.Root, s.ID}
	if s.Info != nil {
		pending = append(pending, *s.Info)
	}
	if s.Encrypt != nil {
		pending = append(pending, *s.Encrypt)
	}
	reachable := map[int]bool{}
	for len(pending) > 0 {
		if err := c.Err(); err != nil {
			return err
		}
		object := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if ref, ok := object.(types.IndirectRef); ok {
			number := ref.ObjectNumber.Value()
			if reachable[number] {
				continue
			}
			reachable[number] = true
			value, err := s.Dereference(ref)
			if err != nil {
				return err
			}
			pending = append(pending, value)
			continue
		}
		switch value := object.(type) {
		case types.Dict:
			for _, child := range value {
				pending = append(pending, child)
			}
		case types.StreamDict:
			pending = append(pending, value.Dict)
		case types.Array:
			pending = append(pending, value...)
		}
	}
	for number, entry := range s.Table {
		if err := c.Err(); err != nil {
			return err
		}
		if number > 0 && entry != nil && !entry.Free && !reachable[number] {
			if err := s.FreeObject(number); err != nil {
				return err
			}
		}
	}
	return s.EnsureValidFreeList()
}
