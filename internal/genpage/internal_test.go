// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package genpage

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestOffsetsBeyondCrossReferenceRangeAreRejected(t *testing.T) {
	t.Parallel()

	d := &document{out: &sink{w: bufio.NewWriter(io.Discard)}, offsets: []int64{0, maxOffsetDigits + 1}}

	_, err := d.finish()
	if !errors.Is(err, ErrDocumentTooLarge) || !strings.Contains(err.Error(), "cross-reference") {
		t.Errorf("got %v", err)
	}
}

func TestLargestRepresentableOffsetIsWritten(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	d := &document{out: &sink{w: bufio.NewWriter(&out)}, offsets: []int64{0, maxOffsetDigits}}

	_, err := d.finish()
	if err != nil || !strings.Contains(out.String(), "\n9999999999 00000 n \n") {
		t.Errorf("err %v, xref %q", err, out.String())
	}
}
