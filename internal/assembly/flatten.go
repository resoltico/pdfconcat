// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly

type (
	// Contribution is one PDF or blank entry of the flattened sequence. Counts stay compact: a blank of a
	// million pages is one Contribution.
	Contribution struct {
		// TextOverrides references the original item declarations, independent of appearance deduplication.
		TextOverrides *TextStyle
		// Path is the absolute, lexically cleaned path of a PDF; empty for a blank.
		Path string
		// Origin is the item that wrote the contribution; explain it with Locate.
		Origin Origin
		// Count is the number of pages of a blank; for a PDF it is 1, one occurrence of the source.
		Count int64
		// Index is the position of the contribution in Flattened.Contributions.
		Index int
		// File indexes Flattened.Files for a PDF and is -1 for a blank.
		File int
		// Style indexes Flattened.Styles for a blank and is -1 for a PDF.
		Style int
		Kind  ItemKind
	}

	// SourceFile is one distinct source PDF, identified by its normalized path.
	SourceFile struct {
		Path string
		// FirstUse is the first contribution that names the file.
		FirstUse Origin
		// Uses is the number of occurrences; repeated sources are never merged in the sequence.
		Uses int
	}

	// FontFileUse is one distinct font file a blank's text asks for.
	FontFileUse struct {
		// Path is the absolute, lexically cleaned path.
		Path string
		// Declarations are each distinct declaration of this captured file, in input order.
		Declarations []Origin
	}

	// LayeredStyle is one distinct appearance after item over job defaults, before page sizes are known.
	// Values deduplicate independently of declaration origins; each consumer retains its original
	// declaration layer. A file font's effective path is absolute with no base.
	LayeredStyle struct {
		Style BlankStyle
		// FirstUse is the first blank that has this appearance.
		FirstUse Origin
	}

	// Flattened is the job as an ordered list of contributions with its distinct sources, fonts, and
	// appearances, each in order of first use. Resolve turns it into a Layout once the sources' page
	// geometry is known.
	Flattened struct {
		// Source explains every Origin.
		Source Source
		// TextDefaults references the job defaults; callers must keep job declarations immutable.
		TextDefaults  *TextStyle
		Contributions []Contribution
		Files         []SourceFile
		Fonts         []FontFileUse
		Styles        []LayeredStyle
		// GeneratedPages is the sum of the blank counts.
		GeneratedPages int64
	}

	// scopeFrame is the position in one group of the iterative walk.
	scopeFrame struct {
		base  string
		items []Item
		next  int
	}

	// fontResolution is the outcome of resolving one declared font.
	fontResolution struct {
		declared map[Origin]bool
		err      error
		path     string
		code     Code
	}

	// flattener holds the state of one flattening walk.
	flattener struct {
		job        *Job
		result     *Flattened
		fileIndex  map[string]int
		fontIndex  map[string]int
		fonts      map[Font]fontResolution
		styleIndex map[BlankStyle]int
		problems   Errors
		style      PathStyle
	}
)

// Flatten expands the job's groups into the ordered contributions in one pass, resolving every path
// against its group's directory. It does not expand counts, copy subtrees, or deduplicate anything in
// the sequence. It retains references to the job's text declarations for exact provenance; callers
// must not mutate those declarations while using the flattened job. Validation ensures every limit; the problems
// found while resolving paths and appearance values are returned together as a Errors.
func Flatten(job *Job) (*Flattened, error) {
	return flattenAs(job, HostPathStyle())
}

// flattenAs is Flatten under the path rules of style.
func flattenAs(job *Job, style PathStyle) (*Flattened, error) {
	if job.Source == nil {
		return nil, job.Validate() // reports the missing source; nothing can be located without one
	}

	err := job.Validate()
	if err != nil {
		return nil, Errors{errorAt(job.Source, job.Origin, "", StageJob, CodeInvalidJob, err, "%v", err)}
	}

	walker := &flattener{
		job:        job,
		style:      style,
		result:     &Flattened{Source: job.Source, TextDefaults: &job.Defaults.Text},
		fileIndex:  map[string]int{},
		fontIndex:  map[string]int{},
		fonts:      map[Font]fontResolution{},
		styleIndex: map[BlankStyle]int{},
	}

	base, ok := walker.scope(job.Base, &job.Dir, job.Origin)
	if ok {
		walker.walk(base)
	}

	if len(walker.problems) > 0 {
		return nil, walker.problems
	}

	return walker.result, nil
}

// scope resolves a declared directory against base. A base that is not absolute is reported at the job
// itself, and a directory that cannot be resolved at its declaration.
func (f *flattener) scope(base string, dir *Field[string], jobOrigin Origin) (string, bool) {
	code, path, err := resolvePathAs(f.style, base, "directory", dir.Value)
	if err == nil {
		return path, true
	}

	if code == CodeBaseNotAbsolute {
		f.problems = append(f.problems, errorAt(f.job.Source, jobOrigin, "", StagePath, code, err, "%v", err))
	} else {
		f.problems = append(f.problems, errorAt(f.job.Source, dir.Origin, "/dir", StagePath, code, err, "%v", err))
	}

	return "", false
}

// walk visits the items depth-first in input order with an explicit stack, so a deeply nested plan costs
// no call depth and no subtree is ever copied.
func (f *flattener) walk(base string) {
	stack := []scopeFrame{{base: base, items: f.job.Items}}

	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.next == len(top.items) {
			stack = stack[:len(stack)-1]

			continue
		}

		item := &top.items[top.next]
		top.next++

		switch item.Kind {
		case ItemGroup:
			groupBase, ok := f.scope(top.base, &item.Dir, item.Origin)
			if ok {
				stack = append(stack, scopeFrame{base: groupBase, items: item.Items})
			}
		case ItemPDF:
			f.addPDF(top.base, item)
		case ItemBlank:
			f.addBlank(item)
		default: // Validate rejects every other kind
		}
	}
}

func (f *flattener) addPDF(base string, item *Item) {
	code, path, err := resolvePathAs(f.style, base, "PDF path", item.Path)
	if err != nil {
		f.problems = append(f.problems, errorAt(f.job.Source, item.Origin, "", StagePath, code, err, "%v", err))

		return
	}

	file, known := f.fileIndex[path]
	if !known {
		file = len(f.result.Files)
		f.fileIndex[path] = file
		f.result.Files = append(f.result.Files, SourceFile{Path: path, FirstUse: item.Origin})
	}

	f.result.Files[file].Uses++

	f.result.Contributions = append(f.result.Contributions, Contribution{
		Kind: ItemPDF, Origin: item.Origin, Path: path, Count: 1, File: file, Style: -1, Index: len(f.result.Contributions),
	})
}

func (f *flattener) addBlank(item *Item) {
	style, ok := f.layer(&item.Blank.Style)
	if !ok {
		return
	}

	key := style.withoutOrigins()

	index, known := f.styleIndex[key]
	if !known {
		index = len(f.result.Styles)
		f.styleIndex[key] = index
		f.result.Styles = append(f.result.Styles, LayeredStyle{Style: style, FirstUse: item.Origin})

		err := validateStyleValues(&style)
		if err != nil {
			f.problems = append(f.problems, errorAt(f.job.Source, item.Origin, "/blank", StageStyle, CodeStyleInvalid, err, "%v", err))
		}
	}

	// Validate bounds every count and the sum, so this sum cannot pass MaxGeneratedPages.
	f.result.GeneratedPages += item.Blank.Count
	f.result.Contributions = append(f.result.Contributions, Contribution{
		Kind: ItemBlank, Origin: item.Origin, Count: item.Blank.Count, File: -1, Style: index, Index: len(f.result.Contributions),
		TextOverrides: &item.Blank.Style.Text,
	})
}

// layer applies the job defaults under the item's style, makes a file font's path absolute, and drops the
// origins. It reports false when the font cannot be resolved; the problem is recorded once per declaration.
func (f *flattener) layer(own *BlankStyle) (BlankStyle, bool) {
	layered := own.Over(&f.job.Defaults)

	font := layered.Text.Font
	if font.IsSet() && !font.Value.IsDefault() {
		resolved := f.resolveFont(&font)
		if resolved.err != nil {
			return BlankStyle{}, false
		}

		layered.Text.Font = Set(Font{File: resolved.path}, font.Origin)
	}

	return layered, true
}

func (f *flattener) resolveFont(font *Field[Font]) fontResolution {
	known, seen := f.fonts[font.Value]
	if !seen {
		known.code, known.path, known.err = resolvePathAs(f.style, font.Value.Base, "font file", font.Value.File)
		known.declared = map[Origin]bool{}
		f.fonts[font.Value] = known
	}

	if known.declared[font.Origin] {
		return known
	}

	known.declared[font.Origin] = true
	if known.err != nil {
		problem := errorAt(f.job.Source, font.Origin, "/blank/text/font", StagePath, known.code, known.err, "%v", known.err)
		f.problems = append(f.problems, problem)

		return known
	}

	index, listed := f.fontIndex[known.path]
	if !listed {
		index = len(f.result.Fonts)
		f.fontIndex[known.path] = index
		f.result.Fonts = append(f.result.Fonts, FontFileUse{Path: known.path})
	}

	f.result.Fonts[index].Declarations = append(f.result.Fonts[index].Declarations, font.Origin)

	return known
}

// withoutOrigins returns the style with every field's origin cleared.
func (s *BlankStyle) withoutOrigins() BlankStyle {
	return BlankStyle{
		Size:       s.Size.bare(),
		Background: s.Background.bare(),
		Text: TextStyle{
			Value:    s.Text.Value.bare(),
			Font:     s.Text.Font.bare(),
			Size:     s.Text.Size.bare(),
			Color:    s.Text.Color.bare(),
			Anchor:   s.Text.Anchor.bare(),
			X:        s.Text.X.bare(),
			Y:        s.Text.Y.bare(),
			Width:    s.Text.Width.bare(),
			Align:    s.Text.Align.bare(),
			Leading:  s.Text.Leading.bare(),
			Overflow: s.Text.Overflow.bare(),
		},
	}
}

// bare returns the field without its origin.
func (f Field[T]) bare() Field[T] {
	f.Origin = Origin{}

	return f
}
