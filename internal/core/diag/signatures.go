// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package diag

import (
	"fmt"
	"strconv"
)

// SignatureDiscardReason identifies a failed ownership or fidelity proof.
type SignatureDiscardReason string

// SignatureAppearanceClosingLookaheadBytes bounds the physical closing-token proof.
const SignatureAppearanceClosingLookaheadBytes = 4 << 10

// Signature discard proof failures.
const (
	SignatureOwnership              SignatureDiscardReason = "ownership"
	SignaturePermissions            SignatureDiscardReason = "permissions"
	SignatureVisibility             SignatureDiscardReason = "visibility"
	SignatureDegeneratePrint        SignatureDiscardReason = "degenerate_print"
	SignatureAppearanceNonTrivia    SignatureDiscardReason = "appearance_non_trivia"
	SignatureAppearanceEncoding     SignatureDiscardReason = "appearance_encoding"
	SignatureAppearanceBoundary     SignatureDiscardReason = "appearance_boundary"
	SignatureAppearanceClosingLimit SignatureDiscardReason = "appearance_closing_limit"
	SignatureAppearance             SignatureDiscardReason = "appearance"
	SignatureRegeneration           SignatureDiscardReason = "regeneration"
	SignatureTransparencySetting    SignatureDiscardReason = "transparency_setting"
	SignatureCompositing            SignatureDiscardReason = "compositing"
	SignatureContentBoundary        SignatureDiscardReason = "content_boundary"
)

// SignatureDiscardUnsupported explains why signature normalization was refused.
func SignatureDiscardUnsupported(reason SignatureDiscardReason) Explanation {
	var message string
	switch reason {
	case SignatureOwnership:
		message = "The source has signature state with an unproved or conflicting owner."
	case SignaturePermissions:
		message = "The source has a permission or validation structure that cannot be discarded safely."
	case SignatureVisibility:
		message = "The signature widget's normal screen and print visibility cannot be preserved as page artwork."
	case SignatureDegeneratePrint:
		message = "The printable signature widget has a degenerate rectangle and an appearance whose lack of ink cannot be proved in print mode."
	case SignatureAppearanceNonTrivia:
		message = "The printable signature widget has a degenerate rectangle and its decoded appearance contains more than PDF whitespace and comments."
	case SignatureAppearanceEncoding:
		message = "The printable degenerate signature widget has an appearance with damaged encoding or extra encoded data."
	case SignatureAppearanceBoundary:
		message = "The printable degenerate signature widget has an appearance with an unproved stream length or malformed closing tokens."
	case SignatureAppearanceClosingLimit:
		message = fmt.Sprintf("The signature appearance reaches the %d-byte closing-token lookahead limit.", SignatureAppearanceClosingLookaheadBytes)
	case SignatureAppearance:
		message = "The signature widget has no provably selected static normal appearance."
	case SignatureRegeneration:
		message = "The form requests appearance regeneration or its regeneration setting cannot be proved inactive."
	case SignatureTransparencySetting:
		message = "The signature widget has a malformed blend mode or alpha setting."
	case SignatureCompositing:
		message = "The signature widget could render differently as page artwork because of stacking or compositing."
	case SignatureContentBoundary:
		message = "The original page graphics state cannot be safely isolated from the signature artwork."
	default:
		message = "The source signature state cannot be discarded safely."
	}
	return explain("signature_discard_unsupported", message, string(reason))
}

// SignatureAppearanceObject identifies the stream referenced by the located appearance slot.
func SignatureAppearanceObject(object, generation int64) Explanation {
	return explain("signature_appearance_object", fmt.Sprintf("The appearance slot references PDF object %d with generation %d.", object, generation),
		strconv.FormatInt(object, 10), strconv.FormatInt(generation, 10))
}

// SourceSignatureDiscardGuidance describes how to check eligibility for an unsigned copy.
func SourceSignatureDiscardGuidance(member, policy string) Explanation {
	return explain("source_signature_discard_guidance", fmt.Sprintf(
		"If you deliver the original separately and want an unsigned copy, set %q: %q in a plan and run pdfconcat check --plan FILE to see whether the source qualifies.",
		member, policy), member, policy)
}

// XFAWithoutStaticFields explains why an XFA-only source cannot be composed.
func XFAWithoutStaticFields() Explanation {
	return explain("xfa_without_static_fields", "The source contains XFA form content without a usable static form. Supply a PDF with a static form rendition.")
}

// SignatureDiscardCounts records the removed state of one admitted source.
type SignatureDiscardCounts struct {
	PopulatedSignatureFields int
	Widgets                  int
	SignatureValues          int
	DocumentTimestamps       int
	PermissionStructures     int
	ValidationStores         int
	SignatureFlagEntries     int
}

// SignatureDiscard returns the typed counts carried by a discard finding.
func (d Diagnostic) SignatureDiscard() (SignatureDiscardCounts, bool) {
	if d.signatureDiscard == nil {
		return SignatureDiscardCounts{}, false
	}
	return *d.signatureDiscard, true
}

// SignatureDiscarded reports the effect on the composed copy with its evidence.
func SignatureDiscarded(counts SignatureDiscardCounts) Diagnostic {
	d := Report(SourceSignatureDiscarded, explain("signature_state_discarded",
		"Composition discards the listed source signature state. Any retained signature mark becomes static page artwork. Use the original for authentication."))
	d.Identity.Phase = "source_observation"
	d.signatureDiscard = &counts
	number := strconv.Itoa
	AttachEvidence(&d, explain("signature_discard_evidence",
		fmt.Sprintf("Counts of removed state are populated signature fields (%d), widgets (%d), signature values (%d), document timestamps (%d), permission structures (%d), validation stores (%d) and signature flag entries (%d).",
			counts.PopulatedSignatureFields, counts.Widgets, counts.SignatureValues, counts.DocumentTimestamps, counts.PermissionStructures, counts.ValidationStores, counts.SignatureFlagEntries),
		number(counts.PopulatedSignatureFields), number(counts.Widgets), number(counts.SignatureValues), number(counts.DocumentTimestamps),
		number(counts.PermissionStructures), number(counts.ValidationStores), number(counts.SignatureFlagEntries)))
	return d
}

// UndefinedXFAReference explains a broken reference in the original XFA declaration.
func UndefinedXFAReference(object, generation int64) Explanation {
	return explain("undefined_xfa_reference", fmt.Sprintf("The XFA declaration references undefined PDF object %d %d.", object, generation),
		strconv.FormatInt(object, 10), strconv.FormatInt(generation, 10))
}
