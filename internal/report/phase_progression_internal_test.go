// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
package report

import (
	"testing"
)

func TestPhaseProgressionRequiresCompletedPredecessors(t *testing.T) {
	states := []PhaseState{PhaseNotRun, PhaseIncomplete, PhaseComplete}
	for _, instructions := range states {
		for _, inspection := range states {
			for _, layout := range states {
				for _, verification := range states {
					phases := Phases{Instructions: instructions, InputInspection: inspection, Layout: layout, OutputVerification: verification}
					vector := []PhaseState{instructions, inspection, layout, verification}
					allowed := true
					for index := 1; index < len(vector); index++ {
						if vector[index] != PhaseNotRun && vector[index-1] != PhaseComplete {
							allowed = false
						}
					}
					if accepted := phasesFault(&phases) == nil; accepted != allowed {
						t.Errorf("phases=%+v accepted=%t allowed=%t", phases, accepted, allowed)
					}
				}
			}
		}
	}
}
