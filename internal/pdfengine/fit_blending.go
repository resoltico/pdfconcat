// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const (
	fitMaskAlpha          = "Alpha"
	fitPattern            = "Pattern"
	fitMatrix             = "Matrix"
	fitCalGray            = "CalGray"
	fitCalRGB             = "CalRGB"
	fitDefaultRGB         = "DefaultRGB"
	fitTransparency       = "Transparency"
	fitGrayComponents     = 1
	fitRGBComponents      = 3
	fitCMYKComponents     = 4
	fitICCHeaderBytes     = 128
	fitICCInputOffset     = 16
	fitICCSignatureOffset = 36
	fitICCFieldBytes      = 4
	fitDeviceGray         = "DeviceGray"
	fitDeviceRGB          = "DeviceRGB"
	fitDeviceCMYK         = "DeviceCMYK"
	fitColorSpace         = "ColorSpace"
)

func (i *fitProgramInspector) blendingSpace(ctx context.Context, scope types.Dict, object types.Object, depth int) (int, error) {
	if err := i.step(ctx); err != nil {
		return 0, fitResourceError(err)
	}

	if depth >= fitProgramDepthLimit {
		return 0, fmt.Errorf("%w: used blending color space depth limit exceeded", errFitUnsupported)
	}

	value, err := i.pdf.DereferenceContext(ctx, object)
	if err != nil {
		return 0, fitResourceError(err)
	}

	if name, ok := value.(types.Name); ok {
		return i.namedBlendingSpace(ctx, scope, string(name), depth)
	}

	array, ok := value.(types.Array)
	if !ok || len(array) != 2 {
		return 0, fmt.Errorf("%w: blending space must be a device/CIE independent-component space", errFitUnsupported)
	}

	return i.arrayBlendingSpace(ctx, array)
}

func (i *fitProgramInspector) namedBlendingSpace(ctx context.Context, scope types.Dict, name string, depth int) (int, error) {
	components, defaultName := fitDeviceComponents(name)
	if components > 0 {
		return i.defaultBlendingSpace(ctx, scope, components, defaultName, depth)
	}

	binding, err := i.resource(ctx, scope, fitColorSpace, name)
	if err != nil {
		return 0, fitResourceError(err)
	}

	replacement := binding.object

	return i.blendingSpace(ctx, scope, replacement, depth+1)
}

func fitDeviceComponents(name string) (int, string) {
	switch name {
	case fitDeviceGray:
		return fitGrayComponents, "DefaultGray"
	case fitDeviceRGB:
		return fitRGBComponents, fitDefaultRGB
	case fitDeviceCMYK:
		return fitCMYKComponents, "DefaultCMYK"
	default:
		return 0, ""
	}
}

func (i *fitProgramInspector) defaultBlendingSpace(
	ctx context.Context,
	scope types.Dict,
	components int,
	name string,
	depth int,
) (int, error) {
	spaces, err := i.pdf.DereferenceDictContext(ctx, scope[fitColorSpace])
	if err != nil {
		return 0, fitResourceError(err)
	}

	replacement, found := spaces[name]
	if !found {
		return components, nil
	}

	actual, err := i.blendingSpace(ctx, scope, replacement, depth+1)
	if err != nil {
		return 0, fitResourceError(err)
	}

	if actual != components {
		return 0, fmt.Errorf("%w: used /%s changes component count", errFitUnsupported, name)
	}

	return actual, nil
}

func (i *fitProgramInspector) arrayBlendingSpace(ctx context.Context, array types.Array) (int, error) {
	kind, err := i.name(ctx, array[0])
	if err != nil {
		return 0, fitResourceError(err)
	}

	switch kind {
	case fitCalGray, fitCalRGB:
		parameters, readErr := i.pdf.DereferenceDictContext(ctx, array[1])
		if readErr != nil {
			return 0, fitResourceError(readErr)
		}

		if parameters == nil {
			return 0, fmt.Errorf("%w: calibration parameters must be a dictionary", errFitUnsupported)
		}

		if err = i.boundedResource(ctx, parameters, 0, map[string]bool{}); err != nil {
			return 0, fitResourceError(err)
		}

		if kind == fitCalGray {
			return fitGrayComponents, nil
		}

		return fitRGBComponents, nil
	case fitICCBased:
		return i.blendingICC(ctx, array[1])
	default:
		return 0, fmt.Errorf("%w: blending family /%.64s has no independent additive/subtractive components", errFitUnsupported, kind)
	}
}

func (i *fitProgramInspector) blendingICC(ctx context.Context, object types.Object) (int, error) {
	stream, err := fitReadProgramStream(ctx, i.pdf, object)
	if err != nil {
		return 0, fitResourceError(err)
	}

	declared, err := i.integer(ctx, stream.Dict["N"])
	if err != nil {
		return 0, fitResourceError(err)
	}

	content, err := i.programBytes(ctx, fitObjectID(object, stream.Dict), stream)
	if err != nil {
		return 0, fitResourceError(err)
	}

	actual, err := fitICCComponents(content)
	if err != nil {
		return 0, fitResourceError(err)
	}

	if actual != declared {
		return 0, fmt.Errorf("%w: blending ICC /N does not match input space", errFitUnsupported)
	}

	return actual, nil
}

func fitICCComponents(content []byte) (int, error) {
	if len(content) < fitICCHeaderBytes || string(content[fitICCSignatureOffset:fitICCSignatureOffset+fitICCFieldBytes]) != "acsp" {
		return 0, fmt.Errorf("%w: blending ICC profile lacks valid header", errFitUnsupported)
	}

	size := int64(binary.BigEndian.Uint32(content[:fitICCFieldBytes]))
	if size < fitICCHeaderBytes || size > int64(len(content)) {
		return 0, fmt.Errorf("%w: blending ICC declared length is invalid", errFitUnsupported)
	}

	switch string(content[fitICCInputOffset : fitICCInputOffset+fitICCFieldBytes]) {
	case "GRAY":
		return fitGrayComponents, nil
	case "RGB ":
		return fitRGBComponents, nil
	case "CMYK":
		return fitCMYKComponents, nil
	default:
		return 0, fmt.Errorf("%w: blending ICC input space is not independent Gray/RGB/CMYK", errFitUnsupported)
	}
}
