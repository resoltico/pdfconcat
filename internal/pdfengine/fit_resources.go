// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	cs "github.com/benoitkugler/pdf/contentstream"
	"github.com/benoitkugler/pdf/reader/parser"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type (
	fitProgramInspector struct {
		pdf                                                                *model.Context
		simpleFonts                                                        map[string]fitSimpleMetrics
		decoded                                                            map[string][]byte
		parsed                                                             map[fitProgramKey][]fitInstruction
		active                                                             map[fitInvocationKey]bool
		complete                                                           map[fitResultKey]bool
		pageResources                                                      types.Dict
		chain                                                              []string
		fit                                                                PageFit
		bytes                                                              int64
		operations, edges, objects, tokens, inlineWork, work, contextCount int
	}

	fitProgramKey  struct{ identity, scope string }
	fitInstruction struct {
		operation cs.Operation
		span      parser.ContentSpan
	}
	fitFontSelection struct {
		object   types.Object
		identity string
	}
	fitInvocationContext struct {
		entry    fitGraphicsState
		identity int
	}
	fitPatternSelection struct {
		context                       *fitInvocationContext
		scope                         types.Dict
		name                          string
		originalAnchor, emittedAnchor Affine
	}

	fitGraphicsState struct {
		context                                                        *fitInvocationContext
		font                                                           fitFontSelection
		mask                                                           string
		fill, stroke                                                   fitPatternSelection
		matrix, sourceMatrix, textMatrix, textLine                     Affine
		fontSize, leading, charSpace, wordSpace, horizontal, rise      float64
		textRender                                                     int
		colorRestricted, fillPattern, strokePattern, textPositionKnown bool
	}
	fitInvocationKey struct {
		program, scope, fallback, font, fill, stroke, mask string
		render                                             int
		colorRestricted, fillPattern, strokePattern        bool
	}
	fitResultKey struct {
		invocation                                                                       fitInvocationKey
		fillContext, strokeContext                                                       int
		fitTolerance                                                                     [2]float64
		fillOriginalAnchor, fillEmittedAnchor, strokeOriginalAnchor, strokeEmittedAnchor Affine
		matrix, sourceMatrix, textMatrix, textLine                                       Affine
		fontSize, leading, charSpace, wordSpace, horizontal, rise                        float64
		textPositionKnown                                                                bool
	}
)

const (
	fitTextFillStrokeClip        = 6
	fitTokenByteLimit            = 1 << 20
	fitParserTokenLimit          = 1048576
	fitParserObjectLimit         = 1048576
	fitContainerDepthLimit       = 64
	fitOperandCountLimit         = 1024
	fitInlineEncodedLimit        = 8 << 20
	fitInlineDecodedLimit        = 16 << 20
	fitInlineWorkProgramLimit    = 64 << 20
	fitImageDimensionLimit       = 32768
	fitImagePixelLimit           = 16777216
	fitEncodingDifferenceLimit   = 1024
	fitEdgeLabelByteLimit        = 96
	fitTextPercentBase           = 100
	fitSpaceCode                 = 32
	fitAffineCoefficients        = 6
	fitProgramDepthLimit         = 64
	fitProgramStateLimit         = 32768
	fitProgramOperationLimit     = 262144
	fitSourceOperationLimit      = 1048576
	fitInvocationEdgeLimit       = 1048576
	fitGraphicsStackLimit        = 256
	fitSourceObjectLimit         = 2097152
	fitSourceTokenLimit          = 4194304
	fitSourceInlineWorkByteLimit = 128 << 20
	fitResourceWorkLimit         = 4194304
)

func fitIdentityMatrix() Affine { return Affine{1, 0, 0, 1, 0, 0} }

func newFitProgramInspector(pdf *model.Context) *fitProgramInspector {
	return &fitProgramInspector{
		simpleFonts: map[string]fitSimpleMetrics{}, pdf: pdf,
		decoded:  map[string][]byte{},
		parsed:   map[fitProgramKey][]fitInstruction{},
		active:   map[fitInvocationKey]bool{},
		complete: map[fitResultKey]bool{},
	}
}

func (i *fitProgramInspector) inspectPage(ctx context.Context, page types.Dict, resources types.Object, fit PageFit) error {
	i.fit = fit

	scope, err := i.pdf.DereferenceDictContext(ctx, resources)
	if err != nil {
		return fitResourceError(err)
	}

	if err = i.group(ctx, page["Group"], scope); err != nil {
		return fmt.Errorf("source page /Group: %w", err)
	}

	remaining := min(fitProgramByteLimit, fitSourceProgramByteLimit-i.bytes)

	content, err := fitLogicalContentLimit(ctx, i.pdf, page, remaining)
	if err != nil {
		return fitContentError(err)
	}

	return i.inspect(ctx, content, resources, fit.Matrix)
}

func (i *fitProgramInspector) inspect(ctx context.Context, content []byte, resources types.Object, matrix Affine) error {
	scope, err := i.pdf.DereferenceDictContext(ctx, resources)
	if err != nil {
		return fmt.Errorf("fit program resources: %w", err)
	}

	i.pageResources = scope

	identity := fmt.Sprintf("page-content:%x", sha256.Sum256(content))
	if _, found := i.decoded[identity]; !found {
		i.bytes += int64(len(content))

		i.decoded[identity] = content
	}

	return i.visit(
		ctx,
		identity,
		content,
		scope, &fitGraphicsState{
			matrix:            matrix,
			sourceMatrix:      fitIdentityMatrix(),
			textMatrix:        fitIdentityMatrix(),
			textLine:          fitIdentityMatrix(),
			horizontal:        1,
			textPositionKnown: true,
		}, "page",
	)
}

func (i *fitProgramInspector) visit(
	ctx context.Context,
	identity string,
	content []byte,
	scope types.Dict,
	state *fitGraphicsState,
	edge string,
) error {
	ownedState := *state
	state = &ownedState

	if err := ctx.Err(); err != nil {
		return fitResourceError(err)
	}

	if i.edges >= fitInvocationEdgeLimit {
		return fmt.Errorf("%w: invoked resource edge limit %d", errFitUnsupported, fitInvocationEdgeLimit)
	}

	i.edges++
	key := fitInvocation(identity, scope, state)

	key.fallback = fitScopeID(i.pageResources)
	if i.active[key] {
		return fmt.Errorf("%w: active content invocation cycle through %s", errFitUnsupported, strings.Join(append(i.chain, edge), " -> "))
	}

	result := i.resultKey(key, state)
	if i.complete[result] {
		return nil
	}

	if err := i.uncachedInvocationBudget(); err != nil {
		return fitResourceError(err)
	}

	i.active[key] = true

	i.chain = append(i.chain, edge)
	defer func() { delete(i.active, key); i.chain = i.chain[:len(i.chain)-1] }()

	instructions, err := i.parse(ctx, identity, content, scope)
	if err != nil {
		return fitResourceError(err)
	}

	state.context = i.invocationContext(state)

	if err = i.executeProgram(ctx, identity, instructions, scope, state); err != nil {
		return fitResourceError(err)
	}

	i.complete[result] = true

	return nil
}

func fitInvocation(identity string, scope types.Dict, state *fitGraphicsState) fitInvocationKey {
	return fitInvocationKey{
		colorRestricted: state.colorRestricted,
		program:         identity,
		scope:           fitScopeID(scope),
		font:            state.font.identity,
		fill:            state.fill.name + fitScopeID(state.fill.scope),
		stroke:          state.stroke.name + fitScopeID(state.stroke.scope),
		mask:            state.mask,
		render:          state.textRender,
		fillPattern:     state.fillPattern,
		strokePattern:   state.strokePattern,
	}
}
func fitScopeID(scope types.Dict) string { return fmt.Sprintf("%p", scope) }
func fitObjectID(object types.Object, dict types.Dict) string {
	if ref, ok := object.(types.IndirectRef); ok {
		return ref.PDFString()
	}

	return fitScopeID(dict)
}

func (i *fitProgramInspector) parse(ctx context.Context, identity string, content []byte, scope types.Dict) ([]fitInstruction, error) {
	key := fitProgramKey{identity: identity, scope: fitScopeID(scope)}
	if cached, found := i.parsed[key]; found {
		return cached, nil
	}

	if i.objects >= fitSourceObjectLimit || i.tokens >= fitSourceTokenLimit || i.inlineWork >= fitSourceInlineWorkByteLimit {
		return nil, fmt.Errorf("%w: aggregate source parser object/token/inline-work budget exhausted", errFitUnsupported)
	}

	limits := parser.ContentLimits{
		ProgramBytes:       int(fitProgramByteLimit),
		TokenBytes:         fitTokenByteLimit,
		Tokens:             min(fitParserTokenLimit, fitSourceTokenLimit-i.tokens),
		Objects:            min(fitParserObjectLimit, fitSourceObjectLimit-i.objects),
		Depth:              fitContainerDepthLimit,
		Operands:           fitOperandCountLimit,
		Operations:         fitProgramOperationLimit,
		InlineEncodedBytes: fitInlineEncodedLimit,
		InlineDecodedBytes: fitInlineDecodedLimit,
		InlineWorkBytes:    min(fitInlineWorkProgramLimit, fitSourceInlineWorkByteLimit-i.inlineWork),
		ImageDimension:     fitImageDimensionLimit,
		ImagePixels:        fitImagePixelLimit,
	}

	contentParser, err := parser.NewBoundedContentParser(ctx, content, limits)
	if err != nil {
		return nil, fmt.Errorf("fit bounded parser: %w", err)
	}

	contentParser.SetInlineColorResolver(func(rawName string) (int, error) { return i.inlineColorComponents(ctx, scope, rawName) })

	var instructions []fitInstruction

	for !contentParser.IsEOF() {
		if i.operations >= fitSourceOperationLimit {
			return nil, fmt.Errorf("%w: source operation limit %d", errFitUnsupported, fitSourceOperationLimit)
		}

		op, parseErr := contentParser.ParseContentElement(nil)
		if parseErr != nil {
			return nil, fmt.Errorf("fit bounded program %s: %w", identity, parseErr)
		}

		i.operations++

		instructions = append(instructions, fitInstruction{operation: op, span: contentParser.Span()})
	}

	stats := contentParser.Statistics()
	i.objects += stats.Objects
	i.tokens += stats.Tokens
	i.inlineWork += stats.InlineWorkBytes
	i.parsed[key] = instructions

	return instructions, nil
}

func fitCompose(outer, inner Affine) (Affine, error) {
	matrix := Affine{
		fitComposeTerms(outer[0], inner[0], outer[2], inner[1]),
		fitComposeTerms(outer[1], inner[0], outer[3], inner[1]),
		fitComposeTerms(outer[0], inner[2], outer[2], inner[3]),
		fitComposeTerms(outer[1], inner[2], outer[3], inner[3]),
		float64(fitComposeTerms(outer[0], inner[4], outer[2], inner[5]) + outer[4]),
		float64(fitComposeTerms(outer[1], inner[4], outer[3], inner[5]) + outer[5]),
	}
	for _, value := range matrix {
		if !finiteAppearanceNumber(value) {
			return Affine{}, fmt.Errorf("%w: composed content matrix is not finite", errFitGeometry)
		}
	}

	return matrix, nil
}

// Explicit binary64 rounding prevents fused evaluation from changing the proof's operation graph.
func fitComposeTerms(a, x, c, y float64) float64 {
	first := float64(a * x)
	second := float64(c * y)

	return float64(first + second)
}

func fitOriginalMatrix(span parser.ContentSpan) Affine {
	var matrix Affine
	for n, value := range span.Numbers {
		matrix[n] = value.Value
	}

	return matrix
}

func (i *fitProgramInspector) step(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fitResourceError(err)
	}

	if i.work >= fitResourceWorkLimit {
		return fmt.Errorf("%w: executed resource work exceeds %d items", errFitUnsupported, fitResourceWorkLimit)
	}

	i.work++

	return nil
}

func fitContextID(invocation *fitInvocationContext) int {
	if invocation == nil {
		return 0
	}

	return invocation.identity
}

func fitSelectPattern(rawName string, scope types.Dict, state *fitGraphicsState) (fitPatternSelection, error) {
	name, err := types.DecodeName(rawName)
	if err != nil {
		return fitPatternSelection{}, fitResourceError(err)
	}

	return fitPatternSelection{
		name:           name,
		scope:          scope,
		context:        state.context,
		originalAnchor: state.context.entry.sourceMatrix,
		emittedAnchor:  state.context.entry.matrix,
	}, nil
}

func (i *fitProgramInspector) resultKey(key fitInvocationKey, state *fitGraphicsState) fitResultKey {
	return fitResultKey{
		fitTolerance: [2]float64{
			min(fitSheetTolerance, i.fit.Original.Width*i.fit.Scale*fitRelativeTolerance),
			min(fitSheetTolerance, i.fit.Original.Height*i.fit.Scale*fitRelativeTolerance),
		},
		fillContext:          fitContextID(state.fill.context),
		strokeContext:        fitContextID(state.stroke.context),
		fillOriginalAnchor:   state.fill.originalAnchor,
		fillEmittedAnchor:    state.fill.emittedAnchor,
		strokeOriginalAnchor: state.stroke.originalAnchor,
		strokeEmittedAnchor:  state.stroke.emittedAnchor,
		invocation:           key,
		matrix:               state.matrix,
		sourceMatrix:         state.sourceMatrix,
		textMatrix:           state.textMatrix,
		fontSize:             state.fontSize,
		textLine:             state.textLine,
		leading:              state.leading,
		charSpace:            state.charSpace,
		wordSpace:            state.wordSpace,
		horizontal:           state.horizontal,
		rise:                 state.rise,
		textPositionKnown:    state.textPositionKnown,
	}
}

func (i *fitProgramInspector) invocationContext(state *fitGraphicsState) *fitInvocationContext {
	i.contextCount++
	entry := *state
	entry.context = nil

	return &fitInvocationContext{identity: i.contextCount, entry: entry}
}

func (i *fitProgramInspector) executeProgram(
	ctx context.Context,
	identity string,
	instructions []fitInstruction,
	scope types.Dict,
	state *fitGraphicsState,
) error {
	var stack []fitGraphicsState

	for _, instruction := range instructions {
		if err := i.step(ctx); err != nil {
			return fitResourceError(err)
		}

		err := i.execute(ctx, instruction, scope, state, &stack)
		if err != nil {
			return fmt.Errorf(
				"%w: program %s byte %d:%d (%s): %w",
				errFitUnsupported,
				identity,
				instruction.span.Start,
				instruction.span.End,
				strings.Join(i.chain, " -> "),
				err,
			)
		}
	}

	return nil
}

func (i *fitProgramInspector) uncachedInvocationBudget() error {
	if len(i.chain) >= fitProgramDepthLimit {
		return fmt.Errorf("%w: invoked content depth limit %d", errFitUnsupported, fitProgramDepthLimit)
	}

	if len(i.complete)+len(i.active) >= fitProgramStateLimit {
		return fmt.Errorf("%w: invoked dispatch context limit %d", errFitUnsupported, fitProgramStateLimit)
	}

	return nil
}
