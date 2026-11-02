// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
)

// validationNotices preserves advisory evidence and selects the first reported
// repair as a refusal. "Skipped" describes a validation divergence; it does not
// establish that PDF content was removed. Message text never selects policy.
func validationNotices(report model.ValidationReport) ([]diag.Diagnostic, error) {
	var ds []diag.Diagnostic
	var repair error
	for _, n := range report.Notices() {
		d := diag.New(diag.SourceValidationNotice, "The PDF validator reported a compatibility notice.")
		d.Detail = strings.ToValidUTF8(string(n.Phase)+"/"+string(n.Disposition)+": "+n.Message, "�")
		location := diag.PDFLocation{Path: strings.ToValidUTF8(n.Path, "�")}
		if n.ObjectNumber > 0 {
			location.Object = n.ObjectNumber
		}
		if n.PageNumber > 0 {
			location.Page = n.PageNumber
		}
		if location != (diag.PDFLocation{}) {
			d.PDFLocation = &location
		}
		ds = append(ds, d)
		if n.Disposition == model.NoticeRepaired && repair == nil {
			refusal := diag.New(diag.SourceUnsupported, "Sources for which the PDF validator reports repairs are unsupported. Obtain a reviewed repair or deliver the original separately.")
			refusal.Detail, refusal.PDFLocation = d.Detail, d.PDFLocation
			repair = &policyError{finding: refusal, err: n.Cause}
		}
	}
	return ds, repair
}
