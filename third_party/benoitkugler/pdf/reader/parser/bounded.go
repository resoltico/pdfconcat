// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Ervins Strauhmanis

package parser

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"

	tkn "github.com/benoitkugler/pstokenizer"
)

// ContentLimits bounds incremental content parsing, including binary inline-image work.
// Every limit is positive; callers choose finite policy limits before parsing starts.
type ContentLimits struct {
	InlineWorkBytes                                                        int
	ProgramBytes, TokenBytes, Tokens, Objects, Depth, Operands, Operations int
	InlineEncodedBytes, InlineDecodedBytes, ImageDimension, ImagePixels    int
}

// Number records an original numeric token before float32 model conversion.
type Number struct {
	Value      float64
	Start, End int
	Spelling   string
}

// ContentSpan describes the most recently parsed operation and its original numeric tokens.
type ContentSpan struct {
	Start, End int
	Numbers    []Number
	Tokens     []tkn.Token
}
type boundedParser struct {
	components                 func(string) (int, error)
	ctx                        context.Context
	limits                     ContentLimits
	depth, objects, operations int
	span                       ContentSpan
	inlineWork                 int
}

// NewBoundedContentParser preserves the maintained grammar and incrementally parses checked bytes.
func NewBoundedContentParser(ctx context.Context, data []byte, limits ContentLimits) (*Parser, error) {
	if limits.Objects <= 0 || limits.Depth <= 0 || limits.Operands <= 0 || limits.Operations <= 0 || limits.InlineEncodedBytes <= 0 || limits.InlineDecodedBytes <= 0 || limits.InlineWorkBytes <= 0 || limits.ImageDimension <= 0 || limits.ImagePixels <= 0 {
		return nil, errors.New("invalid content parser limits")
	}
	tk, err := tkn.NewBoundedTokenizer(ctx, data, tkn.Limits{ProgramBytes: limits.ProgramBytes, TokenBytes: limits.TokenBytes, Tokens: limits.Tokens})
	if err != nil {
		return nil, err
	}
	p := NewParserFromTokenizer(tk)
	p.ContentStreamMode = true
	p.bounded = &boundedParser{ctx: ctx, limits: limits}
	return p, nil
}

// IsEOF reports the maintained tokenizer's end condition. ParseContentElement still checks errors.
func (p *Parser) IsEOF() bool { return p.tokens.IsEOF() }

// Span returns original numeric spellings and positions without reserializing the program.
func (p *Parser) Span() ContentSpan { return p.bounded.span }
func (b *boundedParser) enter() error {
	if err := b.ctx.Err(); err != nil {
		return err
	}
	if b.depth >= b.limits.Depth {
		return errors.New("container depth limit exceeded")
	}
	if b.objects >= b.limits.Objects {
		return errors.New("object count limit exceeded")
	}
	b.depth++
	b.objects++
	return nil
}
func (b *boundedParser) begin(start int) error {
	if b.operations >= b.limits.Operations {
		return errors.New("operation count limit exceeded")
	}
	b.operations++
	b.span = ContentSpan{Start: start}
	return b.ctx.Err()
}
func (b *boundedParser) token(t tkn.Token) error {
	b.span.Tokens = append(b.span.Tokens, t)
	if t.Kind == tkn.StartProc || t.Kind == tkn.EndProc || t.Kind == tkn.CharString {
		return errors.New("PostScript token is not PDF content")
	}
	if !t.IsNumber() {
		return nil
	}
	spelling := strings.TrimSpace(string(t.Raw))
	if strings.ContainsAny(spelling, "eE#") {
		return errors.New("nondecimal numeric token is not PDF content")
	}
	v, err := strconv.ParseFloat(spelling, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return errors.New("numeric token is not finite float64")
	}
	b.span.Numbers = append(b.span.Numbers, Number{Value: v, Start: t.Start, End: t.End, Spelling: spelling})
	return nil
}

// SetInlineColorResolver resolves only the used inline color-space component count in the original resource scope.
func (p *Parser) SetInlineColorResolver(resolve func(string) (int, error)) {
	p.bounded.components = resolve
}

// ContentStatistics records measured parser work, not viewer/rasterizer work.
type ContentStatistics struct{ Tokens, Objects, Operations, InlineWorkBytes int }

// Statistics returns current measured work under the named bounds.
func (p *Parser) Statistics() ContentStatistics {
	return ContentStatistics{Tokens: p.tokens.TokenCount(), Objects: p.bounded.objects, Operations: p.bounded.operations, InlineWorkBytes: p.bounded.inlineWork}
}
