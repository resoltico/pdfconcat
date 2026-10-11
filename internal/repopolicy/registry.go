// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Entry records one exact, defended mutation judgment.
type Entry struct {
	ID               string   `yaml:"id"`
	Tool             string   `yaml:"tool"`
	Kind             string   `yaml:"kind"`
	RetainedProperty string   `yaml:"retained_property"`
	Path             string   `yaml:"path"`
	Operator         string   `yaml:"operator"`
	Anchor           string   `yaml:"anchor"`
	Column           *int     `yaml:"column"`
	Statuses         []string `yaml:"statuses"`
	Rationale        string   `yaml:"-"`
}

// Registry contains precise mutation exceptions; lint configuration and raw coverage have separate authorities.
type Registry struct {
	Version    int      `yaml:"version"`
	Exceptions []*Entry `yaml:"exceptions"`
}

const (
	ToolMutation            = "mutation"
	KindEquivalentMutant    = "equivalent-mutant"
	KindOutOfDomainMutant   = "out-of-domain-mutant"
	yamlSingleDocumentError = "%w: expected one YAML document and EOF"
	yamlEOFFailedError      = "%w: expected one YAML document and EOF: %w"
	fieldPath               = "path"
	fieldColumn             = "column"
	minRationaleWords       = 8
	minPropertyWords        = 3
)

var (
	ErrRegistry     = errors.New("quality exception registry")
	idPattern       = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)+$`)
	pathMetaPattern = regexp.MustCompile(`[*?\[\]{}()|\\^$+<>"'\s]`)
)

func LoadRegistry(file string) (*Registry, error) {
	content, err := readFile(file)
	if err != nil {
		return nil, err
	}

	registry, err := ParseRegistry(content)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}

	return registry, nil
}

// ParseRegistry parses registry content, rejecting unknown fields, attaching each entry's rationale
// comment, and validating the result.

func ParseRegistry(content []byte) (*Registry, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)

	var registry Registry

	err := decoder.Decode(&registry)
	if err != nil {
		return nil, fmt.Errorf("%w: decode: %w", ErrRegistry, err)
	}

	var trailing any
	if endErr := decoder.Decode(&trailing); endErr == nil {
		return nil, fmt.Errorf(yamlSingleDocumentError, ErrRegistry)
	} else if !errors.Is(endErr, io.EOF) {
		return nil, fmt.Errorf(yamlEOFFailedError, ErrRegistry, endErr)
	}

	var document yaml.Node

	err = yaml.Unmarshal(content, &document)
	if err != nil {
		return nil, fmt.Errorf("%w: decode comments: %w", ErrRegistry, err)
	}

	if columnErr := checkColumnTypes(&document); columnErr != nil {
		return nil, columnErr
	}

	attachRationales(&document, registry.Exceptions)

	err = registry.Validate()
	if err != nil {
		return nil, err
	}

	return &registry, nil
}

// checkColumnTypes prevents YAML float-to-integer coercion of exact byte positions.

func checkColumnTypes(node *yaml.Node) error {
	if node.Kind == yaml.MappingNode {
		for index := 0; index+1 < len(node.Content); index += 2 {
			if node.Content[index].Value == fieldColumn && node.Content[index+1].Tag != "!!int" {
				return fmt.Errorf("%w: column must be an integer", ErrRegistry)
			}
		}
	}

	for _, child := range node.Content {
		if err := checkColumnTypes(child); err != nil {
			return err
		}
	}

	return nil
}

// attachRationales copies the comment above each exceptions item onto the matching entry.

func attachRationales(document *yaml.Node, entries []*Entry) {
	if len(document.Content) == 0 {
		return
	}

	root := document.Content[0]

	for index := 0; index+1 < len(root.Content); index += 2 {
		if root.Content[index].Value != "exceptions" {
			continue
		}

		for position, item := range root.Content[index+1].Content {
			if position < len(entries) && entries[position] != nil {
				entries[position].Rationale = rationaleOf(item)
			}
		}
	}
}

// rationaleOf returns the comment attached directly above a sequence item.

func rationaleOf(item *yaml.Node) string {
	head := item.HeadComment
	if head == "" && len(item.Content) > 0 {
		head = item.Content[0].HeadComment
	}

	lines := strings.Split(head, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
	}

	return strings.TrimSpace(strings.Join(lines, " "))
}

// Validate checks the structure of every entry and the uniqueness of identifiers and exact coverage scopes. It reports every
// problem it finds, not only the first.

func (r *Registry) For(tool string) []*Entry {
	var entries []*Entry

	for _, entry := range r.Exceptions {
		if entry.Tool == tool {
			entries = append(entries, entry)
		}
	}

	return entries
}

// problems lists everything wrong with the entry on its own, without reference to other entries.

func pathProblems(field, value string) []string {
	switch {
	case value == "":
		return []string{field + " is required"}
	case strings.HasPrefix(value, "/") || strings.Contains(value, `\`) || strings.Contains(value, ".."):
		return []string{field + " must be a relative, slash-separated repository path without .."}
	case pathMetaPattern.MatchString(value):
		return []string{field + " must name one exact file; globs, regular expressions and spaces are not allowed"}
	case !strings.HasSuffix(value, goSourceSuffix):
		return []string{field + " must name one Go source file, not a directory or pattern"}
	default:
		return nil
	}
}

// regexProblems requires a present, compilable pattern with enough literal text to identify one diagnostic.

// Validate requires exact, defended and uniquely identified resolved judgments.
func (r *Registry) Validate() error {
	var problems []string
	if r.Version != 1 {
		problems = append(problems, fmt.Sprintf("version is %d, want 1", r.Version))
	}
	seen := map[string]bool{}
	for _, e := range r.Exceptions {
		if e == nil {
			problems = append(problems, "null exception entry")
			continue
		}
		if seen[e.ID] {
			problems = append(problems, "duplicate id: "+e.ID)
		}
		seen[e.ID] = true
		for _, problem := range e.problems() {
			problems = append(problems, fmt.Sprintf("entry %q: %s", e.ID, problem))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrRegistry, strings.Join(problems, "; "))
	}
	return nil
}

func (e *Entry) problems() []string {
	var p []string
	if !idPattern.MatchString(e.ID) {
		p = append(p, "id must be lower-case words joined by hyphens")
	}
	if e.Tool != ToolMutation {
		p = append(p, fmt.Sprintf("tool %q must be mutation", e.Tool))
	}
	if !slices.Contains([]string{KindEquivalentMutant, KindOutOfDomainMutant}, e.Kind) {
		p = append(p, "kind is not valid for mutation")
	}
	if len(strings.Fields(e.RetainedProperty)) < minPropertyWords {
		p = append(p, "retained_property must state the property that remains protected")
	}
	if len(strings.Fields(e.Rationale)) < minRationaleWords {
		p = append(p, "the comment directly above the entry must explain why repair is inappropriate")
	}
	p = append(p, pathProblems(fieldPath, e.Path)...)
	if !slices.Contains(MutationOperators(), e.Operator) {
		p = append(p, "operator is not supported")
	}
	if e.Anchor == "" {
		p = append(p, "anchor is required")
	} else if strings.TrimSpace(e.Anchor) != e.Anchor {
		p = append(p, "anchor must be the trimmed source line")
	}
	if e.Column == nil {
		p = append(p, "column is required")
	} else if *e.Column < 1 {
		p = append(p, "mutation column must be a positive byte column")
	}
	if len(e.Statuses) == 0 {
		p = append(p, "statuses is required")
	}
	for _, status := range e.Statuses {
		if !slices.Contains(acceptableMutantStatuses(), status) {
			p = append(p, fmt.Sprintf("status %q is not one of %s", status, strings.Join(acceptableMutantStatuses(), ", ")))
		}
	}
	return p
}
