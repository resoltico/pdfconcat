// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// progress writes one self-overwriting status line to an interactive terminal. It never writes to
// standard output, and a nil writer disables it. A write failure disables it as well: a broken terminal
// must not fail the command.
type progress struct {
	out      io.Writer
	now      func() time.Time
	last     time.Time
	stage    string
	mutex    sync.Mutex
	width    int
	redraws  int
	disabled bool
}

const (
	// minRedrawInterval is the shortest time between two redraws of a stage's counter.
	minRedrawInterval = 100 * time.Millisecond
	// maxRedraws bounds the counter redraws of a whole run; stage transitions are not counted.
	maxRedraws = 600

	// The stage names shown on the progress line.
	stagePrepare = "prepare"
	stageInspect = "inspect"
	stageRender  = "render"
	stageMerge   = "merge"
	stagePersist = "publish"
)

func newProgress(out io.Writer, now func() time.Time) *progress {
	return &progress{out: out, now: now, disabled: out == nil}
}

// enter starts a stage and always draws its name.
func (p *progress) enter(stage string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.stage = stage
	p.draw("pdfconcat: " + stage)
	p.last = p.now()
}

// step shows how many of total units of the current stage are done. Redraws are limited in rate and in total.
func (p *progress) step(done, total int) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.disabled || p.redraws >= maxRedraws || p.now().Sub(p.last) < minRedrawInterval {
		return
	}

	p.redraws++
	p.last = p.now()
	p.draw(fmt.Sprintf("pdfconcat: %s %d/%d", p.stage, done, total))
}

// erase erases the status line.
func (p *progress) erase() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.width == 0 {
		return
	}

	p.draw("")
	p.width = 0
}

// draw rewrites the line, padding with spaces over what was there. The caller holds the mutex.
func (p *progress) draw(text string) {
	if p.disabled {
		return
	}

	padding := max(p.width-len(text), 0)

	_, err := fmt.Fprintf(p.out, "\r%s%s\r%s", text, strings.Repeat(" ", padding), text)
	if err != nil {
		p.disabled = true

		return
	}

	p.width = max(p.width, len(text))
}
