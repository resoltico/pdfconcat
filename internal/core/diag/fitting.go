// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package diag

import "fmt"

// FittingAffineCannotBeRepresented explains an unrepresentable required affine.
func FittingAffineCannotBeRepresented() Explanation {
	return explain("fit_geometry", "The page requires a fitting transform whose finite geometry or supported forward-placement accuracy cannot be established; use original page sizes.")
}

// FittingAnnotationOwnershipUnsupported explains unsafe shared coordinate ownership.
func FittingAnnotationOwnershipUnsupported() Explanation {
	return explain("fit_annotation_ownership", "An annotation dictionary has multiple retained bindings and at least one page transforms; fitting cannot preserve its coordinate ownership. Use original page sizes or distinct annotation dictionaries.")
}

// FittingWidgetOwnershipUnsupported explains transformed or unproved widget owners.
func FittingWidgetOwnershipUnsupported() Explanation {
	return explain("fit_widget_ownership", "A form widget transforms or has unresolved or conflicting page ownership while other pages transform. Use original page sizes or establish kept widget ownership.")
}

// FittingPrintBoxOmission explains the expected omission of one optional print entry.
func FittingPrintBoxOmission(key string) Explanation {
	return explain("fit_print_box", fmt.Sprintf("Fitting omits this page's %s entry; the expected output uses default printing bounds. Production-print fidelity is unsupported.", key), key)
}

// SourceOccurrenceGeometryIsInconsistent explains a prepared geometry ownership failure.
func SourceOccurrenceGeometryIsInconsistent() Explanation {
	return explain("source_geometry_owner", "Source occurrence geometry does not match its resolved fitting owner.")
}
