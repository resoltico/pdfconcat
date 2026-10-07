// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Package plan decodes plan files (format version 1) into the neutral assembly job tree, strictly and in
// one pass over the JSON token stream, with provenance and declared resource limits.
package plan

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

type (
	// Input describes where a plan came from.
	Input struct {
		// Name is the source name used in diagnostics: the file path, "<stdin>", or "<inline>".
		Name string
		// BaseDir is the initial base directory: the plan file's directory, or --base-dir for standard input and
		// inline plans, or the working directory. It is not interpreted here; it is recorded as Job.Base
		// and as the base of fonts declared in the defaults.
		BaseDir string
	}

	// token is a JSON token with the absolute offset of its first byte.
	token struct {
		value jsontext.Token
		start int64
	}

	// parser reads one plan from the token stream and charges the declared limits as it goes.
	parser struct {
		source  *limitedSource
		doc     *document
		decoder *jsontext.Decoder
		job     *assembly.Job
		stop    cancellation

		generated     int64
		nodes         int
		contributions int
		ticks         int
		// owner is the item whose style fields are being read; Ref 0 is the plan's defaults.
		owner assembly.Ref
	}

	// seenMembers records which required root members were present.
	seenMembers struct {
		version, items bool
	}

	// itemMembers is what an object item turned out to hold.
	itemMembers struct {
		style                          assembly.BlankStyle
		count                          assembly.Field[int64]
		blank, hasCount, dir, hasItems bool
	}
)

const (
	wantedString  = "a string"
	memberVersion = "version"
	// Version is the plan format version this package reads.
	Version = 1

	// cancelCheckInterval is how many tokens pass between context checks.
	cancelCheckInterval = 0xFFF
	trailingBuffer      = 4096

	memberBlank = "blank"
	memberDir   = "dir"
	memberItems = "items"
	memberSize  = "size"
)

// Decode reads exactly one plan object from reader in a single pass over the jsontext token stream. reader
// may block (standard input); ctx cancels the read. Errors are *Error values; decoding stops at the first fault.
func Decode(ctx context.Context, input Input, reader io.Reader) (*assembly.Job, error) {
	stop := cancellationOf(ctx)
	source := newLimitedSource(stop, input.Name, reader)

	reading := &parser{
		stop:   stop,
		source: source,
		doc:    source.doc,
		// AllowDuplicateNames and AllowInvalidUTF8 default to false; stated for the reader of this code.
		decoder: jsontext.NewDecoder(source, jsontext.AllowDuplicateNames(false), jsontext.AllowInvalidUTF8(false)),
		job:     &assembly.Job{Source: source.doc, Base: input.BaseDir},
	}

	err := reading.document()
	if err != nil {
		return nil, err
	}

	return reading.job, nil
}

func (t token) kind() byte { return byte(t.value.Kind()) }

func kindName(kind byte) string {
	switch kind {
	case '"':
		return wantedString
	case '0':
		return "a number"
	case '{':
		return "an object"
	case '[':
		return "an array"
	default: // the remaining value kinds are true and false; null is reported before a kind is named
		return "a boolean"
	}
}

func (p *parser) absolute() int64 { return p.decoder.InputOffset() + int64(p.doc.bomLen) }

func (p *parser) origin(value token) assembly.Origin {
	return assembly.Origin{Ref: p.owner, Offset: value.start}
}

func (p *parser) fail(stage Stage, code Code, offset int64, pointer, format string, args ...any) *Error {
	line, column := p.doc.Position(offset)

	return &Error{
		Code: code, Stage: stage, Message: fmt.Sprintf(format, args...),
		Location: assembly.Location{Source: p.doc.name, Offset: offset, Line: line, Column: column, Pointer: pointer},
	}
}

// failHere reports at a just-read value; the pointer comes from the decoder and is valid only until its next read.
func (p *parser) failHere(stage Stage, code Code, value token, format string, args ...any) *Error {
	return p.fail(stage, code, value.start, string(p.decoder.StackPointer()), format, args...)
}

// failItem reports at an item.
func (p *parser) failItem(stage Stage, code Code, item *assembly.Item, format string, args ...any) *Error {
	return p.fail(stage, code, item.Origin.Offset, p.doc.Pointer(item.Origin.Ref, ""), format, args...)
}

// syntaxError converts a decoder or reader error.
func (p *parser) syntaxError(err error) error {
	if planErr, ok := errors.AsType[*Error](err); ok {
		return planErr
	}

	syntactic, ok := errors.AsType[*jsontext.SyntacticError](err)
	if !ok {
		return p.fail(StageRead, CodeReadFailed, p.absolute(), "", "cannot read the plan: %v", err)
	}

	code := CodeSyntax
	// jsontext quotes buffer-dependent snippets and characters of the input in some messages; drop them so
	// diagnostics do not vary with how the reader chunks the bytes.
	message := strings.Join(
		strings.Fields(regexp.MustCompile("`[^`]*`|'(?:\\\\.|[^'\\\\])*'").ReplaceAllString(syntactic.Err.Error(), "")),
		" ",
	)

	if errors.Is(err, jsontext.ErrDuplicateName) {
		code = CodeDuplicateMember
		message = "duplicate member name (names are compared after unescaping); remove one"
	}

	return p.fail(StageSyntax, code, syntactic.ByteOffset+int64(p.doc.bomLen), string(syntactic.JSONPointer), "%s", message)
}

func isJSONSpace(char byte) bool { return char == ' ' || char == '\t' || char == '\n' || char == '\r' }

// next reads one token and returns it with the absolute offset of its first byte.
func (p *parser) next() (token, error) {
	previous := p.absolute()

	value, err := p.decoder.ReadToken()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return token{}, p.fail(StageSyntax, CodeEmpty, previous, "", "the plan is empty; want one JSON object")
		}

		return token{}, p.syntaxError(err)
	}

	start := previous
	for start < int64(len(p.doc.data)) && (isJSONSpace(p.doc.data[start]) || p.doc.data[start] == ',' || p.doc.data[start] == ':') {
		start++
	}

	p.ticks++
	if p.ticks&cancelCheckInterval == 0 {
		cause := p.stop.err()
		if cause != nil {
			return token{}, p.source.fail(cause)
		}
	}

	return token{value: value, start: start}, nil
}

func (p *parser) want(value token, kind byte, what string) error {
	switch value.kind() {
	case kind:
		return nil
	case 'n':
		return p.failHere(StageShape, CodeNull, value, "null is not allowed here; want %s, or omit the member", what)
	default:
		return p.failHere(StageShape, CodeWrongType, value, "want %s, found %s", what, kindName(value.kind()))
	}
}

func (p *parser) readString(what string) (string, token, error) {
	value, err := p.next()
	if err != nil {
		return "", value, err
	}

	err = p.want(value, '"', what)
	if err != nil {
		return "", value, err
	}

	return value.value.String(), value, nil
}

// caseHint names the known member that differs from name only by case, if any.
func caseHint(known []string, name string) string {
	for _, candidate := range known {
		if strings.EqualFold(candidate, name) {
			return fmt.Sprintf("; member names are case-sensitive, did you mean %q", candidate)
		}
	}

	return ""
}

// members iterates the members of an object whose '{' was just read. handle must consume exactly one
// value. Unknown names are reported with a hint for wrongly cased spellings.
func (p *parser) members(known []string, handle func(name string) error) error {
	for {
		value, err := p.next()
		if err != nil {
			return err
		}

		if value.kind() == '}' {
			return nil
		}

		name := value.value.String()
		if !slices.Contains(known, name) {
			return p.failHere(StageShape, CodeUnknownMember, value, "unknown member %q%s (allowed: %s)",
				name, caseHint(known, name), strings.Join(known, ", "))
		}

		err = handle(name)
		if err != nil {
			return err
		}
	}
}

func (p *parser) openObject(what string) (token, error) {
	value, err := p.next()
	if err != nil {
		return value, err
	}

	return value, p.want(value, '{', what)
}

// countNode charges one structural node; call right after reading the node's first token.
func (p *parser) countNode(value token) error {
	p.nodes++
	if p.nodes > assembly.MaxNodes {
		return p.failHere(StageLimit, CodeLimitNodes, value,
			"plan has more than %d structural nodes (items, blank objects, text objects and font objects); split it into several plans",
			assembly.MaxNodes)
	}

	return nil
}

func (p *parser) document() error {
	// PeekKind forces the first read, which sniffs the byte order mark; any error resurfaces from next.
	p.decoder.PeekKind()

	root, err := p.next()
	if err != nil {
		return err
	}

	if root.kind() != '{' {
		return p.failHere(StageShape, CodeNotObject, root, "a plan must be one JSON object, found %s", kindName(root.kind()))
	}

	p.job.Origin = assembly.Origin{Offset: root.start}

	var seen seenMembers

	err = p.members([]string{"$schema", memberVersion, "output", memberDir, memberBlank, memberItems}, func(name string) error {
		return p.rootMember(name, &seen)
	})
	if err != nil {
		return err
	}

	switch {
	case !seen.version:
		return p.fail(
			StageShape,
			CodeMissingMember,
			root.start,
			"",
			"missing member %q; this build reads plan version %d",
			memberVersion,
			Version,
		)
	case !seen.items:
		return p.fail(StageShape, CodeMissingMember, root.start, "", "missing member %q: the ordered sequence of PDFs and blanks", "items")
	}

	err = p.trailing()
	if err != nil {
		return err
	}

	p.resolveFontBases()

	return nil
}

func (p *parser) rootMember(name string, seen *seenMembers) error {
	var err error

	p.owner = 0

	switch name {
	case "$schema":
		_, _, err = p.readString(wantedString)
	case memberVersion:
		seen.version = true
		err = p.version()
	case "output":
		err = p.output()
	case memberDir:
		p.job.Dir, err = p.readDir()
	case memberBlank:
		p.job.Defaults, err = p.blank()
	default: // "items": members only yields the known names
		seen.items = true
		p.job.Items, err = p.items(0, 0)
	}

	return err
}

func (p *parser) version() error {
	number, err := p.readInteger(math.MinInt64, math.MaxInt64)
	if err != nil {
		return err
	}

	if number.Value != Version {
		return p.fail(StageShape, CodeUnsupportedVer, number.Origin.Offset, "/version",
			"version %d is not supported; this build reads plan version %d", number.Value, Version)
	}

	return nil
}

func (p *parser) output() error {
	text, value, err := p.readString("a non-empty path string")
	if err != nil {
		return err
	}

	if text == "" {
		return p.fail(StageShape, CodeBadValue, value.start, "/output", "output must not be empty; omit it instead")
	}

	err = assembly.CheckPathText(text)
	if err != nil {
		return p.fail(StageShape, CodeBadValue, value.start, "/output", "output: %v", err)
	}

	p.job.Output = assembly.Set(text, p.origin(value))

	return nil
}

func (p *parser) readDir() (assembly.Field[string], error) {
	text, value, err := p.readString("a directory string")
	if err != nil {
		return assembly.Field[string]{}, err
	}

	err = assembly.CheckPathText(text)
	if err != nil {
		return assembly.Field[string]{}, p.failHere(StageShape, CodeBadValue, value, "dir: %v", err)
	}

	return assembly.Set(text, p.origin(value)), nil
}

// trailing requires that only JSON whitespace follows the root object.
func (p *parser) trailing() error {
	position := p.absolute()

	var buffer [trailingBuffer]byte

	for {
		for position < int64(len(p.doc.data)) {
			if !isJSONSpace(p.doc.data[position]) {
				return p.fail(
					StageSyntax,
					CodeTrailingData,
					position,
					"",
					"unexpected data after the top-level object; a plan is exactly one JSON object",
				)
			}

			position++
		}

		length, err := p.source.Read(buffer[:])
		if length == 0 && errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil && !errors.Is(err, io.EOF) {
			return p.syntaxError(err)
		}
	}
}

// items reads an "items" array. depth is the number of enclosing groups; parent is the item that owns the array.
func (p *parser) items(parent assembly.Ref, depth int) ([]assembly.Item, error) {
	open, err := p.next()
	if err != nil {
		return nil, err
	}

	err = p.want(open, '[', "an array of items")
	if err != nil {
		return nil, err
	}

	if depth > assembly.MaxGroupDepth {
		return nil, p.failHere(
			StageLimit,
			CodeLimitDepth,
			open,
			"directory groups nest more than %d levels deep; flatten the plan",
			assembly.MaxGroupDepth,
		)
	}

	var out []assembly.Item

	for index := int32(0); ; index++ {
		value, nextErr := p.next()
		if nextErr != nil {
			return nil, nextErr
		}

		if value.kind() == ']' {
			if len(out) == 0 {
				return nil, p.fail(
					StageShape,
					CodeEmptyItems,
					open.start,
					p.doc.Pointer(parent, "/items"),
					"items must contain at least one entry",
				)
			}

			return out, nil
		}

		item, itemErr := p.item(value, parent, index, depth)
		if itemErr != nil {
			return nil, itemErr
		}

		out = append(out, item)
	}
}

// item reads one entry of an items array whose first token was just read.
func (p *parser) item(value token, parent assembly.Ref, index int32, depth int) (assembly.Item, error) {
	err := p.countNode(value)
	if err != nil {
		return assembly.Item{}, err
	}

	item := assembly.Item{Origin: assembly.Origin{Ref: p.doc.addItem(parent, index), Offset: value.start}}

	switch value.kind() {
	case '"':
		item.Kind, item.Path = assembly.ItemPDF, value.value.String()

		err = p.checkPDFPath(value, item.Path)
		if err == nil {
			err = p.contribute(&item)
		}
	case '{':
		err = p.objectItem(&item, depth)
	case 'n':
		err = p.failHere(
			StageShape,
			CodeNull,
			value,
			"null is not allowed here; want a PDF path string or an object with \"blank\" or \"dir\"",
		)
	default:
		err = p.failHere(StageShape, CodeWrongType, value,
			"want a PDF path string or an object with \"blank\" or \"dir\", found %s", kindName(value.kind()))
	}

	return item, err
}

func (p *parser) checkPDFPath(value token, path string) error {
	if path == "" {
		return p.failHere(StageShape, CodeBadValue, value, "a PDF path must not be empty")
	}

	if assembly.CheckPathText(path) != nil {
		return p.failHere(StageShape, CodeBadValue, value, "a PDF path must not contain a NUL character")
	}

	return nil
}

func (p *parser) contribute(item *assembly.Item) error {
	p.contributions++
	if p.contributions > assembly.MaxContributions {
		return p.failItem(StageLimit, CodeLimitContribs, item,
			"plan has more than %d PDF and blank entries after flattening groups; split it into several plans", assembly.MaxContributions)
	}

	return nil
}

// objectItem reads an object item: either {"blank": {...}, "count": N} or {"dir": "...", "items": [...]}.
func (p *parser) objectItem(item *assembly.Item, depth int) error {
	var found itemMembers

	err := p.members([]string{memberBlank, "count", memberDir, memberItems}, func(name string) error {
		return p.itemMember(name, item, depth, &found)
	})
	if err != nil {
		return err
	}

	kind := found.kind()
	if kind == assembly.ItemBlank {
		return p.finishBlank(item, &found)
	}

	if kind == assembly.ItemGroup {
		item.Kind = kind

		return nil
	}

	return p.itemShapeError(item, &found)
}

// kind classifies an object item by the members it holds; 0 means they fit no kind.
func (f *itemMembers) kind() assembly.ItemKind {
	switch {
	case f.blank && !f.dir && !f.hasItems:
		return assembly.ItemBlank
	case f.dir && f.hasItems && !f.blank && !f.hasCount:
		return assembly.ItemGroup
	default:
		return 0
	}
}

func (p *parser) itemShapeError(item *assembly.Item, found *itemMembers) error {
	if found.dir && !found.hasItems && !found.blank && !found.hasCount {
		return p.failItem(StageShape, CodeMissingMember, item, `a directory group needs "items"`)
	}

	return p.failItem(StageShape, CodeAmbiguousItem, item,
		`an object item is either {"blank": {...}, "count": N} or {"dir": "...", "items": [...]}`)
}

func (p *parser) itemMember(name string, item *assembly.Item, depth int, found *itemMembers) error {
	var err error

	p.owner = item.Origin.Ref

	switch name {
	case memberBlank:
		found.blank = true
		found.style, err = p.blank()
	case "count":
		found.hasCount = true
		found.count, err = p.readInteger(1, assembly.MaxBlankCount)
	case memberDir:
		found.dir = true
		item.Dir, err = p.readDir()
	default: // "items": members only yields the known names
		found.hasItems = true
		item.Items, err = p.items(item.Origin.Ref, depth+1)
	}

	return err
}

func (p *parser) finishBlank(item *assembly.Item, found *itemMembers) error {
	blank := &assembly.BlankItem{Style: found.style, Count: 1, CountOrigin: item.Origin}
	if found.hasCount {
		blank.Count, blank.CountOrigin = found.count.Value, found.count.Origin
	}

	item.Kind, item.Blank = assembly.ItemBlank, blank

	var err error

	p.generated, err = assembly.AddCounts(p.generated, blank.Count, assembly.MaxGeneratedPages)
	if err != nil {
		return p.failItem(
			StageLimit,
			CodeLimitPages,
			item,
			"plan generates more than %d blank pages in total",
			assembly.MaxGeneratedPages,
		)
	}

	return p.contribute(item)
}

// resolveFontBases fixes the base directory of every file-backed font to the scope that declared it. A
// scope's directory can be written after the members that use it, so this runs once the document is read.
func (p *parser) resolveFontBases() {
	p.job.Defaults.Text.Font = withBase(p.job.Defaults.Text.Font, p.job.Base)
	resolveItemFonts(p.job.Items, p.job.ItemsBase())
}

func resolveItemFonts(items []assembly.Item, base string) {
	for index := range items {
		item := &items[index]

		if item.Kind == assembly.ItemBlank {
			item.Blank.Style.Text.Font = withBase(item.Blank.Style.Text.Font, base)
		}

		if item.Kind == assembly.ItemGroup {
			resolveItemFonts(item.Items, assembly.JoinDir(base, item.Dir.Value))
		}
	}
}

func withBase(field assembly.Field[assembly.Font], base string) assembly.Field[assembly.Font] {
	if field.IsSet() && !field.Value.IsDefault() {
		field.Value.Base = base
	}

	return field
}
