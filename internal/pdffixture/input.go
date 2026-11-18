// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdffixture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
)

// SourceInput adapts fixture bytes to the assembly snapshot contract.
type SourceInput []byte

// Open acquires a fresh reader on fixture bytes.
func (f SourceInput) Open(context.Context) (io.ReadSeekCloser, [32]byte, error) {
	return sourceReader{bytes.NewReader(f)}, sha256.Sum256(f), nil
}

type sourceReader struct{ *bytes.Reader }

func (sourceReader) Close() error { return nil }
