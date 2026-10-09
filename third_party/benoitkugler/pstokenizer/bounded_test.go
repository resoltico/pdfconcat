// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Ervins Strauhmanis

package tokenizer

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type cancelAfterChecks struct {
	context.Context
	checks int
}

func (c *cancelAfterChecks) Err() error {
	c.checks++
	if c.checks >= 3 {
		return context.Canceled
	}
	return nil
}
func TestBoundedTokenizerConstructorLookaheadAndCancellation(t *testing.T) {
	limits := Limits{ProgramBytes: 1 << 20, TokenBytes: 1 << 18, Tokens: 100}
	ctx := &cancelAfterChecks{Context: context.Background()}
	if _, err := NewBoundedTokenizer(ctx, []byte("("+strings.Repeat("x", 10000)+")"), limits); !errors.Is(err, context.Canceled) {
		t.Fatalf("constructor failed to cancel during bounded scan: %v", err)
	}
	for _, data := range []string{"q", "/Escaped#20Name", "(escaped\\(string)", "<4142>", "%comment\nq"} {
		tk, err := NewBoundedTokenizer(context.Background(), []byte(data), limits)
		if err != nil {
			t.Fatalf("ordinary bounded input %q: %v", data, err)
		}
		token, err := tk.NextToken()
		if err != nil {
			t.Fatal(err)
		}
		if string(token.Raw) != data[token.Start:token.End] || token.End <= token.Start {
			t.Fatalf("source token span %q: %+v", data, token)
		}
	}
}
func TestBoundedTokenizerTokenProgramAndCountBoundaries(t *testing.T) {
	limits := Limits{ProgramBytes: 16, TokenBytes: 8, Tokens: 2}
	if _, err := NewBoundedTokenizer(context.Background(), []byte("(123456)"), limits); err != nil {
		t.Fatalf("exact token byte boundary: %v", err)
	}
	if _, err := NewBoundedTokenizer(context.Background(), []byte("(1234567)"), limits); err == nil {
		t.Fatal("token byte limit+1 accepted")
	}
	if _, err := NewBoundedTokenizer(context.Background(), []byte(strings.Repeat(" ", 17)), limits); err == nil {
		t.Fatal("program byte limit+1 accepted")
	}
	tk, err := NewBoundedTokenizer(context.Background(), []byte("q Q q"), limits)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tk.NextToken()
	if err == nil {
		_, err = tk.NextToken()
	}
	if err == nil {
		_, err = tk.NextToken()
	}
	if err == nil {
		t.Fatal("token count limit+1 accepted")
	}
}
