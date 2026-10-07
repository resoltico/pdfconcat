// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package genpage_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/genpage"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

func TestSequentialProducerSharesFontBeforeOptimizationAndPreservesText(t *testing.T) {
	t.Parallel()

	for _, count := range []int{100, 1000} {
		t.Run(strconv.Itoa(count), func(t *testing.T) { t.Parallel(); checkProducedFont(t, count) })
	}
}

func checkProducedFont(t *testing.T, count int) {
	t.Helper()
	font := defaultFont(t)

	var (
		shaper typeset.Shaper
		data   bytes.Buffer
	)

	produced := 0

	_, err := genpage.WriteProduced(t.Context(), &data, count, []*typeset.Font{font, font}, func(index int) (genpage.Page, error) {
		produced++
		text := fmt.Sprintf("PAGE %04d\nx́ q̄", index)
		block, err := shaper.Place(font, params(text))

		return genpage.Page{Width: 300, Height: 200, Text: block}, err
	})
	if err != nil {
		t.Fatal(err)
	}

	if produced != count || bytes.Count(data.Bytes(), []byte(fontFileReference)) != 1 {
		t.Fatal("sequential producer repeated shaping or embedded fonts")
	}

	path := filepath.Join(t.TempDir(), "produced.pdf")
	if writeErr := os.WriteFile(path, data.Bytes(), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	if got := qpdfCheck(t, path); got != count {
		t.Fatalf("qpdf pages %d want %d", got, count)
	}

	t.Logf("before optimization: pages=%d produced=%d FontFile2_references=1 resource_bytes=%d", count, produced, data.Len())

	pages := extractRaw(t, path)

	for index := range count {
		want := fmt.Sprintf("PAGE %04d x́ q̄", index)
		if strings.Join(strings.Fields(pages[index]), " ") != want {
			t.Fatalf("producer lost text on page %d", index+1)
		}
	}
}

func TestSequentialProducerRejectsInvalidInputsAndPropagatesFailure(t *testing.T) {
	t.Parallel()
	font := defaultFont(t)
	good := textPage(t, font, "valid")
	noFont := good
	noFont.Text = &typeset.Placed{Size: 12, Lines: good.Text.Lines}
	badSize := good
	block := *good.Text
	block.Size = -1
	badSize.Text = &block
	outside := good

	outside.Width = 0
	for _, row := range []struct {
		want  error
		fonts []*typeset.Font
		page  genpage.Page
		count int
	}{
		{count: 0, fonts: nil, page: good, want: genpage.ErrNoPages},
		{count: genpage.MaxPages + 1, fonts: nil, page: good, want: genpage.ErrTooManyPages},
		{count: 1, fonts: []*typeset.Font{nil}, page: good, want: genpage.ErrInvalidPage},
		{count: 1, fonts: nil, page: good, want: genpage.ErrInvalidPage},
		{count: 1, fonts: []*typeset.Font{font}, page: noFont, want: genpage.ErrInvalidPage},
		{count: 1, fonts: []*typeset.Font{font}, page: badSize, want: genpage.ErrInvalidPage},
		{count: 1, fonts: []*typeset.Font{font}, page: outside, want: genpage.ErrInvalidPage},
	} {
		_, err := genpage.WriteProduced(
			t.Context(),
			io.Discard,
			row.count,
			row.fonts,
			func(int) (genpage.Page, error) { return row.page, nil },
		)
		if !errors.Is(err, row.want) {
			t.Fatalf("producer domain failure: %v want %v", err, row.want)
		}
	}

	_, err := genpage.WriteProduced(
		t.Context(),
		io.Discard,
		1,
		nil,
		func(int) (genpage.Page, error) { return genpage.Page{}, io.ErrUnexpectedEOF },
	)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("producer failure lost: %v", err)
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = genpage.WriteProduced(canceled, io.Discard, 1, nil, func(int) (genpage.Page, error) {
		t.Fatal("producer called after cancellation")
		return genpage.Page{}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("producer cancellation lost: %v", err)
	}
}
