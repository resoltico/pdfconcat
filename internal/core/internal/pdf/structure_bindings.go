// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// The default namespace is the PDF 1.7 vocabulary (ISO 32000-1 §14.8.4).
// Explicit namespace roles retain their namespace's own vocabulary/mappings.
var defaultRoles = strings.Fields("Document Part Art Sect Div BlockQuote Caption TOC TOCI Index NonStruct Private P H H1 H2 H3 H4 H5 H6 L LI Lbl LBody Table TR TH TD THead TBody TFoot Span Quote Note Reference BibEntry Code Link Annot Ruby RB RT RP Warichu WT WP Figure Formula Form")

func standardDefaultRole(name string) bool { return slices.Contains(defaultRoles, name) }

func defaultRole(s *model.Context, roles types.Dict, name string) error {
	seen := map[string]bool{}
	for roles[name] != nil {
		if seen[name] {
			return errors.New("cyclic structure role map")
		}
		seen[name] = true
		target, err := nameValue(s, roles[name])
		if err != nil {
			return err
		}
		name = string(target)
	}
	if !standardDefaultRole(name) {
		return errors.New("structure role has no standard default-namespace binding: " + name)
	}
	return nil
}

// Validate resolved bindings in original and reopened output graphs. A valid
// object reference alone does not establish that its role/class still exists.
func validateStructureBindings(c context.Context, s *model.Context, root types.Dict) error {
	roles, err := s.DereferenceDict(root["RoleMap"])
	if err != nil {
		return err
	}
	classes, err := s.DereferenceDict(root["ClassMap"])
	if err != nil {
		return err
	}
	return structureElements(c, s, root["K"], nil, func(d types.Dict, _ types.Object) error {
		role, err := nameValue(s, d["S"])
		if err != nil {
			return err
		}
		if d["NS"] == nil {
			if err = defaultRole(s, roles, string(role)); err != nil {
				return err
			}
		}
		if d["C"] != nil {
			_, err = mapAttributeClasses(s, d["C"], func(name types.Name) (types.Object, error) {
				attribute, e := s.Dereference(classes[string(name)])
				if e != nil {
					return nil, e
				}
				if attribute == nil {
					return nil, errors.New("structure class has no ClassMap binding: " + string(name))
				}
				return name, nil
			}, 0)
		}
		return err
	}, 0)
}
