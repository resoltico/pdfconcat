// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/fault"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

// OccurrenceFacts describes one retained selection, without duplicating source identity.
type OccurrenceFacts struct {
	fittingPolicy        *packet.FitPolicy
	fitting              fittingPlan
	fitFindings          []diag.Diagnostic
	DestinationLocations map[string][]diag.PDFLocation
	packet.SourceFacts
	DestNames              []string
	PDF20AppearanceIssue   diag.AnnotationIssue
	AnnotationFittingIssue diag.AnnotationIssue
	OutlineDepth           int
	StructureCoverage      string
	HasUncoveredContent    bool
	UncoveredLocation      *diag.PDFLocation
	UnboundMCIDLocation    *diag.PDFLocation
}

// SourceFacts retains immutable whole-file identity and policy facts.
type SourceFacts struct {
	FieldCollectionLocation *diag.PDFLocation
	OccurrenceFacts
	SubsetPrefixes                         []string
	SHA256                                 [32]byte
	Bytes                                  int64
	Version                                string
	FormDefaultsDigest                     string
	HasFields, HasWidgets, NeedAppearances bool
}

type policyError struct {
	finding diag.Diagnostic
	err     error
}

func (e *policyError) Error() string { return e.finding.Message }
func (e *policyError) Unwrap() error { return e.err }
func refuse(code diag.Code, message diag.Explanation) error {
	return &policyError{finding: diag.Report(code, message), err: errors.New("PDF source refused")}
}

func pdfFailure(code diag.Code, message diag.Explanation, err error) error {
	d := diag.Report(code, message)
	d.Identity = diag.FindingIdentity{}
	d.Detail = strings.ToValidUTF8(err.Error(), "�")
	var policy *policyError
	if errors.As(err, &policy) {
		d.PDFLocation = policy.finding.PDFLocation
		if policy.finding.Detail != "" {
			d.Detail += ": " + policy.finding.Detail
		}
	}
	return &policyError{finding: d, err: err}
}

func diagnostic(err error, fallback diag.Code) diag.Diagnostic {
	code := fallback
	var policy *policyError
	errors.As(err, &policy)
	var backendFault fault.Panic
	if code == diag.SourceInvalid && errors.As(err, &backendFault) {
		code = diag.SourceProcessingFailed
	}
	d := diag.Report(code, diag.PDFProcessingFailure(err.Error()))
	if policy != nil {
		d = policy.finding
	}
	if policy == nil && code == diag.SourceInvalid {
		diag.ApplyExplanation(&d, diag.ThePDFCouldNotBeParsedOrValidatedObtainA())
		d.Identity = diag.FindingIdentity{}
		d.Detail = strings.ToValidUTF8(err.Error(), "�")
	}
	if policy == nil && code == diag.SourceProcessingFailed {
		diag.ApplyExplanation(&d, diag.ThePDFBackendFailedUnexpectedly())
		d.Identity = diag.FindingIdentity{}
		d.Detail = strings.ToValidUTF8(err.Error(), "�")
	}
	var semantic *structureIntegrityError
	if d.PDFLocation == nil && errors.As(err, &semantic) {
		loc := semantic.location
		d.PDFLocation = &loc
	}
	var attributed *model.ValidationError
	if d.PDFLocation == nil && errors.As(err, &attributed) && attributed.ObjectNumber() > 0 {
		d.PDFLocation = &diag.PDFLocation{Object: attributed.ObjectNumber()}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		interrupted := diag.Report(diag.Interrupted, diag.TheOperationWasInterrupted())
		d.Code, d.Severity, d.Message, d.Identity = interrupted.Code, interrupted.Severity, interrupted.Message, interrupted.Identity
	}
	return d
}

func readSource(c context.Context, data []byte, policy packet.StructurePolicy) (s *model.Context, origins sourceOrigins, ds []diag.Diagnostic, err error) {
	return readSourceReader(c, bytes.NewReader(data), SourcePolicies{Structure: policy})
}

// SourcePolicies are the effective source-admission choices for a packet.
type SourcePolicies struct {
	Structure       packet.StructurePolicy
	OptionalContent packet.OptionalContentPolicy
}

func readSourceReader(c context.Context, reader io.ReadSeeker, policies SourcePolicies) (s *model.Context, origins sourceOrigins, ds []diag.Diagnostic, err error) {
	defer func() {
		if value := recover(); value != nil {
			err = pdfFailure(diag.SourceProcessingFailed, diag.ThePDFBackendFailedUnexpectedly(), fmt.Errorf("%v", value))
		}
	}()
	// pdfcpu v0.16.4 scans the whole file, including displaced headers.
	// Recognition is deliberately weaker than parsing: a truncated recognizable
	// header is malformed PDF, not a different input format.
	// Most inputs start with the header. For displaced or truncated headers
	// use streaming recognition with overlap, then let pdfcpu parse the file.
	buffered := bufio.NewReaderSize(contentReader{context: c, reader: reader}, 4<<10)
	header := []byte("%PDF-")
	for {
		chunk, readErr := buffered.Peek(buffered.Size())
		if bytes.Contains(chunk, header) {
			break
		}
		if errors.Is(readErr, io.EOF) {
			return nil, origins, nil, refuse(diag.SourceNotPDF, diag.TheSourceIsNotAPDFNoRecognizablePDFHeader())
		}
		if readErr != nil {
			return nil, origins, nil, readErr
		}
		_, _ = buffered.Discard(len(chunk) - len(header) + 1)
	}
	if _, err = reader.Seek(0, io.SeekStart); err != nil {
		return nil, origins, nil, err
	}
	conf, err := configuration()
	if err != nil {
		return s, origins, ds, pdfFailure(diag.SourceProcessingFailed, diag.ThePDFBackendCouldNotBeInitialized(), err)
	}
	conf.ValidationMode = model.ValidationStrict
	conf.ZeroOffsetPolicy = model.AdmitUnreferencedZeroOffsets
	s, err = readPDFContext(c, reader, conf)
	if errors.Is(err, pdfcpu.ErrEncrypted) || errors.Is(err, pdfcpu.ErrWrongPassword) || errors.Is(err, pdfcpu.ErrUnsupportedEncryptionFeature) || s != nil && s.Encrypt != nil {
		return s, origins, ds, refuse(diag.SourceEncrypted, diag.EncryptedSourcesAreUnsupportedIncludingPDFsThatOpenWithoutA())
	}
	if err != nil {
		return s, origins, ds, err
	}
	if len(s.Read.MismatchedObjectHeaders) != 0 {
		if err := checkReadReferences(c, s, false); err != nil {
			return s, origins, ds, err
		}
	}
	for _, entry := range s.ZeroOffsetEntries {
		ds = append(ds, diag.IgnoredXRefEntry(diag.XRefEntry{Object: entry.Object, Generation: entry.Generation, Revision: entry.Revision, Offset: entry.XRefOffset}))
	}
	pages, e := rawPages(c, s)
	if e != nil {
		return s, origins, ds, e
	}
	s.PageCount = len(pages)
	originalContent, e := observePageContent(c, s, pages)
	if e != nil {
		return s, origins, ds, e
	}
	if e = originalSignatureChecks(c, s); e != nil {
		return s, origins, ds, e
	}
	layers, e := planOptionalContent(c, s, policies.OptionalContent, pages, originalContent)
	if e != nil {
		return s, origins, ds, e
	}
	if e = inspectRetainedDestinations(c, s, pages, true); e != nil {
		return s, origins, ds, e
	}
	undefinedReferences, e := isolateUndefinedSourceReferences(c, s)
	if e != nil {
		return s, origins, ds, e
	}
	findings, e := admitSourceStructure(c, s, policies.Structure)
	ds = append(ds, findings...)
	if e != nil {
		return s, origins, ds, pdfFailure(diag.SourceInvalid, diag.TheDeclaredSourceStructureCannotBePreservedUseStructureDiscard(), e)
	}
	pages, err = rawPages(c, s)
	if err != nil {
		return s, origins, ds, err
	}
	if err = checkDocument(c, s, pages); err != nil {
		return s, origins, ds, err
	}
	origins, err = sourceOriginsFor(c, s)
	if err != nil {
		return s, origins, ds, err
	}
	origins.undefinedReferences = undefinedReferences
	origins.catalog, e = captureCatalogSites(s, origins)
	if e != nil {
		return s, origins, ds, e
	}
	var restoreNamedOperands []func()
	origins.destinations, err = readDestinationDefinitions(c, s, &restoreNamedOperands)
	if err != nil {
		return s, origins, ds, err
	}
	cat, err := s.Catalog()
	if err != nil {
		return s, origins, ds, err
	}
	// Remove only source viewer directives, after inspecting the original graph.
	// Other discarded catalog structures still require their own validation.
	for _, key := range []string{"PageLayout", "PageMode", "ViewerPreferences"} {
		if _, present := cat[key]; !present {
			continue
		}
		delete(cat, key)
		d := diag.Report(diag.SourceViewerSettingsRemoved, diag.SourceViewerSettingsAreOmittedFromComposedOutput())
		d.PDFLocation = &diag.PDFLocation{Object: s.Root.ObjectNumber.Value(), Path: "Catalog." + key}
		ds = append(ds, d)
	}
	s.PageLayout, s.PageMode, s.ViewerPref = nil, nil, nil
	// Parsing is strict; semantic compatibility validation tolerates ancillary
	// producer differences while page/content preservation is checked directly.
	s.Configuration.ValidationMode = model.ValidationRelaxed
	s.XRefTable.ValidationMode = model.ValidationRelaxed
	observed, report, err := validateObservedPages(c, s, pages)
	for _, restore := range slices.Backward(restoreNamedOperands) {
		restore()
	}
	findings, repair := validationNotices(report)
	ds = append(ds, findings...)
	if err != nil {
		if repair != nil {
			ds = append(ds, diagnostic(repair, diag.SourceUnsupported))
		}
		return s, origins, ds, err
	}
	if repair != nil {
		return s, origins, ds, repair
	}
	if err = validateStructure(c, s); err != nil {
		return s, origins, ds, pdfFailure(diag.SourceInvalid, diag.TheSourceStructureIsMalformedOrUnsupported(), err)
	}
	if err = validateObservedEncoding(c, s, observed); err != nil {
		return s, origins, ds, err
	}
	if layers.group != (types.IndirectRef{}) {
		finding, e := layers.apply(c, s, pages)
		if e != nil {
			return s, origins, ds, e
		}
		ds = append(ds, finding)
	}
	return s, origins, ds, nil
}

// checkDocument enforces structural and fidelity safeguards on original source
// graphs and serialized output alike. Viewer normalization is not part of it.
func checkDocument(c context.Context, s *model.Context, pages []pageRef) error {
	s.PageCount = len(pages)
	if err := walkObjects(c, s, func(d types.Dict) error {
		if resource, ok := d["Resources"]; ok {
			resourceDict, e := s.DereferenceDict(resource)
			if e != nil {
				return e
			}
			resourceType, e := nameValue(s, resourceDict["Type"])
			if e != nil {
				return e
			}
			resourceFieldType, e := s.Dereference(resourceDict["FT"])
			if e != nil {
				return e
			}
			if resourceType == types.Name("Annot") || resourceFieldType != nil {
				return refuse(diag.SourceUnsupported, diag.AResourceDictionarySharedWithAnAnnotationOrFieldIs())
			}
		}
		subtype, e := nameValue(s, d["Subtype"])
		if e != nil {
			return e
		}
		if subtype == types.Name("Widget") {
			rect, e := s.DereferenceArray(d["Rect"])
			if e != nil || len(rect) != 4 {
				return refuse(diag.SourceInvalid, diag.WidgetRectMustContainFourCoordinates())
			}
		}
		fieldType, e := nameValue(s, d["FT"])
		if e != nil {
			return e
		}
		if fieldType == types.Name("Sig") {
			value, e := s.Dereference(d["V"])
			if e != nil {
				return e
			}
			if value != nil {
				return refuse(diag.SourceSigned, diag.APopulatedSignatureFieldIsUnsupportedBecauseCompositionCannotPreserve())
			}
		}
		kind, e := nameValue(s, d["Type"])
		if e != nil {
			return e
		}
		if kind == types.Name("Sig") || kind == types.Name("DocTimeStamp") {
			return refuse(diag.SourceSigned, diag.TheSourceDeclaresASignatureOrTimestampValueWhoseAssurance())
		}
		for key := range d {
			decoded, e := types.DecodeName(key)
			if e == nil && decoded != key {
				return refuse(diag.SourceUnsupported, diag.UnsupportedLiteralHashDictionaryKeyQUpstreamWouldChangeIts(key))
			}
		}
		return nil
	}); err != nil {
		return err
	}
	cat, err := s.Catalog()
	if err != nil {
		return err
	}
	for _, policy := range []struct {
		key     string
		code    diag.Code
		message diag.Explanation
	}{
		{"Perms", diag.SourceSigned, diag.CatalogPermissionStructuresRefused()},
		{"DocTimeStamp", diag.SourceSigned, diag.DocumentTimestampsRefused()},
	} {
		value, e := s.Dereference(cat[policy.key])
		if e != nil {
			return e
		}
		if value != nil {
			return refuse(policy.code, policy.message)
		}
	}
	rendering, e := s.Dereference(cat["NeedsRendering"])
	if e != nil {
		return e
	}
	if rendering == types.Boolean(true) {
		return refuse(diag.SourceDynamicForm, diag.TheSourceRequestsDynamicRenderingWhoseGeneratedAppearanceCompositionCannot())
	}
	if _, err := fields(c, s); err != nil {
		return err
	}
	return nil
}

type pageTreeNode struct {
	dict   types.Dict
	parent *pageTreeNode
}

type pageRef struct {
	ref    types.IndirectRef
	dict   types.Dict
	parent *pageTreeNode
}

// rawPages rejects content-replacing tree repair before pdfcpu validation runs.
func rawPages(c context.Context, s *model.Context) ([]pageRef, error) {
	if s.Root == nil || !liveReference(s, *s.Root) {
		return nil, sourceSite{path: "Trailer.Root"}.wrap(refuse(diag.SourceDamaged, diag.APageTreeReferenceIsMissingFreeOrHasThe()))
	}
	catalog, err := s.Catalog()
	if err != nil {
		return nil, err
	}
	capacity := min(max(s.PageCount, 0), 100_000)
	visited := make(map[types.IndirectRef]bool, capacity)
	pages := make([]pageRef, 0, capacity)
	var walk func(types.Object, int, *pageTreeNode) (int, error)
	walk = func(o types.Object, depth int, parent *pageTreeNode) (int, error) {
		if err := c.Err(); err != nil {
			return 0, err
		}
		if depth > 64 {
			return 0, refuse(diag.SourceUnsupported, diag.PageTreeExceedsTheSupportedDepthOf())
		}
		ref, indirect := o.(types.IndirectRef)
		if !indirect || visited[ref] {
			return 0, refuse(diag.SourceDamaged, diag.PageTreeContainsADirectRepeatedOrCyclicNode())
		}
		if !liveReference(s, ref) {
			return 0, refuse(diag.SourceDamaged, diag.APageTreeReferenceIsMissingFreeOrHasThe())
		}
		visited[ref] = true
		d, err := s.DereferenceDict(o)
		if err != nil || len(d) == 0 {
			return 0, refuse(diag.SourceDamaged, diag.PageTreeObjectSIsMissingOrEmpty(fmt.Sprint(ref)))
		}
		typ, err := s.Dereference(d["Type"])
		if err != nil {
			return 0, err
		}
		switch typ {
		case types.Name("Page"):
			kids, err := s.Dereference(d["Kids"])
			if err != nil {
				return 0, err
			}
			if kids != nil {
				return 0, refuse(diag.SourceDamaged, diag.APageHasNonNullKidsReadersMayDropIts())
			}
			pages = append(pages, pageRef{ref: ref, dict: d, parent: parent})
			return 1, nil
		case types.Name("Pages"):
			node := &pageTreeNode{dict: d, parent: parent}
			kids, err := s.DereferenceArray(d["Kids"])
			if err != nil || len(kids) == 0 {
				return 0, refuse(diag.SourceDamaged, diag.PagesRequiresNonemptyKids())
			}
			count := 0
			for _, kid := range kids {
				n, err := walk(kid, depth+1, node)
				if err != nil {
					return 0, err
				}
				count += n
			}
			v, err := s.Dereference(d["Count"])
			if err != nil || v != types.Integer(count) {
				return 0, refuse(diag.SourceDamaged, diag.PageTreeCountDisagreesWithDLeaves(int64(count)))
			}
			return count, nil
		default:
			return 0, refuse(diag.SourceDamaged, diag.PageTreeNodeSHasNoSupportedType(fmt.Sprint(ref)))
		}
	}
	_, err = walk(catalog["Pages"], 0, nil)
	return pages, err
}

// walkObjects visits direct containers in all materialized indirect objects once.
// Indirect links are handled by the outer xref loop, avoiding graph recursion.
func walkObjects(c context.Context, s *model.Context, visit func(types.Dict) error) error {
	return objectLocations(c, s, true, func(d types.Dict, _ diag.PDFLocation) error { return visit(d) })
}

func observe(c context.Context, s *model.Context, origins sourceOrigins, policy *packet.FitPolicy, geometries []pageGeometry) (SourceFacts, []diag.Diagnostic, error) {
	facts := SourceFacts{OccurrenceFacts: OccurrenceFacts{SourceFacts: packet.SourceFacts{Pages: s.PageCount}}, Version: s.XRefTable.Version().String()}
	if policy != nil {
		normalized, err := policy.Normalize()
		if err != nil {
			return facts, nil, err
		}
		facts.fittingPolicy = &normalized
	}
	prefixes, err := importedSubsetPrefixes(c, s)
	facts.SubsetPrefixes = prefixes
	if err != nil {
		return facts, nil, err
	}
	catalog, err := s.Catalog()
	if err != nil {
		return facts, nil, err
	}
	marked, e := markedContent(c, s)
	if e != nil {
		return facts, nil, e
	}
	facts.StructureCoverage = marked.coverage()
	facts.HasUncoveredContent = !marked.complete
	facts.UncoveredLocation = marked.uncovered
	facts.UnboundMCIDLocation = marked.unboundMCID

	var ds []diag.Diagnostic
	base := catalogSite(s)
	add := func(code diag.Code, message diag.Explanation, site sourceSite) {
		ds = append(ds, sourceWarning(code, message, diag.TheOriginalSourceEntryIsOmittedByOutputPolicy(), site))
	}

	for _, entry := range []struct {
		key  string
		code diag.Code
	}{{"PageLabels", diag.SourcePageLabelsRemoved}, {"OutputIntents", diag.SourceOutputIntentsRemoved}, {"OpenAction", diag.SourceDocumentActionsRemoved}, {"AA", diag.SourceDocumentActionsRemoved}, {"AF", diag.SourceAttachmentsRemoved}} {
		if catalog[entry.key] != nil {
			add(entry.code, diag.SWillBeRemovedFromTheOutputCatalog(entry.key), origins.catalogEntry(entry.key, base.key(entry.key)))
		}
	}
	names, err := s.DereferenceDict(catalog["Names"])
	if err != nil {
		return facts, ds, err
	}
	if names["JavaScript"] != nil {
		add(diag.SourceDocumentActionsRemoved, diag.DocumentJavaScriptWillBeRemoved(), origins.catalogEntry("Names.JavaScript", base.key("Names").anchor(catalog["Names"], origins).key("JavaScript")))
	}
	if names["EmbeddedFiles"] != nil {
		add(diag.SourceAttachmentsRemoved, diag.EmbeddedFileIndexesWillBeRemoved(), origins.catalogEntry("Names.EmbeddedFiles", base.key("Names").anchor(catalog["Names"], origins).key("EmbeddedFiles")))
	}
	form, err := s.DereferenceDict(catalog["AcroForm"])
	if err != nil {
		return facts, ds, err
	}
	if form["XFA"] != nil {
		add(diag.SourceXfaRemoved, diag.XFAWillBeRemovedStaticFieldsRemain(), origins.catalogEntry("AcroForm.XFA", base.key("AcroForm").anchor(catalog["AcroForm"], origins).key("XFA")))
	}
	rendering, err := s.Dereference(form["NeedAppearances"])
	if err != nil {
		return facts, ds, err
	}
	facts.NeedAppearances = rendering == types.Boolean(true)
	roots, err := s.DereferenceArray(form["Fields"])
	if err != nil {
		return facts, ds, err
	}
	facts.HasFields = len(roots) > 0
	if facts.HasFields {
		facts.FieldCollectionLocation = origins.catalogEntry("AcroForm.Fields", base.key("AcroForm").anchor(catalog["AcroForm"], origins).key("Fields")).location()
	}
	if facts.NeedAppearances && facts.HasFields {
		site := origins.catalogEntry("AcroForm.NeedAppearances", base.key("AcroForm").anchor(catalog["AcroForm"], origins).key("NeedAppearances"))
		ds = append(ds, sourceWarning(diag.FormAppearanceRegenerationRequested, diag.TheSourceRequestsViewerRegeneratedFieldAppearances(), diag.TheOriginalNeedAppearancesRequestIsRetainedAsAViewerRequest(), site))
	}
	if facts.HasFields {
		signature := types.Dict{"DR": form["DR"], "DA": form["DA"], "Q": form["Q"]}
		facts.FormDefaultsDigest, err = resourceIdentity(s, signature)
		if err != nil {
			return facts, ds, err
		}
	}
	err = walkObjects(c, s, func(d types.Dict) error {
		subtype, e := nameValue(s, d["Subtype"])
		if e != nil {
			return e
		}
		if subtype == types.Name("Widget") {
			facts.HasWidgets = true
		}

		return nil
	})
	if err != nil {
		return facts, ds, err
	}
	facts.fitting, err = planFitting(geometries, facts.fittingPolicy, 1, origins)
	if err != nil {
		return facts, ds, err
	}
	facts.PageSizes = fittingRuns(facts.fitting)
	facts.FirstSize = geometries[0].size
	facts.LastSize = geometries[len(geometries)-1].size

	destinations, err := readDestinations(c, s)
	if err != nil {
		return facts, ds, err
	}
	destinationNames := make([]string, 0, len(destinations))
	for name := range destinations {
		destinationNames = append(destinationNames, name)
	}
	slices.Sort(destinationNames)
	facts.DestinationLocations = map[string][]diag.PDFLocation{}
	for _, name := range destinationNames {
		d := destinations[name]
		sites := d.sites
		if original, ok := origins.destinations[name]; ok {
			sites = original.sites
		}
		if d.page < 1 {
			for _, site := range sites {
				ds = append(ds, sourceWarning(diag.DestinationsDropped, diag.ADestinationOutsideItsSourceDocumentWillBeOmitted(), diag.DestinationDefinitionQHasNoPageTargetInTheOriginal(name), site))
			}
			continue
		}
		facts.DestNames = append(facts.DestNames, name)
		for _, site := range sites {
			if location := site.location(); location != nil {
				facts.DestinationLocations[name] = append(facts.DestinationLocations[name], *location)
			}
		}
	}
	facts.PDF20AppearanceIssue, err = appearanceIssue(c, s)
	if err != nil {
		return facts, ds, err
	}
	facts.AnnotationFittingIssue, err = annotationFittingIssue(c, s, geometries, facts.fitting)
	if err != nil {
		return facts, ds, err
	}
	facts.Selection = []packet.PageRange{{First: 1, Last: s.PageCount}}
	facts.OutlineDepth, err = outlineDepth(c, s)
	if err != nil {
		return facts, ds, err
	}
	slices.Sort(facts.DestNames)
	return facts, ds, nil
}

func generation(e *model.XRefTableEntry) int {
	if e.Generation == nil {
		return 0
	}
	return *e.Generation
}

// liveReference checks identity before dereferencing: the backend's low-level
// dereference API can resolve an object number despite a generation mismatch.
func liveReference(s *model.Context, ref types.IndirectRef) bool {
	e := s.Table[ref.ObjectNumber.Value()]
	return e != nil && !e.Free && generation(e) == ref.GenerationNumber.Value()
}
