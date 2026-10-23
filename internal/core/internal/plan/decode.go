// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package plan decodes strict, located version 3 assembly plans.
package plan

import (
	"bytes"
	_ "embed"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
)

// MaxBytes bounds the original UTF-8 plan, including an optional BOM.
const MaxBytes = 64 << 20

// Schema is the editor-facing plan v3 JSON Schema.
//
//go:embed plan.schema.json
var Schema []byte

type node struct {
	Ref         packet.Ref
	Source, Dir string
	Selection   []packet.PageRange
	Page        *packet.PageStyle
	Count       int
	Contents    *packet.ContentsItem
	Items       []node
	Title       string
	StartOn     packet.StartOn
	Divider     *packet.PageStyle
}

type fault struct {
	code             diag.Code
	message, pointer string
	offset           int64
}

func (e *fault) Error() string { return e.message }

type decoder struct {
	d                  *jsontext.Decoder
	data               []byte
	bom                int64
	entries, generated int
	pairs, contents    int
	contentsPointer    string
	contentsOffset     int64
}

// Decode charges the original input bytes, including an optional leading BOM.
// The caller owns file/stream I/O and cancellation; this function reads no files.
func Decode(data []byte, file, base string) (packet.Plan, []diag.Diagnostic) {
	p := decoder{data: data}
	if len(data) > MaxBytes {
		return packet.Plan{}, []diag.Diagnostic{diag.New(diag.PlanTooLarge, "Plan exceeds 64 MiB.")}
	}
	input := data
	if bytes.HasPrefix(input, []byte{0xef, 0xbb, 0xbf}) {
		p.bom = 3
		input = input[3:]
	}
	p.d = jsontext.NewDecoder(bytes.NewReader(input))
	result, err := p.document(base)
	if err == nil {
		_, err = p.d.ReadToken()
		if errors.Is(err, io.EOF) {
			err = nil
		} else if err == nil {
			err = p.fail(diag.PlanSyntax, "Expected one JSON object; trailing data is not allowed.")
		}
	}
	if err != nil {
		return packet.Plan{}, []diag.Diagnostic{p.diagnostic(err, file)}
	}
	return result, nil
}

func (p *decoder) diagnostic(err error, file string) diag.Diagnostic {
	f := &fault{code: diag.PlanSyntax, message: err.Error(), pointer: string(p.d.StackPointer()), offset: p.d.InputOffset()}
	var own *fault
	var syntax *jsontext.SyntacticError
	var semantic *json.SemanticError
	switch {
	case errors.As(err, &own):
		f = own
	case errors.As(err, &syntax):
		f.offset = syntax.ByteOffset
		f.pointer = string(syntax.JSONPointer)
		if errors.Is(err, jsontext.ErrDuplicateName) {
			f.code = diag.PlanDuplicateMember
		}
	case errors.As(err, &semantic):
		f.code = diag.PlanType
		f.offset = semantic.ByteOffset
		f.pointer = string(semantic.JSONPointer)
	}
	offset := min(max(f.offset+p.bom, 0), int64(len(p.data)))
	before := p.data[:offset]
	line := bytes.Count(before, []byte{'\n'}) + 1
	last := bytes.LastIndexByte(before, '\n')
	d := diag.New(f.code, f.message)
	d.File = file
	d.Location = &diag.Location{File: file, Ref: f.pointer, Offset: offset, Line: line, Column: int(offset) - last}
	if f.pointer != "" {
		d.Items = []string{f.pointer}
	}
	return d
}

func (p *decoder) fail(code diag.Code, message string) error {
	return &fault{code: code, message: message, pointer: string(p.d.StackPointer()), offset: p.d.InputOffset()}
}

// start is the next value's first byte, excluding commas, colons and whitespace.
func (p *decoder) start() int64 {
	n := p.d.InputOffset() + p.bom
	for n < int64(len(p.data)) && strings.ContainsRune(" \r\n\t,:", rune(p.data[n])) {
		n++
	}
	return n - p.bom
}

func (p *decoder) expect(kind jsontext.Kind) error {
	start := p.start()
	t, err := p.d.ReadToken()
	if err != nil {
		return err
	}
	if t.Kind() == 'n' {
		return &fault{code: diag.PlanNull, message: "null is not allowed.", pointer: string(p.d.StackPointer()), offset: start}
	}
	if t.Kind() != kind {
		return &fault{code: diag.PlanType, message: fmt.Sprintf("Expected %q, received %q.", kind, t.Kind()), pointer: string(p.d.StackPointer()), offset: start}
	}
	return nil
}

func (p *decoder) object(member func(string) error) error {
	if err := p.expect('{'); err != nil {
		return err
	}
	for p.d.PeekKind() != '}' {
		t, err := p.d.ReadToken()
		if err != nil {
			return err
		}
		if t.Kind() != '"' {
			return p.fail(diag.PlanType, "Expected an object member.")
		}
		if err = member(t.String()); err != nil {
			return err
		}
	}
	_, err := p.d.ReadToken()
	return err
}

func (p *decoder) scalar(target any) error {
	if p.d.PeekKind() == 'n' {
		return p.expect('"')
	}
	return json.UnmarshalDecode(p.d, target, json.RejectUnknownMembers(true))
}

func (p *decoder) text() (string, error) {
	var value string
	err := p.scalar(&value)
	return value, err
}
func (p *decoder) path() (string, error) {
	s, err := p.text()
	if err != nil {
		return "", err
	}
	if err = packet.ValidatePath(s); err != nil {
		return "", p.fail(diag.PathInvalid, err.Error())
	}
	return s, nil
}
func (p *decoder) unknown(name string) error {
	return p.fail(diag.PlanUnknownMember, fmt.Sprintf("Unknown member %q (names are case-sensitive).", name))
}

func (p *decoder) document(base string) (packet.Plan, error) {
	var result packet.Plan
	var defaults packet.PageStyle
	var nodes []node
	var dir string
	seenVersion, seenItems := false, false
	err := p.object(func(name string) error {
		var err error
		switch name {
		case "$schema":
			_, err = p.text()
		case "version":
			seenVersion = true
			start := p.start()
			var token jsontext.Token
			token, err = p.d.ReadToken()
			if err == nil {
				switch {
				case token.Kind() == 'n':
					err = &fault{code: diag.PlanNull, message: "null is not allowed.", pointer: "/version", offset: start}
				case token.Kind() == '0':
					if token.String() != "3" {
						err = &fault{code: diag.PlanVersionUnsupported, message: "Supported plan version is 3.", pointer: "/version", offset: start}
					}
				default:
					err = p.fail(diag.PlanType, "version must be the JSON integer 3.")
				}
			}
		case "output":
			result.Output, err = p.path()
		case "dir":
			dir, err = p.path()
		case "fit_to":
			var size packet.Size
			size, err = p.size(false)
			if err == nil {
				result.FitTo = &size
			}
		case "page":
			defaults, err = p.style(false)
		case "page_numbers":
			result.PageNumbers, err = p.pageNumbers()
		case "items":
			seenItems = true
			nodes, err = p.items(0)
		default:
			err = p.unknown(name)
		}
		return err
	})
	if err != nil {
		return result, err
	}
	if !seenVersion {
		return result, p.fail(diag.PlanVersionUnsupported, "Missing version; supported plan version is 3.")
	}
	if !seenItems {
		return result, p.fail(diag.PlanType, "Required member items is missing.")
	}
	if result.Output != "" {
		result.Output, _ = packet.Path(base, result.Output)
	}
	if defaults.Text.Font.Value.Path != "" {
		defaults.Text.Font.Value.Path, _ = packet.Path(base, defaults.Text.Font.Value.Path)
	}
	defaults = defaults.Over(packet.Defaults())
	scope := base
	if dir != "" {
		scope, _ = packet.Path(base, dir)
	}
	result = packet.NewPlan(result.Output, result.FitTo, result.PageNumbers, defaults, p.resolveItems(nodes, scope, defaults))
	if p.contents > 0 {
		sections := result.Sections()
		if len(sections) == 0 {
			return result, &fault{code: diag.PlanValue, message: "Contents requires at least one titled section.", pointer: p.contentsPointer, offset: p.contentsOffset}
		}
	}
	return result, nil
}

func (p *decoder) items(depth int) ([]node, error) {
	if depth > 64 {
		return nil, p.fail(diag.PlanLimit, "Group nesting exceeds 64.")
	}
	if err := p.expect('['); err != nil {
		return nil, err
	}
	var nodes []node
	for p.d.PeekKind() != ']' {
		n, err := p.item(depth)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	if len(nodes) == 0 {
		return nil, p.fail(diag.PlanValue, "items must not be empty.")
	}
	_, err := p.d.ReadToken()
	return nodes, err
}

func (p *decoder) item(depth int) (node, error) {
	n := node{Count: 1}
	if p.d.PeekKind() != '{' {
		err := p.expect('{')
		var typeFault *fault
		if errors.As(err, &typeFault) && typeFault.code == diag.PlanType {
			typeFault.message = `An item must be a source object, generated page object, contents object, or container object; for example {"source":"source.pdf"}.`
		}
		return n, err
	}
	seen := map[string]bool{}
	err := p.object(func(name string) error {
		seen[name] = true
		var err error
		switch name {
		case "source":
			n.Source, err = p.path()
		case "pages":
			n.Selection, err = p.selection()
		case "page":
			var style packet.PageStyle
			style, err = p.style(false)
			n.Page = &style
		case "dir":
			n.Dir, err = p.path()
		case "items":
			n.Items, err = p.items(depth + 1)
		case "count":
			n.Count, err = p.count()
		case "title":
			n.Title, err = p.title()
		case "start_on":
			err = p.enumeration(&n.StartOn)
		case "divider":
			var style packet.PageStyle
			style, err = p.style(true)
			n.Divider = &style
		case "contents":
			start, pointer := p.start(), string(p.d.StackPointer())
			n.Contents, err = p.contentsStyle()
			if err == nil {
				p.contents++
				if p.contents > 1 {
					err = &fault{code: diag.PlanValue, message: "A packet may contain at most one contents item.", pointer: pointer, offset: start}
				} else {
					p.contentsPointer, p.contentsOffset = pointer, start
				}
			}
		default:
			err = p.unknown(name)
		}
		return err
	})
	n.Ref = string(p.d.StackPointer())
	if err != nil {
		return n, err
	}
	source := seen["source"] && len(seen) == 1 || seen["source"] && seen["pages"] && len(seen) == 2
	page := seen["page"] && (len(seen) == 1 || seen["count"] && len(seen) == 2)
	contents := seen["contents"] && len(seen) == 1
	container := seen["items"] && (seen["dir"] || seen["title"] || seen["start_on"]) && (!seen["divider"] || seen["title"])
	for _, key := range []string{"source", "pages", "page", "count", "contents"} {
		container = container && !seen[key]
	}
	if !source && !page && !contents && !container {
		return n, p.fail(diag.PlanItemShape, "An item must be a source, generated page, contents item, or useful container; divider requires a title.")
	}
	generated := 0
	if n.Page != nil {
		generated = n.Count
	}
	if n.Divider != nil {
		generated++
	}
	if message := charge(&p.entries, &p.generated, generated); message != "" {
		return n, p.fail(diag.PlanLimit, message)
	}
	return n, nil
}

func (p *decoder) title() (string, error) {
	value, err := p.text()
	if err != nil {
		return "", err
	}
	if utf8.RuneCountInString(value) > 10000 {
		return "", p.fail(diag.PlanLimit, "Title exceeds 10000 characters.")
	}
	visible := false
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			return "", p.fail(diag.PlanValue, "Titles must be single-line text without control or format characters.")
		}
		visible = visible || !unicode.IsSpace(r)
	}
	if !visible {
		return "", p.fail(diag.PlanValue, "Title must not be empty or whitespace-only.")
	}
	return value, nil
}

func (p *decoder) contentsStyle() (*packet.ContentsItem, error) {
	style := &packet.ContentsItem{Title: "Contents"}
	err := p.object(func(name string) error {
		var err error
		switch name {
		case "title":
			style.Title, err = p.title()
		case "size":
			style.Size, err = p.size(false)
		default:
			err = p.unknown(name)
		}
		return err
	})
	return style, err
}

func (p *decoder) pageNumbers() (*packet.PageNumbers, error) {
	numbers := &packet.PageNumbers{Size: 9, Margin: 8 * 72 / 25.4}
	err := p.object(func(name string) error {
		var err error
		switch name {
		case "size":
			numbers.Size, err = p.length(true)
		case "margin":
			numbers.Margin, err = p.length(false)
			if err == nil && numbers.Margin < 0 {
				err = p.fail(diag.PlanValue, "Page-number margin must not be negative.")
			}
		default:
			err = p.unknown(name)
		}
		return err
	})
	return numbers, err
}

func (p *decoder) pageIndex() (int, error) {
	start := p.start()
	t, err := p.d.ReadToken()
	if err != nil {
		return 0, err
	}
	if t.Kind() == 'n' {
		return 0, &fault{code: diag.PlanNull, message: "null is not allowed.", pointer: string(p.d.StackPointer()), offset: start}
	}
	if t.Kind() != '0' {
		return 0, p.fail(diag.PlanType, "Source page indexes must be JSON integers.")
	}
	if strings.ContainsAny(t.String(), ".eE") {
		return 0, p.fail(diag.PlanValue, "Source page indexes must use positive integer tokens.")
	}
	n, err := strconv.ParseUint(t.String(), 10, 64)
	if err != nil || n > math.MaxInt {
		return 0, p.fail(diag.PlanValue, "Source page index is outside the supported positive range.")
	}
	if n == 0 {
		return 0, p.fail(diag.PlanValue, "Source page indexes start at 1.")
	}
	return int(n), nil
}

func (p *decoder) selection() ([]packet.PageRange, error) {
	if err := p.expect('['); err != nil {
		return nil, err
	}
	var ranges []packet.PageRange
	for p.d.PeekKind() != ']' {
		if err := p.expect('['); err != nil {
			return nil, err
		}
		first, err := p.pageIndex()
		if err != nil {
			return nil, err
		}
		last, err := p.pageIndex()
		if err != nil {
			return nil, err
		}
		if err = p.expect(']'); err != nil {
			return nil, err
		}
		if first > last {
			return nil, p.fail(diag.PlanValue, "Source page ranges must be ascending inclusive pairs.")
		}
		p.pairs++
		if p.pairs > packet.MaxSelectionPairs {
			return nil, p.fail(diag.PlanLimit, "Plan exceeds 100000 source selection pairs.")
		}
		if len(ranges) > 0 {
			previous := &ranges[len(ranges)-1]
			if first <= previous.Last {
				return nil, p.fail(diag.PlanValue, "Source page ranges must be ascending and disjoint.")
			}
			if previous.Last < math.MaxInt && first == previous.Last+1 {
				previous.Last = last
				continue
			}
		}
		ranges = append(ranges, packet.PageRange{First: first, Last: last})
	}
	if len(ranges) == 0 {
		return nil, p.fail(diag.PlanValue, "Source pages selection must not be empty.")
	}
	_, err := p.d.ReadToken()
	return ranges, err
}

func (p *decoder) count() (int, error) {
	start := p.start()
	t, err := p.d.ReadToken()
	if err != nil {
		return 0, err
	}
	if t.Kind() == 'n' {
		return 0, &fault{code: diag.PlanNull, message: "null is not allowed.", pointer: string(p.d.StackPointer()), offset: start}
	}
	if t.Kind() != '0' {
		return 0, p.fail(diag.PlanType, "count must be a JSON number.")
	}
	s := t.String()
	if strings.ContainsAny(s, ".eE") {
		return 0, p.fail(diag.PlanValue, "count must use an integer token, such as 3 (not 3.0 or 3e0).")
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n > packet.MaxGeneratedPages {
		return 0, p.fail(diag.PlanLimit, "count exceeds 100000.")
	}
	if n < 1 {
		return 0, p.fail(diag.PlanValue, "count must be at least 1.")
	}
	return int(n), nil
}

func (p *decoder) resolveItems(nodes []node, base string, defaults packet.PageStyle) []packet.Item {
	style := func(authored *packet.PageStyle) packet.PageStyle {
		result := *authored
		if result.Text.Font.Value.Path != "" {
			result.Text.Font.Value.Path, _ = packet.Path(base, result.Text.Font.Value.Path)
		}
		return result.Over(defaults)
	}
	items := make([]packet.Item, 0, len(nodes))
	for _, n := range nodes {
		switch {
		case n.Source != "":
			path, _ := packet.Path(base, n.Source)
			items = append(items, &packet.SourceItem{Ref: n.Ref, Path: path, Selection: n.Selection})
		case n.Page != nil:
			items = append(items, &packet.PageItem{Ref: n.Ref, Style: style(n.Page), Count: n.Count})
		case n.Contents != nil:
			item := *n.Contents
			item.Ref = n.Ref
			items = append(items, &item)
		case n.Items != nil:
			scope := base
			if n.Dir != "" {
				scope, _ = packet.Path(base, n.Dir)
			}
			group := &packet.GroupItem{Ref: n.Ref, Title: n.Title, StartOn: n.StartOn, Items: p.resolveItems(n.Items, scope, defaults)}
			if n.Divider != nil {
				divider := style(n.Divider)
				group.Divider = &divider
			}
			items = append(items, group)
		}
	}
	return items
}

func (p *decoder) size(inherit bool) (packet.Size, error) {
	s, err := p.text()
	if err != nil {
		return packet.Size{}, err
	}
	size, err := packet.ParseSize(s, inherit)
	if err != nil {
		return packet.Size{}, p.fail(diag.PlanValue, err.Error())
	}
	return size, nil
}
func (p *decoder) length(positive bool) (float64, error) {
	var n float64
	var err error
	if p.d.PeekKind() == '"' {
		var s string
		s, err = p.text()
		if err != nil {
			return 0, err
		}
		n, err = packet.Length(s)
	} else {
		n, err = p.number()
	}
	if err != nil {
		var semantic *json.SemanticError
		var own *fault
		if errors.As(err, &own) {
			return 0, err
		}
		if errors.As(err, &semantic) {
			return 0, err
		}
		return 0, p.fail(diag.PlanValue, err.Error())
	}
	if n < -packet.MaxSide || n > packet.MaxSide || positive && n < 1 {
		return 0, p.fail(diag.PlanValue, "Length is outside its point range (positive lengths must be at least 1).")
	}
	return n, nil
}

func (p *decoder) style(titleDivider bool) (packet.PageStyle, error) {
	var s packet.PageStyle
	err := p.object(func(name string) error {
		var err error
		var from *packet.Ref
		switch name {
		case "size":
			s.Size.Value, err = p.size(true)
			from = &s.Size.From
		case "background":
			from = &s.Background.From
			var value string
			value, err = p.text()
			if err == nil {
				s.Background.Value, err = packet.ParseColor(value, true)
				if err != nil {
					err = p.fail(diag.PlanValue, err.Error())
				}
			}
		case "text":
			return p.textStyle(&s, titleDivider)
		default:
			return p.unknown(name)
		}
		if err == nil {
			*from = string(p.d.StackPointer())
		}
		return err
	})
	return s, err
}

func (p *decoder) textStyle(s *packet.PageStyle, titleDivider bool) error {
	return p.object(func(name string) error {
		var err error
		var from *packet.Ref
		switch name {
		case "value":
			from = &s.Text.Value.From
			if titleDivider {
				return &fault{code: diag.PlanValue, message: "Divider text.value is supplied by its section title.", pointer: string(p.d.StackPointer()), offset: p.start()}
			}
			s.Text.Value.Value, err = p.text()
			if err == nil && utf8.RuneCountInString(s.Text.Value.Value) > 10000 {
				err = p.fail(diag.PlanLimit, "Text exceeds 10000 characters.")
			}
		case "font":
			from = &s.Text.Font.From
			s.Text.Font.Value, err = p.font()
		case "size":
			from = &s.Text.Size.From

			s.Text.Size.Value, err = p.length(true)
		case "color":
			from = &s.Text.Color.From
			var value string
			value, err = p.text()
			if err == nil {
				var c *packet.Color
				c, err = packet.ParseColor(value, false)
				if err == nil {
					s.Text.Color.Value = *c
				} else {
					err = p.fail(diag.PlanValue, err.Error())
				}
			}
		case "x":
			from = &s.Text.X.From
			s.Text.X.Value, err = p.length(false)
		case "y":
			from = &s.Text.Y.From
			s.Text.Y.Value, err = p.length(false)
		case "width":
			from = &s.Text.Width.From
			s.Text.Width.Value, err = p.length(true)
		case "leading":
			from = &s.Text.Leading.From
			s.Text.Leading.Value, err = p.number()
			if err == nil && (s.Text.Leading.Value < 1 || s.Text.Leading.Value > 10) {
				err = p.fail(diag.PlanValue, "leading must be 1–10.")
			}
		case "anchor":
			from = &s.Text.Anchor.From
			err = p.enumeration(&s.Text.Anchor.Value)
		case "align":
			from = &s.Text.Align.From
			err = p.enumeration(&s.Text.Align.Value)
		case "overflow":
			from = &s.Text.Overflow.From
			err = p.enumeration(&s.Text.Overflow.Value)
		default:
			return p.unknown(name)
		}
		if err == nil {
			*from = string(p.d.StackPointer())
		}
		return err
	})
}
func (p *decoder) enum(values ...string) (string, error) {
	s, err := p.text()
	if err != nil {
		return "", err
	}
	for _, v := range values {
		if s == v {
			return s, nil
		}
	}
	return "", p.fail(diag.PlanValue, "Expected one of: "+strings.Join(values, ", "))
}
func (p *decoder) enumeration(value interface {
	UnmarshalText([]byte) error
	Values() []string
}) error {
	text, err := p.text()
	if err != nil {
		return err
	}
	if err = value.UnmarshalText([]byte(text)); err != nil {
		return p.fail(diag.PlanValue, "Expected one of: "+strings.Join(value.Values(), ", "))
	}
	return nil
}
func (p *decoder) font() (packet.Font, error) {
	if p.d.PeekKind() == '"' {
		_, err := p.enum("default")
		return packet.Font{}, err
	}
	var file string
	err := p.object(func(name string) error {
		if name != "file" {
			return p.unknown(name)
		}
		var err error
		file, err = p.path()
		return err
	})
	if err == nil && file == "" {
		err = p.fail(diag.PlanType, "font requires a file member.")
	}
	return packet.Font{Path: file}, err
}

func (p *decoder) number() (float64, error) {
	var n float64
	err := p.scalar(&n)
	var semantic *json.SemanticError
	if errors.As(err, &semantic) && semantic.JSONKind == '0' {
		return 0, p.fail(diag.PlanValue, "Number is outside the supported finite range.")
	}
	return n, err
}

func charge(entries, generated *int, count int) string {
	*entries++
	if *entries > packet.MaxEntries {
		return "Plan exceeds 100000 authored items."
	}
	*generated += count
	if *generated > packet.MaxGeneratedPages {
		return "Plan generates more than 100000 pages before contents and padding."
	}
	return ""
}
