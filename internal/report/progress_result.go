// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

// WithProgressInterrupted records unavailable requested telemetry in the final
// response while retaining the work outcome and the summary's byte budget.
func (r Response) WithProgressInterrupted() Response {
	switch content := r.content.(type) {
	case *Summary:
		content.ProgressInterrupted = true
		content.bound()
	case *Report:
		annotated := *content
		annotated.ProgressInterrupted = true
		r.content = &annotated
	default:
	}

	return r
}
