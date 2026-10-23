// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"path/filepath"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/files"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

// Assembly records changes and warnings from the actual merge.
type Assembly struct {
	FieldsRenamed map[int][]packet.FieldRename
	Diagnostics   []diag.Diagnostic
}

// Assemble imports each occurrence independently and writes only after final validation.
func Assemble(c context.Context, layout packet.Layout, facts map[string]SourceFacts, occurrences map[string]OccurrenceFacts, pages map[int][]Page, fit *packet.Size, fonts map[typeset.Digest]typeset.EmbeddedFont, w io.Writer) (result Assembly, err error) {
	var current *packet.PlacedPart
	defer func() {
		if value := recover(); value != nil {
			err = refuse(diag.AssemblyFailed, fmt.Sprintf("PDF backend panic: %v", value))
		}
		if err != nil && current != nil {
			d := diagnostic(err, diag.AssemblyFailed)
			err = &policyError{code: d.Code, err: err, file: current.Source, items: []string{current.Pointer}}
		}
	}()
	names, nameErr := generatedFontNames(fonts, facts)
	if nameErr != nil {
		return result, nameErr
	}
	result.FieldsRenamed = map[int][]packet.FieldRename{}
	var dest *model.Context
	allDests := map[string]destination{}
	usedNames := map[string]bool{}
	var importedOutlines []importedOutlineRoot
	var generatedLinks []generatedLink
	compatibility := CheckCompatibility(layout, facts, occurrences, fit)
	if len(compatibility.Diagnostics) > 0 {
		d := compatibility.Diagnostics[0]
		return result, &policyError{code: d.Code, err: errors.New(d.Message), file: d.File, items: d.Items}
	}
	needAppearances := false
	add := func(code diag.Code, message string, index int) {
		d := diag.New(code, message, layout.Parts[index].Pointer)
		d.File = layout.Parts[index].Source
		result.Diagnostics = append(result.Diagnostics, d)
	}
	for index, part := range layout.Parts {
		current = &layout.Parts[index]
		if err = c.Err(); err != nil {
			return result, err
		}
		var s *model.Context
		if part.Source != "" {
			data, e := files.Read(c, part.Source, 0)
			if e != nil {
				return result, &policyError{code: diag.SourceUnreadable, err: e}
			}
			inspected, ok := facts[part.Source]
			if !ok || sha256.Sum256(data) != inspected.SHA256 {
				return result, refuse(diag.SourceChanged, "Source bytes changed after inspection: "+part.Source)
			}
			s, err = read(c, data)
			if err != nil {
				return result, err
			}
			occurrence, ok := occurrences[part.Pointer]
			if !ok {
				return result, errors.New("source occurrence was not inspected")
			}
			removed, e := projectSelection(c, s, occurrence.Selection)
			if e != nil {
				return result, e
			}
			if removed {
				add(diag.SourceNavigationRemoved, "Navigation targets omitted from this source occurrence were removed.", index)
			}
			needAppearances = needAppearances || inspected.NeedAppearances
		} else {
			var links []generatedLink
			s, links, err = generatedContext(c, pages[index], part.Pages, fonts, names)
			if err != nil {
				return result, err
			}
			generatedLinks = append(generatedLinks, links...)
		}
		if fit != nil {
			if err = fitContext(c, s, fit); err != nil {
				return result, err
			}
		}
		detached, renamed, e := detachDestinations(c, s, usedNames, part.FirstPage)
		if e != nil {
			return result, e
		}
		for name, d := range detached {
			allDests[name] = d
		}
		if renamed {
			add(diag.DestinationsRenamed, "Colliding destination names were renamed for this occurrence.", index)
		}
		if part.Source != "" {
			root, e := detachOutlineRoot(s, part.Owner)
			if e != nil {
				return result, e
			}
			importedOutlines = append(importedOutlines, root)
		}
		records, e := fields(c, s)
		if e != nil {
			return result, e
		}
		if dest == nil {
			dest = s
		} else {
			if err = pdfcpu.MergeXRefTables(c, filepath.Base(part.Source), s, dest, false, false); err != nil {
				return result, err
			}
		}
		for _, record := range records {
			actual, e := qualifiedName(dest, record.dict)
			if e != nil {
				return result, e
			}
			if actual != record.name {
				result.FieldsRenamed[index] = append(result.FieldsRenamed[index], packet.FieldRename{From: record.name, To: actual})
			}
		}
		if len(result.FieldsRenamed[index]) > 0 {
			add(diag.FormFieldsRenamed, "pdfcpu namespaced fields in this occurrence; the report contains the actual mapping.", index)
		}
	}
	current = nil
	if dest == nil {
		return result, errors.New("no parts to assemble")
	}
	if err = rebuildDestinations(c, dest, allDests); err != nil {
		return result, err
	}
	if err = graftOutlines(c, dest, layout, importedOutlines); err != nil {
		return result, err
	}
	if err = finalizeGeneratedLinks(c, dest, generatedLinks); err != nil {
		return result, err
	}
	if err = applyPageNumbers(c, dest, layout, fonts, names); err != nil {
		return result, err
	}
	if err = cleanCatalog(dest, compatibility.version, needAppearances); err != nil {
		return result, err
	}
	if err = api.OptimizeContext(c, dest); err != nil {
		return result, err
	}
	if err = api.ValidateContext(c, dest); err != nil {
		return result, err
	}
	if err = freeUnreachable(c, dest); err != nil {
		return result, err
	}
	output := &outputWriter{writer: w}
	err = api.WriteContext(c, dest, output)
	if output.err != nil {
		return result, &policyError{code: diag.OutputWriteFailed, err: output.err}
	}
	return result, err
}

// outputWriter distinguishes destination I/O failures from backend serialization
// errors returned by the same WriteContext call.
type outputWriter struct {
	writer io.Writer
	err    error
}

func (w *outputWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if w.err == nil {
		w.err = err
	}
	return n, err
}

var catalogKeys = map[string]bool{"Type": true, "Pages": true, "AcroForm": true, "Names": true, "Dests": true, "Outlines": true, "Version": true}

func cleanCatalog(s *model.Context, version model.Version, needAppearances bool) error {
	cat, err := s.Catalog()
	if err != nil {
		return err
	}
	for key := range cat {
		if !catalogKeys[key] {
			delete(cat, key)
		}
	}
	for key := range s.Names {
		if key != "Dests" {
			delete(s.Names, key)
		}
	}
	form, err := s.DereferenceDict(cat["AcroForm"])
	if err != nil {
		return err
	}
	delete(form, "XFA")
	if len(form) > 0 && needAppearances {
		form["NeedAppearances"] = types.Boolean(true)
	}
	cat["Version"] = types.Name(version.String())
	s.HeaderVersion = &version
	s.RootVersion = &version
	// These caches would otherwise restore removed catalog structures at write time.
	s.Info = nil
	return nil
}

// Verify reopens the serialized output, checking page count and final catalog policy.
func Verify(c context.Context, path string, expected int64) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = refuse(diag.OutputInvalid, fmt.Sprintf("Output parser panic: %v", value))
		}
	}()
	data, err := files.Read(c, path, 0)
	if err != nil {
		return &policyError{code: diag.OutputInvalid, err: err}
	}
	s, err := read(c, data)
	if err != nil {
		return &policyError{code: diag.OutputInvalid, err: err}
	}
	if int64(s.PageCount) != expected {
		return refuse(diag.OutputPageCountMismatch, fmt.Sprintf("Expected %d output pages; found %d.", expected, s.PageCount))
	}
	cat, err := s.Catalog()
	if err != nil {
		return &policyError{code: diag.OutputInvalid, err: err}
	}
	for key := range cat {
		if !catalogKeys[key] {
			return refuse(diag.OutputInvalid, "Unexpected output catalog key: "+key)
		}
	}
	names, err := s.DereferenceDict(cat["Names"])
	if err != nil {
		return err
	}
	for key := range names {
		if key != "Dests" {
			return refuse(diag.OutputInvalid, "Unexpected output name tree: "+key)
		}
	}
	form, err := s.DereferenceDict(cat["AcroForm"])
	if err != nil {
		return err
	}
	if form["XFA"] != nil {
		return refuse(diag.OutputInvalid, "XFA remains in output.")
	}
	return nil
}

// Diagnostic preserves typed product failures at the application boundary.
func Diagnostic(err error) diag.Diagnostic { return diagnostic(err, diag.AssemblyFailed) }
