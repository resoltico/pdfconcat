// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
package report

import (
	"fmt"
	"testing"
)

func TestOutcomeRelationshipMatrix(t *testing.T) {
	states := []PhaseState{PhaseNotRun, PhaseIncomplete, PhaseComplete}
	for _, command := range []string{"build", "check", "help"} {
		for _, status := range []Status{StatusOK, StatusInvalid, StatusFailed, StatusInterrupted} {
			for _, published := range []bool{false, true} {
				for _, verification := range states {
					name := fmt.Sprintf("%s/%s/published=%t/verification=%s", command, status, published, verification)
					t.Run(name, func(t *testing.T) {
						r := &Report{Command: command, Status: status, Phases: Phases{OutputVerification: verification}, Publication: Publication{Published: published}}
						accepted := (&validator{report: r}).outcomeRelationships() == nil
						// Explicit allowed publication outcomes; failed means an observed postcommit failure.
						allowed := !published
						if published {
							allowed = command == "build" && verification == PhaseComplete && (status == StatusOK || status == StatusFailed)
						}
						if command == "check" && verification != PhaseNotRun {
							allowed = false
						}
						if accepted != allowed {
							t.Fatalf("accepted=%t, allowed=%t", accepted, allowed)
						}
					})
				}
			}
		}
	}
}
