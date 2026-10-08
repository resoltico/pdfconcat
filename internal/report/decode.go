// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
)

type (
	// Limits bound the decoding of an untrusted saved report.
	Limits struct {
		MaxBytes   int64
		MaxNesting int
		MaxNodes   int64
	}

	// shape describes one object kind of the format for the pre-scan: required and nullable members, and
	// the kind of each member's value. A kind written "[name]" is an array of name objects.
	shape struct {
		children map[string]string
		required []string
		nullable []string
	}

	// frame is one open object or array of the pre-scan.
	frame struct {
		shape      *shape
		element    string // kind of the elements of an array frame
		member     string // the member whose value is being read
		start      int64  // offset where the container began
		seen       uint32 // bit i is set when required[i] has appeared
		isArray    bool
		expectName bool
	}

	// scan is the state of one pre-scan: a single token pass that enforces syntax, nesting, and node limits
	// and the required-member and null rules, before any typed value is materialized.
	scan struct {
		decoder *jsontext.Decoder
		shapes  map[string]shape
		pending *Error // first shape fault; reported unless the version or kind says the file is not ours
		version string // the top-level format_version token, "" when absent
		kind    string // the top-level kind string, when it is one
		name    string
		stack   []frame
		nodes   int64
		limits  Limits
		bom     int64
	}
)

const (
	memberCommand   = "command"
	memberRecovery  = "recovery"
	memberProducer  = "producer"
	memberBounds    = "bounds"
	memberInkBounds = "ink_bounds"
	shapeRect       = "rect"
	byteOrderMark   = "\xef\xbb\xbf"
	readChunk       = 64 << 10
	cancelInterval  = 0xFFF
	kindSeparators  = " \t\r\n,:"
	versionMember   = "format_version"
	kindMember      = "kind"
	maxRequiredBits = 32

	phaseLayout       = "layout"
	memberCounts      = "counts"
	memberLocation    = "location"
	memberOrigin      = "origin"
	memberPhases      = "phases"
	memberPublication = "publication"
	memberRange       = "range"
	memberSize        = "size"
	memberText        = "text"
	memberWidth       = "width"
)

// DefaultLimits are the format's limits: 256 MiB, nesting 64, 2,000,000 decoded structural nodes.
func DefaultLimits() Limits {
	return Limits{MaxBytes: MaxReportBytes, MaxNesting: MaxNesting, MaxNodes: MaxNodes}
}

// Decode reads a saved report as an untrusted file with the default limits.
func Decode(ctx context.Context, name string, reader io.Reader) (*Report, error) {
	return DecodeLimited(ctx, name, reader, DefaultLimits())
}

// DecodeLimited reads one saved report from reader. The input is rejected, with a located *Error, when it is
// larger than the byte limit, not exactly one strict JSON object, nested or populated beyond the limits,
// an unsupported version, or not shaped like a report: unknown, duplicate, missing, or null members,
// wrong types, dangling references, duplicate IDs, impossible ranges. The first fault is reported.
func DecodeLimited(ctx context.Context, name string, reader io.Reader, limits Limits) (*Report, error) {
	data, err := readLimited(ctx, name, reader, limits.MaxBytes)
	if err != nil {
		return nil, err
	}

	return decodeBytes(ctx, name, data, limits)
}

func readLimited(ctx context.Context, name string, reader io.Reader, limit int64) ([]byte, error) {
	var data []byte

	chunk := make([]byte, readChunk)

	for {
		err := ctx.Err()
		if err != nil {
			return nil, newError(
				StatusInterrupted,
				StageRead,
				CodeInterrupted,
				&Location{File: name},
				err,
				"reading the report was interrupted",
			)
		}

		n, err := reader.Read(chunk)
		data = append(data, chunk[:n]...)

		if int64(len(data)) > limit {
			return nil, newError(StatusInvalid, StageLimit, CodeTooLarge, &Location{File: name}, nil,
				"the report is larger than the %d byte limit", limit)
		}

		if errors.Is(err, io.EOF) {
			return data, nil
		}

		if err != nil {
			return nil, newError(
				StatusFailed,
				StageRead,
				CodeReadFailed,
				&Location{File: name},
				err,
				"cannot read the report: %v",
				err,
			)
		}
	}
}

func decodeBytes(ctx context.Context, name string, data []byte, limits Limits) (*Report, error) {
	bom := int64(0)

	if bytes.HasPrefix(data, []byte(byteOrderMark)) {
		data = data[len(byteOrderMark):]
		bom = int64(len(byteOrderMark))
	}

	prescan := &scan{
		decoder: jsontext.NewDecoder(bytes.NewReader(data), jsontext.AllowDuplicateNames(false), jsontext.AllowInvalidUTF8(false)),
		shapes:  shapes(),
		limits:  limits,
		bom:     bom,
		name:    name,
	}

	err := prescan.run(ctx, data)
	if err != nil {
		return nil, err
	}

	var report Report

	err = json.Unmarshal(data, &report, json.RejectUnknownMembers(true))
	if err != nil {
		return nil, prescan.unmarshalError(data, err)
	}

	err = report.Validate()
	if err != nil {
		found, _ := AsError(err)

		return nil, prescan.locate(data, found.Diagnostic.Location.Pointer, found)
	}

	return &report, nil
}

// at builds a location for an absolute-offset-free data offset.
func (s *scan) at(data []byte, offset int64, pointer string) *Location {
	line, column := lineColumn(data, offset)
	absolute := offset + s.bom

	return &Location{File: s.name, Offset: &absolute, Line: line, Column: column, Pointer: pointer}
}

func lineColumn(data []byte, offset int64) (int, int) {
	offset = min(offset, int64(len(data)))
	before := data[:offset]
	line := bytes.Count(before, []byte("\n")) + 1
	column := int(offset) - bytes.LastIndexByte(before, '\n')

	return line, column
}

func (s *scan) failAt(data []byte, stage Stage, code Code, offset int64, pointer string, cause error, format string, args ...any) *Error {
	return newError(StatusInvalid, stage, code, s.at(data, offset, pointer), cause, format, args...)
}

// locate gives a shape fault found after unmarshaling the offset of its pointer, when the pointer exists.
func (s *scan) locate(data []byte, pointer string, found *Error) *Error {
	offset, ok := offsetOf(data, pointer)
	if !ok {
		found.Diagnostic.Location = &Location{File: s.name, Pointer: pointer}

		return found
	}

	found.Diagnostic.Location = s.at(data, offset, pointer)

	return found
}

// offsetOf finds where the member or value at pointer starts, by one more token pass over data.
func offsetOf(data []byte, pointer string) (int64, bool) {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))

	for {
		before := decoder.InputOffset()

		_, err := decoder.ReadToken()
		if err != nil {
			return 0, false
		}

		if string(decoder.StackPointer()) == pointer {
			for before < int64(len(data)) && strings.IndexByte(kindSeparators, data[before]) >= 0 {
				before++
			}

			return before, true
		}
	}
}

func (s *scan) unmarshalError(data []byte, err error) *Error {
	pointer, offset, code := "", int64(0), CodeWrongType

	semantic, ok := errors.AsType[*json.SemanticError](err)
	if ok {
		pointer, offset = string(semantic.JSONPointer), semantic.ByteOffset
	}

	if errors.Is(err, json.ErrUnknownName) {
		code = CodeUnknownMember
	}

	return s.failAt(data, StageShape, code, offset, pointer, err, "%v", err)
}

func (s *scan) syntaxError(data []byte, err error) *Error {
	offset, pointer := int64(0), ""

	syntactic, ok := errors.AsType[*jsontext.SyntacticError](err)
	if ok {
		offset, pointer = syntactic.ByteOffset, string(syntactic.JSONPointer)
	}

	code := CodeSyntax
	if errors.Is(err, jsontext.ErrDuplicateName) {
		code = CodeDuplicateMember
	}

	return s.failAt(data, StageSyntax, code, offset, pointer, err, "%v", err)
}

func (s *scan) run(ctx context.Context, data []byte) error {
	for tokens := 0; ; tokens++ {
		if tokens&cancelInterval == 0 {
			err := ctx.Err()
			if err != nil {
				return newError(
					StatusInterrupted,
					StageRead,
					CodeInterrupted,
					&Location{File: s.name},
					err,
					"decoding the report was interrupted",
				)
			}
		}

		start := s.decoder.InputOffset()

		token, err := s.decoder.ReadToken()
		if err != nil {
			return s.readFailure(data, err)
		}

		err = s.token(data, token, firstByte(data, start))
		if err != nil {
			return err
		}

		if len(s.stack) == 0 {
			break
		}
	}

	return s.finish(data)
}

// firstByte is the offset of the token's first byte: after the whitespace and separators that precede it.
func firstByte(data []byte, from int64) int64 {
	for from < int64(len(data)) && strings.IndexByte(kindSeparators, data[from]) >= 0 {
		from++
	}

	return from
}

func (s *scan) readFailure(data []byte, err error) error {
	if errors.Is(err, io.EOF) && s.nodes == 0 {
		return s.failAt(data, StageSyntax, CodeEmpty, 0, "", nil, "the report is empty; expected one JSON object")
	}

	return s.syntaxError(data, err)
}

// finish rejects trailing data, then decides between the version, kind, and first shape fault.
func (s *scan) finish(data []byte) error {
	_, err := s.decoder.ReadToken()
	if !errors.Is(err, io.EOF) {
		return s.failAt(data, StageSyntax, CodeTrailingData, s.decoder.InputOffset(), "", err,
			"data other than whitespace follows the report object")
	}

	switch {
	case s.version == "":
		return invalid(
			StageShape,
			CodeUnsupportedVersion,
			&Location{File: s.name},
			"A complete format_version 2 report is required. Preserve historical JSON; use a fresh check with "+
				"the current job and a NEW report target.",
		)
	case s.version != "" && s.version != strconv.Itoa(Version):
		return s.locate(data, "/"+versionMember, invalid(
			StageShape,
			CodeUnsupportedVersion,
			nil,
			"This build reads format_version %d only. Preserve historical JSON; use a fresh check with "+
				"the current job and a NEW report target.",
			Version,
		))
	case s.kind != "" && s.kind != KindReport:
		return s.locate(data, "/"+kindMember, invalid(StageShape, CodeWrongKind, nil,
			"expected a complete report (kind %q), not a %s; save the full report with --report and query that", KindReport, s.kind))
	case s.pending != nil:
		return s.pending
	default:
		return nil
	}
}

func (s *scan) count(data []byte, offset int64) error {
	s.nodes++

	if s.nodes > s.limits.MaxNodes {
		return s.failAt(data, StageLimit, CodeLimitNodes, offset, "", nil,
			"the report holds more than %d JSON values", s.limits.MaxNodes)
	}

	return nil
}

func (s *scan) token(data []byte, token jsontext.Token, offset int64) error {
	kind := token.Kind()

	if len(s.stack) == 0 && kind != jsontext.KindBeginObject {
		return s.failAt(data, StageShape, CodeNotObject, offset, "", nil, "a report is one JSON object")
	}

	if kind == jsontext.KindEndObject || kind == jsontext.KindEndArray {
		return s.closeContainer(data)
	}

	if top := s.top(); top != nil && !top.isArray && top.expectName {
		top.expectName = false
		top.member = token.String()
		markSeen(top)

		return nil
	}

	err := s.count(data, offset)
	if err != nil {
		return err
	}

	if kind == jsontext.KindBeginObject || kind == jsontext.KindBeginArray {
		return s.open(data, kind, offset)
	}

	if kind == jsontext.KindNull {
		s.nullValue(data, offset)
	} else {
		s.scalar(token)
	}

	s.valueDone()

	return nil
}

func (s *scan) top() *frame {
	if len(s.stack) == 0 {
		return nil
	}

	return &s.stack[len(s.stack)-1]
}

func markSeen(f *frame) {
	if f.shape == nil {
		return
	}

	if index := slices.Index(f.shape.required, f.member); index >= 0 && index < maxRequiredBits {
		f.seen |= 1 << index
	}
}

// childKind is the shape kind of the value about to open below the top frame ("" when unshaped).
func (s *scan) childKind() string {
	top := s.top()

	switch {
	case top == nil:
		return KindReport
	case top.isArray:
		return top.element
	case top.shape == nil:
		return ""
	default:
		return top.shape.children[top.member]
	}
}

func (s *scan) open(data []byte, kind jsontext.Kind, offset int64) error {
	child := s.childKind()
	next := frame{start: offset, expectName: kind == jsontext.KindBeginObject, isArray: kind == jsontext.KindBeginArray}

	if kind == jsontext.KindBeginArray {
		next.element = strings.TrimSuffix(strings.TrimPrefix(child, "["), "]")
	} else if found, ok := s.shapes[child]; ok {
		next.shape = &found
	}

	s.stack = append(s.stack, next)

	if len(s.stack) > s.limits.MaxNesting {
		return s.failAt(data, StageLimit, CodeLimitDepth, offset, string(s.decoder.StackPointer()), nil,
			"the report nests deeper than %d levels", s.limits.MaxNesting)
	}

	return nil
}

func (s *scan) closeContainer(data []byte) error {
	top := s.stack[len(s.stack)-1]
	s.stack = s.stack[:len(s.stack)-1]

	if top.shape != nil && s.pending == nil {
		for index, name := range top.shape.required {
			if top.seen&(1<<index) == 0 {
				s.pending = s.failAt(data, StageShape, CodeMissingMember, top.start, string(s.decoder.StackPointer()), nil,
					"the object lacks the required member %q", name)

				break
			}
		}
	}

	s.valueDone()

	return nil
}

// valueDone marks the end of a value: the enclosing object expects the next member name.
func (s *scan) valueDone() {
	if top := s.top(); top != nil && !top.isArray {
		top.expectName = true
	}
}

// nullValue handles null values inside an open container; token rejects scalar roots before calling it.
func (s *scan) nullValue(data []byte, offset int64) {
	top := s.top()
	// Unknown containers are diagnosed by typed decoding at their ancestor member.
	if top.shape == nil && !top.isArray {
		return
	}

	allowed := !top.isArray && slices.Contains(top.shape.nullable, top.member)
	if !allowed && s.pending == nil {
		s.pending = s.failAt(data, StageShape, CodeNull, offset, string(s.decoder.StackPointer()), nil,
			"null is not allowed here; omit optional members instead")
	}
}

// scalar records the top-level version and kind tokens.
func (s *scan) scalar(token jsontext.Token) {
	top := s.top()
	if len(s.stack) != 1 || top.isArray {
		return
	}

	switch top.member {
	case versionMember:
		s.version = token.String()
	case kindMember:
		if token.Kind() == jsontext.KindString {
			s.kind = token.String()
		}
	default:
	}
}

// shapes returns the table of the format's object kinds.
func shapes() map[string]shape {
	return map[string]shape{
		KindReport: {
			required: []string{
				versionMember, "attempt_id", kindMember, "status", memberCommand, memberPhases, memberCounts, memberPublication,
				ViewDiagnostics, ViewParts, "sources", "fonts", "styles", "diagnostic_count", "error_count", "warning_count",
			},
			children: map[string]string{
				memberProducer:    memberProducer,
				memberPhases:      memberPhases,
				memberCounts:      memberCounts,
				memberPublication: memberPublication,
				ViewDiagnostics:   "[diagnostic]",
				ViewParts:         "[part]",
				"sources":         "[source]",
				"fonts":           "[font]",
				"styles":          "[style]",
			},
		},
		memberProducer: {required: []string{"tool", "version", "commit", "go", "platform"}},
		memberPhases:   {required: []string{"instructions", "input_inspection", phaseLayout, "output_verification"}},
		memberCounts: {
			required: []string{"source_pages", "generated_pages", "total_pages"},
			nullable: []string{"source_pages", "generated_pages", "total_pages"},
		},
		memberPublication: {required: []string{"report_status", "published"}},
		"diagnostic": {
			required: []string{"severity", "stage", "code", "message"},
			children: map[string]string{memberLocation: memberLocation, memberRecovery: memberRecovery, "consumers": "[string]"},
		},
		memberRecovery: {required: []string{"action", memberCommand}, children: map[string]string{memberLocation: memberLocation}},
		memberLocation: {required: []string{"file"}},
		"part": {
			required: []string{"id", "kind", memberOrigin, memberRange, "pages"},
			nullable: []string{memberRange, "pages"},
			children: map[string]string{memberOrigin: "position", memberRange: memberRange},
		},
		"position":  {required: []string{"file"}},
		memberRange: {required: []string{"start", "end"}},
		"source":    {required: []string{"path", "bytes"}, nullable: []string{"bytes"}},
		"font":      {required: []string{"digest", "name"}},
		"style": {
			required: []string{"background", memberSize},
			children: map[string]string{memberSize: memberSize, memberText: memberText},
		},
		memberSize:     {required: []string{memberOrigin, memberWidth, "height"}},
		memberText:     textShape(),
		"text_finding": {required: []string{"kind", "detail", "line"}},
		shapeRect:      {required: []string{"x", "y", memberWidth, "height"}},
	}
}

func textShape() shape {
	return shape{
		required: []string{
			"value",
			"color",
			"anchor",
			"align",
			"overflow",
			memberSize,
			"x",
			"y",
			memberWidth,
			"leading",
			memberBounds,
			memberInkBounds,
			"font",
		},
		nullable: []string{memberBounds, memberInkBounds},
		children: map[string]string{memberBounds: shapeRect, memberInkBounds: shapeRect, "findings": "[text_finding]"},
	}
}
