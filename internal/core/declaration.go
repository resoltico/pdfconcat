// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/core/diag"
)

// Declaration is the complete authored packet, before native path resolution.
// Values and presence are independent of provenance. Admission owns a deep copy.
type Declaration struct {
	Metadata     *Metadata
	Timestamp    *int64
	Structure    StructurePolicy
	Output, Dir  string
	FitTo        *Size
	PageNumbers  *PageNumbers
	PageDefaults PageStyle
	Items        []Item
}

func copyValue[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
func copyStyle(s PageStyle) PageStyle { s.Background.Value = copyValue(s.Background.Value); return s }
func copyMetadata(m *Metadata) *Metadata {
	if m == nil {
		return nil
	}
	return &Metadata{Title: copyValue(m.Title), Author: copyValue(m.Author), Subject: copyValue(m.Subject), Language: copyValue(m.Language)}
}
func finite(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }
func validSize(s Size, inherit bool) bool {
	return inherit && s == (Size{}) || finite(s.Width) && finite(s.Height) && s.Width >= 1 && s.Width <= MaxSide && s.Height >= 1 && s.Height <= MaxSide
}
func validTitle(s string) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxAuthoredFieldRunes || strings.TrimSpace(s) == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}
func validateStyle(s PageStyle) error {
	if !utf8.ValidString(s.Text.Font.Value.Path) || strings.ContainsRune(s.Text.Font.Value.Path, 0) {
		return errors.New("font path must be valid UTF-8 without NUL")
	}
	t := s.Text
	if !validSize(s.Size.Value, true) {
		return errors.New("page size must be inherit or 1–14400 points per side")
	}
	if !utf8.ValidString(t.Value.Value) || strings.ContainsRune(t.Value.Value, 0) || utf8.RuneCountInString(t.Value.Value) > MaxAuthoredFieldRunes {
		return errors.New("text must be valid UTF-8 without NUL, at most 10000 characters")
	}
	for _, v := range []float64{t.Size.Value, t.X.Value, t.Y.Value, t.Width.Value, t.Leading.Value} {
		if !finite(v) || math.Abs(v) > MaxSide {
			return errors.New("text geometry must be finite and within the point limits")
		}
	}
	if t.Size.Value < 1 || t.Leading.Value < 1 || t.Leading.Value > 10 || t.Width.Present && t.Width.Value < 1 {
		return errors.New("text size and width must be at least 1 point; leading must be 1–10")
	}
	if !slices.Contains(t.Anchor.Value.Values(), string(t.Anchor.Value)) || !slices.Contains(t.Align.Value.Values(), string(t.Align.Value)) || !slices.Contains(t.Wrap.Value.Values(), string(t.Wrap.Value)) || !slices.Contains(t.Overflow.Value.Values(), string(t.Overflow.Value)) {
		return errors.New("unknown text style value")
	}
	return nil
}

// ValidateDeclaration validates semantic rules before traversal and clones every
// caller-owned mutable container. A shared subtree is copied with node accounting;
// an active cycle is refused before recursion. Missing identities are deterministic.
func ValidateDeclaration(d Declaration) (Declaration, []diag.Diagnostic) {
	fail := func(code diag.Code, message, ref string) (Declaration, []diag.Diagnostic) {
		return Declaration{}, []diag.Diagnostic{diag.New(code, message, ref)}
	}
	if d.Metadata != nil {
		if err := ValidateMetadata(*d.Metadata); err != nil {
			return fail(diag.PlanValue, err.Error(), "metadata")
		}
	}
	for _, path := range []string{d.Output, d.Dir} {
		if !utf8.ValidString(path) || strings.ContainsRune(path, 0) {
			return fail(diag.PathInvalid, "Paths must be valid UTF-8 without NUL.", "paths")
		}
	}
	d.Metadata = copyMetadata(d.Metadata)
	d.Timestamp = copyValue(d.Timestamp)
	d.FitTo = copyValue(d.FitTo)
	if d.Timestamp != nil && (*d.Timestamp < 0 || *d.Timestamp > MaxTimestamp) {
		return fail(diag.PlanValue, "Timestamp is outside the supported UTC range.", "timestamp")
	}
	if d.Structure == "" {
		d.Structure = StructurePreserve
	}
	if d.Structure != StructurePreserve && d.Structure != StructureDiscard {
		return fail(diag.PlanValue, "Structure must be preserve or discard.", "structure")
	}
	if d.FitTo != nil && !validSize(*d.FitTo, false) {
		return fail(diag.PlanValue, "Fitting requires finite page sides from 1 through 14400 points.", "fit_to")
	}
	d.PageDefaults = copyStyle(d.PageDefaults.Over(Defaults()))
	if err := validateStyle(d.PageDefaults); err != nil {
		return fail(diag.PlanValue, err.Error(), "page_defaults")
	}
	if d.PageNumbers != nil {
		n := *d.PageNumbers
		n.Kinds = slices.Clone(n.Kinds)

		if n.Mode != "physical" && n.Mode != "consecutive" || n.Start < 1 || n.Start > MaxPageIndex || !finite(n.Size) || n.Size < 1 || n.Size > MaxSide || !finite(n.Margin) || n.Margin < 0 || n.Margin > MaxSide {
			return fail(diag.PlanValue, "Invalid page-number declaration.", "page_numbers")
		}
		switch n.Position {
		case "top-left", "top-center", "top-right", "bottom-left", "bottom-center", "bottom-right":
		default:
			return fail(diag.PlanValue, "Unknown page-number position.", "page_numbers")
		}
		kinds := map[PartKind]bool{}
		for _, kind := range n.Kinds {
			if kind != PartSource && kind != PartPage && kind != PartDivider && kind != PartContents || kinds[kind] {
				return fail(diag.PlanValue, "Invalid or repeated page-number kind.", "page_numbers")
			}
			kinds[kind] = true
		}
		d.PageNumbers = &n
	}
	nodes, generated, pairs, contents, sections := 0, 0, 0, 0, 0
	active := map[Item]bool{}
	refs := map[string]bool{}
	var problem []diag.Diagnostic
	refuse := func(code diag.Code, text, ref string) {
		if problem == nil {
			problem = []diag.Diagnostic{diag.New(code, text, ref)}
		}
	}
	textBytes := 0
	chargeText := func(text, ref string) {
		if len(text) > MaxAuthoredTextBytes-textBytes {
			refuse(diag.PlanLimit, "Authored text exceeds 64 MiB.", ref)
			return
		}
		textBytes += len(text)
	}
	chargeText(d.PageDefaults.Text.Value.Value, "page_defaults")
	if d.Metadata != nil {
		for _, value := range []*string{d.Metadata.Title, d.Metadata.Author, d.Metadata.Subject, d.Metadata.Language} {
			if value != nil {
				chargeText(*value, "metadata")
			}
		}
	}
	var walk func([]Item, int, string) []Item
	walk = func(items []Item, depth int, parent string) []Item {
		if depth > MaxNesting {
			refuse(diag.PlanLimit, "Group nesting exceeds 64.", parent)
			return nil
		}
		if len(items) == 0 {
			refuse(diag.PlanValue, "Items must not be empty.", parent)
			return nil
		}
		result := make([]Item, 0, len(items))
		for index, item := range items {
			if problem != nil {
				break
			}
			nodes++
			if nodes > MaxEntries {
				refuse(diag.PlanLimit, "Plan exceeds 100000 authored items.", parent)
				break
			}
			ref := fmt.Sprintf("%s/%d", parent, index)
			if item == nil {
				refuse(diag.PlanItemShape, "An item must be a source, page, contents or group.", ref)
				break
			}
			switch v := item.(type) {
			case *SourceItem:
				if v == nil {
					refuse(diag.PlanItemShape, "Source item is nil.", ref)
					continue
				}
			case *PageItem:
				if v == nil {
					refuse(diag.PlanItemShape, "Page item is nil.", ref)
					continue
				}
			case *ContentsItem:
				if v == nil {
					refuse(diag.PlanItemShape, "Contents item is nil.", ref)
					continue
				}
			case *GroupItem:
				if v == nil {
					refuse(diag.PlanItemShape, "Group item is nil.", ref)
					continue
				}
			default:
				refuse(diag.PlanItemShape, "Unknown item variant.", ref)
				continue
			}
			if active[item] {
				refuse(diag.PlanValue, "Item graph contains a cycle.", ref)
				break
			}
			if supplied := item.itemRef(); supplied != "" {
				ref = supplied
			}
			if !utf8.ValidString(ref) || strings.ContainsRune(ref, 0) || refs[ref] {
				refuse(diag.PlanValue, "Item identities must be nonempty, valid and unique.", ref)
				break
			}
			refs[ref] = true
			active[item] = true
			switch v := item.(type) {
			case *SourceItem:
				c := *v
				c.Ref = ref
				c.Numbering = copyValue(v.Numbering)
				c.Selection = slices.Clone(v.Selection)
				if c.Selection != nil && len(c.Selection) == 0 {
					refuse(diag.PlanValue, "Source pages selection must not be empty.", ref)
				}
				if c.Path == "" || !utf8.ValidString(c.Path) || strings.ContainsRune(c.Path, 0) {
					refuse(diag.PathInvalid, "Source path must be valid and nonempty.", ref)
				}
				previous := 0
				for _, r := range c.Selection {
					pairs++
					if r.First < 1 || r.Last < r.First || r.First <= previous {
						refuse(diag.PlanValue, "Source ranges must be positive, ascending and disjoint.", ref)
					}
					previous = r.Last
				}
				normalized := make([]PageRange, 0, len(c.Selection))
				for _, span := range c.Selection {
					if len(normalized) > 0 && normalized[len(normalized)-1].Last < math.MaxInt && span.First == normalized[len(normalized)-1].Last+1 {
						normalized[len(normalized)-1].Last = span.Last
					} else {
						normalized = append(normalized, span)
					}
				}
				if c.Selection != nil {
					c.Selection = normalized
				}
				if pairs > MaxSelectionPairs {
					refuse(diag.PlanLimit, "Plan exceeds 100000 source selection pairs.", ref)
				}
				result = append(result, &c)
			case *PageItem:
				c := *v
				c.Ref = ref
				c.Numbering = copyValue(v.Numbering)
				c.Style = copyStyle(v.Style)
				if c.Style.Text.Value.Present {
					chargeText(c.Style.Text.Value.Value, ref)
				}
				if c.Count > MaxGeneratedPages {
					refuse(diag.PlanLimit, "Generated count exceeds 100000.", ref)
					break
				}
				generated += c.Count
				if c.Count < 1 {
					refuse(diag.PlanValue, "Generated page count must be positive.", ref)
				}
				if err := validateStyle(c.Style.Over(d.PageDefaults)); err != nil {
					refuse(diag.PlanValue, err.Error(), ref)
				}
				result = append(result, &c)
			case *ContentsItem:
				c := *v
				c.Ref = ref
				c.Numbering = copyValue(v.Numbering)
				contents++
				chargeText(c.Title, ref)
				if c.Title == "" && !c.TitlePresent {
					c.Title = "Contents"
				}
				if !validTitle(c.Title) || !validSize(c.Size, true) || contents > 1 {
					refuse(diag.PlanValue, "Contents requires a valid title and size, and may occur only once.", ref)
				}
				result = append(result, &c)
			case *GroupItem:
				c := *v
				c.Ref = ref
				c.Numbering = copyValue(v.Numbering)
				if !utf8.ValidString(c.Dir) || strings.ContainsRune(c.Dir, 0) {
					refuse(diag.PathInvalid, "Directory must be valid UTF-8 without NUL.", ref)
				}
				if c.StartOn != "" && c.StartOn != "any" && c.StartOn != "odd" {
					refuse(diag.PlanValue, "Group start must be any or odd.", ref)
				}
				if c.Title != "" || c.TitlePresent {
					chargeText(c.Title, ref)
					sections++
					if !validTitle(c.Title) {
						refuse(diag.PlanValue, "Section title must be visible single-line text.", ref)
					}
				}
				if c.Divider != nil {
					if c.Divider.Text.Value.Present {
						refuse(diag.PlanValue, "Divider text is supplied by its section title.", ref)
					}
					style := copyStyle(*c.Divider)
					c.Divider = &style
					generated++
					if c.Title == "" && !c.TitlePresent {
						refuse(diag.PlanValue, "Divider requires a titled section.", ref)
					}
					if err := validateStyle(style.Over(d.PageDefaults)); err != nil {
						refuse(diag.PlanValue, err.Error(), ref)
					}
				}
				c.Items = walk(v.Items, depth+1, ref)
				result = append(result, &c)
			}
			delete(active, item)
			if generated > MaxGeneratedPages || generated < 0 {
				refuse(diag.PlanLimit, "Plan exceeds 100000 generated pages.", ref)
			}
		}
		return result
	}
	d.Items = walk(d.Items, 0, "item")
	if contents > 0 && sections == 0 {
		refuse(diag.PlanValue, "Contents requires at least one titled section.", "items")
	}
	if problem != nil {
		return Declaration{}, problem
	}
	return d, nil
}

// Plan constructs traversal only from a declaration admitted by the semantic boundary.
func (d Declaration) Plan() Plan {
	items := cloneItems(d.Items)
	var apply func([]Item)
	apply = func(items []Item) {
		for _, item := range items {
			switch v := item.(type) {
			case *PageItem:
				v.Style = copyStyle(v.Style.Over(d.PageDefaults))
			case *GroupItem:
				if v.Divider != nil {
					s := copyStyle(v.Divider.Over(d.PageDefaults))
					v.Divider = &s
				}
				apply(v.Items)
			case *SourceItem, *ContentsItem:
			}
		}
	}
	apply(items)
	p := NewPlan(d.Output, d.FitTo, d.PageNumbers, d.PageDefaults, items)
	p.metadata = copyMetadata(d.Metadata)
	p.timestamp = copyValue(d.Timestamp)
	p.structure = d.Structure
	return p
}

func cloneItems(items []Item) []Item {
	result := make([]Item, len(items))
	for index, item := range items {
		switch v := item.(type) {
		case *SourceItem:
			c := *v
			c.Numbering = copyValue(v.Numbering)
			c.Selection = slices.Clone(v.Selection)
			result[index] = &c
		case *PageItem:
			c := *v
			c.Numbering = copyValue(v.Numbering)
			c.Style = copyStyle(v.Style)
			result[index] = &c
		case *ContentsItem:
			c := *v
			c.Numbering = copyValue(v.Numbering)
			result[index] = &c
		case *GroupItem:
			c := *v
			c.Numbering = copyValue(v.Numbering)
			if v.Divider != nil {
				s := copyStyle(*v.Divider)
				c.Divider = &s
			}
			c.Items = cloneItems(v.Items)
			result[index] = &c
		}
	}
	return result
}
