// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package plan_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/plan"
)

type (
	// repeatedByte is an endless stream of one byte.
	repeatedByte byte

	// countingReader counts the bytes read through it.
	countingReader struct {
		reader io.Reader
		bytes  atomic.Int64
	}
)

const (
	blankWithText = `{"blank":{"text":{}}}`
	blankWithFont = `{"blank":{"text":{"font":{"file":"f.ttf"}}}}`
)

// nodeLimitPlan builds a plan of count blank items with a text object (3 nodes each) followed by extra plain
// PDF strings (1 node each) and the closing member.
func nodeLimitPlan(blanks, plain int, last string) string {
	text := `{"version":1,"items":[` + strings.Repeat(blankWithText+",", blanks) + strings.Repeat(`"a",`, plain)

	return text + last + "]}"
}

func TestExactNodeLimit(t *testing.T) {
	t.Parallel()

	// 83333 blank items with a text object are 249999 nodes; one more string makes exactly 250000.
	mustDecode(t, nodeLimitPlan(83333, 0, shortStringJSON))

	planErr := codedError(t, errorOf(decodeString(t, nodeLimitPlan(83333, 1, shortStringJSON))), plan.CodeLimitNodes)
	if planErr.Location.Pointer != "/items/83334" || planErr.Stage != plan.StageLimit {
		t.Fatalf(diagnosticFormat, planErr)
	}
}

func TestFontObjectsCountAsNodes(t *testing.T) {
	t.Parallel()

	// An item with a text object holding a font object is 4 nodes: 62500 of them are exactly 250000.
	mustDecode(t, repeated(blankWithFont, 62500))
	wantCode(t, errorOf(decodeString(t, repeated(blankWithFont, 62501))), plan.CodeLimitNodes)
}

func TestNodeLimitInsideStyleObjects(t *testing.T) {
	t.Parallel()

	// The limit can be crossed by the text object or the font object themselves, not only by an item.
	text := codedError(t, errorOf(decodeString(t, nodeLimitPlan(83332, 2, blankWithText))), plan.CodeLimitNodes)
	if !strings.HasSuffix(text.Location.Pointer, "/text") {
		t.Fatalf(pointerFailureFormat, text.Location.Pointer)
	}

	font := codedError(t, errorOf(decodeString(t, nodeLimitPlan(83332, 1, blankWithFont))), plan.CodeLimitNodes)
	if !strings.HasSuffix(font.Location.Pointer, "/font") {
		t.Fatalf(pointerFailureFormat, font.Location.Pointer)
	}

	// The explicit built-in font name is a string, not a node.
	mustDecode(t, nodeLimitPlan(83332, 1, `{"blank":{"text":{"font":"default"}}}`))
}

func TestExactContributionLimit(t *testing.T) {
	t.Parallel()

	mustDecode(t, repeated(shortStringJSON, assembly.MaxContributions))

	planErr := codedError(t, errorOf(decodeString(t, repeated(shortStringJSON, assembly.MaxContributions+1))), plan.CodeLimitContribs)
	if planErr.Location.Pointer != "/items/100000" {
		t.Fatalf(pointerFailureFormat, planErr.Location.Pointer)
	}

	// Groups count their members, not themselves: 25000 groups of 4 PDFs are exactly the limit.
	group := `{"dir":"g","items":["a","b","c","d"]}`

	mustDecode(t, repeated(group, 25000))
	wantCode(t, errorOf(decodeString(t, repeated(group, 25001))), plan.CodeLimitContribs)
}

func TestExactGroupDepthLimit(t *testing.T) {
	t.Parallel()

	mustDecode(t, nestedGroups(assembly.MaxGroupDepth))

	planErr := codedError(t, errorOf(decodeString(t, nestedGroups(assembly.MaxGroupDepth+1))), plan.CodeLimitDepth)
	if strings.Count(planErr.Location.Pointer, rootMember) < assembly.MaxGroupDepth {
		t.Fatalf(pointerFailureFormat, planErr.Location.Pointer)
	}
}

func TestExactGeneratedPageLimit(t *testing.T) {
	t.Parallel()

	mustDecode(t, `{"version":1,"items":[{"blank":{},"count":500000},{"blank":{},"count":499999},{"blank":{}}]}`)

	over := `{"version":1,"items":[{"blank":{},"count":500000},{"blank":{},"count":500000},{"blank":{}}]}`

	planErr := codedError(t, errorOf(decodeString(t, over)), plan.CodeLimitPages)
	if planErr.Location.Pointer != "/items/2" {
		t.Fatalf(pointerFailureFormat, planErr.Location.Pointer)
	}
}

func TestExactBlankCountBoundary(t *testing.T) {
	t.Parallel()

	mustDecode(t, `{"version":1,"items":[{"blank":{},"count":1000000}]}`)
	wantCode(t, errorOf(decodeString(t, `{"version":1,"items":[{"blank":{},"count":1000001}]}`)), plan.CodeOutOfRange)
	wantCode(t, errorOf(decodeString(t, `{"version":1,"items":[{"blank":{},"count":0}]}`)), plan.CodeOutOfRange)
}

// paddedPlan streams the minimal plan and prefix, followed by whitespace to exactly total bytes.
func paddedPlan(prefix string, total int) io.Reader {
	return io.MultiReader(
		strings.NewReader(prefix+minimalPlan),
		io.LimitReader(repeatedByte(' '), int64(total-len(prefix)-len(minimalPlan))),
	)
}

// TestByteLimitIsExact: 64 MiB is accepted, one more byte is rejected, with and without a byte order mark
// (which counts).
func TestByteLimitIsExact(t *testing.T) {
	t.Parallel()

	for _, prefix := range []string{"", "\xef\xbb\xbf"} {
		_, err := plan.Decode(context.Background(), inputFor(), paddedPlan(prefix, plan.MaxPlanBytes))
		if err != nil {
			t.Fatalf("exactly the limit with prefix %q: %v", prefix, err)
		}

		_, err = plan.Decode(context.Background(), inputFor(), paddedPlan(prefix, plan.MaxPlanBytes+1))
		planErr := codedError(t, err, plan.CodeTooLarge)

		if planErr.Location.Offset != plan.MaxPlanBytes || planErr.Stage != plan.StageLimit {
			t.Fatalf(diagnosticFormat, planErr)
		}
	}
}

// Read fills buffer with the byte.
func (r repeatedByte) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}

	buffer[0] = byte(r)

	for filled := 1; filled < len(buffer); filled *= 2 {
		copy(buffer[filled:], buffer[:filled])
	}

	return len(buffer), nil
}

// TestByteLimitInsideAString: a legal document whose single string member pushes it past 64 MiB.
func TestByteLimitInsideAString(t *testing.T) {
	t.Parallel()

	reader := io.MultiReader(strings.NewReader(`{"version":1,"$schema":"`), io.LimitReader(repeatedByte('a'), plan.MaxPlanBytes))

	_, err := plan.Decode(context.Background(), inputFor(), reader)
	wantCode(t, err, plan.CodeTooLarge)
}

func (c *countingReader) Read(buffer []byte) (int, error) {
	length, err := c.reader.Read(buffer)
	c.bytes.Add(int64(length))

	if err != nil {
		return length, io.EOF // the pipe is closed by the test, which is the end of the data
	}

	return length, nil
}

// abortsEarly feeds an endless-looking compact array and checks that the decoder gives up with code long before
// the byte limit: the limits are charged while reading, not after building a huge graph.
func abortsEarly(t *testing.T, entry string, code plan.Code, readBudget int64) {
	t.Helper()

	reader, writer := io.Pipe()

	go func() {
		_, writeErr := io.WriteString(writer, `{"version":1,"items":[`)
		for writeErr == nil {
			_, writeErr = io.WriteString(writer, entry+",")
		}
	}()

	counter := &countingReader{reader: reader}
	_, err := plan.Decode(context.Background(), inputFor(), counter)
	wantCode(t, err, code)

	err = reader.CloseWithError(io.ErrClosedPipe)
	if err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}

	if counter.bytes.Load() > readBudget {
		t.Fatalf("read %d bytes before aborting; budget %d", counter.bytes.Load(), readBudget)
	}
}

func TestHugeShortStringArrayAbortsEarly(t *testing.T) {
	t.Parallel()

	abortsEarly(t, shortStringJSON, plan.CodeLimitContribs, 1<<20)
}

func TestHugeShortBlankArrayHitsTheNodeLimit(t *testing.T) {
	t.Parallel()

	abortsEarly(t, blankWithText, plan.CodeLimitNodes, 8<<20)
}
