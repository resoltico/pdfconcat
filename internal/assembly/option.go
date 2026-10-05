// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly

// Option is a value that may be unspecified, so that layered configuration
// can distinguish "not set" from a legitimate zero value.
type Option[T any] struct {
	value T
	set   bool
}

// Some returns an Option holding value.
func Some[T any](value T) Option[T] {
	return Option[T]{value: value, set: true}
}

// IsSet reports whether the option holds a value.
func (o Option[T]) IsSet() bool {
	return o.set
}

// OrElse returns the held value, or fallback when the option is unset.
func (o Option[T]) OrElse(fallback T) T {
	if o.set {
		return o.value
	}

	return fallback
}

// Over returns o when it is set, otherwise base.
func (o Option[T]) Over(base Option[T]) Option[T] {
	if o.set {
		return o
	}

	return base
}

// ParseSome parses text with parse and returns the result as a set Option.
func ParseSome[T any](text string, parse func(string) (T, error)) (Option[T], error) {
	value, err := parse(text)
	if err != nil {
		return Option[T]{}, err
	}

	return Some(value), nil
}
