// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

type (
	// Entry is one registry exception. Which fields apply depends on Tool and Effect; Validate
	// rejects entries with missing or surplus fields.
	Entry struct {
		// Setting is the dotted .golangci.yml path of the list a setting-item entry extends.
		Setting string `yaml:"setting,omitempty"`
		// Operator is the mutation operator of a mutation entry, e.g. CONDITIONALS_BOUNDARY.
		Operator string `yaml:"operator,omitempty"`
		// Anchor is the exact trimmed source line at which an excluded block or mutant starts.
		Anchor string `yaml:"anchor,omitempty"`
		// Function names the function a coverage entry applies to: Name or Receiver.Name.
		Function string `yaml:"function,omitempty"`
		// GOOS restricts an entry to one operating system; empty applies everywhere.
		GOOS string `yaml:"goos,omitempty"`
		// Source is an optional regular expression matching the diagnosed source line.
		Source string `yaml:"source,omitempty"`
		// Message is a regular expression matching the diagnostic text of a diagnostic exclusion.
		Message string `yaml:"message,omitempty"`
		// Path is one repository file, as a slash-separated path.
		Path string `yaml:"path,omitempty"`
		// Linter names the linter an entry applies to.
		Linter string `yaml:"linter,omitempty"`
		// Effect is how a golangci-lint entry reaches .golangci.yml.
		Effect string `yaml:"effect,omitempty"`
		// RetainedProperty states what stays protected despite the exception.
		RetainedProperty string `yaml:"retained_property"`
		// Kind is the justification kind.
		Kind string `yaml:"kind"`
		// Tool is one of the Tool constants.
		Tool string `yaml:"tool"`
		// ID is the stable identifier.
		ID string `yaml:"id"`
		// Rationale is the YAML comment attached above the entry; it is not a YAML field.
		Rationale string `yaml:"-"`
		// Values are the exact items a setting-item entry adds.
		Values []string `yaml:"values,omitempty"`
		// ConflictsWith lists enabled linters that cannot be satisfied together with the excepted rule.
		ConflictsWith []string `yaml:"conflicts_with,omitempty"`
		// SupersededBy lists enabled linters that already enforce the excepted rule.
		SupersededBy []string `yaml:"superseded_by,omitempty"`
		// Column is the exact physical byte column of a mutation's operator token.
		Column *int `yaml:"column,omitempty"`
		// Statuses lists the judged mutation outcomes the entry accepts: lived or timed-out.
		Statuses []string `yaml:"statuses,omitempty"`
	}

	// Registry is the parsed .quality-exceptions.yml.
	Registry struct {
		// Exceptions lists every exception to a quality gate.
		Exceptions []*Entry `yaml:"exceptions"`
		// CoverageThresholdPercent is the minimum reachable statement coverage.
		CoverageThresholdPercent float64 `yaml:"coverage_threshold_percent"`
		// Version is the registry format version.
		Version int `yaml:"version"`
	}
)

// Tool names as written in the registry's tool field.
const (
	ToolLint     = "golangci-lint"
	ToolCoverage = "coverage"
	ToolMutation = "mutation"

	// EffectDisableLinter switches a whole linter off in .golangci.yml.
	EffectDisableLinter = "disable-linter"
	// EffectSettingItem adds items to a linter's ignore, disable or skip list.
	EffectSettingItem = "setting-item"
	// EffectExcludeDiagnostic adds one exclusion rule for one diagnostic in one file.
	EffectExcludeDiagnostic = "exclude-diagnostic"

	// KindDeprecatedRule marks a linter the tool itself has deprecated.
	KindDeprecatedRule = "deprecated-rule"
	// KindIncompatibleRule marks a rule that cannot be satisfied together with another enabled linter.
	KindIncompatibleRule = "incompatible-rule"
	// KindDuplicateRule marks a rule that another enabled linter already enforces.
	KindDuplicateRule = "duplicate-rule"
	// KindDesignIncompat marks a rule that contradicts a documented design decision.
	KindDesignIncompat = "design-incompatible"
	// KindIgnoredValue marks a value an ignore list accepts.
	KindIgnoredValue = "ignored-value"
	// KindFalsePositive marks a diagnostic that is wrong about the code.
	KindFalsePositive = "false-positive"
	// KindIntendedPattern marks a diagnostic about code that is deliberately written that way.
	KindIntendedPattern = "intended-pattern"
	// KindUnreachableBranch marks a statement no test can execute.
	KindUnreachableBranch = "unreachable-branch"
	// KindUnreachablePlat marks a statement that exists only for another operating system's run.
	KindUnreachablePlat = "unreachable-platform-branch"
	// KindGeneratedCode marks generated source.
	KindGeneratedCode = "generated-code"
	// KindEquivalentMutant marks a mutant that cannot change observable behavior.
	KindEquivalentMutant = "equivalent-mutant"
	// KindOutOfDomainMutant marks a mutant that only changes behavior outside the supported domain.
	KindOutOfDomainMutant = "out-of-domain-mutant"

	yamlSingleDocumentError = "%w: expected one YAML document and EOF"
	yamlEOFFailedError      = "%w: expected one YAML document and EOF: %w"

	fieldAnchor        = "anchor"
	fieldColumn        = "column"
	fieldConflictsWith = "conflicts_with"
	fieldFunction      = "function"
	fieldGOOS          = "goos"
	fieldLinter        = "linter"
	fieldMessage       = "message"
	fieldOperator      = "operator"
	fieldPath          = "path"
	fieldSetting       = "setting"
	fieldSource        = "source"
	fieldStatuses      = "statuses"
	fieldSupersededBy  = "superseded_by"
	fieldValues        = "values"

	// minRationaleWords is the fewest words an attached comment needs to count as a rationale.
	minRationaleWords = 8
	// minPropertyWords is the fewest words a retained_property needs to name a property.
	minPropertyWords = 3
	// minLiteralRunes is the fewest literal characters a message or source pattern must contain, so
	// that it identifies one diagnostic rather than matching nearly everything.
	minLiteralRunes = 6
	// maxPercent is the upper bound of the coverage threshold.
	maxPercent = 100
)

var (
	// ErrRegistry reports a malformed or invalid registry.
	ErrRegistry = errors.New("quality exception registry")

	idPattern       = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)+$`)
	pathMetaPattern = regexp.MustCompile(`[*?\[\]{}()|\\^$+<>"'\s]`)
)

// kindsByTool lists the justification kinds each tool's entries may use.
func kindsByTool() map[string][]string {
	return map[string][]string{
		ToolLint: {
			KindDeprecatedRule, KindIncompatibleRule, KindDuplicateRule, KindDesignIncompat,
			KindIgnoredValue, KindFalsePositive, KindIntendedPattern,
		},
		ToolCoverage: {KindUnreachableBranch, KindUnreachablePlat, KindGeneratedCode},
		ToolMutation: {KindEquivalentMutant, KindOutOfDomainMutant},
	}
}

// LoadRegistry reads, parses and validates the registry file.
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
func (r *Registry) Validate() error {
	var problems []string

	if r.Version != 1 {
		problems = append(problems, fmt.Sprintf("version is %d, want 1", r.Version))
	}

	if r.CoverageThresholdPercent != maxPercent {
		problems = append(
			problems,
			fmt.Sprintf("coverage_threshold_percent %v must be 100 for the reviewed reachable denominator", r.CoverageThresholdPercent),
		)
	}

	seen := map[string]bool{}
	coverageScopes := map[[4]string]string{}

	for _, entry := range r.Exceptions {
		if entry == nil {
			problems = append(problems, "null exception entry")
			continue
		}

		if seen[entry.ID] {
			problems = append(problems, fmt.Sprintf("duplicate id %q", entry.ID))
		}

		seen[entry.ID] = true

		if entry.Tool == ToolCoverage {
			scope := [4]string{entry.Path, entry.Function, entry.Anchor, entry.GOOS}
			if previous, exists := coverageScopes[scope]; exists {
				problems = append(problems, fmt.Sprintf("duplicate coverage scope in %q and %q", previous, entry.ID))
			}

			coverageScopes[scope] = entry.ID
		}

		for _, problem := range entry.problems() {
			problems = append(problems, fmt.Sprintf("entry %q: %s", entry.ID, problem))
		}
	}

	if len(problems) == 0 {
		return nil
	}

	return fmt.Errorf("%w: %s", ErrRegistry, strings.Join(problems, "; "))
}

// For returns the entries of one tool, in registry order.
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
func (e *Entry) problems() []string {
	var problems []string

	if !idPattern.MatchString(e.ID) {
		problems = append(problems, "id must be lower-case words joined by hyphens")
	}

	kinds, knownTool := kindsByTool()[e.Tool]
	if !knownTool {
		return append(problems, fmt.Sprintf("tool %q is not one of %s, %s, %s", e.Tool, ToolLint, ToolCoverage, ToolMutation))
	}

	if !slices.Contains(kinds, e.Kind) {
		problems = append(problems, fmt.Sprintf("kind %q is not valid for %s; use one of %s", e.Kind, e.Tool, strings.Join(kinds, ", ")))
	}

	if len(strings.Fields(e.RetainedProperty)) < minPropertyWords {
		problems = append(problems, "retained_property must state the property that remains protected")
	}

	if len(strings.Fields(e.Rationale)) < minRationaleWords {
		problems = append(problems, "the comment directly above the entry must explain why repair is inappropriate")
	}

	switch e.Tool {
	case ToolLint:
		problems = append(problems, e.lintProblems()...)
	case ToolCoverage:
		problems = append(problems, e.coverageProblems()...)
	case ToolMutation:
		problems = append(problems, e.mutationProblems()...)
	default:
	}

	return problems
}

// present reports which optional fields the entry sets, keyed by the registry's field names.
func (e *Entry) present() map[string]bool {
	return map[string]bool{
		fieldAnchor:        e.Anchor != "",
		fieldColumn:        e.Column != nil,
		fieldConflictsWith: len(e.ConflictsWith) > 0,
		fieldFunction:      e.Function != "",
		fieldGOOS:          e.GOOS != "",
		fieldLinter:        e.Linter != "",
		fieldMessage:       e.Message != "",
		fieldOperator:      e.Operator != "",
		fieldPath:          e.Path != "",
		fieldSetting:       e.Setting != "",
		fieldSource:        e.Source != "",
		fieldStatuses:      len(e.Statuses) > 0,
		fieldSupersededBy:  len(e.SupersededBy) > 0,
		fieldValues:        len(e.Values) > 0,
	}
}

// fieldProblems requires the fields in want and forbids every other optional field except those in
// optional.
func (e *Entry) fieldProblems(want, optional []string) []string {
	var problems []string

	present := e.present()

	for _, name := range slices.Sorted(maps.Keys(present)) {
		switch {
		case slices.Contains(want, name) && !present[name]:
			problems = append(problems, fmt.Sprintf("%s is required for %s entries", name, e.describe()))
		case present[name] && !slices.Contains(want, name) && !slices.Contains(optional, name):
			problems = append(problems, fmt.Sprintf("%s does not apply to %s entries", name, e.describe()))
		default:
		}
	}

	return problems
}

func (e *Entry) lintProblems() []string {
	var problems []string

	switch e.Effect {
	case EffectDisableLinter:
		problems = e.requireKind(KindDeprecatedRule, KindIncompatibleRule, KindDuplicateRule, KindDesignIncompat)
		problems = append(problems, e.fieldProblems([]string{fieldLinter}, []string{fieldConflictsWith, fieldSupersededBy})...)
	case EffectSettingItem:
		problems = e.requireKind(KindIgnoredValue, KindIncompatibleRule, KindDuplicateRule)
		problems = append(
			problems,
			e.fieldProblems([]string{fieldSetting, fieldValues}, []string{fieldConflictsWith, fieldSupersededBy})...)
		problems = append(problems, e.settingItemProblems()...)
	case EffectExcludeDiagnostic:
		problems = e.requireKind(KindFalsePositive, KindIntendedPattern)
		problems = append(problems, e.fieldProblems(
			[]string{fieldLinter, fieldPath, fieldMessage}, []string{fieldSource, fieldGOOS})...)
		problems = append(problems, pathProblems(fieldPath, e.Path)...)
		problems = append(problems, regexProblems(fieldMessage, e.Message)...)
		problems = append(problems, optionalRegexProblems(fieldSource, e.Source)...)
		problems = append(problems, e.goosProblems()...)
	default:
		problems = append(problems, fmt.Sprintf("effect %q is not one of %s, %s, %s",
			e.Effect, EffectDisableLinter, EffectSettingItem, EffectExcludeDiagnostic))
	}

	return append(problems, e.premiseProblems()...)
}

func (e *Entry) settingItemProblems() []string {
	var problems []string

	if !strings.HasPrefix(e.Setting, "linters.") && !strings.HasPrefix(e.Setting, "formatters.") {
		problems = append(problems, "setting must be a dotted path under linters. or formatters.")
	}

	seen := map[string]bool{}

	for _, value := range e.Values {
		if strings.TrimSpace(value) == "" || seen[value] {
			problems = append(problems, "values must be non-blank and unique")
		}

		seen[value] = true
	}

	return problems
}

func (e *Entry) goosProblems() []string {
	if e.GOOS != "" && !slices.Contains([]string{"darwin", "linux", "windows"}, e.GOOS) {
		return []string{fmt.Sprintf("goos %q is not a supported operating system", e.GOOS)}
	}

	return nil
}

// premiseProblems checks that a kind that depends on another linter names it.
func (e *Entry) premiseProblems() []string {
	var problems []string

	if e.Kind == KindIncompatibleRule && len(e.ConflictsWith) == 0 {
		problems = append(problems, "an incompatible-rule entry must list the enabled linters it conflicts with in conflicts_with")
	}

	if e.Kind == KindDuplicateRule && len(e.SupersededBy) == 0 {
		problems = append(problems, "a duplicate-rule entry must list the enabled linters that already enforce it in superseded_by")
	}

	if e.Kind != KindIncompatibleRule && len(e.ConflictsWith) > 0 {
		problems = append(problems, "conflicts_with applies only to incompatible-rule entries")
	}

	if e.Kind != KindDuplicateRule && len(e.SupersededBy) > 0 {
		problems = append(problems, "superseded_by applies only to duplicate-rule entries")
	}

	return problems
}

func (e *Entry) coverageProblems() []string {
	problems := e.fieldProblems([]string{fieldPath, fieldFunction, fieldAnchor}, []string{fieldGOOS})
	problems = append(problems, pathProblems(fieldPath, e.Path)...)
	problems = append(problems, e.goosProblems()...)

	if strings.TrimSpace(e.Anchor) != e.Anchor {
		problems = append(problems, "anchor must be the trimmed source line")
	}

	if e.Kind == KindUnreachablePlat && e.GOOS == "" {
		problems = append(problems, "an unreachable-platform-branch entry must name the operating system in goos")
	}

	if e.Effect != "" {
		problems = append(problems, "effect applies only to golangci-lint entries")
	}

	return problems
}

func (e *Entry) mutationProblems() []string {
	problems := e.fieldProblems([]string{fieldPath, fieldOperator, fieldAnchor, fieldColumn, fieldStatuses}, nil)
	problems = append(problems, pathProblems(fieldPath, e.Path)...)

	if !slices.Contains(MutationOperators(), e.Operator) {
		problems = append(problems, fmt.Sprintf("operator %q is not one of %s", e.Operator, strings.Join(MutationOperators(), ", ")))
	}

	if e.Column != nil && *e.Column < 1 {
		problems = append(problems, "mutation column must be a positive byte column")
	}

	for _, status := range e.Statuses {
		if !slices.Contains(acceptableMutantStatuses(), status) {
			problems = append(problems, fmt.Sprintf("status %q is not one of %s", status, strings.Join(acceptableMutantStatuses(), ", ")))
		}
	}

	if strings.TrimSpace(e.Anchor) != e.Anchor {
		problems = append(problems, "anchor must be the trimmed source line")
	}

	if e.Effect != "" {
		problems = append(problems, "effect applies only to golangci-lint entries")
	}

	return problems
}

func (e *Entry) requireKind(kinds ...string) []string {
	if slices.Contains(kinds, e.Kind) {
		return nil
	}

	return []string{fmt.Sprintf("kind %q does not fit effect %s; use one of %s", e.Kind, e.Effect, strings.Join(kinds, ", "))}
}

func (e *Entry) describe() string {
	if e.Effect != "" {
		return e.Tool + " " + e.Effect
	}

	return e.Tool
}

// pathProblems requires a single exact repository file: relative, slash-separated, free of glob and
// regular-expression syntax, and naming a Go file. Directories and patterns are what make an
// exception broader than the code it defends.
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
func regexProblems(field, value string) []string {
	if value == "" {
		return []string{field + " is required"}
	}

	return optionalRegexProblems(field, value)
}

// optionalRegexProblems checks a pattern only when it is present.
func optionalRegexProblems(field, value string) []string {
	if value == "" {
		return nil
	}

	pattern, err := syntax.Parse(value, syntax.Perl)
	if err != nil {
		return []string{fmt.Sprintf("%s is not a valid regular expression: %v", field, err)}
	}

	literals, constrained := constrainedPredicate(pattern)
	if !constrained || literals < minLiteralRunes {
		return []string{field + " is too broad: it needs at least six literal characters naming the diagnostic"}
	}

	return nil
}

// constrainedPredicate allows literal text and whitespace spacing, with optional outer anchors.
// Arbitrary alternatives, wildcards and optional literals cannot identify one diagnostic reliably.

func constrainedPredicate(pattern *syntax.Regexp) (int, bool) {
	switch pattern.Op {
	case syntax.OpLiteral:
		return len(pattern.Rune), pattern.Flags&syntax.FoldCase == 0
	case syntax.OpBeginText, syntax.OpEndText:
		return 0, true
	case syntax.OpConcat:
		return constrainedSequence(pattern.Sub)
	case syntax.OpPlus:
		return 0, whitespaceRepeat(pattern)
	case syntax.OpNoMatch, syntax.OpEmptyMatch, syntax.OpCharClass, syntax.OpAnyCharNotNL, syntax.OpAnyChar,
		syntax.OpBeginLine, syntax.OpEndLine, syntax.OpWordBoundary, syntax.OpNoWordBoundary, syntax.OpCapture,
		syntax.OpStar, syntax.OpQuest, syntax.OpRepeat, syntax.OpAlternate:
		return 0, false
	}

	return 0, false
}

func constrainedSequence(parts []*syntax.Regexp) (int, bool) {
	total := 0

	for _, part := range parts {
		count, valid := constrainedPredicate(part)
		if !valid {
			return 0, false
		}

		total += count
	}

	return total, true
}

func whitespaceRepeat(pattern *syntax.Regexp) bool {
	if len(pattern.Sub) != 1 || pattern.Sub[0].Op != syntax.OpCharClass {
		return false
	}

	for index := 0; index < len(pattern.Sub[0].Rune); index += 2 {
		for character := pattern.Sub[0].Rune[index]; character <= pattern.Sub[0].Rune[index+1]; character++ {
			if !strings.ContainsRune(" \t\n\r\f", character) {
				return false
			}
		}
	}

	return true
}
