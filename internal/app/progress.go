// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

// progressStep limits redraws: at most about 100 updates per phase.
const progressStep = 100

// progress renders a single self-overwriting status line; a nil writer disables it.
type progress struct {
	out   io.Writer
	label string
	total int

	mu    sync.Mutex
	done  int
	drawn int // Number of completed units at the last redraw.
	width int // Widest line drawn so far, in characters.
}

func newProgress(out io.Writer, label string, total int) *progress {
	return &progress{out: out, label: label, total: total}
}

// advance records one completed unit and redraws at most about progressSteps times.
func (p *progress) advance() {
	if p.out == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.done++

	step := max(p.total/progressStep, 1)
	if p.done == p.total || p.done-p.drawn >= step {
		p.drawn = p.done
		p.draw(fmt.Sprintf("%s %d/%d", p.label, p.done, p.total))
	}
}

// finish erases the status line.
func (p *progress) finish() {
	if p.out == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.width > 0 {
		p.draw(strings.Repeat(" ", p.width))
		p.draw("")
	}
}

// draw rewrites the status line. A write failure disables further progress output,
// since a broken progress stream must never fail the assembly itself.
func (p *progress) draw(text string) {
	if p.out == nil {
		return
	}

	p.width = max(p.width, len(text))

	_, err := fmt.Fprintf(p.out, "\r%s", text)
	if err != nil {
		p.out = nil
	}
}
