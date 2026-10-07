// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package typeset_test

import (
	"strings"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/typeset"
)

// terminationDeadline is far above the milliseconds these operations take.
const (
	terminationDeadline       = 10 * time.Second
	defaultFontPostScriptName = "NotoSans-Regular"
)

// mustTerminate runs operation and ends the whole test binary with a panic when it does not return
// within terminationDeadline. A failing assertion could not report a loop that never ends, and the other
// tests of the package would wait on the same loop until the test timeout.
func mustTerminate(name string, operation func()) {
	done := make(chan struct{})

	go func() {
		defer close(done)

		operation()
	}()

	select {
	case <-done:
	case <-time.After(terminationDeadline):
		panic(name + " did not terminate")
	}
}

func TestFontParsingTerminates(t *testing.T) {
	t.Parallel()

	var (
		font *typeset.Font
		err  error
	)

	mustTerminate("parsing the default font", func() { font, err = typeset.LoadDefaultFont() })

	if err != nil || font.PostScriptName() != defaultFontPostScriptName {
		t.Fatalf("%v", err)
	}
}

func TestCorruptCmapGroupTerminates(t *testing.T) {
	t.Parallel()

	const maximumCode = 0xFFFFFFFF

	var err error

	mustTerminate("a cmap group of 2^32 code points", func() {
		_, err = fontWithCmap(t, cmapOf(t, [3]uint32{0, maximumCode, 1}))
	})

	requireErrorMentioning(t, err, "cmap enumerates")
}

func TestLineBreakingTerminates(t *testing.T) {
	t.Parallel()

	var (
		placed *typeset.Placed
		err    error
	)

	font := defaultFont(t)

	mustTerminate("wrapping", func() {
		var shaper typeset.Shaper

		params := baseParams("  111 222   333 444  ")
		params.WrapWidth = 40
		placed, err = shaper.Place(font, params)
	})

	if err != nil || !strings.Contains(lineTexts(placed), "|") {
		t.Errorf("lines %q: %v", lineTexts(placed), err)
	}
}
