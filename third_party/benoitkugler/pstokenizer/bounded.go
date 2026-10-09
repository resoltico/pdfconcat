// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Ervins Strauhmanis

package tokenizer

import (
	"context"
	"errors"
)

// Limits bounds byte-slice tokenization before scans and allocations.
// All limits must be positive. Reader-driven input is deliberately unavailable.
type Limits struct{ ProgramBytes, TokenBytes, Tokens int }
type boundedTokenizer struct {
	ctx           context.Context
	limits        Limits
	start, tokens int
	err           error
}

// NewBoundedTokenizer constructs an incremental PDF tokenizer on checked immutable bytes.
func NewBoundedTokenizer(ctx context.Context, data []byte, limits Limits) (*Tokenizer, error) {
	if ctx == nil || limits.ProgramBytes <= 0 || limits.TokenBytes <= 0 || limits.Tokens <= 0 {
		return nil, errors.New("invalid tokenizer limits or context")
	}
	if len(data) > limits.ProgramBytes {
		return nil, errors.New("program byte limit exceeded")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tk := &Tokenizer{data: data, bounded: &boundedTokenizer{ctx: ctx, limits: limits}}
	tk.SetPosition(0)
	if tk.bounded.err != nil {
		return nil, tk.bounded.err
	}
	return tk, nil
}
func (b *boundedTokenizer) allowRead(pos int) bool {
	if b.err != nil {
		return false
	}
	if pos-b.start >= b.limits.TokenBytes {
		b.err = errors.New("token byte limit exceeded")
		return false
	}
	if pos&1023 == 0 {
		b.err = b.ctx.Err()
	}
	return b.err == nil
}

// SkipRawBytes advances over binary bytes without tokenizing image payload.
// Call SetPosition after the maintained filter has established the actual EOD boundary.
func (tk *Tokenizer) SkipRawBytes(n int) ([]byte, error) {
	if n < 0 || n > len(tk.data)-tk.currentPos {
		return nil, errors.New("binary data length exceeds input")
	}
	out := tk.data[tk.currentPos : tk.currentPos+n]
	tk.currentPos += n
	tk.pos = tk.currentPos
	tk.nextPos = tk.currentPos
	tk.aToken = Token{Kind: EOF}
	tk.aaToken = tk.aToken
	tk.aError = nil
	tk.aaError = nil
	return out, nil
}

// TokenCount reports actual bounded token discoveries, including maintained lookahead.
func (tk *Tokenizer) TokenCount() int {
	if tk.bounded == nil {
		return 0
	}
	return tk.bounded.tokens
}
