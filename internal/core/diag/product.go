// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package diag

import (
	"fmt"
	"strconv"
)

// AContentsTitleHasAWordWiderThanItsColumn renders the core-owned explanation.
func AContentsTitleHasAWordWiderThanItsColumn() Explanation {
	return explain("AContentsTitleHasAWordWiderThanItsColumn", "A contents title has a word wider than its column; shorten it or choose a larger canvas.")
}

// AContentsTitleHasNoVisibleRow renders the core-owned explanation.
func AContentsTitleHasNoVisibleRow() Explanation {
	return explain("AContentsTitleHasNoVisibleRow", "A contents title has no visible row.")
}

// AContentsTitleSPaintedInkCrossesItsNumberColumn renders the core-owned explanation.
func AContentsTitleSPaintedInkCrossesItsNumberColumn() Explanation {
	return explain("AContentsTitleSPaintedInkCrossesItsNumberColumn", "A contents title's painted ink crosses its number column; shorten it or choose a larger canvas.")
}

// ADestinationOutsideItsSourceDocumentWillBeOmitted renders the core-owned explanation.
func ADestinationOutsideItsSourceDocumentWillBeOmitted() Explanation {
	return explain("ADestinationOutsideItsSourceDocumentWillBeOmitted", "A destination outside its source document will be omitted.")
}

// AFormFieldReferenceIsMissingFreeOrHasThe renders the core-owned explanation.
func AFormFieldReferenceIsMissingFreeOrHasThe() Explanation {
	return explain("AFormFieldReferenceIsMissingFreeOrHasThe", "A form-field reference is missing, free, or has the wrong object generation.")
}

// APageContentStreamIsMissingHasTheWrongGeneration renders the core-owned explanation.
func APageContentStreamIsMissingHasTheWrongGeneration() Explanation {
	return explain("APageContentStreamIsMissingHasTheWrongGeneration", "A page content stream is missing, has the wrong generation, or is not a stream.")
}

// APageContentStreamReferencesAMissingOrFreeObject renders the core-owned explanation.
func APageContentStreamReferencesAMissingOrFreeObject() Explanation {
	return explain("APageContentStreamReferencesAMissingOrFreeObject", "A page content stream references a missing or free object, or the wrong object generation.")
}

// APageHasNonNullKidsReadersMayDropIts renders the core-owned explanation.
func APageHasNonNullKidsReadersMayDropIts() Explanation {
	return explain("APageHasNonNullKidsReadersMayDropIts", "A Page has non-null Kids; readers may drop its content.")
}

// APageTreeReferenceIsMissingFreeOrHasThe renders the core-owned explanation.
func APageTreeReferenceIsMissingFreeOrHasThe() Explanation {
	return explain("APageTreeReferenceIsMissingFreeOrHasThe", "A page-tree reference is missing, free, or has the wrong object generation.")
}

// APopulatedSignatureFieldIsUnsupportedBecauseCompositionCannotPreserve renders the core-owned explanation.
func APopulatedSignatureFieldIsUnsupportedBecauseCompositionCannotPreserve() Explanation {
	return explain("APopulatedSignatureFieldIsUnsupportedBecauseCompositionCannotPreserve", "A populated signature field is unsupported because composition cannot preserve its signature assurance. Deliver the original separately; unsigned null fields are allowed.")
}

// ARectangleMustContainFourCoordinates renders the core-owned explanation.
func ARectangleMustContainFourCoordinates() Explanation {
	return explain("ARectangleMustContainFourCoordinates", "A rectangle must contain four coordinates.")
}

// AResourceDictionarySharedWithAnAnnotationOrFieldIs renders the core-owned explanation.
func AResourceDictionarySharedWithAnAnnotationOrFieldIs() Explanation {
	return explain("AResourceDictionarySharedWithAnAnnotationOrFieldIs", "A resource dictionary shared with an annotation or field is unsupported; upstream validation would discard that role.")
}

// AStructureBoundingBoxHasNoPageScopeFittingCannot renders the core-owned explanation.
func AStructureBoundingBoxHasNoPageScopeFittingCannot() Explanation {
	return explain("AStructureBoundingBoxHasNoPageScopeFittingCannot", "A structure bounding box has no page scope; fitting cannot preserve its geometry. Use original page sizes or a reviewed source with explicit page association.")
}

// AWholeContentsRowCannotFitBeneathTheHeadingAnd renders the core-owned explanation.
func AWholeContentsRowCannotFitBeneathTheHeadingAnd() Explanation {
	return explain("AWholeContentsRowCannotFitBeneathTheHeadingAnd", "A whole contents row cannot fit beneath the heading and above the page-number exclusion; shorten the title, use a larger canvas or reduce page-number size/margin.")
}

// AnAbsentIDTreeIsDerivedFromUniqueValidatedStructureIDs renders the core-owned explanation.
func AnAbsentIDTreeIsDerivedFromUniqueValidatedStructureIDs() Explanation {
	return explain("AnAbsentIDTreeIsDerivedFromUniqueValidatedStructureIDs", "An absent IDTree is derived from unique validated structure IDs; retained semantics are unchanged.")
}

// AnOutlineReferenceIsMissingFreeOrHasTheWrong renders the core-owned explanation.
func AnOutlineReferenceIsMissingFreeOrHasTheWrong() Explanation {
	return explain("AnOutlineReferenceIsMissingFreeOrHasTheWrong", "An outline reference is missing, free, or has the wrong object generation.")
}

// AnOutputDestinationIsRequiredForBuild renders the core-owned explanation.
func AnOutputDestinationIsRequiredForBuild() Explanation {
	return explain("AnOutputDestinationIsRequiredForBuild", "An output destination is required for build.")
}

// AnUnclaimedStaleParentTreeEntryIsOmittedRetainedSemanticOwnership renders the core-owned explanation.
func AnUnclaimedStaleParentTreeEntryIsOmittedRetainedSemanticOwnership() Explanation {
	return explain("AnUnclaimedStaleParentTreeEntryIsOmittedRetainedSemanticOwnership", "An unclaimed stale ParentTree entry is omitted; retained semantic ownership is unchanged.")
}

// AssemblyFactsDoNotMatchTheirOwningParts renders the core-owned explanation.
func AssemblyFactsDoNotMatchTheirOwningParts() Explanation {
	return explain("AssemblyFactsDoNotMatchTheirOwningParts", "Assembly facts do not match their owning parts.")
}

// AuthoredSectionsAndImportedBookmarksExceedThePDFBackendS renders the core-owned explanation.
func AuthoredSectionsAndImportedBookmarksExceedThePDFBackendS() Explanation {
	return explain("AuthoredSectionsAndImportedBookmarksExceedThePDFBackendS", "Authored sections and imported bookmarks exceed the PDF backend's supported outline hierarchy; reduce section nesting or source bookmark depth.")
}

// CollidingDestinationNamesWereRenamedForThisOccurrence renders the core-owned explanation.
func CollidingDestinationNamesWereRenamedForThisOccurrence() Explanation {
	return explain("CollidingDestinationNamesWereRenamedForThisOccurrence", "Colliding destination names were renamed for this occurrence.")
}

// CompositionFactsDoNotMatchTheirOwningParts renders the core-owned explanation.
func CompositionFactsDoNotMatchTheirOwningParts() Explanation {
	return explain("CompositionFactsDoNotMatchTheirOwningParts", "Composition facts do not match their owning parts.")
}

// ContentsHeadingAndPageNumberExclusionLeaveNoRoomFor renders the core-owned explanation.
func ContentsHeadingAndPageNumberExclusionLeaveNoRoomFor() Explanation {
	return explain("ContentsHeadingAndPageNumberExclusionLeaveNoRoomFor", "Contents heading and page-number exclusion leave no room for entries; use a larger canvas or reduce page-number size/margin.")
}

// ContentsHeadingDoesNotFitItsWidthShortenItOr renders the core-owned explanation.
func ContentsHeadingDoesNotFitItsWidthShortenItOr() Explanation {
	return explain("ContentsHeadingDoesNotFitItsWidthShortenItOr", "Contents heading does not fit its width; shorten it or choose a larger canvas.")
}

// ContentsHeadingInkCrossesItsRightMarginShortenItOr renders the core-owned explanation.
func ContentsHeadingInkCrossesItsRightMarginShortenItOr() Explanation {
	return explain("ContentsHeadingInkCrossesItsRightMarginShortenItOr", "Contents heading ink crosses its right margin; shorten it or choose a larger canvas.")
}

// ContentsIndentationAndNumberColumnLeaveNoUsableTitleWidth renders the core-owned explanation.
func ContentsIndentationAndNumberColumnLeaveNoUsableTitleWidth() Explanation {
	return explain("ContentsIndentationAndNumberColumnLeaveNoUsableTitleWidth", "Contents indentation and number column leave no usable title width; reduce section nesting or choose a larger canvas.")
}

// ContentsMarginsLeaveNoUsableHeadingWidthChooseALarger renders the core-owned explanation.
func ContentsMarginsLeaveNoUsableHeadingWidthChooseALarger() Explanation {
	return explain("ContentsMarginsLeaveNoUsableHeadingWidthChooseALarger", "Contents margins leave no usable heading width; choose a larger canvas.")
}

// ContentsRequiresAtLeastOneAuthoredSection renders the core-owned explanation.
func ContentsRequiresAtLeastOneAuthoredSection() Explanation {
	return explain("ContentsRequiresAtLeastOneAuthoredSection", "Contents requires at least one authored section.")
}

// ContentsTitleAndNumberBoundsCannotFitInTheAvailable renders the core-owned explanation.
func ContentsTitleAndNumberBoundsCannotFitInTheAvailable() Explanation {
	return explain("ContentsTitleAndNumberBoundsCannotFitInTheAvailable", "Contents title and number bounds cannot fit in the available row; use a larger canvas or reduce page-number size/margin.")
}

// CyclicFormDefaultResourcesAreUnsupportedForCompatibilityComparison renders the core-owned explanation.
func CyclicFormDefaultResourcesAreUnsupportedForCompatibilityComparison() Explanation {
	return explain("CyclicFormDefaultResourcesAreUnsupportedForCompatibilityComparison", "Cyclic form default resources are unsupported for compatibility comparison.")
}

// DefaultMarginsLeaveNoUsableTextWidthGiveAnExplicit renders the core-owned explanation.
func DefaultMarginsLeaveNoUsableTextWidthGiveAnExplicit() Explanation {
	return explain("DefaultMarginsLeaveNoUsableTextWidthGiveAnExplicit", "Default margins leave no usable text width; give an explicit width of at least 1 point or a wider page.")
}

// DestinationDefinitionQHasNoPageTargetInTheOriginal renders the core-owned explanation.
func DestinationDefinitionQHasNoPageTargetInTheOriginal(originalName string) Explanation {
	return explain("DestinationDefinitionQHasNoPageTargetInTheOriginal", fmt.Sprintf("Destination definition %q has no page target in the original source.", originalName), originalName)
}

// DestinationNameTreeExceedsDepth renders the core-owned explanation.
func DestinationNameTreeExceedsDepth() Explanation {
	return explain("DestinationNameTreeExceedsDepth", "Destination name tree exceeds depth 64.")
}

// DestinationNameTreeRepeatsANode renders the core-owned explanation.
func DestinationNameTreeRepeatsANode() Explanation {
	return explain("DestinationNameTreeRepeatsANode", "Destination name tree repeats a node.")
}

// DirectItemsExceedTheAuthoredItemOrGeneratedPageBudget renders the core-owned explanation.
func DirectItemsExceedTheAuthoredItemOrGeneratedPageBudget() Explanation {
	return explain("DirectItemsExceedTheAuthoredItemOrGeneratedPageBudget", "Direct items exceed the authored item or generated-page budget.")
}

// DirectObjectNestingExceeds renders the core-owned explanation.
func DirectObjectNestingExceeds() Explanation {
	return explain("DirectObjectNestingExceeds", "Direct object nesting exceeds 128.")
}

// DocumentJavaScriptWillBeRemoved renders the core-owned explanation.
func DocumentJavaScriptWillBeRemoved() Explanation {
	return explain("DocumentJavaScriptWillBeRemoved", "Document JavaScript will be removed.")
}

// DuplicateDestinationByteStringKeysAreUnsupported renders the core-owned explanation.
func DuplicateDestinationByteStringKeysAreUnsupported() Explanation {
	return explain("DuplicateDestinationByteStringKeysAreUnsupported", "Duplicate destination byte-string keys are unsupported.")
}

// EffectiveSOURCEDATEEPOCHMustBeUnixSecondsFromThrough renders the core-owned explanation.
func EffectiveSOURCEDATEEPOCHMustBeUnixSecondsFromThrough() Explanation {
	return explain("EffectiveSOURCEDATEEPOCHMustBeUnixSecondsFromThrough", "Effective SOURCE_DATE_EPOCH must be Unix seconds from 0 through 253402300799.")
}

// EmbeddedFileIndexesWillBeRemoved renders the core-owned explanation.
func EmbeddedFileIndexesWillBeRemoved() Explanation {
	return explain("EmbeddedFileIndexesWillBeRemoved", "Embedded-file indexes will be removed.")
}

// EncryptedSourcesAreUnsupportedIncludingPDFsThatOpenWithoutA renders the core-owned explanation.
func EncryptedSourcesAreUnsupportedIncludingPDFsThatOpenWithoutA() Explanation {
	return explain("EncryptedSourcesAreUnsupportedIncludingPDFsThatOpenWithoutA", "Encrypted sources are unsupported, including PDFs that open without a password. Request an unrestricted, unencrypted assembly copy or deliver the originals separately.")
}

// ExpectedDOutputPagesFoundD renders the core-owned explanation.
func ExpectedDOutputPagesFoundD(expectedPages int64, actualPages int64) Explanation {
	return explain("ExpectedDOutputPagesFoundD", fmt.Sprintf("Expected %d output pages; found %d.", expectedPages, actualPages), strconv.FormatInt(expectedPages, 10), strconv.FormatInt(actualPages, 10))
}

// FittingInternalPageDestinationsWithNumericPositionCoordinatesIsUnsupported renders the core-owned explanation.
func FittingInternalPageDestinationsWithNumericPositionCoordinatesIsUnsupported() Explanation {
	return explain("FittingInternalPageDestinationsWithNumericPositionCoordinatesIsUnsupported", "A retained local destination has numeric position coordinates on a page that transforms; use original page sizes or a destination without position coordinates.")
}

// FittingSourceFormWidgetsIsUnsupported renders the core-owned explanation.
func FittingSourceFormWidgetsIsUnsupported() Explanation {
	return explain("FittingSourceFormWidgetsIsUnsupported", "Fitting source form widgets is unsupported.")
}

// FontPostScriptNameQContainsALiteralHashFollowedBy renders the core-owned explanation.
func FontPostScriptNameQContainsALiteralHashFollowedBy(postScriptName string) Explanation {
	return explain("FontPostScriptNameQContainsALiteralHashFollowedBy", fmt.Sprintf("Font PostScript name %q contains a literal hash followed by two hexadecimal digits that the PDF backend may decode again; use a font with a different PostScript name.", postScriptName), postScriptName)
}

// FormDefaultResourcesAreNestedTooDeeplyForCompatibilityComparison renders the core-owned explanation.
func FormDefaultResourcesAreNestedTooDeeplyForCompatibilityComparison() Explanation {
	return explain("FormDefaultResourcesAreNestedTooDeeplyForCompatibilityComparison", "Form default resources are nested too deeply for compatibility comparison.")
}

// FormFieldTreeExceedsTheSupportedDepthOf renders the core-owned explanation.
func FormFieldTreeExceedsTheSupportedDepthOf() Explanation {
	return explain("FormFieldTreeExceedsTheSupportedDepthOf", "Form field tree exceeds the supported depth of 64.")
}

// FormFieldTreeRepeatsANode renders the core-owned explanation.
func FormFieldTreeRepeatsANode() Explanation {
	return explain("FormFieldTreeRepeatsANode", "Form field tree repeats a node.")
}

// InvalidApplicationRequest renders the core-owned explanation.
func InvalidApplicationRequest() Explanation {
	return explain("InvalidApplicationRequest", "Invalid application request.")
}

// MeasuredTextExtendsOutsideThePageOrTextBlock renders the core-owned explanation.
func MeasuredTextExtendsOutsideThePageOrTextBlock() Explanation {
	return explain("MeasuredTextExtendsOutsideThePageOrTextBlock", "Measured text extends outside the page or text block.")
}

// NavigationTargetsOmittedFromThisSourceOccurrenceWereRemoved renders the core-owned explanation.
func NavigationTargetsOmittedFromThisSourceOccurrenceWereRemoved() Explanation {
	return explain("NavigationTargetsOmittedFromThisSourceOccurrenceWereRemoved", "Navigation targets omitted from this source occurrence were removed.")
}

// NoStructureHierarchyIsSuppliedOrphanStructureClaimsOwnershipAre renders the core-owned explanation.
func NoStructureHierarchyIsSuppliedOrphanStructureClaimsOwnershipAre() Explanation {
	return explain("NoStructureHierarchyIsSuppliedOrphanStructureClaimsOwnershipAre", "No structure hierarchy is supplied; orphan structure claims/ownership are omitted and meaningful source content remains untagged.")
}

// NonemptyFormsRequireIdenticalResolvedDRDAQRepresentationsThese renders the core-owned explanation.
func NonemptyFormsRequireIdenticalResolvedDRDAQRepresentationsThese() Explanation {
	return explain("NonemptyFormsRequireIdenticalResolvedDRDAQRepresentationsThese", "Nonempty forms require identical resolved DR/DA/Q representations; these differ; assemble separately or flatten with a suitable PDF tool.")
}

// OddDestinationNameArray renders the core-owned explanation.
func OddDestinationNameArray() Explanation {
	return explain("OddDestinationNameArray", "Odd destination name array.")
}

// OriginalDestinationQIsNowQ renders the core-owned explanation.
func OriginalDestinationQIsNowQ(originalName string, replacementName string) Explanation {
	return explain("OriginalDestinationQIsNowQ", fmt.Sprintf("Original destination %q is now %q.", originalName, replacementName), originalName, replacementName)
}

// OriginalFieldQIsNowQ renders the core-owned explanation.
func OriginalFieldQIsNowQ(originalName string, replacementName string) Explanation {
	return explain("OriginalFieldQIsNowQ", fmt.Sprintf("Original field %q is now %q.", originalName, replacementName), originalName, replacementName)
}

// OutlineNodesMustBeDistinctIndirectObjects renders the core-owned explanation.
func OutlineNodesMustBeDistinctIndirectObjects() Explanation {
	return explain("OutlineNodesMustBeDistinctIndirectObjects", "Outline nodes must be distinct indirect objects.")
}

// PDFValidationChangedThePageSequenceOrOriginalEncodedContent renders the core-owned explanation.
func PDFValidationChangedThePageSequenceOrOriginalEncodedContent() Explanation {
	return explain("PDFValidationChangedThePageSequenceOrOriginalEncodedContent", "PDF validation changed the page sequence or original encoded content streams.")
}

// PageContentCouldNotBeDecodedCompletelyObtainAReviewed renders the core-owned explanation.
func PageContentCouldNotBeDecodedCompletelyObtainAReviewed() Explanation {
	return explain("PageContentCouldNotBeDecodedCompletelyObtainAReviewed", "Page content could not be decoded completely. Obtain a reviewed repair or deliver the original separately.")
}

// PageContentStreamsMustBeIndirectReferencesToExistingStreams renders the core-owned explanation.
func PageContentStreamsMustBeIndirectReferencesToExistingStreams() Explanation {
	return explain("PageContentStreamsMustBeIndirectReferencesToExistingStreams", "Page content streams must be indirect references to existing streams.")
}

// PageContentUsesAnUnsupportedEncodingSupplyAReviewedReadable renders the core-owned explanation.
func PageContentUsesAnUnsupportedEncodingSupplyAReviewedReadable() Explanation {
	return explain("PageContentUsesAnUnsupportedEncodingSupplyAReviewedReadable", "Page content uses an unsupported encoding. Supply a reviewed, readable PDF or deliver the original separately.")
}

// PageContentsMustResolveToAStreamOrAnArray renders the core-owned explanation.
func PageContentsMustResolveToAStreamOrAnArray() Explanation {
	return explain("PageContentsMustResolveToAStreamOrAnArray", "Page Contents must resolve to a stream or an array of existing streams.")
}

// PageContentsReferencesAMissingOrFreeObjectOrThe renders the core-owned explanation.
func PageContentsReferencesAMissingOrFreeObjectOrThe() Explanation {
	return explain("PageContentsReferencesAMissingOrFreeObjectOrThe", "Page Contents references a missing or free object, or the wrong object generation.")
}

// PageHasNoEffectiveMediaBox renders the core-owned explanation.
func PageHasNoEffectiveMediaBox() Explanation {
	return explain("PageHasNoEffectiveMediaBox", "Page has no effective MediaBox.")
}

// PageMustHaveFinitePositiveDisplayedPhysicalDimensions renders the core-owned explanation.
func PageMustHaveFinitePositiveDisplayedPhysicalDimensions() Explanation {
	return explain("PageMustHaveFinitePositiveDisplayedPhysicalDimensions", "Page must have finite positive displayed physical dimensions.")
}

// PageNumberExclusionLeavesNoUsableTextHeight renders the core-owned explanation.
func PageNumberExclusionLeavesNoUsableTextHeight() Explanation {
	return explain("PageNumberExclusionLeavesNoUsableTextHeight", "Page-number exclusion leaves no usable text height.")
}

// PageRotationMustBeAFiniteMultipleOfDegrees renders the core-owned explanation.
func PageRotationMustBeAFiniteMultipleOfDegrees() Explanation {
	return explain("PageRotationMustBeAFiniteMultipleOfDegrees", "Page rotation must be a finite multiple of 90 degrees.")
}

// PageSEffectiveCropBoxMediaBoxFrameMustHaveFinitePositive renders the core-owned explanation.
func PageSEffectiveCropBoxMediaBoxFrameMustHaveFinitePositive() Explanation {
	return explain("PageSEffectiveCropBoxMediaBoxFrameMustHaveFinitePositive", "Page's effective CropBox–MediaBox frame must have finite positive dimensions.")
}

// PageTreeContainsADirectRepeatedOrCyclicNode renders the core-owned explanation.
func PageTreeContainsADirectRepeatedOrCyclicNode() Explanation {
	return explain("PageTreeContainsADirectRepeatedOrCyclicNode", "Page tree contains a direct, repeated or cyclic node.")
}

// PageTreeCountDisagreesWithDLeaves renders the core-owned explanation.
func PageTreeCountDisagreesWithDLeaves(leafCount int64) Explanation {
	return explain("PageTreeCountDisagreesWithDLeaves", fmt.Sprintf("Page-tree Count disagrees with %d leaves.", leafCount), strconv.FormatInt(leafCount, 10))
}

// PageTreeExceedsTheSupportedDepthOf renders the core-owned explanation.
func PageTreeExceedsTheSupportedDepthOf() Explanation {
	return explain("PageTreeExceedsTheSupportedDepthOf", "Page tree exceeds the supported depth of 64.")
}

// PageTreeNodeSHasNoSupportedType renders the core-owned explanation.
func PageTreeNodeSHasNoSupportedType(objectReference string) Explanation {
	return explain("PageTreeNodeSHasNoSupportedType", fmt.Sprintf("Page-tree node %s has no supported Type.", objectReference), objectReference)
}

// PageTreeObjectSIsMissingOrEmpty renders the core-owned explanation.
func PageTreeObjectSIsMissingOrEmpty(objectReference string) Explanation {
	return explain("PageTreeObjectSIsMissingOrEmpty", fmt.Sprintf("Page-tree object %s is missing or empty.", objectReference), objectReference)
}

// PageUserUnitMustBeAFinitePositiveNumber renders the core-owned explanation.
func PageUserUnitMustBeAFinitePositiveNumber() Explanation {
	return explain("PageUserUnitMustBeAFinitePositiveNumber", "Page UserUnit must be a finite positive number.")
}

// PagesRequiresNonemptyKids renders the core-owned explanation.
func PagesRequiresNonemptyKids() Explanation {
	return explain("PagesRequiresNonemptyKids", "Pages requires nonempty Kids.")
}

// PartialSelectionOfSourcesContainingFormFieldsOrWidgetsIs renders the core-owned explanation.
func PartialSelectionOfSourcesContainingFormFieldsOrWidgetsIs() Explanation {
	return explain("PartialSelectionOfSourcesContainingFormFieldsOrWidgetsIs", "Partial selection of sources containing form fields or widgets is unsupported; select the whole source or flatten it with a suitable PDF tool.")
}

// PdfcpuNamespacedFieldsInThisOccurrenceTheReportContainsThe renders the core-owned explanation.
func PdfcpuNamespacedFieldsInThisOccurrenceTheReportContainsThe() Explanation {
	return explain("PdfcpuNamespacedFieldsInThisOccurrenceTheReportContainsThe", "pdfcpu namespaced fields in this occurrence; the report contains the actual mapping.")
}

// PredictedCollidingDestinationNamesWillBeRenamedForAffectedOccurrences renders the core-owned explanation.
func PredictedCollidingDestinationNamesWillBeRenamedForAffectedOccurrences() Explanation {
	return explain("PredictedCollidingDestinationNamesWillBeRenamedForAffectedOccurrences", "Predicted: colliding destination names will be renamed for affected occurrences.")
}

// PredictedPdfcpuMayNamespaceThisLaterFormIncludingDistinctNames renders the core-owned explanation.
func PredictedPdfcpuMayNamespaceThisLaterFormIncludingDistinctNames() Explanation {
	return explain("PredictedPdfcpuMayNamespaceThisLaterFormIncludingDistinctNames", "Predicted: pdfcpu may namespace this later form, including distinct names; build records actual mappings.")
}

// ReportAndOutputMustBeDifferentFiles renders the core-owned explanation.
func ReportAndOutputMustBeDifferentFiles() Explanation {
	return explain("ReportAndOutputMustBeDifferentFiles", "Report and output must be different files.")
}

// ResolvedContentsItemIsMissing renders the core-owned explanation.
func ResolvedContentsItemIsMissing() Explanation {
	return explain("ResolvedContentsItemIsMissing", "Resolved contents item is missing.")
}

// RetainedSourceContentHasIncompleteStructuralCoveragePreservationDoesNot renders the core-owned explanation.
func RetainedSourceContentHasIncompleteStructuralCoveragePreservationDoesNot() Explanation {
	return explain("RetainedSourceContentHasIncompleteStructuralCoveragePreservationDoesNot", "Retained source content has incomplete structural coverage; preservation does not invent missing semantics.")
}

// SIsOmitted renders the core-owned explanation.
func SIsOmitted(originalSite string) Explanation {
	return explain("SIsOmitted", fmt.Sprintf("%s is omitted.", originalSite), originalSite)
}

// SWillBeRemovedFromTheOutputCatalog renders the core-owned explanation.
func SWillBeRemovedFromTheOutputCatalog(catalogKey string) Explanation {
	return explain("SWillBeRemovedFromTheOutputCatalog", fmt.Sprintf("%s will be removed from the output catalog.", catalogKey), catalogKey)
}

// SelectedRangeDDIsOutsideTheSourceSD renders the core-owned explanation.
func SelectedRangeDDIsOutsideTheSourceSD(firstPage int64, lastPage int64, sourcePages int64) Explanation {
	return explain("SelectedRangeDDIsOutsideTheSourceSD", fmt.Sprintf("Selected range [%d,%d] is outside the source's %d pages.", firstPage, lastPage, sourcePages), strconv.FormatInt(firstPage, 10), strconv.FormatInt(lastPage, 10), strconv.FormatInt(sourcePages, 10))
}

// SelectedRangesMustBeAscendingAndDisjoint renders the core-owned explanation.
func SelectedRangesMustBeAscendingAndDisjoint() Explanation {
	return explain("SelectedRangesMustBeAscendingAndDisjoint", "Selected ranges must be ascending and disjoint.")
}

// SignatureValuesAreNotSupportedUnsignedSignatureFieldsAreAllowed renders the core-owned explanation.
func SignatureValuesAreNotSupportedUnsignedSignatureFieldsAreAllowed() Explanation {
	return explain("SignatureValuesAreNotSupportedUnsignedSignatureFieldsAreAllowed", "Signature values are not supported; unsigned signature fields are allowed.")
}

// SourceBytesChangedAfterInspectionS renders the core-owned explanation.
func SourceBytesChangedAfterInspectionS(sourcePath string) Explanation {
	return explain("SourceBytesChangedAfterInspectionS", fmt.Sprintf("Source bytes changed after inspection: %s", sourcePath), sourcePath)
}

// SourceInspectionReturnedInconsistentOccurrenceOwnership renders the core-owned explanation.
func SourceInspectionReturnedInconsistentOccurrenceOwnership() Explanation {
	return explain("SourceInspectionReturnedInconsistentOccurrenceOwnership", "Source inspection returned inconsistent occurrence ownership.")
}

// SourceOccurrenceFactsAreUnavailable renders the core-owned explanation.
func SourceOccurrenceFactsAreUnavailable() Explanation {
	return explain("SourceOccurrenceFactsAreUnavailable", "Source occurrence facts are unavailable.")
}

// SourceStructureClaimsAndOwnershipAreDiscardedByExplicitPolicy renders the core-owned explanation.
func SourceStructureClaimsAndOwnershipAreDiscardedByExplicitPolicy() Explanation {
	return explain("SourceStructureClaimsAndOwnershipAreDiscardedByExplicitPolicy", "Source structure claims and ownership are discarded by explicit policy.")
}

// SourceURIBaseIsRemovedRetainedRelativeURIActionsMay renders the core-owned explanation.
func SourceURIBaseIsRemovedRetainedRelativeURIActionsMay() Explanation {
	return explain("SourceURIBaseIsRemovedRetainedRelativeURIActionsMay", "Source URI Base is removed; retained relative URI actions may resolve differently.")
}

// SourceViewerSettingsAreOmittedFromComposedOutput renders the core-owned explanation.
func SourceViewerSettingsAreOmittedFromComposedOutput() Explanation {
	return explain("SourceViewerSettingsAreOmittedFromComposedOutput", "Source viewer settings are omitted from composed output.")
}

// SourcesForWhichThePDFValidatorReportsRepairsAreUnsupported renders the core-owned explanation.
func SourcesForWhichThePDFValidatorReportsRepairsAreUnsupported() Explanation {
	return explain("SourcesForWhichThePDFValidatorReportsRepairsAreUnsupported", "Sources for which the PDF validator reports repairs are unsupported. Obtain a reviewed repair or deliver the original separately.")
}

// TheComposedPDFCouldNotBeVerified renders the core-owned explanation.
func TheComposedPDFCouldNotBeVerified() Explanation {
	return explain("TheComposedPDFCouldNotBeVerified", "The composed PDF could not be verified.")
}

// TheComposedPDFFailedFinalValidation renders the core-owned explanation.
func TheComposedPDFFailedFinalValidation() Explanation {
	return explain("TheComposedPDFFailedFinalValidation", "The composed PDF failed final validation.")
}

// TheComposedPDFFailedOutputVerification renders the core-owned explanation.
func TheComposedPDFFailedOutputVerification() Explanation {
	return explain("TheComposedPDFFailedOutputVerification", "The composed PDF failed output verification.")
}

// TheComposedPDFRequiredAnUnsupportedRepair renders the core-owned explanation.
func TheComposedPDFRequiredAnUnsupportedRepair() Explanation {
	return explain("TheComposedPDFRequiredAnUnsupportedRepair", "The composed PDF required an unsupported repair.")
}

// TheDeclaredSourceStructureCannotBePreservedUseStructureDiscard renders the core-owned explanation.
func TheDeclaredSourceStructureCannotBePreservedUseStructureDiscard() Explanation {
	return explain("TheDeclaredSourceStructureCannotBePreservedUseStructureDiscard", "The declared source structure cannot be preserved. Use structure: \"discard\" for untagged output, obtain a reviewed repair, or deliver the original separately.")
}

// TheOperationWasInterrupted renders the core-owned explanation.
func TheOperationWasInterrupted() Explanation {
	return explain("TheOperationWasInterrupted", "The operation was interrupted.")
}

// TheOriginalNeedAppearancesRequestIsRetainedAsAViewerRequest renders the core-owned explanation.
func TheOriginalNeedAppearancesRequestIsRetainedAsAViewerRequest() Explanation {
	return explain("TheOriginalNeedAppearancesRequestIsRetainedAsAViewerRequest", "The original NeedAppearances request is retained as a viewer request, not regenerated artwork.")
}

// TheOriginalSourceEntryIsOmittedByOutputPolicy renders the core-owned explanation.
func TheOriginalSourceEntryIsOmittedByOutputPolicy() Explanation {
	return explain("TheOriginalSourceEntryIsOmittedByOutputPolicy", "The original source entry is omitted by output policy.")
}

// ThePDFBackendCouldNotBeInitialized renders the core-owned explanation.
func ThePDFBackendCouldNotBeInitialized() Explanation {
	return explain("ThePDFBackendCouldNotBeInitialized", "The PDF backend could not be initialized.")
}

// ThePDFBackendFailedDuringAssembly renders the core-owned explanation.
func ThePDFBackendFailedDuringAssembly() Explanation {
	return explain("ThePDFBackendFailedDuringAssembly", "The PDF backend failed during assembly.")
}

// ThePDFBackendFailedUnexpectedly renders the core-owned explanation.
func ThePDFBackendFailedUnexpectedly() Explanation {
	return explain("ThePDFBackendFailedUnexpectedly", "The PDF backend failed unexpectedly.")
}

// ThePDFCouldNotBeParsedOrValidatedObtainA renders the core-owned explanation.
func ThePDFCouldNotBeParsedOrValidatedObtainA() Explanation {
	return explain("ThePDFCouldNotBeParsedOrValidatedObtainA", "The PDF could not be parsed or validated. Obtain a reviewed repair or deliver the original separately.")
}

// ThePDFValidatorReportedACompatibilityNotice renders the core-owned explanation.
func ThePDFValidatorReportedACompatibilityNotice() Explanation {
	return explain("ThePDFValidatorReportedACompatibilityNotice", "The PDF validator reported a compatibility notice.")
}

// TheSameDestinationByteNameHasConflictingNameTreeAnd renders the core-owned explanation.
func TheSameDestinationByteNameHasConflictingNameTreeAnd() Explanation {
	return explain("TheSameDestinationByteNameHasConflictingNameTreeAnd", "The same destination byte name has conflicting name-tree and legacy targets; this combination is unsupported.")
}

// TheSelectedStructureCannotPreserveTableHeaderAssociationsIncludeThe renders the core-owned explanation.
func TheSelectedStructureCannotPreserveTableHeaderAssociationsIncludeThe() Explanation {
	return explain("TheSelectedStructureCannotPreserveTableHeaderAssociationsIncludeThe", "The selected structure cannot preserve table header associations. Include the referenced header pages, select the whole source, or explicitly discard structure.")
}

// TheSerializedPDFDisagreesWithTheResolvedContract renders the core-owned explanation.
func TheSerializedPDFDisagreesWithTheResolvedContract() Explanation {
	return explain("TheSerializedPDFDisagreesWithTheResolvedContract", "The serialized PDF disagrees with the resolved contract.")
}

// TheSourceDeclaresASignatureOrTimestampValueWhoseAssurance renders the core-owned explanation.
func TheSourceDeclaresASignatureOrTimestampValueWhoseAssurance() Explanation {
	return explain("TheSourceDeclaresASignatureOrTimestampValueWhoseAssurance", "The source declares a signature or timestamp value whose assurance composition cannot preserve. Deliver the original separately; signatures are not authenticated.")
}

// TheSourceDeclaresASignatureOrTimestampWhoseAssuranceComposition renders the core-owned explanation.
func TheSourceDeclaresASignatureOrTimestampWhoseAssuranceComposition() Explanation {
	return explain("TheSourceDeclaresASignatureOrTimestampWhoseAssuranceComposition", "The source declares a signature or timestamp whose assurance composition cannot preserve. Deliver the original separately.")
}

// TheSourceIsNotAPDFNoRecognizablePDFHeader renders the core-owned explanation.
func TheSourceIsNotAPDFNoRecognizablePDFHeader() Explanation {
	return explain("TheSourceIsNotAPDFNoRecognizablePDFHeader", "The source is not a PDF: no recognizable PDF header was found.")
}

// TheSourceRequestsDynamicRenderingWhoseGeneratedAppearanceCompositionCannot renders the core-owned explanation.
func TheSourceRequestsDynamicRenderingWhoseGeneratedAppearanceCompositionCannot() Explanation {
	return explain("TheSourceRequestsDynamicRenderingWhoseGeneratedAppearanceCompositionCannot", "The source requests dynamic rendering, whose generated appearance composition cannot preserve. Deliver the original separately.")
}

// TheSourceRequestsViewerRegeneratedFieldAppearances renders the core-owned explanation.
func TheSourceRequestsViewerRegeneratedFieldAppearances() Explanation {
	return explain("TheSourceRequestsViewerRegeneratedFieldAppearances", "The source requests viewer-regenerated field appearances.")
}

// TheSourceStructureCannotBePreserved renders the core-owned explanation.
func TheSourceStructureCannotBePreserved() Explanation {
	return explain("TheSourceStructureCannotBePreserved", "The source structure cannot be preserved.")
}

// TheSourceStructureIsMalformedOrUnsupported renders the core-owned explanation.
func TheSourceStructureIsMalformedOrUnsupported() Explanation {
	return explain("TheSourceStructureIsMalformedOrUnsupported", "The source structure is malformed or unsupported.")
}

// UnboundMarkedContentIDsSupplyNoHierarchyMeaningfulSourceContent renders the core-owned explanation.
func UnboundMarkedContentIDsSupplyNoHierarchyMeaningfulSourceContent() Explanation {
	return explain("UnboundMarkedContentIDsSupplyNoHierarchyMeaningfulSourceContent", "Unbound marked-content IDs supply no hierarchy; meaningful source content remains untagged.")
}

// UnboundSourceMarkedContentIDsAreIgnoredByExplicitStructure renders the core-owned explanation.
func UnboundSourceMarkedContentIDsAreIgnoredByExplicitStructure() Explanation {
	return explain("UnboundSourceMarkedContentIDsAreIgnoredByExplicitStructure", "Unbound source marked-content IDs are ignored by explicit structure discard; streams are unchanged.")
}

// UnsupportedDestinationCoordinateValue renders the core-owned explanation.
func UnsupportedDestinationCoordinateValue() Explanation {
	return explain("UnsupportedDestinationCoordinateValue", "Unsupported destination coordinate value.")
}

// UnsupportedLiteralHashDictionaryKeyQUpstreamWouldChangeIts renders the core-owned explanation.
func UnsupportedLiteralHashDictionaryKeyQUpstreamWouldChangeIts(dictionaryKey string) Explanation {
	return explain("UnsupportedLiteralHashDictionaryKeyQUpstreamWouldChangeIts", fmt.Sprintf("Unsupported literal-hash dictionary key %q; upstream would change its identity.", dictionaryKey), dictionaryKey)
}

// UnsupportedLiteralHashNameIdentityUpstreamMayDecodeItTwice renders the core-owned explanation.
func UnsupportedLiteralHashNameIdentityUpstreamMayDecodeItTwice() Explanation {
	return explain("UnsupportedLiteralHashNameIdentityUpstreamMayDecodeItTwice", "Unsupported literal-hash name identity; upstream may decode it twice.")
}

// WidgetRectMustContainFourCoordinates renders the core-owned explanation.
func WidgetRectMustContainFourCoordinates() Explanation {
	return explain("WidgetRectMustContainFourCoordinates", "Widget Rect must contain four coordinates.")
}

// XFAWillBeRemovedStaticFieldsRemain renders the core-owned explanation.
func XFAWillBeRemovedStaticFieldsRemain() Explanation {
	return explain("XFAWillBeRemovedStaticFieldsRemain", "XFA will be removed; static fields remain.")
}

// OutputFormContentCouldNotBeDecodedCompletely explains output verification refusal.
func OutputFormContentCouldNotBeDecodedCompletely() Explanation {
	return explain("output_form_decode", "Output Form content could not be decoded completely.")
}

// CatalogPermissionStructuresRefused explains assurance lost by composition.
func CatalogPermissionStructuresRefused() Explanation {
	return explain("catalog_permissions", "The source declares catalog permission structures, which composition does not preserve. Deliver the original separately; certification is not authenticated.")
}

// DocumentTimestampsRefused explains unsupported timestamp assurance.
func DocumentTimestampsRefused() Explanation {
	return explain("document_timestamp", "Document timestamps are not supported.")
}

// OptionalContentRefused explains unsupported visibility-layer assurance.
func OptionalContentRefused() Explanation {
	return explain("optional_content", "The source declares optional-content settings (visibility layers), whose display state composition does not preserve. Deliver the original separately.")
}

// NavigationEvidence records a complete explanation and all original binding facts.
func NavigationEvidence(name string, targetObject, targetGeneration int64, targetPath string, targetPage int64, action, outline bool) Explanation {
	target := fmt.Sprintf("source object %d %d %s", targetObject, targetGeneration, targetPath)
	var text string
	switch {
	case outline && targetPage > 0:
		text = fmt.Sprintf("The targetless outline leaf was omitted. Target original source page %d is not retained.", targetPage)
	case outline:
		text = "The targetless outline leaf was omitted."
	case name != "" && targetPage > 0:
		text = fmt.Sprintf("Destination definition %q was omitted. Target original source page %d is not retained.", name, targetPage)
	case name != "":
		text = fmt.Sprintf("Destination definition %q was omitted.", name)
	case action && targetPage > 0:
		text = fmt.Sprintf("The action edge was removed with its dependent Next subtree; invalid local target at %s. Target original source page %d is not retained.", target, targetPage)
	case action:
		text = fmt.Sprintf("The action edge and dependent Next subtree were removed; the invalid local target is %s.", target)
	case targetPage > 0:
		text = fmt.Sprintf("The source navigation binding was removed. Target original source page %d is not retained.", targetPage)
	default:
		text = "The source navigation binding was removed."
	}
	return explain("navigation_binding", text, name, strconv.FormatInt(targetObject, 10), strconv.FormatInt(targetGeneration, 10), targetPath, strconv.FormatInt(targetPage, 10), strconv.FormatBool(action), strconv.FormatBool(outline))
}

// PredictedFieldScopeEvidence records the prediction's original field scope.
func PredictedFieldScopeEvidence() Explanation {
	return explain("predicted_field_scope", "The original Fields collection is a possible rename scope; no actual new name is predicted.")
}

// DestinationCollisionEvidence records the original name, not any guessed replacement.
func DestinationCollisionEvidence(originalName string) Explanation {
	return explain("predicted_destination_collision", fmt.Sprintf("Original destination %q collides; no actual replacement name is predicted.", originalName), originalName)
}

// StructureDiscardEvidence records the number of discarded ownership entries.
func StructureDiscardEvidence(entries int64) Explanation {
	return explain("structure_discard_entries", fmt.Sprintf("Detached %d structure claims/ownership entries; page and Form streams are unchanged.", entries), strconv.FormatInt(entries, 10))
}

// OrphanStructureEvidence records the number of orphan claims.
func OrphanStructureEvidence(entries int64) Explanation {
	return explain("orphan_structure_entries", fmt.Sprintf("Detached %d claims/ownership entries without changing page or Form streams.", entries), strconv.FormatInt(entries, 10))
}

// StaleStructureIndexEvidence records the original key and target.
func StaleStructureIndexEvidence(key int64, target string) Explanation {
	return explain("stale_structure_index", fmt.Sprintf("Key %d, target %s: no source object claims this key; forward hierarchy and all live reverse bindings were validated.", key, target), strconv.FormatInt(key, 10), target)
}

// DerivedStructureIDsEvidence records the established ID count.
func DerivedStructureIDsEvidence(ids int64) Explanation {
	return explain("derived_structure_ids", fmt.Sprintf("Indexed %d existing structure IDs; no ID or content was invented.", ids), strconv.FormatInt(ids, 10))
}

// URIBaseEvidence records the original removed base site as typed facts.
func URIBaseEvidence(object, generation int64, path string) Explanation {
	return explain("uri_base_removed", fmt.Sprintf("Relative action retained; URI Base at source object %d %d %s is omitted.", object, generation, path), strconv.FormatInt(object, 10), strconv.FormatInt(generation, 10), path)
}
