// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package layout

import "github.com/resoltico/pdfconcat/internal/assembly"

// CodeFor exposes the classification of typeset errors.
func CodeFor(err error) assembly.Code { return codeOf(err) }
