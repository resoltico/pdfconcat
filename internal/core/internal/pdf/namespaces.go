// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func validateNamespaces(c context.Context, s *model.Context, root types.Dict) error {
	list, err := s.DereferenceArray(root["Namespaces"])
	if err != nil {
		return err
	}
	if len(list) > 0 && !s.PDF20() {
		return errors.New("structure namespaces require PDF 2.0")
	}
	spaces := map[int]types.Dict{}
	for _, value := range list {
		ref, ok := value.(types.IndirectRef)
		if !ok {
			return errors.New("namespace must be indirect")
		}
		d, e := s.DereferenceDict(ref)
		if e != nil {
			return e
		}
		kind, e := nameValue(s, d["Type"])
		if e != nil {
			return e
		}
		if d == nil || kind != types.Name("Namespace") {
			return errors.New("invalid namespace dictionary")
		}
		name, e := s.DereferenceText(d["NS"])
		if e != nil || name == "" {
			return errors.New("namespace lacks a name")
		}
		spaces[ref.ObjectNumber.Value()] = d
	}
	global, err := s.DereferenceDict(root["RoleMap"])
	if err != nil {
		return err
	}
	for n, space := range spaces {
		mapping, e := s.DereferenceDict(space["RoleMapNS"])
		if e != nil {
			return e
		}
		for name := range mapping {
			if _, e = resolveNamespaceRole(c, s, spaces, global, namespaceRole{namespace: n, name: name}); e != nil {
				return e
			}
		}
	}
	return structureElements(c, s, root["K"], nil, func(d types.Dict, _ types.Object) error {
		if d["NS"] == nil {
			return nil
		}
		ref, ok := d["NS"].(types.IndirectRef)
		if !ok || spaces[ref.ObjectNumber.Value()] == nil {
			return errors.New("structure namespace is absent from Namespaces")
		}
		name, e := nameValue(s, d["S"])
		if e != nil {
			return e
		}
		_, e = resolveNamespaceRole(c, s, spaces, global, namespaceRole{namespace: ref.ObjectNumber.Value(), name: string(name)})
		return e
	}, 0)
}

func renameNamespaceDefaultRoles(s *model.Context, list types.Array, prefix string, roles types.Dict) error {
	for _, value := range list {
		d, err := s.DereferenceDict(value)
		if err != nil {
			return err
		}
		mapping, err := s.DereferenceDict(d["RoleMapNS"])
		if err != nil {
			return err
		}
		for key, value := range mapping {
			resolved, e := s.Dereference(value)
			if e != nil {
				return e
			}
			if name, ok := resolved.(types.Name); ok && roles[string(name)] != nil {
				mapping[key] = types.Name(prefix + string(name))
			}
		}
	}
	return nil
}

type namespaceRole struct {
	namespace int
	name      string
}

func resolveNamespaceRole(c context.Context, s *model.Context, spaces map[int]types.Dict, global types.Dict, initial namespaceRole) (namespaceRole, error) {
	seen := map[namespaceRole]bool{}
	current := initial
	for {
		if err := c.Err(); err != nil {
			return namespaceRole{}, err
		}
		if seen[current] {
			return namespaceRole{}, errors.New("cyclic namespace role mapping")
		}
		seen[current] = true
		mapping := global
		if current.namespace != 0 {
			d := spaces[current.namespace]
			if d == nil {
				return namespaceRole{}, errors.New("role target namespace is absent from Namespaces")
			}
			var e error
			mapping, e = s.DereferenceDict(d["RoleMapNS"])
			if e != nil {
				return namespaceRole{}, e
			}
		}
		value, e := s.Dereference(mapping[current.name])
		if e != nil {
			return namespaceRole{}, e
		}
		if value == nil {
			if current.namespace == 0 {
				return current, defaultRole(s, global, current.name)
			}
			return current, nil
		}
		switch v := value.(type) {
		case types.Name:
			current = namespaceRole{name: string(v)}
		case types.Array:
			if len(v) != 2 {
				return namespaceRole{}, errors.New("invalid namespaced role target")
			}
			name, e := nameValue(s, v[0])
			if e != nil {
				return namespaceRole{}, e
			}
			ns, ok := v[1].(types.IndirectRef)
			if !ok {
				return namespaceRole{}, errors.New("role target namespace must be indirect")
			}
			current = namespaceRole{namespace: ns.ObjectNumber.Value(), name: string(name)}
		default:
			return namespaceRole{}, errors.New("invalid namespace role mapping")
		}
	}
}
