// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"os"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/cli"
)

// progressOwner creates the process transport lazily after command parsing and
// keeps it idle for exceptions after the scheduler joins. Close is last.
type progressOwner struct {
	file        *os.File
	transport   *progressTransport
	unavailable bool
}

func (owner *progressOwner) WriteRecord(ctx context.Context, record []byte) error {
	if owner.transport == nil {
		return os.ErrClosed
	}

	return owner.transport.WriteRecord(ctx, record)
}

func (owner *progressOwner) newSession(ctx context.Context, mode cli.ProgressMode, attempt string) *app.Progress {
	if mode != cli.ProgressJSON {
		terminal, err := isProgressTerminal(owner.file)
		if err != nil {
			owner.unavailable = true
			return nil
		}

		if !terminal {
			return nil
		}
	}

	transport, err := newProgressTransport(owner.file)
	if err != nil {
		owner.unavailable = true
		return nil
	}

	owner.transport = transport

	return app.NewProgress(ctx, mode, attempt, owner)
}

func (owner *progressOwner) interrupted() bool {
	return owner.unavailable || owner.transport != nil && owner.transport.Interrupted()
}

func (owner *progressOwner) release() {
	if owner.transport != nil {
		if err := owner.transport.Close(); err != nil {
			owner.unavailable = true
		}
	}
}
