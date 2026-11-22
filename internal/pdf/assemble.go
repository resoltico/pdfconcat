// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"path/filepath"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

// Assemble imports each occurrence independently and writes only after final validation.
func Assemble(c context.Context, a Assembly, w io.Writer) (result Assembled, err error) {
	var current *Part
	defer func() {
		if value := recover(); value != nil {
			err = pdfFailure(diag.AssemblyFailed, diag.ThePDFBackendFailedDuringAssembly(), fmt.Errorf("%v", value))
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
	result.Parts = make(map[packet.PartIdentity]PartResult, len(a.Parts))
	for _, part := range a.Parts {
		key := part.Identity()
		if _, exists := result.Parts[key]; exists {
			return result, errors.New("duplicate assembly part identity")
		}
		result.Parts[key] = PartResult{}
	}
	var dest *model.Context
	allDests := map[string]destination{}
	importedNavigation := map[navigationOwner]observedNavigation{}
	usedNames := map[string]bool{}
	var importedOutlines []importedOutlineRoot
	var generatedLinks []generatedLink
	var structures []structurePart
	compatibility := CheckCompatibility(a)
	for _, d := range compatibility.Diagnostics {
		if d.Severity == diag.Error {
			return result, &policyError{finding: d, err: errors.New(d.Message)}
		}
	}
	result.Diagnostics = append(result.Diagnostics, compatibility.Diagnostics...)
	needAppearances := false
	for index, part := range a.Parts {
		current = &a.Parts[index]
		if err = c.Err(); err != nil {
			return result, err
		}
		var s *model.Context
		var origins sourceOrigins
		if part.Source != "" {
			inspected := part.Occurrence.File
			s, origins, err = admitSnapshot(c, part, SourcePolicies{Structure: a.Structure, OptionalContent: a.OptionalContent})
			if err != nil {
				return result, err
			}
			occurrence := part.Occurrence.Facts
			removed, e := projectSelection(c, s, occurrence.Selection, origins)
			if e != nil {
				return result, e
			}
			for _, d := range navigationDiagnostics(removed) {
				d.File = part.Source
				d.Items = []string{part.Ref}
				d.Identity.Phase = "assembly"
				result.Diagnostics = append(result.Diagnostics, d)
			}
			uri, e := uriBaseWarnings(c, s, origins, true)
			for _, d := range uri {
				d.File = part.Source
				d.Items = []string{part.Ref}
				result.Diagnostics = append(result.Diagnostics, d)
			}
			if e != nil {
				return result, e
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
				return result, pdfFailure(diag.SourceInvalid, diag.TheSourceStructureCannotBePreserved(), err)
			}
		} else {
			if err = discardStructure(c, s); err != nil {
				return result, err
			}
		}
		if part.Source != "" {
			err = fitContext(c, s, part.Occurrence.Facts.fitting, origins)
		} else {
			pages, e := rawPages(c, s)
			if e != nil {
				return result, e
			}
			geometries, e := geometriesForPages(c, s, pages)
			if e != nil {
				return result, e
			}
			origins, e = sourceOriginsForPages(c, s, pages)
			if e != nil {
				return result, e
			}
			plans, e := planFitting(geometries, a.Fitting, part.Repeat, origins)
			if e != nil {
				return result, e
			}
			err = fitGeometries(c, s, geometries, plans, origins)
		}
		if err != nil {
			return result, err
		}
		if err = checkRetainedSourceReferences(c, s, origins); err != nil {
			return result, err
		}
		detached, renamed, e := detachDestinations(c, s, usedNames, part.FirstPage, origins)
		if e != nil {
			return result, e
		}
		for name, d := range detached {
			allDests[name] = d
		}
		if part.Source != "" {
			e = observeNavigation(c, s, detached, func(owner navigationOwner, found observedNavigation) error {
				if !found.symbolic {
					found.value.page += int(part.FirstPage) - 1
				}
				importedNavigation[owner] = found
				return nil
			})
			if e != nil {
				return result, e
			}
		}
		for _, rename := range renamed {
			for _, site := range rename.sites {
				d := sourceWarning(diag.DestinationsRenamed, diag.CollidingDestinationNamesWereRenamedForThisOccurrence(), diag.OriginalDestinationQIsNowQ(rename.from, rename.to), site)
				d.File = part.Source
				d.Items = []string{part.Ref}
				d.Identity.Phase = "assembly"
				result.Diagnostics = append(result.Diagnostics, d)
			}
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
			structure, e := captureStructurePart(s, root)
			if e != nil {
				return result, e
			}
			structures = append(structures, structure)
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
		renames := []packet.FieldRename{}
		for _, record := range records {
			actual, e := qualifiedName(dest, record.dict)
			if e != nil {
				return result, e
			}
			if actual != record.name {
				renames = append(renames, packet.FieldRename{From: record.name, To: actual})
				d := sourceWarning(diag.FormFieldsRenamed, diag.PdfcpuNamespacedFieldsInThisOccurrenceTheReportContainsThe(), diag.OriginalFieldQIsNowQ(record.name, actual), record.site)
				d.File = part.Source
				d.Items = []string{part.Ref}
				d.Identity.Phase = "assembly"
				result.Diagnostics = append(result.Diagnostics, d)
			}
		}
		result.Parts[part.Identity()] = PartResult{Known: true, Renames: renames}
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
	dest.ZeroOffsetPolicy = model.RejectZeroOffsets
	dest.Configuration.ValidationMode = model.ValidationStrict
	dest.XRefTable.ValidationMode = model.ValidationStrict
	if err = api.OptimizeContext(c, dest); err != nil {
		return result, err
	}
	report, validationErr := validatePreservingPages(c, dest)
	if validationErr != nil {
		return result, pdfFailure(diag.AssemblyFailed, diag.TheComposedPDFFailedFinalValidation(), validationErr)
	}
	if _, repair := validationNotices(report); repair != nil {
		return result, pdfFailure(diag.AssemblyFailed, diag.TheComposedPDFRequiredAnUnsupportedRepair(), repair)
	}
	if err = freeUnreachable(c, dest); err != nil {
		return result, err
	}
	if err = verifyPlannedGeometry(c, dest, a); err != nil {
		return result, pdfFailure(diag.AssemblyFailed, diag.TheComposedPDFFailedFinalValidation(), err)
	}
	navigation, err := captureOutputNavigation(c, dest, importedNavigation, allDests)
	if err != nil {
		return result, pdfFailure(diag.AssemblyFailed, diag.TheComposedPDFFailedFinalValidation(), err)
	}
	result.navigation = &navigation
	output := &outputWriter{writer: w}
	err = serialize(c, dest, output)
	if output.err != nil {
		return result, &policyError{finding: diag.Report(diag.OutputWriteFailed, diag.OutputWritingFailure(output.err.Error())), err: output.err}
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

var retainedSourceCatalogKeys = map[string]bool{
	"Type": true, "Pages": true, "AcroForm": true, "Names": true, "Dests": true, "Outlines": true,
}

func outputCatalogKey(key string) bool {
	switch key {
	case "Version", "StructTreeRoot", "MarkInfo", "Lang", "Metadata", "ViewerPreferences", "PageLabels":
		return true
	default:
		return retainedSourceCatalogKeys[key]
	}
}

func cleanCatalog(s *model.Context, version model.Version, needAppearances bool) error {
	cat, err := s.Catalog()
	if err != nil {
		return err
	}
	for key := range cat {
		if !retainedSourceCatalogKeys[key] {
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
	s.Info, s.DirectInfoDict = nil, nil
	return nil
}

// verify owns reopening, panic classification and the serialized output checks.
func verify(c context.Context, reader io.ReadSeeker, expected int64, a *Assembly, navigation *navigationObservation) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = pdfFailure(diag.OutputInvalid, diag.TheComposedPDFCouldNotBeVerified(), fmt.Errorf("%v", value))
		}
	}()
	s, err := readSerializedOutput(c, reader, navigation)
	if err != nil {
		return pdfFailure(diag.OutputInvalid, diag.TheComposedPDFFailedOutputVerification(), err)
	}
	if int64(s.PageCount) != expected {
		return refuse(diag.OutputPageCountMismatch, diag.ExpectedDOutputPagesFoundD(expected, int64(s.PageCount)))
	}
	if a != nil {
		if err = verifyPlannedGeometry(c, s, *a); err != nil {
			return pdfFailure(diag.OutputInvalid, diag.TheComposedPDFFailedOutputVerification(), err)
		}
		if err = verifyDocumentFacts(c, s, *a); err != nil {
			return pdfFailure(diag.OutputInvalid, diag.TheSerializedPDFDisagreesWithTheResolvedContract(), err)
		}
	}
	return nil
}

func admitSnapshot(c context.Context, part Part, policies SourcePolicies) (s *model.Context, origins sourceOrigins, err error) {
	changed := func() error {
		return refuse(diag.SourceChanged, diag.SourceBytesChangedAfterInspectionS(part.Source))
	}
	if part.Input == nil {
		return nil, sourceOrigins{}, changed()
	}
	reader, digest, err := part.Input.Open(c)
	if err != nil {
		return nil, sourceOrigins{}, snapshotFailure(part.Source, err)
	}
	defer func() {
		closeErr := reader.Close()
		if closeErr == nil {
			return
		}
		if err == nil {
			err = snapshotFailure(part.Source, closeErr)
			return
		}
		finding := diagnostic(err, diag.AssemblyFailed)
		if finding.Detail != "" {
			finding.Detail += "\n"
		}
		finding.Detail += snapshotFailure(part.Source, closeErr).finding.Detail
		err = &policyError{finding: finding, err: errors.Join(err, closeErr)}
	}()
	if digest != part.Occurrence.File.SHA256 {
		return nil, sourceOrigins{}, changed()
	}
	// Re-admission is the fallback for mutable backend graphs. The acquired
	// bytes are fixed, and the compiled selection facts retain original sites.
	s, origins, _, err = readSourceReader(c, reader, policies)
	if err == nil {
		// The read stage materializes lazy parsed objects before returning.
		s.Read.RS = nil
	}
	return s, origins, err
}

func snapshotFailure(path string, err error) *policyError {
	if d, ok := diag.SourceScratchDiagnostic(err, path); ok {
		return &policyError{finding: d, err: err}
	}
	cause := diag.IOCause{Reason: err.Error(), File: path}
	var native *fs.PathError
	if errors.As(err, &native) {
		cause.Reason, cause.NativeDetail = native.Err.Error(), err.Error()
	}
	return &policyError{finding: diag.IOFailure(diag.SourceChanged, cause), err: err}
}

// Diagnostic preserves typed product failures at the application boundary.
func Diagnostic(err error) diag.Diagnostic { return diagnostic(err, diag.AssemblyFailed) }

// Verify checks serialized page count and catalog policy without authored expectations.
func Verify(c context.Context, data []byte, expected int64) error {
	return verify(c, bytes.NewReader(data), expected, nil, nil)
}
