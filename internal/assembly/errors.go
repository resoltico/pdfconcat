// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly

import "errors"

// Sentinel errors for the domain values. Every error returned by a parser or validator wraps one of them,
// so callers can classify a failure with [errors.Is] while the message stays specific.
var (
	// ErrInvalidLength reports text that is not a length.
	ErrInvalidLength = errors.New("invalid length")
	// ErrInvalidColor reports text that is not a color or background.
	ErrInvalidColor = errors.New("invalid color")
	// ErrInvalidPageSize reports text that is not a page size.
	ErrInvalidPageSize = errors.New("invalid page size")
	// ErrUnknownName reports a name that is not one of the allowed names of an enumeration.
	ErrUnknownName = errors.New("unknown name")
	// ErrOutOfRange reports a value outside its documented domain: not finite, too small, or too large.
	ErrOutOfRange = errors.New("value out of range")
	// ErrInvalidText reports text that is not valid UTF-8 or is too long.
	ErrInvalidText = errors.New("invalid text")
	// ErrInvalidPath reports a path that no filesystem can hold.
	ErrInvalidPath = errors.New("invalid path")
	// ErrInvalidJob reports a structural fault in a job: an empty sequence, a malformed item, or a crossed limit.
	ErrInvalidJob = errors.New("invalid job")
	// ErrUnresolvedSize reports a generated page that inherits its size when no source page is available.
	ErrUnresolvedSize = errors.New(`page size "inherit" has no source page to inherit from; give the blank an explicit size`)
)
