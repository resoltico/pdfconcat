// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"context"

	"github.com/resoltico/pdfconcat/internal/core/internal/result"
)

// Execute is the sole application use-case flow. Request rejection performs no I/O.
func Execute(c context.Context, request Request) Result {
	if !request.valid() {
		return invalidRequest(request.operation)
	}
	admission := admit(c, request)
	r := admission.result
	if !hasErrors(r.Diagnostics) {
		prepared := prepare(c, admission.plan)
		r.Diagnostics = append(r.Diagnostics, prepared.diagnostics...)
		composition := compose(c, admission.plan, prepared, r, request.operation)
		r = composition.result
		if !hasErrors(r.Diagnostics) && request.operation == Build {
			built := build(c, admission.plan, prepared, composition, request.options.Overwrite)
			r.Output = &built.output
			r.Diagnostics = append(r.Diagnostics, built.diagnostics...)
			for i, names := range built.renames {
				r.Parts[i].FieldsRenamed = names
			}
		}
	}
	return finalize(c, r, admission)
}
func finalize(c context.Context, r result.Result, admission admissionOutcome) result.Result {
	noteCancellation(c, &r)
	if !admission.reportRefused {
		writeReport(&r, admission.roles)
	}
	noteCancellation(c, &r)
	return r.Full()
}
