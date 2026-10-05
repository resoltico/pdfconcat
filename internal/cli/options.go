// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package cli

import (
	"strconv"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

// Option names.
const (
	optHelp            = "--help"
	optHelpShort       = "-h"
	optVersion         = "--version"
	optPrintSchema     = "--print-schema"
	optOutput          = "--output"
	optOutputShort     = "-o"
	optPlan            = "--plan"
	optOverwrite       = "--overwrite"
	optDryRun          = "--dry-run"
	optJSON            = "--json"
	optBlank           = "--blank"
	optBlankSize       = "--blank-size"
	optBlankBackground = "--blank-background"
	optBlankText       = "--blank-text"
	optBlankFont       = "--blank-font"
	optBlankFontSize   = "--blank-font-size"
	optBlankColor      = "--blank-color"
	optBlankAnchor     = "--blank-anchor"
	optBlankX          = "--blank-x"
	optBlankY          = "--blank-y"
	optBlankWidth      = "--blank-width"
	optBlankAlign      = "--blank-align"
	optBlankLeading    = "--blank-leading"
	optEndOfOptions    = "--"
)

// valueOption describes an option that takes one value.
type valueOption struct {
	// Values of path options may not be empty.
	path  bool
	apply func(*Request, string) error
}

// valueOptions returns every option that takes a value, keyed by its canonical long name.
func valueOptions() map[string]valueOption {
	styleOption := func(apply func(*assembly.BlankStyle, string) error) valueOption {
		return valueOption{apply: func(request *Request, value string) error { return apply(&request.Blank, value) }}
	}
	lengthOption := func(target func(*assembly.BlankStyle) *assembly.Option[assembly.Length]) valueOption {
		return styleOption(func(blank *assembly.BlankStyle, value string) error {
			return setParsed(target(blank), value, assembly.ParseLength)
		})
	}

	return map[string]valueOption{
		optOutput: {path: true, apply: func(request *Request, value string) error { request.Output = value; return nil }},
		optPlan:   {path: true, apply: func(request *Request, value string) error { request.PlanPath = value; return nil }},

		optBlankSize: styleOption(func(blank *assembly.BlankStyle, value string) error {
			return setParsed(&blank.Size, value, assembly.ParsePageSize)
		}),
		optBlankBackground: styleOption(func(blank *assembly.BlankStyle, value string) error {
			return setParsed(&blank.Background, value, assembly.ParseColor)
		}),
		optBlankText: styleOption(func(blank *assembly.BlankStyle, value string) error {
			blank.Text.Value = assembly.Some(value)
			return nil
		}),
		optBlankFont: styleOption(func(blank *assembly.BlankStyle, value string) error {
			return setParsed(&blank.Text.Font, value, assembly.ParseFont)
		}),
		optBlankFontSize: lengthOption(func(blank *assembly.BlankStyle) *assembly.Option[assembly.Length] { return &blank.Text.Size }),
		optBlankColor: styleOption(func(blank *assembly.BlankStyle, value string) error {
			return setParsed(&blank.Text.Color, value, assembly.ParseColor)
		}),
		optBlankAnchor: styleOption(func(blank *assembly.BlankStyle, value string) error {
			return setParsed(&blank.Text.Anchor, value, assembly.ParseAnchor)
		}),
		optBlankX:     lengthOption(func(blank *assembly.BlankStyle) *assembly.Option[assembly.Length] { return &blank.Text.X }),
		optBlankY:     lengthOption(func(blank *assembly.BlankStyle) *assembly.Option[assembly.Length] { return &blank.Text.Y }),
		optBlankWidth: lengthOption(func(blank *assembly.BlankStyle) *assembly.Option[assembly.Length] { return &blank.Text.Width }),
		optBlankAlign: styleOption(func(blank *assembly.BlankStyle, value string) error {
			return setParsed(&blank.Text.Align, value, assembly.ParseTextAlign)
		}),
		optBlankLeading: styleOption(func(blank *assembly.BlankStyle, value string) error {
			leading, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return Usagef("invalid leading %q: want a number such as 1.2", value)
			}

			blank.Text.Leading = assembly.Some(leading)

			return nil
		}),
	}
}

// setParsed parses value into target, reporting failures as usage errors.
func setParsed[T any](target *assembly.Option[T], value string, parse func(string) (T, error)) error {
	option, err := assembly.ParseSome(value, parse)
	if err != nil {
		return &UsageError{Message: err.Error()}
	}

	*target = option

	return nil
}

// canonicalName maps short option spellings to their long names.
func canonicalName(name string) string {
	if name == optOutputShort {
		return optOutput
	}

	return name
}

// isFlag reports whether name is an option that takes no value.
func isFlag(name string) bool {
	switch name {
	case optHelp, optHelpShort, optVersion, optPrintSchema, optOverwrite, optDryRun, optJSON, optBlank, optEndOfOptions:
		return true
	default:
		return false
	}
}
