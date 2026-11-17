// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package model

import "fmt"

// MissingAnnotationAppearanceError is the typed PDF 2.0 missing-AP category.
// Other dictionary/dereference/shape errors keep their own causes.
type MissingAnnotationAppearanceError struct{ Subtype string }

func (e *MissingAnnotationAppearanceError) Error() string {
	return fmt.Sprintf("PDF 2.0 %s annotation requires an appearance dictionary (AP)", e.Subtype)
}
