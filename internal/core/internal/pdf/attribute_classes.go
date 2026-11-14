// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"errors"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// mapAttributeClasses preserves revisions while resolving name/array objects at
// every boundary, including indirect array members. The depth bound also
// prevents cyclic indirect arrays from becoming unbounded recursion.
func mapAttributeClasses(s *model.Context, o types.Object, rename func(types.Name) (types.Object, error), depth int) (types.Object, error) {
	if depth > 256 {
		return nil, errors.New("structure class nesting exceeds 256")
	}
	value, err := s.Dereference(o)
	if err != nil {
		return nil, err
	}
	switch v := value.(type) {
	case types.Name:
		return rename(v)
	case types.Integer:
		if v.Value() < 0 {
			return nil, errors.New("structure class revision must be nonnegative")
		}
		return v, nil
	case types.Array:
		out := make(types.Array, len(v))
		for i, child := range v {
			out[i], err = mapAttributeClasses(s, child, rename, depth+1)
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	default:
		return nil, errors.New("structure class must be a name or names/revisions array")
	}
}
