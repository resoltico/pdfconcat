// SPDX-License-Identifier: AGPL-3.0-only
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
)

// Assemble imports each occurrence independently and writes only after final validation.
func Assemble(c context.Context, a Assembly, w io.Writer) (result Assembled, err error) {
	var current *Part
	defer func() {
		if value := recover(); value != nil {
			err = pdfFailure(diag.AssemblyFailed, "The PDF backend failed during assembly.", fmt.Errorf("%v", value))
		}
		if err != nil && current != nil {
			d := diagnostic(err, diag.AssemblyFailed)
			d.File, d.Items = current.Source, []string{current.Ref}
			err = &policyError{finding: d, err: err}
		}
	}()
	if err = c.Err(); err != nil {
		return result, err
	}
	names, nameErr := generatedFontNames(a.Fonts, a.Parts)
	if nameErr != nil {
		return result, nameErr
	}
	result.Renames = make([][]packet.FieldRename, len(a.Parts))
	for i, part := range a.Parts {
		if part.Kind == packet.PartSource {
			result.Renames[i] = []packet.FieldRename{}
		}
	}
	var dest *model.Context
	allDests := map[string]destination{}
	usedNames := map[string]bool{}
	var importedOutlines []importedOutlineRoot
	var generatedLinks []generatedLink
	var structures []structurePart
	compatibility := CheckCompatibility(a)
	if len(compatibility.Diagnostics) > 0 {
		d := compatibility.Diagnostics[0]
		return result, &policyError{finding: d, err: errors.New(d.Message)}
	}
	needAppearances := false
	add := func(code diag.Code, message string, index int) {
		d := diag.New(code, message, a.Parts[index].Ref)
		d.File = a.Parts[index].Source
		result.Diagnostics = append(result.Diagnostics, d)
	}
	for index, part := range a.Parts {
		current = &a.Parts[index]
		if err = c.Err(); err != nil {
			return result, err
		}
		var s *model.Context
		if part.Source != "" {
			data, e := files.Read(c, part.Source, 0)
			if e != nil {
				return result, pdfFailure(diag.SourceChanged, "The inspected source is no longer readable.", e)
			}
			inspected := part.Occurrence.File
			if sha256.Sum256(data) != inspected.SHA256 {
				return result, refuse(diag.SourceChanged, "Source bytes changed after inspection: "+part.Source)
			}
			// Inspection already reported source findings; re-admission enforces the
			// same policy against the bytes just checked above.
			s, _, err = readSource(c, data)
			if err != nil {
				return result, err
			}
			occurrence := part.Occurrence.Facts
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
			count := len(part.Generated)
			if part.Repeat > 1 {
				if len(part.Generated) != 1 {
					return result, ErrInvalidPage
				}
				count = part.Repeat
			}
			if count != part.Pages {
				return result, ErrInvalidPage
			}
			s, links, err = generatedContext(c, part.Generated, count, a.Fonts, names)
			if err != nil {
				return result, err
			}
			generatedLinks = append(generatedLinks, links...)
		}
		if a.Structure != packet.StructureDiscard {
			if err = pruneStructure(c, s); err != nil {
				return result, pdfFailure(diag.SourceInvalid, "The source structure cannot be preserved.", err)
			}
		} else {
			if err = discardStructure(c, s); err != nil {
				return result, err
			}
		}
		if a.FitTo != nil {
			if err = fitContext(c, s, a.FitTo); err != nil {
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
		if a.Structure != packet.StructureDiscard {
			root, e := structureRoot(s)
			if e != nil {
				return result, e
			}
			cat, e := s.Catalog()
			if e != nil {
				return result, e
			}
			structures = append(structures, structurePart{root: root, catalog: cat})
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
				result.Renames[index] = append(result.Renames[index], packet.FieldRename{From: record.name, To: actual})
			}
		}
		if len(result.Renames[index]) > 0 {
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
	if err = graftOutlines(c, dest, a, importedOutlines); err != nil {
		return result, err
	}
	if err = finalizeGeneratedLinks(c, dest, generatedLinks); err != nil {
		return result, err
	}
	if err = applyPageNumbers(c, dest, a.Parts, a.Fonts, names, a.NumberLayout, a.Numbering); err != nil {
		return result, err
	}
	if err = cleanCatalog(dest, compatibility.version, needAppearances); err != nil {
		return result, err
	}
	if a.Structure != packet.StructureDiscard {
		if err = combineStructure(c, dest, structures); err != nil {
			return result, err
		}
	}
	if err = setDocumentMetadata(dest, a.Metadata, a.Timestamp); err != nil {
		return result, err
	}
	if err = setPageLabels(dest, a.Numbering); err != nil {
		return result, err
	}
	// Source tolerance must not repair defects introduced by composition.
	dest.Configuration.ValidationMode = model.ValidationStrict
	dest.XRefTable.ValidationMode = model.ValidationStrict
	if err = api.OptimizeContext(c, dest); err != nil {
		return result, err
	}
	report, validationErr := validatePreservingPages(c, dest)
	if validationErr != nil {
		return result, pdfFailure(diag.AssemblyFailed, "The composed PDF failed final validation.", validationErr)
	}
	if _, repair := validationNotices(report); repair != nil {
		return result, pdfFailure(diag.AssemblyFailed, "The composed PDF required an unsupported repair.", repair)
	}
	if err = freeUnreachable(c, dest); err != nil {
		return result, err
	}
	output := &outputWriter{writer: w}
	err = serialize(c, dest, output)
	if output.err != nil {
		return result, &policyError{finding: diag.New(diag.OutputWriteFailed, output.err.Error()), err: output.err}
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

var catalogKeys = map[string]bool{"Type": true, "Pages": true, "AcroForm": true, "Names": true, "Dests": true, "Outlines": true, "Version": true, "StructTreeRoot": true, "MarkInfo": true, "Lang": true, "Metadata": true, "ViewerPreferences": true, "PageLabels": true}

func cleanCatalog(s *model.Context, version model.Version, needAppearances bool) error {
	cat, err := s.Catalog()
	if err != nil {
		return err
	}
	for _, key := range []string{"StructTreeRoot", "MarkInfo", "Lang", "Metadata", "ViewerPreferences", "PageLabels"} {
		delete(cat, key)
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

// verify owns reopening, panic classification and the serialized output checks.
func verify(c context.Context, path string, expected int64, a *Assembly) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = pdfFailure(diag.OutputInvalid, "The composed PDF could not be verified.", fmt.Errorf("%v", value))
		}
	}()
	data, err := files.Read(c, path, 0)
	if err != nil {
		return pdfFailure(diag.OutputInvalid, "The composed PDF failed output verification.", err)
	}
	s, err := readOutput(c, data)
	if err != nil {
		return pdfFailure(diag.OutputInvalid, "The composed PDF failed output verification.", err)
	}
	if int64(s.PageCount) != expected {
		return refuse(diag.OutputPageCountMismatch, fmt.Sprintf("Expected %d output pages; found %d.", expected, s.PageCount))
	}
	if a != nil {
		if err = verifyDocumentFacts(c, s, *a); err != nil {
			return pdfFailure(diag.OutputInvalid, "The serialized PDF disagrees with the resolved contract.", err)
		}
	}
	return nil
}

// Diagnostic preserves typed product failures at the application boundary.
func Diagnostic(err error) diag.Diagnostic { return diagnostic(err, diag.AssemblyFailed) }

// Verify checks serialized page count and catalog policy without authored expectations.
func Verify(c context.Context, path string, expected int64) error {
	return verify(c, path, expected, nil)
}
