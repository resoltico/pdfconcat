// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

// outcomeRelationships is shared by producer serialization and saved-report decoding.
// It validates structural truth; it cannot authenticate a historical publication.
func (v *validator) outcomeRelationships() *fault {
	r := v.report
	if r.Command == commandCheck && r.Phases.OutputVerification != PhaseNotRun {
		return newFault("/phases/output_verification", CodeInvalidValue, "check does not render or verify an output PDF")
	}
	if !r.Publication.Published {
		return nil
	}
	if r.Command != commandBuild {
		return newFault("/publication/published", CodeInvalidValue, "only build can publish a PDF")
	}
	if r.Status != StatusOK && r.Status != StatusFailed {
		return newFault("/status", CodeInvalidValue, "a published build can be ok or failed after commit, never invalid or interrupted")
	}
	if r.Phases.OutputVerification != PhaseComplete {
		return newFault("/phases/output_verification", CodeInvalidValue, "publication requires completed output verification")
	}
	return nil
}
