/*
Copyright 2026 The pdfcpu Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package model

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// PreserveSourceNullReferences freezes null source identities before any object is inserted.
// Source policy must inspect original declarations first. Live indirect identities are retained;
// direct containers are visited once per table entry without following indirect graph edges.
func (x *XRefTable) PreserveSourceNullReferences(c context.Context) error {
	if err := c.Err(); err != nil {
		return err
	}
	numbers := slices.Sorted(maps.Keys(x.Table))
	nullObjects := types.IntSet{}
	for _, number := range numbers {
		if err := c.Err(); err != nil {
			return err
		}
		entry := x.Table[number]
		if entry == nil || entry.Free {
			continue
		}
		if lazy, ok := entry.Object.(types.LazyObjectStreamObject); ok {
			object, err := lazy.DecodedObject(c)
			if err != nil {
				return fmt.Errorf("source object %d: %w", number, err)
			}
			if err := ProcessRefCountsWithError(x, object); err != nil {
				return fmt.Errorf("source object %d: %w", number, err)
			}
			entry.Object = object
		}
		if entry.Object == nil {
			nullObjects[number] = true
		}
	}
	rewrite := sourceNullRewrite{x: x, nullObjects: nullObjects}
	for _, number := range numbers {
		entry := x.Table[number]
		if entry == nil || entry.Free {
			continue
		}
		object, err := rewrite.object(c, entry.Object, 0)
		if err != nil {
			return fmt.Errorf("source object %d: %w", number, err)
		}
		entry.Object = object
	}
	if x.Info != nil && rewrite.null(*x.Info) {
		x.Info = nil
	}
	if _, err := rewrite.object(c, x.DirectInfoDict, 0); err != nil {
		return fmt.Errorf("source direct Info: %w", err)
	}
	if _, err := rewrite.object(c, x.ID, 0); err != nil {
		return fmt.Errorf("source ID: %w", err)
	}
	if x.AdditionalStreams != nil {
		if _, err := rewrite.object(c, *x.AdditionalStreams, 0); err != nil {
			return fmt.Errorf("source AdditionalStreams: %w", err)
		}
	}
	return c.Err()
}

type sourceNullRewrite struct {
	x           *XRefTable
	nullObjects types.IntSet
}

func (r sourceNullRewrite) null(ref types.IndirectRef) bool {
	entry, found := r.x.FindTableEntryForIndRef(&ref)
	return !found || entry.Free || r.nullObjects[ref.ObjectNumber.Value()]
}

func (r sourceNullRewrite) object(c context.Context, object types.Object, depth int) (types.Object, error) {
	if err := c.Err(); err != nil {
		return nil, err
	}
	if err := r.x.CheckRecursionDepth("source null references", depth); err != nil {
		return nil, err
	}
	switch value := object.(type) {
	case types.IndirectRef:
		if r.null(value) {
			return nil, nil
		}
	case types.Dict:
		for _, key := range slices.Sorted(maps.Keys(value)) {
			rewritten, err := r.object(c, value[key], depth+1)
			if err != nil {
				return nil, err
			}
			value[key] = rewritten
		}
	case types.Array:
		for index, child := range value {
			rewritten, err := r.object(c, child, depth+1)
			if err != nil {
				return nil, err
			}
			value[index] = rewritten
		}
	case types.StreamDict:
		if _, err := r.object(c, value.Dict, depth); err != nil {
			return nil, err
		}
		for index := range value.FilterPipeline {
			if _, err := r.object(c, value.FilterPipeline[index].DecodeParms, depth); err != nil {
				return nil, err
			}
		}
	case types.ObjectStreamDict:
		if _, err := r.object(c, value.StreamDict, depth); err != nil {
			return nil, err
		}
		// ObjArray is parsing storage, not retained source objects; table entries own the decoded values.
	case types.XRefStreamDict:
		if _, err := r.object(c, value.StreamDict, depth); err != nil {
			return nil, err
		}
	}
	return object, nil
}
