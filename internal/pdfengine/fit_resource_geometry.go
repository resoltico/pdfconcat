// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func (i *fitProgramInspector) resourceMatrix(ctx context.Context, dict types.Dict, key string) (Affine, error) {
	object, found := dict[key]
	if !found {
		return fitIdentityMatrix(), nil
	}

	array, err := i.pdf.DereferenceArrayContext(ctx, object)
	if err != nil {
		return Affine{}, fitResourceError(err)
	}

	if len(array) != fitAffineCoefficients {
		return Affine{}, fmt.Errorf("%w: invoked /%s requires six numbers", errFitUnsupported, key)
	}

	var matrix Affine
	for index, value := range array {
		matrix[index], err = i.pdf.DereferenceNumberContext(ctx, value)
		if err != nil {
			return Affine{}, fitResourceError(err)
		}

		if !finiteAppearanceNumber(matrix[index]) {
			return Affine{}, fmt.Errorf("%w: invoked /%s[%d] must be finite", errFitUnsupported, key, index)
		}
	}

	return matrix, nil
}

func (i *fitProgramInspector) resourceBox(ctx context.Context, dict types.Dict, key string, state *fitGraphicsState) error {
	box, err := i.resourceBoxValues(ctx, dict, key)
	if err != nil {
		return fitResourceError(err)
	}

	if box[0] >= box[2] || box[1] >= box[3] {
		return fmt.Errorf("%w: invoked resource BBox must be nonempty", errFitUnsupported)
	}

	return i.programBox(box, box, state)
}

func (i *fitProgramInspector) resourceBoxValues(ctx context.Context, dict types.Dict, key string) ([4]float64, error) {
	array, err := i.pdf.DereferenceArrayContext(ctx, dict[key])
	if err != nil {
		return [4]float64{}, fitResourceError(err)
	}

	if len(array) != boxEntries {
		return [4]float64{}, fmt.Errorf("%w: invoked /%s requires four numbers", errFitUnsupported, key)
	}

	var box [4]float64
	for index, value := range array {
		box[index], err = i.pdf.DereferenceNumberContext(ctx, value)
		if err != nil {
			return box, fitResourceError(err)
		}

		if !finiteAppearanceNumber(box[index]) {
			return box, fmt.Errorf("%w: invoked /%s[%d] must be finite", errFitUnsupported, key, index)
		}
	}

	return box, nil
}

func (i *fitProgramInspector) programBox(original, emitted [4]float64, state *fitGraphicsState) error {
	if original[0] > original[2] || original[1] > original[3] {
		return fmt.Errorf("%w: invoked declared box is invalid", errFitUnsupported)
	}

	return fitChildGeometry(i.fit, state.sourceMatrix, state.matrix, original, emitted)
}
