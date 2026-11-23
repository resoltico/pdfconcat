// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package diag

import (
	"fmt"
	"strconv"
)

// SignatureDiscardReason identifies a failed ownership or fidelity proof.
type SignatureDiscardReason string

// Signature discard proof failures.
const (
	SignatureOwnership       SignatureDiscardReason = "ownership"
	SignaturePermissions     SignatureDiscardReason = "permissions"
	SignatureVisibility      SignatureDiscardReason = "visibility"
	SignatureDegeneratePrint SignatureDiscardReason = "degenerate_print"
	SignatureAppearance      SignatureDiscardReason = "appearance"
	SignatureCompositing     SignatureDiscardReason = "compositing"
	SignatureContentBoundary SignatureDiscardReason = "content_boundary"
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
		message = "The printable signature widget has a degenerate rectangle and an appearance that can still paint in print mode."
	case SignatureAppearance:
		message = "The signature widget has no provably selected static normal appearance."
	case SignatureCompositing:
		message = "The signature widget could render differently as page artwork because of stacking or compositing."
	case SignatureContentBoundary:
		message = "The original page graphics state cannot be safely isolated from the signature artwork."
	default:
		message = "The source signature state cannot be discarded safely."
	}
	return explain("signature_discard_unsupported", message, string(reason))
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

// SignatureStateDiscarded describes the effect on the composed copy.
func SignatureStateDiscarded() Explanation {
	return explain("signature_state_discarded", "Composition discards the listed source signature state. Any retained signature mark becomes static page artwork. Use the original for authentication.")
}

// SignatureDiscardEvidence reports the kinds and counts of source state removed.
func SignatureDiscardEvidence(fields, widgets, signatures, timestamps, permissions, validation, flags int) Explanation {
	return explain("signature_discard_evidence", fmt.Sprintf("Counts of removed state are populated signature fields (%d), widgets (%d), signature values (%d), document timestamps (%d), permission structures (%d), validation stores (%d) and signature flag entries (%d).", fields, widgets, signatures, timestamps, permissions, validation, flags),
		strconv.Itoa(fields), strconv.Itoa(widgets), strconv.Itoa(signatures), strconv.Itoa(timestamps), strconv.Itoa(permissions), strconv.Itoa(validation), strconv.Itoa(flags))
}

// UndefinedXFAReference explains a broken reference in the original XFA declaration.
func UndefinedXFAReference(object, generation int64) Explanation {
	return explain("undefined_xfa_reference", fmt.Sprintf("The XFA declaration references undefined PDF object %d %d.", object, generation),
		strconv.FormatInt(object, 10), strconv.FormatInt(generation, 10))
}
