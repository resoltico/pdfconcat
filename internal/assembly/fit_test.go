// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly_test

import (
	"errors"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

func TestFitTargetUsesExistingPaperAuthorityAndRejectsOtherSpellings(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"A4", "Legal"} {
		target, err := assembly.ParseFitTarget(name)
		if err != nil {
			t.Fatal(err)
		}

		dim, err := target.Dim()
		if err != nil {
			t.Fatal(err)
		}

		paper, err := assembly.ParsePageSize(name)
		if err != nil || dim != paper.Dim {
			t.Fatalf("fit dimensions diverged from named paper: %+v %+v %v", dim, paper, err)
		}
	}

	for _, name := range []string{"", "A5", "legal", "US Legal", "140x210mm", "inherit"} {
		if _, err := assembly.ParseFitTarget(name); err == nil {
			t.Fatalf("unsupported fit target accepted: %q", name)
		}
	}
}

func TestFitCanvasResolutionKeepsAuthoredSizeAndUsesTargetForImplicitCanvases(t *testing.T) {
	t.Parallel()

	for _, target := range []assembly.FitTarget{assembly.FitA4, assembly.FitLegal} {
		t.Run(string(target), func(t *testing.T) {
			t.Parallel()

			implicit := assembly.BlankStyle{}
			explicit := sizeStyle(50, 60)
			job := argumentJob(mustBlankItem(t, 1, &implicit, 3), pdfAt(2, aPath),
				mustBlankItem(t, 3, &explicit, 2), mustBlankItem(t, 4, &implicit, 4))
			job.FitTo = assembly.Set(target, assembly.Origin{Ref: 9})
			layout := resolveJob(t, job, source(1, geom(100, 200), geom(300, 400)))

			dim, err := target.Dim()
			if err != nil {
				t.Fatal(err)
			}

			verifyFitCanvases(t, layout, dim, []int{0, 3})

			if dimOf(t, layout, 2) != (assembly.PageDim{Width: 50, Height: 60}) || layout.Placements[2].Size != assembly.SizeExplicit {
				t.Fatal("explicit authored canvas was reflowed at target size")
			}

			generated := argumentJob(mustBlankItem(t, 1, &implicit, 10))
			generated.FitTo = job.FitTo

			resolved := resolveJob(t, generated)
			verifyFitCanvases(t, resolved, dim, []int{0})
		})
	}
}

func verifyFitCanvases(t *testing.T, layout *assembly.Layout, dim assembly.PageDim, indexes []int) {
	t.Helper()

	for _, index := range indexes {
		placement := layout.Placements[index]
		if dimOf(t, layout, index) != dim || placement.Size != assembly.SizeFitTarget || placement.SizeFrom != -1 {
			t.Fatalf("fit target falsely inherited source provenance: %+v", placement)
		}
	}
}

func TestJobValidationRejectsUnsupportedExplicitFitTargets(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"A5", "legal", ""} {
		job := argumentJob(mustBlankItem(t, 1, &assembly.BlankStyle{}, 1))
		job.FitTo = assembly.Set(assembly.FitTarget(name), assembly.Origin{Ref: 9})

		if err := job.Validate(); !errors.Is(err, assembly.ErrInvalidPageSize) {
			t.Fatalf("job accepted unsupported explicit fit target %q: %v", name, err)
		}
	}
}
