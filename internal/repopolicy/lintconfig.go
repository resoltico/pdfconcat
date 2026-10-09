// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

type (
	// LinterInfo is what the golangci-lint binary reports about one linter.
	LinterInfo struct {
		Name       string `json:"name"`
		Deprecated bool   `json:"deprecated"`
	}

	// Issue is one diagnostic from golangci-lint's JSON report.
	Issue struct {
		Linter string
		File   string
		Text   string
		Source string
		Line   int
	}

	// linterListing is the output of `golangci-lint linters --json`.
	linterListing struct {
		Enabled  []LinterInfo `json:"enabled"`
		Disabled []LinterInfo `json:"disabled"`
	}

	// issuePosition is the position of a diagnostic in golangci-lint's JSON report.
	issuePosition struct {
		Filename string `json:"filename"`
		Line     int    `json:"line"`
	}

	// issueRecord is one issue of golangci-lint's JSON report. The tool writes FromLinter, which
	// encoding/json matches case-insensitively with the lower-case tag below.
	issueRecord struct {
		LineRange   *issueRange   `json:"linerange"`
		FromLinter  string        `json:"fromlinter"`
		Text        string        `json:"text"`
		Pos         issuePosition `json:"pos"`
		SourceLines []string      `json:"sourcelines"`
	}

	// issueRange describes displayed source lines, which can extend beyond the diagnostic position.
	issueRange struct {
		From int `json:"from"`
		To   int `json:"to"`
	}

	// issueReport is golangci-lint's JSON report.
	issueReport struct {
		Issues []issueRecord `json:"issues"`
	}
)

const (
	allChecks                  = "all"
	maxSourceLines             = 1000
	maxExportedTypes           = 20
	maxFunctionLines           = 60
	maxFunctionStatements      = 40
	maxCyclomaticComplexity    = 12
	maxPackageComplexity       = 6
	maxCognitiveComplexity     = 15
	minimumMaintainability     = 25
	nestedIfReportingThreshold = 4
	maxInterfaceMethods        = 5

	// pairWidth is the number of nodes a YAML mapping spends on one key and its value.
	pairWidth = 2
	// yamlIndent is the indentation of the rewritten configuration.
	yamlIndent = 2

	// Keys of an exclusion rule in .golangci.yml.
	keyPath    = "path"
	keyLinters = "linters"
	keyText    = "text"
	keySource  = "source"
	keyRules   = "rules"
)

var (
	// ErrLintConfig reports a problem reading a golangci-lint document.
	ErrLintConfig = errors.New("golangci-lint data")

	// suppressionKey matches setting names that disable, exclude, ignore, skip or allow something: the
	// names begin with disable, exclude, ignore or skip in any spelling (ignore-names, ignoredFunctions,
	// skipTestFuncs), or are one of the explicit allow settings.
	suppressionKey = regexp.MustCompile(
		`(?i)^(disable|exclude|ignore|skip)|^allow$|^allow-(unused|no-explanation|leading-spaces)$|-allow-list$`)
)

// requiredSettings pins the strict values of settings whose defaults are looser, so that deleting a
// line cannot silently weaken the linters. Each is a setting, not an exception.
func requiredSettings() map[string]any {
	return map[string]any{
		"linters.settings.funlen.lines":                                maxFunctionLines,
		"linters.settings.funlen.statements":                           maxFunctionStatements,
		"linters.settings.cyclop.max-complexity":                       maxCyclomaticComplexity,
		"linters.settings.cyclop.package-average":                      maxPackageComplexity,
		"linters.settings.gocognit.min-complexity":                     maxCognitiveComplexity,
		"linters.settings.gocyclo.min-complexity":                      maxCyclomaticComplexity,
		"linters.settings.maintidx.under":                              minimumMaintainability,
		"linters.settings.nestif.min-complexity":                       nestedIfReportingThreshold,
		"linters.settings.interfacebloat.max":                          maxInterfaceMethods,
		"run.issues-exit-code":                                         1,
		"issues.new":                                                   false,
		"issues.new-from-rev":                                          "",
		"issues.new-from-patch":                                        "",
		"issues.new-from-merge-base":                                   "",
		"linters.custom":                                               nil,
		"linters.settings.gosec.config":                                nil,
		"run.tests":                                                    true,
		"run.modules-download-mode":                                    "readonly",
		"linters.default":                                              allChecks,
		"linters.exclusions.generated":                                 "disable",
		"linters.exclusions.warn-unused":                               true,
		"formatters.exclusions.generated":                              "disable",
		"issues.max-issues-per-linter":                                 0,
		"issues.max-same-issues":                                       0,
		"issues.uniq-by-line":                                          false,
		"linters.settings.asasalint.use-builtin-exclusions":            false,
		"linters.settings.decorder.disable-dec-order-check":            false,
		"linters.settings.decorder.disable-init-func-first-check":      false,
		"linters.settings.decorder.disable-dec-num-check":              false,
		"linters.settings.decorder.disable-type-dec-num-check":         false,
		"linters.settings.decorder.disable-const-dec-num-check":        false,
		"linters.settings.decorder.disable-var-dec-num-check":          false,
		"linters.settings.errcheck.disable-default-exclusions":         true,
		"linters.settings.errcheck.check-type-assertions":              true,
		"linters.settings.errcheck.check-blank":                        true,
		"linters.settings.exhaustive.default-signifies-exhaustive":     false,
		"linters.settings.forbidigo.exclude-godoc-examples":            false,
		"linters.settings.gochecksumtype.default-signifies-exhaustive": false,
		"linters.settings.goconst.ignore-calls":                        false,
		"linters.settings.goconst.exclude-types":                       []any{},
		"linters.settings.revive.confidence":                           0,
		"linters.settings.gosec.severity":                              "low",
		"linters.settings.gosec.confidence":                            "low",
		"linters.settings.godoclint.default":                           allChecks,
		"linters.settings.iface.enable":                                []any{"identical", "unused", "opaque"},
		"linters.settings.exhaustive.check":                            []any{"switch", "map"},
		"linters.settings.unparam.check-exported":                      true,
		"linters.settings.gocritic.enable-all":                         true,
		"linters.settings.govet.enable-all":                            true,
		"linters.settings.nolintlint.allow-unused":                     false,
		"linters.settings.nolintlint.require-explanation":              true,
		"linters.settings.nolintlint.require-specific":                 true,
		"linters.settings.revive.enable-all-rules":                     true,
		"linters.settings.unused.field-writes-are-uses":                false,
		"linters.settings.unused.post-statements-are-reads":            false,
		"linters.settings.unused.exported-fields-are-used":             false,
		"linters.settings.unused.local-variables-are-used":             false,
		"linters.settings.unused.generated-is-used":                    false,
	}
}

// exclusionRoots are the places where .golangci.yml may carry exclusion lists.
func exclusionRoots() []string {
	return []string{"linters.exclusions", "formatters.exclusions"}
}

// LintConfigIssues compares the exceptions effective in a .golangci.yml document with the registry's
// golangci-lint entries and returns one message per missing, extra or broader item, plus violations
// of the pinned strict settings. A nil result means the file and the registry agree exactly.
func LintConfigIssues(entries []*Entry, config []byte) ([]string, error) {
	document, err := decodeLintConfig(config)
	if err != nil {
		return nil, err
	}

	expected := map[string]bool{}

	for _, entry := range entries {
		if entry.Tool != ToolLint {
			continue
		}

		for _, key := range expectedKeys(entry) {
			expected[key] = true
		}
	}

	actual, problems := effectiveExceptions(document)

	for _, key := range slices.Sorted(maps.Keys(expected)) {
		if !actual[key] {
			problems = append(problems, "registry exception missing from .golangci.yml: "+key)
		}
	}

	for _, key := range slices.Sorted(maps.Keys(actual)) {
		if !expected[key] {
			problems = append(problems, ".golangci.yml carries an exception the registry does not list: "+key)
		}
	}

	problems = append(problems, requiredSettingProblems(document)...)
	problems = append(problems, requiredFormatterProblems(document)...)

	return append(problems, lintActionProblems(document)...), nil
}

// expectedKeys renders the canonical form of each configuration item a registry entry stands for.
func expectedKeys(entry *Entry) []string {
	switch entry.Effect {
	case EffectDisableLinter:
		return []string{"linters.disable: " + entry.Linter}
	case EffectSettingItem:
		keys := make([]string, 0, len(entry.Values))
		for _, value := range entry.Values {
			keys = append(keys, entry.Setting+": "+value)
		}

		return keys
	case EffectExcludeDiagnostic:
		return []string{ruleKey(AnchoredPath(entry.Path), entry.Linter, entry.Message, entry.Source)}
	default:
		return nil
	}
}

// ExclusionRulesYAML renders the registry's diagnostic exclusions as the `rules:` list that
// .golangci.yml must carry under linters.exclusions, ready to paste.
func ExclusionRulesYAML(entries []*Entry) (string, error) {
	rules := make([]map[string]any, 0, len(entries))

	for _, entry := range entries {
		if entry.Tool != ToolLint || entry.Effect != EffectExcludeDiagnostic {
			continue
		}

		rule := map[string]any{keyPath: AnchoredPath(entry.Path), keyLinters: []string{entry.Linter}, keyText: entry.Message}
		if entry.Source != "" {
			rule[keySource] = entry.Source
		}

		rules = append(rules, rule)
	}

	var out bytes.Buffer

	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(yamlIndent)

	err := encoder.Encode(map[string]any{keyRules: rules})
	if err != nil {
		return "", fmt.Errorf("%w: encode rules: %w", ErrLintConfig, err)
	}

	return out.String(), errors.Join(encoder.Close())
}

// AnchoredPath returns the golangci-lint path pattern that matches exactly one repository file.
func AnchoredPath(file string) string {
	return "^" + regexp.QuoteMeta(file) + "$"
}

func ruleKey(pathPattern, linter, text, source string) string {
	key := fmt.Sprintf("linters.exclusions.rules: path=%s linter=%s text=%q", pathPattern, linter, text)
	if source != "" {
		key += fmt.Sprintf(" source=%q", source)
	}

	return key
}

// effectiveExceptions collects every exception present in the document and any structural problem
// (such as an exclusion rule with fields beyond path, linters, text and source).
func effectiveExceptions(document map[string]any) (map[string]bool, []string) {
	found := map[string]bool{}

	var problems []string

	collectLists(document, found)

	for index, raw := range asList(lookup(document, "linters.exclusions.rules")) {
		key, problem := ruleFromConfig(raw)
		if problem != "" {
			problems = append(problems, fmt.Sprintf("linters.exclusions.rules[%d]: %s", index, problem))

			continue
		}

		found[key] = true
	}

	for _, section := range []string{"linters.settings", "formatters.settings"} {
		problems = append(problems, collectSectionSuppressions(section, asMap(lookup(document, section)), found)...)
	}

	return found, problems
}

// collectSectionSuppressions records the suppressions of every linter or formatter in a settings section.
// depguard's allow and deny lists are its policy, checked by the depguard linter itself, not exceptions.
func collectSectionSuppressions(section string, settings map[string]any, found map[string]bool) []string {
	var problems []string

	for _, name := range slices.Sorted(maps.Keys(settings)) {
		if name != "depguard" {
			problems = append(problems, collectSuppressions(section+"."+name, settings[name], found)...)
		}
	}

	return problems
}

// collectLists records the items of the disable list and of the exclusion path and preset lists.
func collectLists(document map[string]any, found map[string]bool) {
	for _, name := range stringList(lookup(document, "linters.disable")) {
		found["linters.disable: "+name] = true
	}

	for _, root := range exclusionRoots() {
		for _, key := range []string{"paths", "paths-except", "presets"} {
			for _, item := range stringList(lookup(document, root+"."+key)) {
				found[root+"."+key+": "+item] = true
			}
		}
	}
}

// ruleFromConfig renders one exclusion rule, rejecting any field that would widen it.
func ruleFromConfig(raw any) (string, string) {
	rule, isMap := raw.(map[string]any)
	if !isMap {
		return "", "is not a mapping"
	}

	for _, key := range slices.Sorted(maps.Keys(rule)) {
		if !slices.Contains([]string{keyPath, keyLinters, keyText, keySource}, key) {
			return "", "uses field " + key + ", which the registry cannot express"
		}
	}

	linters := stringList(rule[keyLinters])
	pathPattern := asString(rule[keyPath])
	text := asString(rule[keyText])

	if pathPattern == "" || text == "" || len(linters) != 1 {
		return "", "must name one path, one linter and a text"
	}

	return ruleKey(pathPattern, linters[0], text, asString(rule[keySource])), ""
}

// collectSuppressions walks one linter's settings and records every active suppression item.
func collectSuppressions(prefix string, node any, found map[string]bool) []string {
	var problems []string

	table, isMap := node.(map[string]any)
	if !isMap {
		return nil
	}

	for _, key := range slices.Sorted(maps.Keys(table)) {
		location := prefix + "." + key

		switch {
		case key == "rules" && strings.HasSuffix(prefix, ".revive"):
			collectDisabledRules(location, table[key], found)
		case key == "checks" && strings.HasSuffix(prefix, ".staticcheck"):
			collectDisabledChecks(location, table[key], found)
		case falseSuppressionSetting(prefix, key):
			collectDisabledSetting(location, table[key], found)
		case suppressionSetting(key) || explicitSuppressionSetting(location):
			if requiredAnalysisSetting(location, key) {
				continue
			}

			problems = append(problems, collectSuppressionItems(location, table[key], found)...)

		default:
			problems = append(problems, collectSuppressions(location, table[key], found)...)
		}
	}

	return problems
}

// collectDisabledChecks records staticcheck check patterns that start with "-".
func collectDisabledChecks(location string, value any, found map[string]bool) {
	for _, check := range stringList(value) {
		if strings.HasPrefix(check, "-") {
			found[location+": "+check] = true
		}
	}
}

// collectDisabledRules records the revive rules switched off with `disabled: true`.
func collectDisabledRules(location string, value any, found map[string]bool) {
	for _, raw := range asList(value) {
		rule := asMap(raw)

		if asBool(rule["disabled"]) {
			found[location+": "+asString(rule["name"])] = true
		}
	}
}

// activeItems returns the items of a suppression value: the elements of a list, "true" for a set
// flag, or the text of a non-empty string. False flags, empty strings and empty lists suppress nothing.
func activeItems(value any) []string {
	switch typed := value.(type) {
	case bool:
		if typed {
			return []string{"true"}
		}

		return nil
	case string:
		if typed != "" {
			return []string{typed}
		}

		return nil
	case []any:
		return stringList(typed)
	default:
		return nil
	}
}

// requiredSettingProblems checks the pinned strict settings.
func requiredSettingProblems(document map[string]any) []string {
	var problems []string

	required := requiredSettings()

	for _, setting := range slices.Sorted(maps.Keys(required)) {
		got := lookup(document, setting)
		if !reflect.DeepEqual(got, required[setting]) {
			problems = append(problems, fmt.Sprintf("strict setting %s is %v, want %v", setting, got, required[setting]))
		}
	}

	if !slices.Contains(stringList(lookup(document, "linters.settings.staticcheck.checks")), allChecks) {
		problems = append(problems, `strict setting linters.settings.staticcheck.checks must include "all"`)
	}

	return append(problems, requiredReviveRuleProblems(document)...)
}

// lookup returns the value at a dotted path, or nil.
func lookup(document map[string]any, dotted string) any {
	var current any = document

	for key := range strings.SplitSeq(dotted, ".") {
		table, isMap := current.(map[string]any)
		if !isMap {
			return nil
		}

		current = table[key]
	}

	return current
}

// asMap returns value as a mapping, or nil when it is anything else.
func asMap(value any) map[string]any {
	typed, matches := value.(map[string]any)
	if !matches {
		return nil
	}

	return typed
}

// asList returns value as a list, or nil when it is anything else.
func asList(value any) []any {
	typed, matches := value.([]any)
	if !matches {
		return nil
	}

	return typed
}

// asString returns value as a string, or "" when it is anything else.
func asString(value any) string {
	typed, matches := value.(string)
	if !matches {
		return ""
	}

	return typed
}

// asBool returns value as a bool, or false when it is anything else.
func asBool(value any) bool {
	typed, matches := value.(bool)

	return matches && typed
}

// stringList returns the scalar items of a YAML list as strings.
func stringList(value any) []string {
	list := asList(value)
	items := make([]string, 0, len(list))

	for _, item := range list {
		switch typed := item.(type) {
		case string:
			items = append(items, typed)
		case int, bool, float64:
			items = append(items, fmt.Sprint(typed))
		default:
		}
	}

	return items
}

// ParseLinterInfo reads the output of `golangci-lint linters --json`.
func ParseLinterInfo(data []byte) (map[string]LinterInfo, error) {
	var listing linterListing

	err := json.Unmarshal(data, &listing)
	if err != nil {
		return nil, fmt.Errorf("%w: decode linter list: %w", ErrLintConfig, err)
	}

	known := map[string]LinterInfo{}

	for _, info := range slices.Concat(listing.Enabled, listing.Disabled) {
		known[info.Name] = info
	}

	if len(known) == 0 {
		return nil, fmt.Errorf("%w: the linter list is empty", ErrLintConfig)
	}

	return known, nil
}

// PremiseIssues checks the premise each lint exception stands on, against the linters the binary
// knows: that a deprecated-rule linter is still deprecated, that every named linter exists, and that
// the linters an incompatible or duplicate rule yields to are themselves still enabled.
func PremiseIssues(entries []*Entry, known map[string]LinterInfo) []string {
	disabled := map[string]bool{}

	for _, entry := range entries {
		if entry.Tool == ToolLint && entry.Effect == EffectDisableLinter {
			disabled[entry.Linter] = true
		}
	}

	var problems []string

	for _, entry := range entries {
		if entry.Tool == ToolLint {
			problems = append(problems, entryPremiseIssues(entry, known, disabled)...)
		}
	}

	return problems
}

func entryPremiseIssues(entry *Entry, known map[string]LinterInfo, disabled map[string]bool) []string {
	var problems []string

	if entry.Linter != "" {
		info, exists := known[entry.Linter]

		switch {
		case !exists:
			problems = append(problems, fmt.Sprintf("%s: golangci-lint has no linter %q", entry.ID, entry.Linter))
		case entry.Kind == KindDeprecatedRule && !info.Deprecated:
			problems = append(problems, fmt.Sprintf("%s: linter %q is no longer deprecated; delete the exception", entry.ID, entry.Linter))
		default:
		}
	}

	for _, other := range slices.Concat(entry.ConflictsWith, entry.SupersededBy) {
		if _, exists := known[other]; !exists {
			problems = append(problems, fmt.Sprintf("%s: golangci-lint has no linter %q", entry.ID, other))
		} else if disabled[other] {
			problems = append(problems, fmt.Sprintf("%s: linter %q is disabled, so the exception no longer rests on it", entry.ID, other))
		}
	}

	return problems
}

// ParseIssues reads golangci-lint JSON output, reporting file names relative to root with forward slashes.
func ParseIssues(data []byte, root string) ([]Issue, error) {
	var parsed issueReport

	err := json.Unmarshal(data, &parsed)
	if err != nil {
		return nil, fmt.Errorf("%w: decode report: %w", ErrLintConfig, err)
	}

	issues := make([]Issue, 0, len(parsed.Issues))

	for _, record := range parsed.Issues {
		source, sourceErr := diagnosticSourceLine(&record)
		if sourceErr != nil {
			return nil, sourceErr
		}

		file := strings.ReplaceAll(record.Pos.Filename, `\`, "/")
		file = strings.TrimPrefix(file, strings.ReplaceAll(root, `\`, "/")+"/")

		issues = append(
			issues,
			Issue{
				Linter: record.FromLinter,
				File:   path.Clean(file),
				Text:   record.Text,
				Source: source,
				Line:   record.Pos.Line,
			},
		)
	}

	return issues, nil
}

// diagnosticSourceLine matches golangci-lint's source exclusion: the physical position's line,
// rather than every line displayed for a multi-line diagnostic.
func diagnosticSourceLine(record *issueRecord) (string, error) {
	if len(record.SourceLines) == 0 {
		return "", nil
	}

	if record.Pos.Line <= 0 {
		return "", fmt.Errorf("%w: diagnostic source has no physical position", ErrLintConfig)
	}

	index := 0
	if record.LineRange != nil && record.LineRange.From != 0 {
		index = record.Pos.Line - record.LineRange.From
		if record.LineRange.From < 0 || index < 0 || record.Pos.Line > record.LineRange.To ||
			len(record.SourceLines)-1 != record.LineRange.To-record.LineRange.From {
			return "", fmt.Errorf("%w: diagnostic source range does not contain its physical position", ErrLintConfig)
		}
	} else if len(record.SourceLines) != 1 || record.LineRange != nil && record.LineRange.To != 0 {
		return "", fmt.Errorf("%w: diagnostic source without a range must contain only its physical line", ErrLintConfig)
	}

	return record.SourceLines[index], nil
}

// StaleDiagnosticEntries returns the diagnostic exclusions that match no issue of an unexcluded
// lint run on hostGOOS. An entry restricted to another operating system is not judged here.
func StaleDiagnosticEntries(entries []*Entry, issues []Issue, hostGOOS string) []*Entry {
	var stale []*Entry

	for _, entry := range entries {
		if entry.Tool != ToolLint || entry.Effect != EffectExcludeDiagnostic {
			continue
		}

		if entry.GOOS != "" && entry.GOOS != hostGOOS {
			continue
		}

		if !matchesAnyIssue(entry, issues) {
			stale = append(stale, entry)
		}
	}

	return stale
}

func matchesAnyIssue(entry *Entry, issues []Issue) bool {
	message := regexp.MustCompile(entry.Message)

	var source *regexp.Regexp
	if entry.Source != "" {
		source = regexp.MustCompile(entry.Source)
	}

	for _, issue := range issues {
		matches := issue.Linter == entry.Linter && issue.File == entry.Path && message.MatchString(issue.Text)
		if matches && (source == nil || source.MatchString(issue.Source)) {
			return true
		}
	}

	return false
}

// ConfigWithoutRules returns the configuration with its diagnostic exclusion rules removed, for the
// run that checks the rules still match something.
func ConfigWithoutRules(config []byte) ([]byte, error) {
	var document yaml.Node

	decoder := yaml.NewDecoder(bytes.NewReader(config))

	err := decoder.Decode(&document)
	if err != nil {
		return nil, fmt.Errorf("%w: decode configuration: %w", ErrLintConfig, err)
	}

	var trailing any
	if endErr := decoder.Decode(&trailing); endErr == nil {
		return nil, fmt.Errorf(yamlSingleDocumentError, ErrLintConfig)
	} else if !errors.Is(endErr, io.EOF) {
		return nil, fmt.Errorf(yamlEOFFailedError, ErrLintConfig, endErr)
	}

	removeRules(&document)

	var out bytes.Buffer

	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(yamlIndent)

	err = encoder.Encode(&document)
	if err != nil {
		return nil, fmt.Errorf("%w: encode configuration: %w", ErrLintConfig, err)
	}

	err = encoder.Close()
	if err != nil {
		return nil, fmt.Errorf("%w: encode configuration: %w", ErrLintConfig, err)
	}

	return out.Bytes(), nil
}

// removeRules empties linters.exclusions.rules wherever it appears in the node tree.
func removeRules(node *yaml.Node) {
	for index, child := range node.Content {
		if node.Kind == yaml.MappingNode && index%pairWidth == 0 && child.Value == keyRules && index+1 < len(node.Content) {
			value := node.Content[index+1]
			if value.Kind == yaml.SequenceNode && looksLikeExclusionRules(value) {
				value.Content = nil
			}
		}

		removeRules(child)
	}
}

// looksLikeExclusionRules distinguishes exclusion rules (mappings with a path or linters) from other lists
// named rules, such as revive's.
func looksLikeExclusionRules(sequence *yaml.Node) bool {
	for _, item := range sequence.Content {
		for index := 0; index+1 < len(item.Content); index += pairWidth {
			if item.Content[index].Value == keyPath || item.Content[index].Value == keyLinters {
				return true
			}
		}
	}

	return len(sequence.Content) == 0
}

// requiredReviveRuleProblems protects the retained configuration thresholds of the two native
// algorithms replaced by mandatory owned-source limits. The disable copies remain registry entries.
func requiredReviveRuleProblems(document map[string]any) []string {
	required := map[string][]any{
		"file-length-limit":  {map[string]any{"max": maxSourceLines, "skipComments": true, "skipBlankLines": true}},
		"max-public-structs": {maxExportedTypes},
	}
	rules := map[string]map[string]any{}

	for _, raw := range asList(lookup(document, "linters.settings.revive.rules")) {
		rule := asMap(raw)
		rules[asString(rule["name"])] = rule
	}

	var problems []string

	for name, arguments := range required {
		rule := rules[name]
		if rule == nil || !asBool(rule["disabled"]) || !reflect.DeepEqual(rule["arguments"], arguments) {
			problems = append(
				problems,
				fmt.Sprintf("strict revive rule %s must remain replaced by the owned-source scan with arguments %v", name, arguments),
			)
		}
	}

	slices.Sort(problems)

	return problems
}

// selectiveSetting recognizes rule filters that do not use an ignore/disable spelling.
func selectiveSetting(key string) bool {
	return slices.Contains([]string{"include", "includes", "enable", "enabled-checks", "enabled-tags", "checks", "check", "default"}, key)
}

func requiredAnalysisSetting(location, key string) bool {
	_, required := requiredSettings()[location]
	return required && slices.Contains([]string{"enable", "check", "default", "disable-default-exclusions"}, key)
}

func suppressionSetting(key string) bool {
	return suppressionKey.MatchString(key) || selectiveSetting(key) || key == "parameters-are-used" ||
		slices.Contains([]string{"allowed-errors", "allowed-errors-wildcard"}, key)
}

func collectDisabledSetting(location string, value any, found map[string]bool) {
	if setting, isBool := value.(bool); isBool && !setting {
		found[location+": false"] = true
	}
}

// falseSuppressionSetting covers reviewed switches whose false value disables an enabled check.
// Errorlint's four checks default to true in the pinned upstream; optional default-false checks
// are not inferred to be exceptions merely because their setting has an analysis-oriented name.
func falseSuppressionSetting(prefix, key string) bool {
	switch prefix {
	case "linters.settings.whitespace":
		return slices.Contains([]string{"multi-if", "multi-func"}, key)
	case "linters.settings.errorlint":
		return slices.Contains([]string{"errorf", "errorf-multi", "asserts", "comparison"}, key)
	default:
		return false
	}
}

func decodeLintConfig(config []byte) (map[string]any, error) {
	var document map[string]any

	decoder := yaml.NewDecoder(bytes.NewReader(config))

	err := decoder.Decode(&document)
	if err != nil {
		return nil, fmt.Errorf("%w: decode configuration: %w", ErrLintConfig, err)
	}

	var trailing any
	if endErr := decoder.Decode(&trailing); endErr == nil {
		return nil, fmt.Errorf(yamlSingleDocumentError, ErrLintConfig)
	} else if !errors.Is(endErr, io.EOF) {
		return nil, fmt.Errorf(yamlEOFFailedError, ErrLintConfig, endErr)
	}

	return document, nil
}

// invalidSuppressionShape rejects values that the registry's scalar setting items cannot express.
func invalidSuppressionShape(value any) bool {
	switch typed := value.(type) {
	case nil, string, bool, int, float64:
		return false
	case []any:
		for _, item := range typed {
			switch item.(type) {
			case string, bool, int, float64:
			default:
				return true
			}
		}

		return false
	default:
		return true
	}
}

func collectSuppressionItems(location string, value any, found map[string]bool) []string {
	if invalidSuppressionShape(value) {
		return []string{location + ": active structured suppression cannot be represented by registry scalar values"}
	}

	for _, item := range activeItems(value) {
		found[location+": "+item] = true
	}

	return nil
}

// requiredFormatterProblems protects the complete formatter contract independently of YAML order.
// The pinned native formatter schedules its own fixed order, so reordering this list is harmless.
func requiredFormatterProblems(document map[string]any) []string {
	want := []string{"gci", "gofmt", "gofumpt", "goimports", "golines"}
	got := stringList(lookup(document, "formatters.enable"))
	slices.Sort(got)

	if !slices.Equal(got, want) {
		return []string{fmt.Sprintf("formatters.enable must contain exactly %v once each; got %v", want, got)}
	}

	return nil
}

// lintActionProblems rejects actions that change source instead of judging the configured inputs.
func lintActionProblems(document map[string]any) []string {
	var problems []string

	if value := lookup(document, "issues.fix"); value != nil {
		fix, valid := value.(bool)
		if !valid || fix {
			problems = append(problems, "issues.fix must be absent or false; lint gates must not repair their inputs")
		}
	}

	if value := lookup(document, "formatters.settings.gofmt.rewrite-rules"); value != nil {
		rules, valid := value.([]any)
		if !valid || len(rules) != 0 {
			problems = append(problems,
				"formatters.settings.gofmt.rewrite-rules must be absent or empty; semantic rewrites are unsupported")
		}
	}

	return problems
}

// explicitSuppressionSetting names the reviewed native exemptions without an ignore/disable spelling.
// Import policies and positive restriction lists are deliberately not inferred from their names.
func explicitSuppressionSetting(location string) bool {
	return slices.Contains([]string{
		"linters.settings.gosmopolitan.escape-hatches",
		"linters.settings.gosmopolitan.allow-time-local",
		"linters.settings.staticcheck.dot-import-whitelist",
		"linters.settings.staticcheck.http-status-code-whitelist",
		"linters.settings.unqueryvet.allowed-patterns",
	}, location)
}
