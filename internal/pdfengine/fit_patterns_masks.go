// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type (
	fitPatternProgram struct {
		object types.Object
		dict   types.Dict
		stream *types.StreamDict
	}
	fitMaskProgram struct {
		object types.Object
		stream *types.StreamDict
	}
)

const (
	fitICCBased = "ICCBased"
	fitDeviceN  = "DeviceN"
	fitShading  = "Shading"
)

func (i *fitProgramInspector) extGState(ctx context.Context, scope types.Dict, name string, state *fitGraphicsState) error {
	binding, err := i.operandResource(ctx, scope, keyExtGState, name)
	if err != nil {
		return fitResourceError(err)
	}

	object := binding.object

	return i.applyExtGState(ctx, object, state, "gs "+binding.label())
}

func (i *fitProgramInspector) applyExtGState(
	ctx context.Context,
	object types.Object,
	state *fitGraphicsState,
	edge string,
) error {
	dict, err := i.pdf.DereferenceDictContext(ctx, object)
	if err != nil {
		return fitResourceError(err)
	}

	if state.colorRestricted {
		if err = fitRestrictedExtGState(dict); err != nil {
			return fitResourceError(err)
		}
	}

	if font, found := dict[keyFont]; found {
		if err = i.extGStateFont(ctx, font, state); err != nil {
			return fitResourceError(err)
		}
	}

	if mask, found := dict["SMask"]; found {
		return i.softMask(ctx, mask, state, edge)
	}

	return nil
}

func (i *fitProgramInspector) softMask(ctx context.Context, mask types.Object, state *fitGraphicsState, edge string) error {
	value, err := i.pdf.DereferenceContext(ctx, mask)
	if err != nil {
		return fitResourceError(err)
	}

	if name, ok := value.(types.Name); ok {
		if name != "None" {
			return fmt.Errorf("%w: applied /SMask name must be /None", errFitUnsupported)
		}

		state.mask = ""

		return nil
	}

	maskDict, err := i.pdf.DereferenceDictContext(ctx, mask)
	if err != nil {
		return fitResourceError(err)
	}

	kind, err := i.name(ctx, maskDict["S"])
	if err != nil {
		return fitResourceError(err)
	}

	if kind != fitMaskAlpha && kind != "Luminosity" {
		return fmt.Errorf("%w: applied /SMask /S must be /Alpha or /Luminosity", errFitUnsupported)
	}

	group, err := i.softMaskGroup(ctx, maskDict, kind)
	if err != nil {
		return fitResourceError(err)
	}

	if err = i.boundedResource(ctx, maskDict["BC"], 0, map[string]bool{}); err != nil {
		return fitResourceError(err)
	}

	if transfer, found := maskDict["TR"]; found {
		if err = i.boundedResource(ctx, transfer, 0, map[string]bool{}); err != nil {
			return fitResourceError(err)
		}
	}

	state.mask = fitObjectID(mask, maskDict)
	initial := *state
	initial.mask = ""

	return i.form(ctx, group.object, group.stream, &initial, edge+" /SMask /G")
}

func (i *fitProgramInspector) pattern(ctx context.Context, selection fitPatternSelection, state *fitGraphicsState) error {
	pattern, err := i.patternProgram(ctx, selection)
	if err != nil {
		return fitResourceError(err)
	}

	state, err = i.patternPlacement(ctx, selection, state, pattern.dict)
	if err != nil {
		return fitResourceError(err)
	}

	kind, err := i.integer(ctx, pattern.dict["PatternType"])
	if err != nil {
		return fitResourceError(err)
	}

	if kind == 2 {
		return i.shadingPattern(ctx, selection, pattern.dict, state)
	}

	if kind != 1 || pattern.stream == nil {
		return fmt.Errorf("%w: painted tiling pattern requires PatternType 1 stream", errFitUnsupported)
	}

	return i.tilingPattern(ctx, selection, pattern, state)
}

func (i *fitProgramInspector) patternProgram(ctx context.Context, selection fitPatternSelection) (fitPatternProgram, error) {
	binding, err := i.resource(ctx, selection.scope, fitPattern, selection.name)
	if err != nil {
		return fitPatternProgram{}, fitResourceError(err)
	}

	resolved, err := i.pdf.DereferenceContext(ctx, binding.object)
	if err != nil {
		return fitPatternProgram{}, fitResourceError(err)
	}

	switch value := resolved.(type) {
	case types.Dict:
		return fitPatternProgram{object: binding.object, dict: value}, nil
	case types.StreamDict:
		return fitPatternProgram{object: binding.object, dict: value.Dict, stream: &value}, nil
	default:
		return fitPatternProgram{}, fmt.Errorf("%w: painted pattern is not dictionary/stream", errFitUnsupported)
	}
}

func (i *fitProgramInspector) patternPlacement(ctx context.Context,
	selection fitPatternSelection,
	state *fitGraphicsState,
	dict types.Dict,
) (*fitGraphicsState, error) {
	ownedState := *state
	state = &ownedState

	matrix, err := i.resourceMatrix(ctx, dict, fitMatrix)
	if err != nil {
		return state, fitResourceError(err)
	}

	state.sourceMatrix, err = fitCompose(selection.originalAnchor, matrix)
	if err != nil {
		return state, fitResourceError(err)
	}

	state.matrix, err = fitCompose(selection.emittedAnchor, matrix)

	return state, fitResourceError(err)
}

func (i *fitProgramInspector) shadingPattern(
	ctx context.Context,
	selection fitPatternSelection,
	dict types.Dict,
	state *fitGraphicsState,
) error {
	if ext, found := dict[keyExtGState]; found {
		if err := i.applyExtGState(ctx, ext, state, "painted shading pattern"); err != nil {
			return fitResourceError(err)
		}
	}

	return i.shadingObject(ctx, dict[fitShading], selection.scope)
}

func (i *fitProgramInspector) tilingPattern(
	ctx context.Context,
	selection fitPatternSelection,
	pattern fitPatternProgram,
	state *fitGraphicsState,
) error {
	if err := i.resourceBox(ctx, pattern.dict, keyBBox, state); err != nil {
		return fitResourceError(err)
	}

	if err := i.tilingSteps(ctx, pattern.dict); err != nil {
		return fitResourceError(err)
	}

	paint, err := i.integer(ctx, pattern.dict["PaintType"])
	if err != nil {
		return fitResourceError(err)
	}

	if paint != 1 && paint != 2 {
		return fmt.Errorf("%w: tiling PaintType must be 1 or 2", errFitUnsupported)
	}

	if pattern.dict[keyResources] == nil {
		return fmt.Errorf("%w: used tiling pattern requires Resources", errFitUnsupported)
	}

	scope, err := i.programScope(ctx, pattern.dict)
	if err != nil {
		return fitResourceError(err)
	}

	state = fitTilingEntry(selection, state, paint)

	identity := fitObjectID(pattern.object, pattern.dict)

	content, err := i.programBytes(ctx, identity, pattern.stream)
	if err != nil {
		return fitResourceError(err)
	}

	return i.visit(ctx, identity, content, scope, state, fitShortEdge("paint /Pattern /"+selection.name))
}

func (i *fitProgramInspector) tilingSteps(ctx context.Context, dict types.Dict) error {
	for _, key := range []string{"XStep", "YStep"} {
		number, err := i.pdf.DereferenceNumberContext(ctx, dict[key])
		if err != nil {
			return fitResourceError(err)
		}

		if number == 0 || !finiteAppearanceNumber(number) {
			return fmt.Errorf("%w: pattern /%s must be finite and nonzero", errFitUnsupported, key)
		}
	}

	return nil
}

func fitTilingEntry(selection fitPatternSelection, placement *fitGraphicsState, paint int) *fitGraphicsState {
	state := selection.context.entry
	state.sourceMatrix = placement.sourceMatrix

	state.matrix = placement.matrix
	if paint == 2 {
		state.fillPattern = false
		state.strokePattern = false
		state.fill = fitPatternSelection{}
		state.stroke = fitPatternSelection{}
		state.colorRestricted = true
	}

	return &state
}

func (i *fitProgramInspector) shading(ctx context.Context, scope types.Dict, name string) error {
	binding, err := i.operandResource(ctx, scope, fitShading, name)
	if err != nil {
		return fitResourceError(err)
	}

	object := binding.object

	return i.shadingObject(ctx, object, scope)
}

func (i *fitProgramInspector) shadingObject(ctx context.Context, object types.Object, scope types.Dict) error {
	// pdfcpu remains the semantic validator for non-program shading/function dictionaries.
	// The preflight bounds actually used streams and graph work before that validator runs.
	resolved, err := i.pdf.DereferenceContext(ctx, object)
	if err != nil {
		return fitResourceError(err)
	}

	var dict types.Dict

	switch value := resolved.(type) {
	case types.Dict:
		dict = value
	case types.StreamDict:
		dict = value.Dict
	default:
		return fmt.Errorf("%w: used shading is not dictionary/stream", errFitUnsupported)
	}

	if _, err = i.colorComponents(ctx, scope, dict[fitColorSpace], 0); err != nil {
		return fitResourceError(err)
	}

	return i.boundedResource(ctx, object, 0, map[string]bool{})
}

func (i *fitProgramInspector) inlineColorComponents(ctx context.Context, scope types.Dict, rawName string) (int, error) {
	name, err := types.DecodeName(rawName)
	if err != nil {
		return 0, fitResourceError(err)
	}

	return i.colorComponents(ctx, scope, types.Name(name), 0)
}

func (i *fitProgramInspector) colorComponents(ctx context.Context, scope types.Dict, object types.Object, depth int) (int, error) {
	if err := i.step(ctx); err != nil {
		return 0, fitResourceError(err)
	}

	if depth >= fitProgramDepthLimit {
		return 0, fmt.Errorf("%w: active color space depth limit exceeded", errFitUnsupported)
	}

	value, err := i.pdf.DereferenceContext(ctx, object)
	if err != nil {
		return 0, fitResourceError(err)
	}

	if name, ok := value.(types.Name); ok {
		return i.namedColorComponents(ctx, scope, string(name), depth)
	}

	array, ok := value.(types.Array)
	if !ok || len(array) == 0 {
		return 0, fmt.Errorf("%w: active color space has no resolvable components", errFitUnsupported)
	}

	return i.arrayColorComponents(ctx, array)
}

func (i *fitProgramInspector) namedColorComponents(ctx context.Context, scope types.Dict, name string, depth int) (int, error) {
	components, defaultName := fitDeviceComponents(name)
	if components > 0 {
		spaces, readErr := i.pdf.DereferenceDictContext(ctx, scope[fitColorSpace])
		if readErr != nil {
			return 0, fitResourceError(readErr)
		}

		if replacement, found := spaces[defaultName]; found {
			return i.colorComponents(ctx, scope, replacement, depth+1)
		}

		return components, nil
	}

	binding, err := i.resource(ctx, scope, fitColorSpace, name)
	if err != nil {
		return 0, fitResourceError(err)
	}

	replacement := binding.object

	return i.colorComponents(ctx, scope, replacement, depth+1)
}

func (i *fitProgramInspector) arrayColorComponents(ctx context.Context, array types.Array) (int, error) {
	kind, err := i.name(ctx, array[0])
	if err != nil {
		return 0, fitResourceError(err)
	}

	switch kind {
	case fitCalGray, "Indexed", "I", "Separation":
		return fitGrayComponents, nil
	case "CalRGB", "Lab":
		return fitRGBComponents, nil
	case fitICCBased:
		return i.iccPaintComponents(ctx, array)
	case fitDeviceN:
		return i.deviceNComponents(ctx, array)
	default:
		return 0, fmt.Errorf("%w: color space /%.64s has unsupported inline-image metrics", errFitUnsupported, kind)
	}
}

func (i *fitProgramInspector) iccPaintComponents(ctx context.Context, array types.Array) (int, error) {
	if len(array) != 2 {
		return 0, fmt.Errorf("%w: active ICCBased color space is invalid", errFitUnsupported)
	}

	stream, err := fitReadProgramStream(ctx, i.pdf, array[1])
	if err != nil {
		return 0, fitResourceError(err)
	}

	count, err := i.integer(ctx, stream.Dict["N"])
	if err != nil {
		return 0, fitResourceError(err)
	}

	if count != fitGrayComponents && count != fitRGBComponents && count != fitCMYKComponents {
		return 0, fmt.Errorf("%w: active ICC component count is invalid", errFitUnsupported)
	}

	return count, nil
}

func (i *fitProgramInspector) deviceNComponents(ctx context.Context, array types.Array) (int, error) {
	if len(array) < 2 {
		return 0, fmt.Errorf("%w: active DeviceN color space is invalid", errFitUnsupported)
	}

	names, err := i.pdf.DereferenceArrayContext(ctx, array[1])
	if err != nil {
		return 0, fitResourceError(err)
	}

	if len(names) == 0 || len(names) > 32 {
		return 0, fmt.Errorf("%w: active DeviceN component limit exceeded", errFitUnsupported)
	}

	return len(names), nil
}

func (i *fitProgramInspector) boundedResource(ctx context.Context, object types.Object, depth int, active map[string]bool) error {
	if err := i.step(ctx); err != nil {
		return fitResourceError(err)
	}

	if depth >= fitProgramDepthLimit || i.edges >= fitInvocationEdgeLimit {
		return fmt.Errorf("%w: used shading/function graph limit exceeded", errFitUnsupported)
	}

	i.edges++

	value, err := i.pdf.DereferenceContext(ctx, object)
	if err != nil {
		return fitResourceError(err)
	}

	switch value := value.(type) {
	case types.Array:
		return i.resourceChildren(ctx, value, depth, active)
	case types.StreamDict:
		return i.resourceDictionary(ctx, object, value.Dict, &value, depth, active)
	case types.Dict:
		return i.resourceDictionary(ctx, object, value, nil, depth, active)
	case types.Float:
		if !finiteAppearanceNumber(float64(value)) {
			return fmt.Errorf("%w: used shading/function number is not finite", errFitUnsupported)
		}
	default:
	}

	return nil
}

func fitUsedResourceFields(dict types.Dict) []types.Object {
	var objects []types.Object

	for _, key := range []string{
		"Function", "Functions", fitColorSpace, "Domain", "Range", "Bounds", "Encode", "Decode",
		"C0", "C1", "N", "Coords", "Extend", "BBox", fitMatrix, "WhitePoint", "BlackPoint", "Gamma",
	} {
		if object, found := dict[key]; found {
			objects = append(objects, object)
		}
	}

	return objects
}

func (i *fitProgramInspector) extGStateFont(ctx context.Context, object types.Object, state *fitGraphicsState) error {
	array, err := i.pdf.DereferenceArrayContext(ctx, object)
	if err != nil {
		return fitResourceError(err)
	}

	if len(array) != 2 {
		return fmt.Errorf("%w: applied ExtGState Font requires font and size", errFitUnsupported)
	}

	font, err := i.pdf.DereferenceDictContext(ctx, array[0])
	if err != nil {
		return fitResourceError(err)
	}

	size, err := i.pdf.DereferenceNumberContext(ctx, array[1])
	if err != nil {
		return fitResourceError(err)
	}

	if !finiteAppearanceNumber(size) {
		return fmt.Errorf("%w: applied ExtGState Font size must be finite", errFitUnsupported)
	}

	state.font = fitFontSelection{object: array[0], identity: fitObjectID(array[0], font)}
	state.fontSize = size

	return nil
}

func (i *fitProgramInspector) softMaskGroup(ctx context.Context, dict types.Dict, kind string) (fitMaskProgram, error) {
	object, found := dict["G"]
	if !found {
		return fitMaskProgram{}, fmt.Errorf("%w: applied SMask has no G", errFitUnsupported)
	}

	stream, err := fitReadProgramStream(ctx, i.pdf, object)
	if err != nil {
		return fitMaskProgram{}, fitResourceError(err)
	}

	subtype, err := i.name(ctx, stream.Dict[keySubtype])
	if err != nil {
		return fitMaskProgram{}, fitResourceError(err)
	}

	if subtype != "Form" {
		return fitMaskProgram{}, fmt.Errorf("%w: applied SMask G must be a transparency Form", errFitUnsupported)
	}

	group, err := i.pdf.DereferenceDictContext(ctx, stream.Dict["Group"])
	if err != nil {
		return fitMaskProgram{}, fitResourceError(err)
	}

	if group == nil {
		return fitMaskProgram{}, fmt.Errorf("%w: applied SMask G requires a nonnull transparency Group", errFitUnsupported)
	}

	if kind == "Luminosity" {
		color, readErr := i.pdf.DereferenceContext(ctx, group["CS"])
		if readErr != nil {
			return fitMaskProgram{}, fitResourceError(readErr)
		}

		if color == nil {
			return fitMaskProgram{}, fmt.Errorf("%w: active luminosity SMask G Group requires explicit nonnull CS", errFitUnsupported)
		}
	}

	return fitMaskProgram{object: object, stream: stream}, nil
}

func (i *fitProgramInspector) resourceChildren(ctx context.Context, objects types.Array, depth int, active map[string]bool) error {
	for _, object := range objects {
		if err := i.boundedResource(ctx, object, depth+1, active); err != nil {
			return fitResourceError(err)
		}
	}

	return nil
}

func (i *fitProgramInspector) resourceDictionary(
	ctx context.Context,
	object types.Object,
	dict types.Dict,
	stream *types.StreamDict,
	depth int,
	active map[string]bool,
) error {
	identity := fitObjectID(object, dict)
	if active[identity] {
		return fmt.Errorf("%w: used shading/function dictionary cycle", errFitUnsupported)
	}

	active[identity] = true
	defer delete(active, identity)

	if stream != nil {
		if _, err := i.programBytes(ctx, identity, stream); err != nil {
			return fitResourceError(err)
		}
	}

	return i.resourceChildren(ctx, types.Array(fitUsedResourceFields(dict)), depth, active)
}
