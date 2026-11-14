// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// withFormResource owns only the dictionaries it changes. The original resource
// graph remains available for page groups and appearances that omit Resources.
func withFormResource(s *model.Context, base types.Dict, name string, obj types.Object) (types.Dict, string, error) {
	resources := clonedDirectDict(base)
	xobjects, err := s.DereferenceDict(resources["XObject"])
	if err != nil {
		return nil, "", err
	}
	xobjects = clonedDirectDict(xobjects)
	prefix := name
	for suffix := 1; xobjects[name] != nil; suffix++ {
		name = fmt.Sprintf("%s%d", prefix, suffix)
	}
	xobjects[name] = obj
	resources["XObject"] = xobjects
	return resources, name, nil
}

// clonedDirectDict gives a newly owned PDF object its own direct containers.
// Indirect references keep their PDF identity. The merge backend remaps direct
// containers per xref entry, so sharing a Go map between entries would remap
// the same references twice even though the PDF graph itself is legal.
func clonedDirectDict(original types.Dict) types.Dict {
	result := types.Dict{}
	for key, value := range original {
		if value != nil {
			value = value.Clone()
		}
		result[key] = value
	}
	return result
}
