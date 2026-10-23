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
func Decode(data []byte, file, base string) (packet.Job, []diag.Diagnostic) {
	p := decoder{data: data}
	if len(data) > MaxBytes {
		return packet.Job{}, []diag.Diagnostic{diag.New("plan_too_large", "Plan exceeds 64 MiB.")}
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
			err = p.fail("plan_syntax", "Expected one JSON object; trailing data is not allowed.")
		}
	}
	if err != nil {
		return packet.Job{}, []diag.Diagnostic{p.diagnostic(err, file)}
	}
	return result, nil
}

func (p *decoder) diagnostic(err error, file string) diag.Diagnostic {
	f := &fault{code: "plan_syntax", message: err.Error(), pointer: string(p.d.StackPointer()), offset: p.d.InputOffset()}
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
			f.code = "plan_duplicate_member"
		}
	case errors.As(err, &semantic):
		f.code = "plan_type"
		f.offset = semantic.ByteOffset
		f.pointer = string(semantic.JSONPointer)
	}
	offset := min(max(f.offset+p.bom, 0), int64(len(p.data)))
	before := p.data[:offset]
	line := bytes.Count(before, []byte{'\n'}) + 1
	last := bytes.LastIndexByte(before, '\n')
	d := diag.New(f.code, f.message)
	d.File = file
	d.Location = &diag.Location{File: file, Pointer: f.pointer, Offset: offset, Line: line, Column: int(offset) - last}
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
		return &fault{code: "plan_null", message: "null is not allowed.", pointer: string(p.d.StackPointer()), offset: start}
	}
	if t.Kind() != kind {
		return &fault{code: "plan_type", message: fmt.Sprintf("Expected %q, received %q.", kind, t.Kind()), pointer: string(p.d.StackPointer()), offset: start}
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
			return p.fail("plan_type", "Expected an object member.")
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
		return "", p.fail("path_invalid", err.Error())
	}
	return s, nil
}
func (p *decoder) unknown(name string) error {
	return p.fail("plan_unknown_member", fmt.Sprintf("Unknown member %q (names are case-sensitive).", name))
}

func (p *decoder) document(base string) (packet.Job, error) {
	var result packet.Job
	var defaults packet.PageStyle
	var nodes []packet.Part
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
					err = &fault{code: "plan_null", message: "null is not allowed.", pointer: "/version", offset: start}
				case token.Kind() == '0':
					if token.String() != "3" {
						err = &fault{code: "plan_version_unsupported", message: "Supported plan version is 3.", pointer: "/version", offset: start}
					}
				default:
					err = p.fail("plan_type", "version must be the JSON integer 3.")
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
		return result, p.fail("plan_version_unsupported", "Missing version; supported plan version is 3.")
	}
	if !seenItems {
		return result, p.fail("plan_type", "Required member items is missing.")
	}
	if result.Output != "" {
		result.Output, _ = packet.Path(base, result.Output)
	}
	if defaults.Text.Font != "" && defaults.Text.Font != "default" {
		defaults.Text.Font, _ = packet.Path(base, defaults.Text.Font)
	}
	defaults = packet.Layer(packet.Defaults(), defaults)
	scope := base
	if dir != "" {
		scope, _ = packet.Path(base, dir)
	}
	result.Parts = nodes
	p.resolveDeclarations(result.Parts, scope, defaults)
	if p.contents > 0 {
		_, sections := packet.Declarations(result)
		if len(sections) == 0 {
			return result, &fault{code: "plan_value", message: "Contents requires at least one titled section.", pointer: p.contentsPointer, offset: p.contentsOffset}
		}
	}
	return result, nil
}

func (p *decoder) items(depth int) ([]packet.Part, error) {
	if depth > 64 {
		return nil, p.fail("plan_limit", "Group nesting exceeds 64.")
	}
	if err := p.expect('['); err != nil {
		return nil, err
	}
	var nodes []packet.Part
	for p.d.PeekKind() != ']' {
		n, err := p.item(depth)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	if len(nodes) == 0 {
		return nil, p.fail("plan_value", "items must not be empty.")
	}
	_, err := p.d.ReadToken()
	return nodes, err
}

func (p *decoder) item(depth int) (packet.Part, error) {
	n := packet.Part{Count: 1}
	if p.d.PeekKind() != '{' {
		err := p.expect('{')
		var typeFault *fault
		if errors.As(err, &typeFault) && typeFault.code == "plan_type" {
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
			n.StartOn, err = p.enum("any", "odd")
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
					err = &fault{code: "plan_value", message: "A packet may contain at most one contents item.", pointer: pointer, offset: start}
				} else {
					p.contentsPointer, p.contentsOffset = pointer, start
				}
			}
		default:
			err = p.unknown(name)
		}
		return err
	})
	n.Pointer = string(p.d.StackPointer())
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
		return n, p.fail("plan_item_shape", "An item must be a source, generated page, contents item, or useful container; divider requires a title.")
	}
	generated := 0
	if n.Page != nil {
		generated = n.Count
	}
	if n.Divider != nil {
		generated++
	}
	if message := charge(&p.entries, &p.generated, generated); message != "" {
		return n, p.fail("plan_limit", message)
	}
	return n, nil
}

func (p *decoder) title() (string, error) {
	value, err := p.text()
	if err != nil {
		return "", err
	}
	if utf8.RuneCountInString(value) > 10000 {
		return "", p.fail("plan_limit", "Title exceeds 10000 characters.")
	}
	visible := false
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			return "", p.fail("plan_value", "Titles must be single-line text without control or format characters.")
		}
		visible = visible || !unicode.IsSpace(r)
	}
	if !visible {
		return "", p.fail("plan_value", "Title must not be empty or whitespace-only.")
	}
	return value, nil
}

func (p *decoder) contentsStyle() (*packet.ContentsStyle, error) {
	style := &packet.ContentsStyle{Title: "Contents"}
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
				err = p.fail("plan_value", "Page-number margin must not be negative.")
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
		return 0, &fault{code: "plan_null", message: "null is not allowed.", pointer: string(p.d.StackPointer()), offset: start}
	}
	if t.Kind() != '0' {
		return 0, p.fail("plan_type", "Source page indexes must be JSON integers.")
	}
	if strings.ContainsAny(t.String(), ".eE") {
		return 0, p.fail("plan_value", "Source page indexes must use positive integer tokens.")
	}
	n, err := strconv.ParseUint(t.String(), 10, 64)
	if err != nil || n > math.MaxInt {
		return 0, p.fail("plan_value", "Source page index is outside the supported positive range.")
	}
	if n == 0 {
		return 0, p.fail("plan_value", "Source page indexes start at 1.")
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
			return nil, p.fail("plan_value", "Source page ranges must be ascending inclusive pairs.")
		}
		p.pairs++
		if p.pairs > packet.MaxSelectionPairs {
			return nil, p.fail("plan_limit", "Plan exceeds 100000 source selection pairs.")
		}
		if len(ranges) > 0 {
			previous := &ranges[len(ranges)-1]
			if first <= previous.Last {
				return nil, p.fail("plan_value", "Source page ranges must be ascending and disjoint.")
			}
			if previous.Last < math.MaxInt && first == previous.Last+1 {
				previous.Last = last
				continue
			}
		}
		ranges = append(ranges, packet.PageRange{First: first, Last: last})
	}
	if len(ranges) == 0 {
		return nil, p.fail("plan_value", "Source pages selection must not be empty.")
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
		return 0, &fault{code: "plan_null", message: "null is not allowed.", pointer: string(p.d.StackPointer()), offset: start}
	}
	if t.Kind() != '0' {
		return 0, p.fail("plan_type", "count must be a JSON number.")
	}
	s := t.String()
	if strings.ContainsAny(s, ".eE") {
		return 0, p.fail("plan_value", "count must use an integer token, such as 3 (not 3.0 or 3e0).")
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n > packet.MaxGeneratedPages {
		return 0, p.fail("plan_limit", "count exceeds 100000.")
	}
	if n < 1 {
		return 0, p.fail("plan_value", "count must be at least 1.")
	}
	return int(n), nil
}

func (p *decoder) resolveDeclarations(parts []packet.Part, base string, defaults packet.PageStyle) {
	style := func(authored *packet.PageStyle) *packet.PageStyle {
		result := *authored
		if result.Text.Font != "" && result.Text.Font != "default" {
			result.Text.Font, _ = packet.Path(base, result.Text.Font)
		}
		result = packet.Layer(defaults, result)
		return &result
	}
	for index := range parts {
		part := &parts[index]
		switch {
		case part.Source != "":
			part.Source, _ = packet.Path(base, part.Source)
		case part.Page != nil:
			part.Page = style(part.Page)
		case part.Items != nil:
			if part.Divider != nil {
				part.Divider = style(part.Divider)
			}
			scope := base
			if part.Dir != "" {
				scope, _ = packet.Path(base, part.Dir)
			}
			p.resolveDeclarations(part.Items, scope, defaults)
		}
	}
}

func (p *decoder) size(inherit bool) (packet.Size, error) {
	s, err := p.text()
	if err != nil {
		return packet.Size{}, err
	}
	size, err := packet.ParseSize(s, inherit)
	if err != nil {
		return packet.Size{}, p.fail("plan_value", err.Error())
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
		return 0, p.fail("plan_value", err.Error())
	}
	if n < -packet.MaxSide || n > packet.MaxSide || positive && n < 1 {
		return 0, p.fail("plan_value", "Length is outside its point range (positive lengths must be at least 1).")
	}
	return n, nil
}

func (p *decoder) style(titleDivider bool) (packet.PageStyle, error) {
	s := packet.PageStyle{Origins: map[string]string{}}
	err := p.object(func(name string) error {
		var err error
		switch name {
		case "size":
			s.Size, err = p.size(true)
		case "background":
			var value string
			value, err = p.text()
			if err == nil {
				s.Background, err = packet.ParseColor(value, true)
				if err != nil {
					err = p.fail("plan_value", err.Error())
				}
			}
		case "text":
			return p.textStyle(&s, titleDivider)
		default:
			return p.unknown(name)
		}
		if err == nil {
			s.Origins[name] = string(p.d.StackPointer())
		}
		return err
	})
	return s, err
}

func (p *decoder) textStyle(s *packet.PageStyle, titleDivider bool) error {
	return p.object(func(name string) error {
		var err error
		originName := name
		switch name {
		case "value":
			if titleDivider {
				return &fault{code: "plan_value", message: "Divider text.value is supplied by its section title.", pointer: string(p.d.StackPointer()), offset: p.start()}
			}
			s.Text.Value, err = p.text()
			if err == nil && utf8.RuneCountInString(s.Text.Value) > 10000 {
				err = p.fail("plan_limit", "Text exceeds 10000 characters.")
			}
		case "font":
			s.Text.Font, err = p.font()
		case "size":
			originName = "font_size"
			s.Text.Size, err = p.length(true)
		case "color":
			var value string
			value, err = p.text()
			if err == nil {
				var c *packet.Color
				c, err = packet.ParseColor(value, false)
				if err == nil {
					s.Text.Color = *c
				} else {
					err = p.fail("plan_value", err.Error())
				}
			}
		case "x":
			s.Text.X, err = p.length(false)
		case "y":
			s.Text.Y, err = p.length(false)
		case "width":
			s.Text.Width, err = p.length(true)
		case "leading":
			s.Text.Leading, err = p.number()
			if err == nil && (s.Text.Leading < 1 || s.Text.Leading > 10) {
				err = p.fail("plan_value", "leading must be 1–10.")
			}
		case "anchor":
			s.Text.Anchor, err = p.enum("top-left", "top", "top-right", "left", "center", "right", "bottom-left", "bottom", "bottom-right")
		case "align":
			s.Text.Align, err = p.enum("left", "center", "right", "justify")
		case "overflow":
			s.Text.Overflow, err = p.enum("error", "allow")
		default:
			return p.unknown(name)
		}
		if err == nil {
			s.Origins[originName] = string(p.d.StackPointer())
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
	return "", p.fail("plan_value", "Expected one of: "+strings.Join(values, ", "))
}
func (p *decoder) font() (string, error) {
	if p.d.PeekKind() == '"' {
		return p.enum("default")
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
		err = p.fail("plan_type", "font requires a file member.")
	}
	return file, err
}

func (p *decoder) number() (float64, error) {
	var n float64
	err := p.scalar(&n)
	var semantic *json.SemanticError
	if errors.As(err, &semantic) && semantic.JSONKind == '0' {
		return 0, p.fail("plan_value", "Number is outside the supported finite range.")
	}
	return n, err
}
