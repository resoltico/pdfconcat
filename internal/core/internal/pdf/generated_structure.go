// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/compose"
)

func tagGeneratedPage(s *model.Context, root types.Dict, rootRef types.IndirectRef, page compose.Page, pageRef types.IndirectRef, annotations []types.IndirectRef) error {
	section := types.Dict{"Type": types.Name("StructElem"), "S": types.Name("Sect"), "P": rootRef, "Pg": pageRef, "K": types.Array{}}
	sectionRef, err := s.IndRefForNewObject(section)
	if err != nil {
		return err
	}
	links := map[int]types.Dict{}
	for index, block := range page.Blocks {
		if block.Text == nil || len(block.Text.Lines) == 0 {
			continue
		}
		role := block.Role
		if role == "" {
			role = compose.RoleParagraph
		}
		mcr := types.Dict{"Type": types.Name("MCR"), "Pg": pageRef, "MCID": types.Integer(index)}
		if block.Link != nil {
			number := *block.Link
			if number < 0 || number >= len(annotations) {
				return ErrInvalidPage
			}
			link := links[number]
			if link == nil {
				entry := types.Dict{"Type": types.Name("StructElem"), "S": types.Name("TOCI"), "P": *sectionRef, "Pg": pageRef}
				entryRef, e := s.IndRefForNewObject(entry)
				if e != nil {
					return e
				}
				link = types.Dict{"Type": types.Name("StructElem"), "S": types.Name("Link"), "P": *entryRef, "Pg": pageRef, "K": types.Array{types.Dict{"Type": types.Name("OBJR"), "Pg": pageRef, "Obj": annotations[number]}}}
				ref, e := s.IndRefForNewObject(link)
				if e != nil {
					return e
				}
				entry["K"] = *ref
				section["K"] = append(semanticKids(section), *entryRef)
				links[number] = link
			}
			link["K"] = append(semanticKids(link), mcr)
		} else {
			elem := types.Dict{"Type": types.Name("StructElem"), "S": types.Name(role), "P": *sectionRef, "Pg": pageRef, "K": mcr}
			ref, e := s.IndRefForNewObject(elem)
			if e != nil {
				return e
			}
			section["K"] = append(semanticKids(section), *ref)
		}
	}
	if len(semanticKids(section)) > 0 {
		root["K"] = append(semanticKids(root), *sectionRef)
	}
	return nil
}

func semanticKids(d types.Dict) types.Array { kids, _ := d["K"].(types.Array); return kids }
