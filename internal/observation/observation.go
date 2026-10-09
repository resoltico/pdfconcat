// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package observation defines cheap typed work milestones. Producers report
// actual work; consumers own scheduling and presentation.
package observation

type (
	// Phase is a once-entered, monotonic boundary in a build or check.
	Phase uint8
	// Unit identifies exactly what a cumulative count measures.
	Unit uint8
	// Milestone is a phase boundary or an actual cumulative work observation.
	// UnitNone has no counter. Total is meaningful only when TotalKnown is true.
	// Advance marks a real work milestone without a measurable counter.
	Milestone struct {
		Completed  int64
		Total      int64
		Phase      Phase
		Unit       Unit
		TotalKnown bool
		Advance    bool
	}
	// Observer accepts a milestone without waiting for presentation or I/O.
	Observer interface{ Observe(milestone Milestone) }
)

const (
	// Preparation resolves instructions, destinations and required resources.
	Preparation Phase = 0
	// InputInspection captures and inspects sources, which can overlap.
	InputInspection Phase = 1
	// Layout measures or renders distinct generated specifications.
	Layout Phase = 2
	// Assembly imports source occurrences and compiles output pages.
	Assembly Phase = 3
	// Optimization enters the backend optimizer.
	Optimization Phase = 4
	// OutputWriting writes the staged PDF once.
	OutputWriting Phase = 5
	// OutputVerification reads and verifies the staged output.
	OutputVerification Phase = 6
	// Publication stages and publishes requested deliverables.
	Publication Phase = 7
	// Finalization includes workspace cleanup and report salvage.
	Finalization Phase = 8
	// PhaseCount bounds consumer state; it is not a valid phase.
	PhaseCount Phase = 9

	// UnitNone denotes an indeterminate milestone without a counter.
	UnitNone Unit = 0
	// CapturedSources counts successfully captured distinct PDF sources.
	CapturedSources Unit = 1
	// ProcessedSources counts returned source attempts, including failures.
	ProcessedSources Unit = 2
	// ProcessedGeneratedSpecs counts returned layout/render spec attempts.
	ProcessedGeneratedSpecs Unit = 3
	// ImportedSources counts actual successful source-document imports,
	// including per-occurrence reimports but excluding generated resources.
	ImportedSources Unit = 4
	// CompiledPages counts actual output-page placements.
	CompiledPages Unit = 5
	// UnitCount bounds consumer state; it is not a valid unit.
	UnitCount        Unit = 6
	unknownPhaseName      = "unknown"
)

// String returns the stable phase name.
func (phase Phase) String() string {
	names := [PhaseCount]string{
		Preparation: "preparation", InputInspection: "input_inspection", Layout: "layout", Assembly: "assembly",
		Optimization: "optimization", OutputWriting: "output_writing", OutputVerification: "output_verification",
		Publication: "publication", Finalization: "finalization",
	}
	if int(phase) >= len(names) {
		return unknownPhaseName
	}

	return names[int(phase)]
}

// String returns the stable scoped counter name.
func (unit Unit) String() string {
	names := [UnitCount]string{
		UnitNone: "", CapturedSources: "captured_sources", ProcessedSources: "processed_sources",
		ProcessedGeneratedSpecs: "processed_generated_specs", ImportedSources: "imported_sources", CompiledPages: "compiled_pages",
	}
	if int(unit) >= len(names) {
		return ""
	}

	return names[int(unit)]
}

// Emit sends a cheap milestone when the consumer has requested observations.
func Emit(observer Observer, milestone Milestone) {
	if observer != nil {
		observer.Observe(milestone)
	}
}
