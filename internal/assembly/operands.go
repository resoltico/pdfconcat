// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly

import (
	"fmt"
	"math"
)

// Operand is one ordered direct operand of the command line: a PDF path, or the --blank directive.
type Operand struct {
	// Path is the PDF path, taken literally; unused for a blank.
	Path string
	// Position is the zero-based position of the argument in argv, excluding the executable name.
	Position int
	// Blank marks the --blank directive: one generated page that inherits its size.
	Blank bool
}

// NewOperandJob compiles ordered direct operands into the same Job a plan produces. Paths resolve against
// workingDir, which must be absolute; each Origin is the operand's argv position. The result is validated.
func NewOperandJob(workingDir string, operands []Operand) (*Job, error) {
	items := make([]Item, 0, len(operands))

	for _, operand := range operands {
		if operand.Position < 0 || int64(operand.Position) > math.MaxUint32 {
			return nil, fmt.Errorf("%w: argument position %d is out of range", ErrOutOfRange, operand.Position)
		}

		origin := Origin{Ref: Ref(operand.Position)}

		if operand.Blank {
			items = append(items, Item{Kind: ItemBlank, Origin: origin, Blank: &BlankItem{Count: 1, CountOrigin: origin}})
		} else {
			items = append(items, NewPDFItem(origin, operand.Path))
		}
	}

	job := &Job{Source: ArgumentSource{}, Base: workingDir, Items: items}

	err := job.Validate()
	if err != nil {
		return nil, err
	}

	return job, nil
}
