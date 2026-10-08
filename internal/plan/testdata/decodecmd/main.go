// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Command decodecmd is a test fixture: it decodes a plan from standard input the way an executable would
// and exits 0 on success, 2 on an invalid plan, and 130 when interrupted. With the argument "naive" it reads
// standard input without the decoder's cancellable reader, as a negative control.
//
// It writes the line "ready" to standard output once it handles SIGINT, and the line "read" each time it is
// about to read standard input, so that a test sends the signal only to a process that is blocked as the
// test intends instead of guessing how long a process takes to get there.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"

	"github.com/resoltico/pdfconcat/internal/plan"
)

// announcingReader says "read" before every read of the wrapped reader.
type announcingReader struct {
	io.Reader

	output io.Writer
}

const (
	exitInvalid     = 2
	exitInterrupted = 130
)

func (r announcingReader) Read(p []byte) (int, error) {
	if _, err := fmt.Fprintln(r.output, "read"); err != nil {
		return 0, fmt.Errorf("write read marker: %w", err)
	}

	count, err := r.Reader.Read(p)
	if err != nil {
		return count, fmt.Errorf("read fixture input: %w", err)
	}

	return count, nil
}

func main() {
	log.SetFlags(0)
	os.Exit(runFixture(os.Args[1:]))
}

func runFixture(args []string) int {
	handled, controlErr := controlCommand(args)
	if controlErr != nil {
		log.Print(controlErr)
		return exitInvalid
	}

	if handled {
		return 0
	}

	output := os.Stdout
	input := announcingReader{Reader: os.Stdin, output: output}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if _, err := fmt.Fprintln(output, "ready"); err != nil {
		log.Print(err)
		stop()

		return exitInvalid
	}

	if len(args) > 0 && args[0] == "naive" {
		_, err := io.Copy(io.Discard, input)
		if err != nil {
			log.Print(err)
			stop()

			return exitInvalid
		}

		return 0
	}

	_, err := plan.Decode(ctx, plan.Input{Name: "<stdin>"}, input)

	var planErr *plan.Error

	switch {
	case err == nil:
	case errors.As(err, &planErr) && planErr.Code == plan.CodeInterrupted:
		log.Print("interrupted")
		stop()

		return exitInterrupted
	default:
		log.Print(err)
		stop()

		return exitInvalid
	}

	return 0
}
