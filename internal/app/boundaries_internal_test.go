// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestAMessageOfExactlyTheBoundIsNotCut(t *testing.T) {
	t.Parallel()

	for _, length := range []int{maxMessageRunes - 1, maxMessageRunes} {
		text := strings.Repeat("ā", length)
		if got := boundedMessage(text); got != text {
			t.Errorf("a message of %d characters was changed to %d characters", length, len([]rune(got)))
		}
	}

	over := strings.Repeat("ā", maxMessageRunes+1)
	if got := boundedMessage(over); got != strings.Repeat("ā", maxMessageRunes)+"…" {
		t.Errorf("a message one character over the bound: %d characters", len([]rune(got)))
	}
}

func TestProgressRedrawsExactlyWhenTheIntervalHasPassed(t *testing.T) {
	t.Parallel()

	var (
		out   bytes.Buffer
		clock = time.Unix(0, 0)
	)

	progress := newProgress(&out, func() time.Time { return clock })
	progress.enter(stageInspect)
	out.Reset()

	clock = clock.Add(minRedrawInterval - time.Nanosecond)

	progress.step(1, 2)

	if out.Len() != 0 {
		t.Errorf("a redraw just before the interval: %q", out.String())
	}

	clock = clock.Add(time.Nanosecond)

	progress.step(2, 2)

	if got, want := out.String(), "\rpdfconcat: inspect 2/2\rpdfconcat: inspect 2/2"; got != want {
		t.Errorf("a redraw exactly at the interval: %q, want %q", got, want)
	}
}

func TestProgressPadsOverTheLongerLineItReplaces(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	progress := newProgress(&out, time.Now)

	// The first line has nothing to cover. The second is shorter than the widest line so far and covers the rest.
	progress.enter(stageInspect)
	progress.enter(stageRender)
	progress.enter(stageMerge)

	want := "\rpdfconcat: inspect\rpdfconcat: inspect" +
		"\rpdfconcat: render \rpdfconcat: render" +
		"\rpdfconcat: merge  \rpdfconcat: merge"
	if out.String() != want {
		t.Errorf("drawn %q, want %q", out.String(), want)
	}
}
