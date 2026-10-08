// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package layout places the text of resolved generated pages. It joins the pure domain of package
// assembly (resolved page sizes and appearances) with package typeset (fonts, shaping, wrapping,
// overflow), so that neither knows about the other.
package layout

import (
	"context"
	"errors"
	"fmt"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

type (
	// FontSet returns a captured font, or the built-in font for the empty path.
	FontSet          func(path string) (*typeset.Font, bool)
	staticTextChecks struct {
		failures []error
		styles   []int
	}
	declarationConstraint struct {
		code       assembly.Code
		origin     assembly.Origin
		constraint int
	}
	placementDeclaration struct {
		member string
		origin assembly.Origin
	}
)

const (
	fontPointer      = "/blank/text/font"
	textValuePointer = "/blank/text/value"
	// StageLayout is text shaping, wrapping, and overflow on a resolved page.
	StageLayout assembly.Stage = "layout"

	// CodeTextOverflow means text with overflow "error" leaves its box or the page.
	CodeTextOverflow assembly.Code = "text_overflow"
	// CodeTextUnsupported means the text has characters the font or the shaper cannot render.
	CodeTextUnsupported assembly.Code = "text_unsupported"
	// CodeTextParameter means a text value is outside what the typesetter supports.
	CodeTextParameter assembly.Code = "text_parameter_invalid"
	// CodeFontInvalid means the font file is damaged: it loads, but an outline the text needs cannot be read.
	CodeFontInvalid assembly.Code = "font_invalid"
	// CodeTextFailed means shaping failed for another reason.
	CodeTextFailed assembly.Code = "text_layout_failed"
)

var errFontNotLoaded = errors.New("font was not loaded")

// Params maps a resolved blank to typeset parameters. The nine anchors and four alignments share their
// grid order with typeset's, offsets are points with +y up, and the wrap width is the block width.
func Params(spec *assembly.BlankSpec) typeset.Params {
	text := &spec.Text

	policy := typeset.OverflowReject
	if text.Overflow == assembly.OverflowAllow {
		policy = typeset.OverflowAllow
	}

	return typeset.Params{
		Text:        text.Value,
		Size:        float64(text.Size),
		PageWidth:   float64(spec.Dim.Width),
		PageHeight:  float64(spec.Dim.Height),
		Anchor:      typeset.Anchor(text.Anchor - assembly.AnchorTopLeft),
		OffsetX:     float64(text.X),
		OffsetY:     float64(text.Y),
		WrapWidth:   float64(text.Width),
		Align:       typeset.Align(text.Align - assembly.AlignLeft),
		LineSpacing: text.Leading,
		Overflow:    policy,
	}
}

// Place lays out the text of every distinct resolved spec exactly once, in table order, and returns the
// placements parallel to layout.Specs. A spec that cannot be placed, or whose text overflows under the
// "error" policy, retains available bounds/findings with diagnostics locating its effective declaration.
// Distinct declarations remain distinct faults despite appearance sharing; complete consumer references
// identify all affected contributions. Geometry-independent rejection has no placement. ctx is checked
// between specs.
func Place(ctx context.Context, layout *assembly.Layout, fonts FontSet) ([]*typeset.Placed, error) {
	return placeEach(ctx, layout, fonts, nil)
}

// PlaceBounds is Place for a check that renders nothing: only the bounds and findings of each placement
// are kept, so a check does not hold every shaped glyph of thousands of distinct texts.
func PlaceBounds(ctx context.Context, layout *assembly.Layout, fonts FontSet) ([]*typeset.Placed, error) {
	return placeEach(ctx, layout, fonts, dropLines)
}

// dropLines releases the shaped lines of a placement, which only rendering needs.
func dropLines(placed *typeset.Placed) { placed.Lines = nil }

func placeEach(ctx context.Context, layout *assembly.Layout, fonts FontSet, finish func(*typeset.Placed)) ([]*typeset.Placed, error) {
	var (
		shaper   typeset.Shaper
		problems assembly.Errors
	)

	placed := make([]*typeset.Placed, len(layout.Specs))

	for index := range layout.Specs {
		err := ctx.Err()
		if err != nil {
			return nil, fmt.Errorf("placing text: %w", err)
		}

		spec := &layout.Specs[index]

		font, found := fonts(spec.Spec.Text.Font.File)
		if !found {
			problems = append(problems, diagnoseEach(layout, index, assembly.CodeFontUnavailable, nil,
				fmt.Sprintf("font %q was not loaded; layout needs the font file", spec.Spec.Text.Font))...)

			continue
		}

		result, err := shaper.Place(font, Params(&spec.Spec))
		if result != nil {
			if finish != nil {
				finish(result)
			}

			placed[index] = result
		}

		if err != nil {
			problems = append(problems, diagnoseEach(layout, index, codeOf(err), err, err.Error())...)

			continue
		}
	}

	if len(problems) > 0 {
		return placed, problems
	}

	return placed, nil
}

func codeOf(err error) assembly.Code {
	var (
		overflow *typeset.OverflowError
		text     *typeset.TextError
		param    *typeset.InvalidParamError
	)

	switch {
	case errors.As(err, &overflow):
		return CodeTextOverflow
	case errors.As(err, &text):
		return CodeTextUnsupported
	case errors.As(err, &param):
		return CodeTextParameter
	case errors.Is(err, errFontNotLoaded):
		return assembly.CodeFontUnavailable
	case errors.Is(err, typeset.ErrMalformedFont):
		return CodeFontInvalid
	default:
		return CodeTextFailed
	}
}

// diagnose builds the error of a spec problem, located at the first contribution that uses the spec.
func diagnose(layout *assembly.Layout, specIndex int, code assembly.Code, cause error, message string) *assembly.Error {
	spec := &layout.Specs[specIndex]

	diagnostic := assembly.NewSharedError(layout.Source, spec.Origins, "/blank", assembly.Error{
		Err: cause, Message: message, Code: code, Stage: StageLayout, Affected: spec.Uses,
	})
	if code == CodeTextUnsupported || code == CodeFontInvalid {
		origin, member := textDeclaration(&spec.Declared, spec.Origins[0], code)

		diagnostic.Related = append(diagnostic.Related, diagnostic.Location)
		diagnostic.Location = assembly.Locate(layout.Source, origin, member)
	}

	return diagnostic
}

// PlaceSpec shapes one resolved spec and returns its located error without discarding overflow geometry.
// A shared Shaper keeps only bounded font caches between sequential pages.
func PlaceSpec(shaper *typeset.Shaper, table *assembly.Layout, index int, fonts FontSet) (*typeset.Placed, error) {
	spec := &table.Specs[index]

	font, found := fonts(spec.Spec.Text.Font.File)
	if !found {
		return nil, diagnoseEach(table, index, assembly.CodeFontUnavailable, nil, errFontNotLoaded.Error())
	}

	placed, err := shaper.Place(font, Params(&spec.Spec))
	if err != nil {
		return placed, diagnoseEach(table, index, codeOf(err), err, err.Error())
	}

	return placed, nil
}

// ValidateText caches geometry-independent constraints and aggregates faults by their actual
// declaration and font/text constraint. Consumer references remain complete after style deduplication.
func ValidateText(ctx context.Context, flat *assembly.Flattened, fonts FontSet) error {
	checks, err := checkStaticStyles(ctx, flat, fonts)
	if err != nil {
		return err
	}

	var problems assembly.Errors

	indexed := map[declarationConstraint]*assembly.Error{}

	for index := range flat.Contributions {
		if contextErr := ctx.Err(); contextErr != nil {
			return fmt.Errorf("validating static text: %w", contextErr)
		}

		contribution := &flat.Contributions[index]
		if contribution.Kind != assembly.ItemBlank {
			continue
		}

		constraint := checks.styles[contribution.Style]

		cause := checks.failures[constraint]
		if cause == nil {
			continue
		}

		code := codeOf(cause)
		origin, member := contributionDeclaration(flat, contribution, code)
		key := declarationConstraint{origin: origin, code: code, constraint: constraint}

		diagnostic, known := indexed[key]
		if !known {
			failure := checks.failures[key.constraint]
			diagnostic = &assembly.Error{
				Err: failure, Message: failure.Error(), Code: key.code, Stage: StageLayout,
				Location: assembly.Locate(flat.Source, key.origin, member),
			}
			indexed[key] = diagnostic
			problems = append(problems, diagnostic)
		}

		diagnostic.Affected++
		diagnostic.Consumers = append(diagnostic.Consumers, contribution.Origin)
	}

	if len(problems) > 0 {
		return problems
	}

	return nil
}

func checkStaticStyles(ctx context.Context, flat *assembly.Flattened, fonts FontSet) (staticTextChecks, error) {
	checks := staticTextChecks{styles: make([]int, len(flat.Styles))}

	var shaper typeset.Shaper

	validated := map[typeset.Digest]map[string]int{}

	for index := range flat.Styles {
		if err := ctx.Err(); err != nil {
			return checks, fmt.Errorf("validating static text: %w", err)
		}

		text := &flat.Styles[index].Style.Text

		font, found := fonts(text.Font.Value.File)
		if !found {
			checks.styles[index] = len(checks.failures)
			checks.failures = append(checks.failures, errFontNotLoaded)

			continue
		}

		perFont := validated[font.Identity()]
		if perFont == nil {
			perFont = map[string]int{}
			validated[font.Identity()] = perFont
		}

		constraint, known := perFont[text.Value.Value]
		if !known {
			constraint = len(checks.failures)
			perFont[text.Value.Value] = constraint
			checks.failures = append(checks.failures, shaper.ValidateText(font, text.Value.Value))
		}

		checks.styles[index] = constraint
	}

	return checks, nil
}

func contributionDeclaration(flat *assembly.Flattened, contribution *assembly.Contribution, code assembly.Code) (assembly.Origin, string) {
	effective := effectiveText(flat, contribution)
	if code == CodeFontInvalid || code == assembly.CodeFontUnavailable {
		if effective.Font.IsSet() {
			return effective.Font.Origin, fontPointer
		}

		return contribution.Origin, fontPointer
	}

	if effective.Value.IsSet() {
		return effective.Value.Origin, textValuePointer
	}

	return contribution.Origin, textValuePointer
}

func effectiveText(flat *assembly.Flattened, contribution *assembly.Contribution) assembly.TextStyle {
	own, defaults := assembly.BlankStyle{}, assembly.BlankStyle{}
	if contribution.TextOverrides != nil {
		own.Text = *contribution.TextOverrides
	}

	if flat.TextDefaults != nil {
		defaults.Text = *flat.TextDefaults
	}

	return own.Over(&defaults).Text
}

func textDeclaration(text *assembly.TextStyle, fallback assembly.Origin, code assembly.Code) (assembly.Origin, string) {
	if code == CodeFontInvalid && text.Font.IsSet() {
		return text.Font.Origin, fontPointer
	}

	if text.Value.IsSet() {
		return text.Value.Origin, textValuePointer
	}

	return fallback, textValuePointer
}

// diagnoseEach preserves different declarations even when their resolved appearance is identical.
func diagnoseEach(table *assembly.Layout, specIndex int, code assembly.Code, cause error, message string) assembly.Errors {
	if table.Flat == nil {
		return assembly.Errors{diagnose(table, specIndex, code, cause, message)}
	}

	var problems assembly.Errors

	indexed := map[string]*assembly.Error{}

	for index, placement := range table.Placements {
		if placement.Spec != specIndex {
			continue
		}

		contribution := &table.Flat.Contributions[index]
		effective := effectiveText(table.Flat, contribution)
		seen := map[string]bool{}

		for _, declaration := range lateDeclarations(&effective, contribution.Origin, code, cause) {
			pointer := table.Source.Pointer(declaration.origin.Ref, declaration.member)
			if seen[pointer] {
				continue
			}

			seen[pointer] = true

			problem, known := indexed[pointer]
			if !known {
				problem = &assembly.Error{
					Err:      cause,
					Message:  message,
					Code:     code,
					Stage:    StageLayout,
					Location: assembly.Locate(table.Source, declaration.origin, declaration.member),
				}
				indexed[pointer] = problem
				problems = append(problems, problem)
			}

			problem.Affected++
			problem.Consumers = append(problem.Consumers, contribution.Origin)
		}
	}

	return problems
}

func lateDeclarations(text *assembly.TextStyle, fallback assembly.Origin, code assembly.Code, cause error) []placementDeclaration {
	if code != CodeTextOverflow {
		origin, member := textDeclaration(text, fallback, code)
		return []placementDeclaration{{origin: origin, member: member}}
	}

	var declarations []placementDeclaration

	overflow, found := errors.AsType[*typeset.OverflowError](cause)
	if found {
		for _, finding := range overflow.Findings {
			declaration, known := findingDeclaration(text, finding.Kind)
			if known {
				declarations = append(declarations, declaration)
			}
		}
	}

	if len(declarations) > 0 {
		return declarations
	}

	if text.Anchor.IsSet() {
		return []placementDeclaration{{origin: text.Anchor.Origin, member: "/blank/text/anchor"}}
	}

	if text.Size.IsSet() {
		return []placementDeclaration{{origin: text.Size.Origin, member: "/blank/text/size"}}
	}

	if text.Value.IsSet() {
		return []placementDeclaration{{origin: text.Value.Origin, member: textValuePointer}}
	}

	return []placementDeclaration{{origin: fallback, member: "/blank"}}
}

func findingDeclaration(text *assembly.TextStyle, kind typeset.FindingKind) (placementDeclaration, bool) {
	switch kind {
	case typeset.FindingWordTooWide:
		if text.Width.IsSet() {
			return placementDeclaration{origin: text.Width.Origin, member: "/blank/text/width"}, true
		}
	case typeset.FindingOutsidePageHorizontal:
		if text.X.IsSet() {
			return placementDeclaration{origin: text.X.Origin, member: "/blank/text/x"}, true
		}
	case typeset.FindingOutsidePageVertical:
		if text.Y.IsSet() {
			return placementDeclaration{origin: text.Y.Origin, member: "/blank/text/y"}, true
		}
	default:
	}

	return placementDeclaration{}, false
}
