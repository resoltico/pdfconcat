// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package plan decodes strict, located version 2 assembly plans.
package plan

import (
	"bytes"
	_ "embed"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/diag"
	"github.com/resoltico/pdfconcat/internal/job"
)

const MaxBytes = 64 << 20

//go:embed plan.schema.json
var Schema []byte

type fault struct {
	code             diag.Code
	message, pointer string
	offset           int64
}

func (e *fault) Error() string { return e.message }

type node struct {
	pointer, source, dir string
	page                 *job.PageStyle
	count                int
	children             []node
}

type decoder struct {
	d                  *jsontext.Decoder
	data               []byte
	bom                int64
	entries, generated int
}

// Decode charges the original input bytes, including an optional leading BOM.
// The caller owns file/stream I/O and cancellation; this function reads no files.
func Decode(data []byte, file, base string) (job.Job, []diag.Diagnostic) {
	p := decoder{data: data}
	if len(data) > MaxBytes {
		return job.Job{}, []diag.Diagnostic{diag.New("plan_too_large", "Plan exceeds 64 MiB.")}
	}
	input := data
	if bytes.HasPrefix(input, []byte{0xef, 0xbb, 0xbf}) {
		p.bom = 3
		input = input[3:]
	}
	p.d = jsontext.NewDecoder(bytes.NewReader(input))
	// A complete, well-formed v1 document has an explicit migration diagnostic,
	// independent of where its old members appear.
	var probe struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(input, &probe); err == nil && probe.Version == 1 {
		location := &fault{code: "plan_version_unsupported", message: "Use version: 2 and rename blank to page (top-level defaults and items).", pointer: "/version"}
		_ = p.object(func(name string) error {
			if name == "version" {
				location.offset = p.start()
			}
			return p.d.SkipValue()
		})
		return job.Job{}, []diag.Diagnostic{p.diagnostic(location, file)}
	}
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
		return job.Job{}, []diag.Diagnostic{p.diagnostic(err, file)}
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
	if err = job.ValidatePath(s); err != nil {
		return "", p.fail("path_invalid", err.Error())
	}
	return s, nil
}
func (p *decoder) unknown(name string) error {
	return p.fail("plan_unknown_member", fmt.Sprintf("Unknown member %q (names are case-sensitive).", name))
}

func (p *decoder) document(base string) (job.Job, error) {
	result := job.Job{BaseDir: base}
	var defaults job.PageStyle
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
				switch token.Kind() {
				case 'n':
					err = &fault{code: "plan_null", message: "null is not allowed.", pointer: "/version", offset: start}
				case '0':
					if token.String() != "2" {
						err = p.fail("plan_version_unsupported", "Use version: 2 and rename blank to page.")
					}
				default:
					err = p.fail("plan_type", "version must be the JSON integer 2.")
				}
			}
		case "output":
			result.Output, err = p.path()
		case "dir":
			dir, err = p.path()
		case "fit_to":
			var size job.Size
			size, err = p.size(false)
			if err == nil {
				result.FitTo = &size
			}
		case "page":
			defaults, err = p.style()
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
		return result, p.fail("plan_version_unsupported", "Missing version; use version: 2 and page instead of blank.")
	}
	if !seenItems {
		return result, p.fail("plan_type", "Required member items is missing.")
	}
	if result.Output != "" {
		result.Output, _ = job.Path(base, result.Output)
	}
	if defaults.Text.Font != "" && defaults.Text.Font != "default" {
		defaults.Text.Font, _ = job.Path(base, defaults.Text.Font)
	}
	defaults = job.Layer(job.Defaults(), defaults)
	scope := base
	if dir != "" {
		scope, _ = job.Path(base, dir)
	}
	p.flatten(nodes, scope, defaults, &result.Parts)
	return result, nil
}

func (p *decoder) items(depth int) ([]node, error) {
	if depth > 64 {
		return nil, p.fail("plan_limit", "Group nesting exceeds 64.")
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
		return nil, p.fail("plan_value", "items must not be empty.")
	}
	_, err := p.d.ReadToken()
	return nodes, err
}

func (p *decoder) item(depth int) (node, error) {
	n := node{count: 1}
	if p.d.PeekKind() == '"' {
		source, err := p.path()
		n.source = source
		n.pointer = string(p.d.StackPointer())
		if err == nil {
			err = p.charge(n)
		}
		return n, err
	}
	// StackPointer identifies an element after its opening token is read.
	seenPage, seenDir, seenItems, seenCount := false, false, false, false
	err := p.object(func(name string) error {
		var err error
		switch name {
		case "page":
			seenPage = true
			var style job.PageStyle
			style, err = p.style()
			n.page = &style
		case "dir":
			seenDir = true
			n.dir, err = p.path()
		case "items":
			seenItems = true
			n.children, err = p.items(depth + 1)
		case "count":
			seenCount = true
			n.count, err = p.count()
		default:
			err = p.unknown(name)
		}
		return err
	})
	n.pointer = string(p.d.StackPointer())
	if err != nil {
		return n, err
	}
	if !(seenPage && !seenDir && !seenItems || !seenPage && !seenCount && seenDir && seenItems) {
		return n, p.fail("plan_item_shape", "An item must have page (and optional count), or dir and items.")
	}
	if seenPage {
		err = p.charge(n)
	}
	return n, err
}

func (p *decoder) charge(n node) error {
	p.entries++
	if p.entries > job.MaxEntries {
		return p.fail("plan_limit", "Plan exceeds 100000 flattened entries.")
	}
	if n.page != nil {
		p.generated += n.count
		if p.generated > job.MaxGeneratedPages {
			return p.fail("plan_limit", "Plan generates more than 100000 pages.")
		}
	}
	return nil
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
	if err != nil || n > job.MaxGeneratedPages {
		return 0, p.fail("plan_limit", "count exceeds 100000.")
	}
	if n < 1 {
		return 0, p.fail("plan_value", "count must be at least 1.")
	}
	return int(n), nil
}

func (p *decoder) flatten(nodes []node, base string, defaults job.PageStyle, parts *[]job.Part) {
	for _, n := range nodes {
		switch {
		case n.source != "":
			source, _ := job.Path(base, n.source)
			*parts = append(*parts, job.Part{Pointer: n.pointer, Source: source})
		case n.page != nil:
			style := *n.page
			if style.Text.Font != "" && style.Text.Font != "default" {
				style.Text.Font, _ = job.Path(base, style.Text.Font)
			}
			style = job.Layer(defaults, style)
			*parts = append(*parts, job.Part{Pointer: n.pointer, Page: &style, Count: n.count})
		default:
			scope, _ := job.Path(base, n.dir)
			p.flatten(n.children, scope, defaults, parts)
		}
	}
}

func (p *decoder) size(inherit bool) (job.Size, error) {
	s, err := p.text()
	if err != nil {
		return job.Size{}, err
	}
	size, err := job.ParseSize(s, inherit)
	if err != nil {
		return job.Size{}, p.fail("plan_value", err.Error())
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
		n, err = job.Length(s)
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
	if n < -job.MaxSide || n > job.MaxSide || positive && n < 1 {
		return 0, p.fail("plan_value", "Length is outside its point range (positive lengths must be at least 1).")
	}
	return n, nil
}

func (p *decoder) style() (job.PageStyle, error) {
	s := job.PageStyle{Origins: map[string]string{}}
	err := p.object(func(name string) error {
		var err error
		switch name {
		case "size":
			s.Size, err = p.size(true)
		case "background":
			var value string
			value, err = p.text()
			if err == nil {
				s.Background, err = job.ParseColor(value, true)
				if err != nil {
					err = p.fail("plan_value", err.Error())
				}
			}
		case "text":
			return p.textStyle(&s)
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

func (p *decoder) textStyle(s *job.PageStyle) error {
	return p.object(func(name string) error {
		var err error
		originName := name
		switch name {
		case "value":
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
				var c *job.Color
				c, err = job.ParseColor(value, false)
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
