// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan_test

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/plan"
)

type hangProbe struct {
	run  func()
	name string
}

const (
	// guardDeadline is far longer than any probe needs. A probe that outlasts it never ends: the loops it
	// covers have no other exit.
	guardDeadline = 10 * time.Second
	// hugeExponent is a number whose exponent a careless conversion would turn into that many digits.
	hugeExponent = "1e100000000"
	// conversionAllocationBudget is what converting hugeExponent may allocate: the conversion works in constant
	// memory, so an allocation as large as the exponent means the digits were materialized.
	conversionAllocationBudget = 1 << 20
	rootMember                 = "/items"
)

// guardAgainstHangs runs before any test. A defect in the reading loops or in the pointer walk does not fail a
// test, it hangs or exhausts memory, and the other tests of the package would then wait on it for ever. Each probe
// is a small operation that must end, and the one allocation probe runs while nothing else does, so its count is
// exact. A violation stops the test binary at once with a message that names the probe.
func guardAgainstHangs() {
	for _, probe := range hangProbes() {
		done := make(chan struct{})

		go func() {
			defer close(done)

			probe.run()
		}()

		select {
		case <-done:
		case <-time.After(guardDeadline):
			panic(fmt.Sprintf("hang guard: %s did not end within %v", probe.name, guardDeadline))
		}
	}

	var before, after runtime.MemStats

	runtime.ReadMemStats(&before)

	_, err := plan.ParseExactInt(hugeExponent)
	if err == nil {
		panic("hang guard: " + hugeExponent + " was converted to an int64")
	}

	runtime.ReadMemStats(&after)

	allocated := after.TotalAlloc - before.TotalAlloc
	if allocated > conversionAllocationBudget {
		panic(fmt.Sprintf("hang guard: converting %s allocated %d bytes, want at most %d",
			hugeExponent, allocated, conversionAllocationBudget))
	}
}

// finished takes the result of a probe, whose only question is whether it came back.
func finished(*assembly.Job, error) {}

func hangProbes() []hangProbe {
	documents, shapes := shapeDocuments(), readShapes()
	probes := make([]hangProbe, 0, 2+len(documents)*len(shapes))

	probes = append(probes,
		hangProbe{name: "locating the root", run: func() {
			job, err := plan.Decode(context.Background(), inputFor(), strings.NewReader(minimalPlan))
			if err == nil {
				assembly.Locate(job.Source, assembly.Origin{}, rootMember)
			}
		}},
		hangProbe{name: "a read failure while scanning for trailing data", run: func() {
			input := io.MultiReader(strings.NewReader(minimalPlan+"  "), failingReader{errBoom})

			finished(plan.Decode(context.Background(), inputFor(), input))
		}},
	)

	for _, document := range documents {
		for _, shape := range shapes {
			probes = append(probes, hangProbe{
				name: "decoding " + document.name + " delivered as " + shape.name,
				run:  func() { finished(plan.Decode(context.Background(), inputFor(), shape.open(document.text))) },
			})
		}
	}

	return probes
}
