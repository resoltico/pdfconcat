// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Ervins Strauhmanis

package parser

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/ascii85"
	"math"
	"os"
	"strings"
	"testing"

	cs "github.com/benoitkugler/pdf/contentstream"
	"github.com/benoitkugler/pdf/model"
)

func boundedLimits() ContentLimits {
	return ContentLimits{ProgramBytes: 1 << 20, TokenBytes: 4096, Tokens: 4096, Objects: 4096, Depth: 16, Operands: 16, Operations: 128, InlineEncodedBytes: 4096, InlineDecodedBytes: 4096, InlineWorkBytes: 1 << 20, ImageDimension: 256, ImagePixels: 65536}
}
func parseBounded(t *testing.T, data []byte, limits ContentLimits) ([]cs.Operation, error) {
	t.Helper()
	p, err := NewBoundedContentParser(context.Background(), data, limits)
	if err != nil {
		return nil, err
	}
	var ops []cs.Operation
	for !p.IsEOF() {
		op, e := p.ParseContentElement(nil)
		if e != nil {
			return nil, e
		}
		ops = append(ops, op)
	}
	return ops, nil
}
func TestBoundedInlineBinaryIsNotTokenized(t *testing.T) {
	payload := []byte(" EI BI /Self Do ")
	data := append([]byte("BI /W 16 /H 1 /BPC 8 /CS /G ID "), payload...)
	data = append(data, []byte(" EI /Actual Do")...)
	ops, err := parseBounded(t, data, boundedLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 2 || !bytes.Equal(ops[0].(cs.OpBeginImage).Image.Content, payload) || ops[1].(cs.OpXObject).XObject != "Actual" {
		t.Fatalf("lost exact binary boundary: %#v", ops)
	}
}
func TestBoundedFlateExpansionAndBoundary(t *testing.T) {
	for _, n := range []int{4096, 4097} {
		var encoded bytes.Buffer
		z := zlib.NewWriter(&encoded)
		_, _ = z.Write(bytes.Repeat([]byte{0}, n))
		_ = z.Close()
		data := append([]byte("BI /W 1 /H 1 /BPC 8 /CS /G /F /Fl ID "), encoded.Bytes()...)
		data = append(data, []byte(" EI /Actual Do")...)
		ops, err := parseBounded(t, data, boundedLimits())
		if n == 4096 {
			if err != nil || len(ops) != 2 {
				t.Fatalf("exact limit: %v", err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "decoded byte limit") {
			t.Fatalf("limit+1 accepted: %v", err)
		}
	}
}
func TestBoundedContainerOperandsTokensAndCancellation(t *testing.T) {
	cases := []struct {
		name, data string
		mutate     func(*ContentLimits)
	}{
		{"depth", "/Span << /K " + strings.Repeat("[", 17) + "0" + strings.Repeat("]", 17) + " >> BDC EMC", nil},
		{"operands", strings.Repeat("1 ", 17) + " scn", nil},
		{"token-bytes", "(" + strings.Repeat("x", 4097) + ") Tj", nil},
		{"operations", strings.Repeat("q Q ", 65), nil},
		{"tokens", strings.Repeat("q Q ", 50), func(l *ContentLimits) { l.Tokens = 99 }},
		{"PostScript", "{ 1 }", nil},
		{"overflow", "BI /W 99999999999999999999 /H 1 /BPC 8 /CS /G ID x EI", nil},
		{"malformed-EOD", "BI /W 1 /H 1 /BPC 8 /CS /G ID x NO", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := boundedLimits()
			if c.mutate != nil {
				c.mutate(&l)
			}
			if _, err := parseBounded(t, []byte(c.data), l); err == nil {
				t.Fatal("negative control accepted")
			}
		})
	}
	data := []byte(strings.Repeat("% comment\n", 10000) + "q Q")
	if _, err := parseBounded(t, data, boundedLimits()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewBoundedContentParser(ctx, data, boundedLimits()); err != context.Canceled {
		t.Fatalf("cancelled parser: %v", err)
	}
}
func TestBoundedNumericOriginalFloat64(t *testing.T) {
	p, err := NewBoundedContentParser(context.Background(), []byte("10000000000000000000000000000000000000000 0 m"), boundedLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ParseContentElement(nil); err != nil {
		t.Fatal(err)
	}
	if p.Span().Numbers[0].Value != 1e40 {
		t.Fatalf("lost original numeric facts: %+v", p.Span())
	}
}

func TestBoundedInlineMaintainedFilterMatrix(t *testing.T) {
	for _, filter := range []model.Name{model.ASCII85, model.ASCIIHex, model.Flate, model.LZW, model.RunLength, model.DCT} {
		t.Run(string(filter), func(t *testing.T) {
			image, err := createImageStream(filter)
			if err != nil {
				t.Fatal(err)
			}
			var content bytes.Buffer
			image.Add(&content)
			// Upstream writer omits the required EOD/EI delimiter; construct the valid trailer explicitly.
			program := append([]byte(nil), content.Bytes()[:content.Len()-2]...)
			program = append(program, []byte(" EI /Actual Do")...)
			operations, err := parseBounded(t, program, boundedLimits())
			if err != nil {
				t.Fatal(err)
			}
			if len(operations) != 2 || !bytes.Equal(operations[0].(cs.OpBeginImage).Image.Content, image.Image.Content) {
				t.Fatal("filter EOD changed encoded bytes or hid following Do")
			}
		})
	}
	encoded, err := os.ReadFile("filters/ccitt/testdata/bw-gopher.ccitt_group3")
	if err != nil {
		t.Fatal(err)
	}
	program := append([]byte("BI /W 153 /H 55 /BPC 1 /CS /G /F /CCF /DP << /Columns 153 /Rows 55 >> ID "), encoded...)
	program = append(program, []byte(" EI /Actual Do")...)
	if operations, err := parseBounded(t, program, boundedLimits()); err != nil || len(operations) != 2 {
		t.Fatalf("CCITT boundary: %v", err)
	}
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	_, _ = writer.Write([]byte{0})
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	var ascii bytes.Buffer
	encoder := ascii85.NewEncoder(&ascii)
	_, _ = encoder.Write(compressed.Bytes())
	if err = encoder.Close(); err != nil {
		t.Fatal(err)
	}
	ascii.WriteString("~>")
	program = append([]byte("BI /W 1 /H 1 /BPC 8 /CS /G /F [/A85 /Fl] ID "), ascii.Bytes()...)
	program = append(program, []byte(" EI /Actual Do")...)
	if operations, err := parseBounded(t, program, boundedLimits()); err != nil || len(operations) != 2 {
		t.Fatalf("first-filter multifilter boundary: %v", err)
	}
}
func TestBoundedInlineAggregateWorkAndRowOverflow(t *testing.T) {
	limits := boundedLimits()
	limits.InlineWorkBytes = 1
	if _, err := parseBounded(t, []byte("BI /W 1 /H 1 /BPC 8 /CS /G ID x EI"), limits); err != nil {
		t.Fatal(err)
	}
	if _, err := parseBounded(t, []byte("BI /W 1 /H 1 /BPC 8 /CS /G ID x EI BI /W 1 /H 1 /BPC 8 /CS /G ID x EI"), limits); err == nil {
		t.Fatal("aggregate work limit+1 accepted")
	}
	limits = boundedLimits()
	limits.ImageDimension = math.MaxInt
	limits.ImagePixels = math.MaxInt
	limits.InlineDecodedBytes = math.MaxInt
	if _, err := parseBounded(t, []byte("BI /W 1152921504606846976 /H 1 /BPC 16 /CS /RGB ID x EI"), limits); err == nil || !strings.Contains(err.Error(), "arithmetic overflow") {
		t.Fatalf("checked row product: %v", err)
	}
}
