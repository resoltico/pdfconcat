// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strings"
)

type (
	// DirectiveViolation is one prohibited suppression comment.
	DirectiveViolation struct {
		File    string
		Kind    string
		Comment string
		Line    int
	}

	// directivePattern is one family of tool-suppression comments.
	directivePattern struct {
		pattern *regexp.Regexp
		kind    string
	}
)

const goSourceSuffix = ".go"

// String formats the violation for a test or gate message.
func (v DirectiveViolation) String() string {
	return fmt.Sprintf("%s:%d: %s directive %q; exceptions belong in .quality-exceptions.yml", v.File, v.Line, v.Kind, v.Comment)
}

// prohibitedDirectives matches the text of a comment (without its // or /* */ delimiters) that
// switches a quality tool off. Each pattern is anchored to the start of the comment, as the tools'
// own parsers anchor them. Gosec also recognizes markers on later block-comment lines. A sentence
// that merely mentions a directive, or a doc example
// indented as code, is not a directive.
func prohibitedDirectives() []directivePattern {
	return []directivePattern{
		{regexp.MustCompile(`(?i)^\s*nolint(\s|:|$)`), "nolint"},
		{regexp.MustCompile(`(?im)^\s*#?nosec(\s|:|$)`), "nosec"},
		{regexp.MustCompile(`(?i)^\s*lint:(file-)?ignore(\s|$)`), "lint:ignore or lint:file-ignore"},
		{
			regexp.MustCompile(
				`(?i)^\s*[a-z][a-z0-9_-]*:(disable|disable-next-line|disable-line|enable|ignore|file-ignore|skip|nocheck)(\s|:|$)`),
			"tool disable/ignore/skip",
		},
	}
}

// skippedDirectories are not owned source: version-control internals and ignored build output.
func skippedDirectories() map[string]bool {
	return map[string]bool{".git": true, ".tools": true, "dist": true}
}

// ScanDirectives parses every Go file under root, hidden directories included, and reports each
// comment that is a tool-suppression directive. Parsing the comments, not the text, keeps string
// literals and prose out of the findings, and leaves //go:build, //go:embed, //go:generate, //line
// and SPDX notices alone. A file that does not parse is an error: it cannot be shown clean. Files
// are read through an [os.Root], so a symbolic link cannot lead the scan outside root.
func ScanDirectives(ctx context.Context, root string) ([]DirectiveViolation, error) {
	files, err := OwnedGoSources(ctx, root)
	if err != nil {
		return nil, err
	}

	var violations []DirectiveViolation

	patterns := prohibitedDirectives()

	err = withRoot(root, func(tree *os.Root) error {
		for _, name := range files {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return fmt.Errorf("owned source scan canceled: %w", ctxErr)
			}

			found, scanErr := scanFile(tree, name, patterns)
			if scanErr != nil {
				return scanErr
			}

			violations = append(violations, found...)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan Go sources for directives: %w", err)
	}

	return violations, nil
}

// scanFile reads one file through the root and reports its directives.
func scanFile(tree *os.Root, name string, patterns []directivePattern) ([]DirectiveViolation, error) {
	content, err := tree.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}

	return parseDirectiveComments(name, content, patterns)
}

// DirectivesIn reports the prohibited directives in one Go source file.
func DirectivesIn(name string, content []byte) ([]DirectiveViolation, error) {
	return parseDirectiveComments(name, content, prohibitedDirectives())
}

func parseDirectiveComments(name string, content []byte, patterns []directivePattern) ([]DirectiveViolation, error) {
	fset := token.NewFileSet()

	parsed, err := parser.ParseFile(fset, name, content, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}

	var violations []DirectiveViolation

	for _, group := range parsed.Comments {
		for _, comment := range group.List {
			kind := directiveKind(commentText(comment.Text), patterns)
			if kind != "" {
				violations = append(violations, DirectiveViolation{
					File: name, Line: fset.PositionFor(comment.Pos(), false).Line, Kind: kind, Comment: comment.Text,
				})
			}
		}
	}

	return violations, nil
}

// directiveKind names the prohibited family a comment's text belongs to, or "" when it is not one.
func directiveKind(text string, patterns []directivePattern) string {
	for _, candidate := range patterns {
		if candidate.pattern.MatchString(text) {
			return candidate.kind
		}
	}

	return ""
}

// commentText strips the comment delimiters.
func commentText(raw string) string {
	if body, isLine := strings.CutPrefix(raw, "//"); isLine {
		return body
	}

	body := strings.TrimPrefix(raw, "/*")

	return strings.TrimSuffix(body, "*/")
}
