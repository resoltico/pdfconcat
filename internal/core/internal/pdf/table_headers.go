// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Structure identifiers are byte strings, not destination names or text strings.
func structureID(s *model.Context, o types.Object) (string, error) {
	value, err := s.Dereference(o)
	if err != nil {
		return "", err
	}
	switch v := value.(type) {
	case types.StringLiteral:
		b, e := types.Unescape(string(v))
		return string(b), e
	case types.HexLiteral:
		b, e := v.Bytes()
		return string(b), e
	default:
		return "", errors.New("structure identifier must be a byte string")
	}
}

// Attribute arrays may contain revision integers. Resolve every boundary and
// bound recursion, including cyclic indirect arrays. Shared classes are copied.
func mapStructureAttributes(s *model.Context, o types.Object, visit func(types.Dict) error, depth int) (types.Object, error) {
	if depth > 256 {
		return nil, errors.New("structure attribute nesting exceeds 256")
	}
	v, err := s.Dereference(o)
	if err != nil {
		return nil, err
	}
	switch a := v.(type) {
	case nil:
		return nil, errors.New("structure attribute is null")
	case types.Integer:
		if a < 0 {
			return nil, errors.New("structure attribute revision must be nonnegative")
		}
		return a, nil
	case types.Array:
		out := make(types.Array, len(a))
		for i, x := range a {
			out[i], err = mapStructureAttributes(s, x, visit, depth+1)
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	case types.Dict:
		out := clonedDirectDict(a)
		if err = visit(out); err != nil {
			return nil, err
		}
		return out, nil
	default:
		return nil, errors.New("structure attributes must be dictionaries or revision arrays")
	}
}

func tableHeaderIDs(s *model.Context, attributes types.Dict) ([]string, error) {
	owner, err := nameValue(s, attributes["O"])
	if err != nil {
		return nil, err
	}
	if owner != "Table" || attributes["Headers"] == nil {
		return nil, nil
	}
	array, err := s.DereferenceArray(attributes["Headers"])
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(array))
	for i, o := range array {
		ids[i], err = structureID(s, o)
		if err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// Check against the retained hierarchy, not a possibly stale pre-selection
// IDTree. Full validation separately proves IDTree/hierarchy equivalence.
func validateTableHeaders(c context.Context, s *model.Context, root types.Dict) error {
	ids := map[string]types.Dict{}
	err := structureElements(c, s, root["K"], nil, func(d types.Dict, _ types.Object) error {
		if d["ID"] != nil {
			id, e := structureID(s, d["ID"])
			if e != nil {
				return e
			}
			if ids[id] != nil {
				return errors.New("duplicate structure ID")
			}
			ids[id] = d
		}
		return nil
	}, 0)
	if err != nil {
		return err
	}
	classes, err := s.DereferenceDict(root["ClassMap"])
	if err != nil {
		return err
	}
	global, err := s.DereferenceDict(root["RoleMap"])
	if err != nil {
		return err
	}
	spaces := map[int]types.Dict{}
	namespaces, err := s.DereferenceArray(root["Namespaces"])
	if err != nil {
		return err
	}
	for _, o := range namespaces {
		ref, ok := o.(types.IndirectRef)
		if !ok {
			return errors.New("namespace must be indirect")
		}
		d, e := s.DereferenceDict(o)
		if e != nil {
			return e
		}
		spaces[ref.ObjectNumber.Value()] = d
	}
	check := func(a types.Dict) error {
		headers, e := tableHeaderIDs(s, a)
		if e != nil {
			return e
		}
		for _, id := range headers {
			target := ids[id]
			if target == nil {
				return fmt.Errorf("table Headers identifier %q has no retained structure element", id)
			}
			name, e := nameValue(s, target["S"])
			if e != nil {
				return e
			}
			initial := namespaceRole{name: string(name)}
			if target["NS"] != nil {
				ref, ok := target["NS"].(types.IndirectRef)
				if !ok {
					return errors.New("namespace must be indirect")
				}
				initial.namespace = ref.ObjectNumber.Value()
			}
			role, e := resolveNamespaceRole(c, s, spaces, global, initial)
			if e != nil {
				return e
			}
			if role.name != "TH" {
				return fmt.Errorf("table Headers identifier %q does not identify a TH element", id)
			}
			if role.namespace != 0 {
				ns, e := s.DereferenceText(spaces[role.namespace]["NS"])
				if e != nil {
					return e
				}
				if ns != "http://iso.org/pdf/ssn" && ns != "http://iso.org/pdf2/ssn" {
					return errors.New("table header role must map to a standard PDF TH role")
				}
			}
		}
		return nil
	}
	return structureElements(c, s, root["K"], nil, func(d types.Dict, _ types.Object) error {
		if d["A"] != nil {
			if _, e := mapStructureAttributes(s, d["A"], check, 0); e != nil {
				return e
			}
		}
		if d["C"] == nil {
			return nil
		}
		_, e := mapAttributeClasses(s, d["C"], func(name types.Name) (types.Object, error) {
			if _, e := mapStructureAttributes(s, classes[string(name)], check, 0); e != nil {
				return nil, e
			}
			return name, nil
		}, 0)
		return e
	}, 0)
}
