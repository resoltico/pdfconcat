// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Package assembly defines PDFConcat's domain values and the neutral job tree: the ordered PDFs, blank
// pages, and directory groups that both the plan decoder and the command-line shortcut compile to.
package assembly

import (
	"errors"
	"fmt"
)

type (
	// ItemKind identifies a job item.
	ItemKind uint8

	// BlankItem is the payload of an ItemBlank.
	BlankItem struct {
		// Style layers over the job defaults.
		Style BlankStyle
		// CountOrigin locates the "count" value, or the item when the count is implicit.
		CountOrigin Origin
		// Count is the number of consecutive pages, 1 to MaxBlankCount (see CheckBlankCount).
		Count int64
	}

	// Item is one entry of the ordered sequence.
	Item struct {
		// Items are the members of an ItemGroup; never empty.
		Items []Item
		// Path is the declared path of an ItemPDF, relative to its group's base unless absolute.
		Path string
		// Blank is set for an ItemBlank.
		Blank *BlankItem
		// Dir is the declared directory of an ItemGroup, relative to the enclosing base unless absolute.
		Dir    Field[string]
		Origin Origin
		Kind   ItemKind
	}

	// Job is the complete, unresolved description of an assembly. It is a bounded tree built once; flattening
	// it into contributions is a separate step that keeps every Origin.
	Job struct {
		// Source explains every Origin in the tree.
		Source Source
		// Items is the ordered sequence; never empty.
		Items []Item
		// Base is the initial base directory: the plan's directory, the --base-dir value, or the working
		// directory. Plan output and the default font resolve against it.
		Base string
		// Output is the declared output path; unset when the job names none. It resolves against Base.
		Output Field[string]
		// Dir is the declared directory for the top-level items, relative to Base.
		Dir Field[string]
		// Defaults is the style every blank layers over. Its font resolves against Base.
		Defaults BlankStyle
		// Origin is the root.
		Origin Origin
	}

	// jobWalk accumulates the bounded quantities of a walk over a job.
	jobWalk struct {
		generated     int64
		nodes         int
		contributions int
	}
)

const (
	// MaxNodes bounds the structural nodes of a job: items (PDF paths, blanks, groups) and the blank, text,
	// and font objects that style them.
	MaxNodes = 250_000
	// MaxGroupDepth bounds the nesting of directory groups.
	MaxGroupDepth = 64
	// MaxContributions bounds the PDF and blank items after groups are flattened.
	MaxContributions = 100_000
	// MaxBlankCount bounds the pages of one blank item, so that a typo such as an extra digit cannot
	// ask for billions of pages.
	MaxBlankCount = 1_000_000
	// MaxGeneratedPages bounds the sum of all blank counts.
	MaxGeneratedPages = 1_000_000

	// ItemPDF is a source PDF.
	ItemPDF ItemKind = 1
	// ItemBlank is Count consecutive generated pages sharing one style.
	ItemBlank ItemKind = 2
	// ItemGroup is a directory prefix for nested items; it has no other effect on the sequence.
	ItemGroup ItemKind = 3
)

// String returns the kind's name.
func (k ItemKind) String() string {
	switch k {
	case ItemPDF:
		return "pdf"
	case ItemBlank:
		return "blank"
	case ItemGroup:
		return "group"
	default:
		return fmt.Sprintf("kind(%d)", uint8(k))
	}
}

// ItemsBase is the directory the top-level items resolve against.
func (j *Job) ItemsBase() string {
	return JoinDir(j.Base, j.Dir.Value)
}

// CheckBlankCount checks that count is between 1 and MaxBlankCount.
func CheckBlankCount(count int64) error {
	if count < 1 || count > MaxBlankCount {
		return fmt.Errorf("%w: blank count %d is out of range; want 1 to %d", ErrOutOfRange, count, MaxBlankCount)
	}

	return nil
}

// AddCounts returns a+b, or an error when either is negative or the sum exceeds limit. It is the
// checked addition for page totals, so a sum can neither wrap nor pass its declared bound.
func AddCounts(a, b, limit int64) (int64, error) {
	if a < 0 || b < 0 || a > limit || b > limit-a {
		return 0, fmt.Errorf("%w: count %d + %d exceeds the limit of %d", ErrOutOfRange, a, b, limit)
	}

	return a + b, nil
}

// styleNodes is the number of structural plan nodes a blank style occupies besides its owner: the blank
// object, plus the text object and a file font's object when they are set. A plan reader sees one thing a
// job cannot: an explicitly empty text object, which it also counts. A job's count is therefore never
// larger than the reader's, and both bound the same quantity.
func styleNodes(style *BlankStyle) int {
	count := 1 // the blank object

	if style.Text != (TextStyle{}) {
		count++ // the text object

		if style.Text.Font.IsSet() && !style.Text.Font.Value.IsDefault() {
			count++ // the font object
		}
	}

	return count
}

// Validate checks every structural bound and invariant of a job: non-empty sequences, valid kinds and
// counts, group depth, node and contribution limits, and the generated page total. It is the check for
// jobs built without the plan decoder, and the oracle for decoded ones.
func (j *Job) Validate() error {
	if j.Source == nil {
		return fmt.Errorf("%w: job has no source", ErrInvalidJob)
	}

	var walk jobWalk

	if !j.Defaults.IsZero() {
		walk.nodes += styleNodes(&j.Defaults)
	}

	err := walk.items(j.Items, 0)
	if err != nil {
		return err
	}

	if walk.contributions > MaxContributions {
		return fmt.Errorf(
			"%w: %d PDF and blank entries after flattening groups; the limit is %d",
			ErrInvalidJob,
			walk.contributions,
			MaxContributions,
		)
	}

	if walk.nodes > MaxNodes {
		return fmt.Errorf("%w: %d structural nodes; the limit is %d", ErrInvalidJob, walk.nodes, MaxNodes)
	}

	return nil
}

func (w *jobWalk) items(items []Item, depth int) error {
	if len(items) == 0 {
		return fmt.Errorf("%w: a sequence needs at least one item", ErrInvalidJob)
	}

	if depth > MaxGroupDepth {
		return fmt.Errorf("%w: directory groups nest more than %d levels deep", ErrInvalidJob, MaxGroupDepth)
	}

	for index := range items {
		err := w.item(&items[index], depth)
		if err != nil {
			return fmt.Errorf("item %d: %w", index, err)
		}
	}

	return nil
}

func (w *jobWalk) item(item *Item, depth int) error {
	w.nodes++

	switch item.Kind {
	case ItemPDF:
		w.contributions++

		if item.Path == "" {
			return fmt.Errorf("%w: a PDF path must not be empty", ErrInvalidJob)
		}

		return CheckPathText(item.Path)
	case ItemBlank:
		return w.blank(item)
	case ItemGroup:
		err := CheckPathText(item.Dir.Value)
		if err != nil {
			return err
		}

		return w.items(item.Items, depth+1)
	default:
		return fmt.Errorf("%w: unknown item kind %v", ErrInvalidJob, item.Kind)
	}
}

func (w *jobWalk) blank(item *Item) error {
	w.contributions++

	if item.Blank == nil {
		return fmt.Errorf("%w: a blank item needs its blank payload", ErrInvalidJob)
	}

	err := CheckBlankCount(item.Blank.Count)
	if err != nil {
		return err
	}

	w.generated, err = AddCounts(w.generated, item.Blank.Count, MaxGeneratedPages)
	if err != nil {
		return err
	}

	w.nodes += styleNodes(&item.Blank.Style)

	return validateStyleValues(&item.Blank.Style)
}

// validateStyleValues checks the values a style holds, whatever way it was built.
func validateStyleValues(style *BlankStyle) error {
	var problems []error

	if style.Size.IsSet() {
		problems = append(problems, style.Size.Value.Validate())
	}

	text := &style.Text

	for _, length := range []Field[Length]{text.Size, text.X, text.Y, text.Width} {
		if length.IsSet() {
			problems = append(problems, length.Value.Validate())
		}
	}

	if leading := text.Leading.Value; text.Leading.IsSet() && !(leading >= MinLeading && leading <= MaxLeading) {
		problems = append(problems, fmt.Errorf("%w: text leading %v is outside %v to %v", ErrOutOfRange, leading, MinLeading, MaxLeading))
	}

	if text.Value.IsSet() {
		problems = append(problems, ValidateTextValue(text.Value.Value))
	}

	return errors.Join(problems...)
}

// NewPDFItem returns an ItemPDF.
func NewPDFItem(origin Origin, path string) Item {
	return Item{Kind: ItemPDF, Origin: origin, Path: path}
}

// NewBlankItem returns an ItemBlank of count pages.
func NewBlankItem(origin Origin, style *BlankStyle, count int64) (Item, error) {
	err := CheckBlankCount(count)
	if err != nil {
		return Item{}, err
	}

	return Item{Kind: ItemBlank, Origin: origin, Blank: &BlankItem{Style: *style, Count: count, CountOrigin: origin}}, nil
}

// NewGroupItem returns an ItemGroup holding items.
func NewGroupItem(origin Origin, dir string, items []Item) Item {
	return Item{Kind: ItemGroup, Origin: origin, Dir: Set(dir, origin), Items: items}
}
