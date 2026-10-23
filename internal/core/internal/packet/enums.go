// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import (
	"fmt"
	"slices"
)

// Anchor is a closed domain vocabulary.
type Anchor string

// Supported Anchor values.
const (
	AnchorTopLeft     Anchor = "top-left"
	AnchorTop         Anchor = "top"
	AnchorTopRight    Anchor = "top-right"
	AnchorLeft        Anchor = "left"
	AnchorCenter      Anchor = "center"
	AnchorRight       Anchor = "right"
	AnchorBottomLeft  Anchor = "bottom-left"
	AnchorBottom      Anchor = "bottom"
	AnchorBottomRight Anchor = "bottom-right"
)

var anchorValues = []string{string(AnchorTopLeft), string(AnchorTop), string(AnchorTopRight), string(AnchorLeft), string(AnchorCenter), string(AnchorRight), string(AnchorBottomLeft), string(AnchorBottom), string(AnchorBottomRight)}

// String returns the wire value.
func (v Anchor) String() string { return string(v) }

// MarshalText encodes a domain value.
func (v Anchor) MarshalText() ([]byte, error) { return []byte(v), nil }

// UnmarshalText rejects unknown values.
func (v *Anchor) UnmarshalText(b []byte) error {
	if !slices.Contains(anchorValues, string(b)) {
		return fmt.Errorf("unknown Anchor: %q", b)
	}
	*v = Anchor(b)
	return nil
}

// Values returns the supported wire values.
func (Anchor) Values() []string { return slices.Clone(anchorValues) }

// Align is a closed domain vocabulary.
type Align string

// Supported Align values.
const (
	AlignLeft    Align = "left"
	AlignCenter  Align = "center"
	AlignRight   Align = "right"
	AlignJustify Align = "justify"
)

var alignValues = []string{string(AlignLeft), string(AlignCenter), string(AlignRight), string(AlignJustify)}

// String returns the wire value.
func (v Align) String() string { return string(v) }

// MarshalText encodes a domain value.
func (v Align) MarshalText() ([]byte, error) { return []byte(v), nil }

// UnmarshalText rejects unknown values.
func (v *Align) UnmarshalText(b []byte) error {
	if !slices.Contains(alignValues, string(b)) {
		return fmt.Errorf("unknown Align: %q", b)
	}
	*v = Align(b)
	return nil
}

// Values returns the supported wire values.
func (Align) Values() []string { return slices.Clone(alignValues) }

// Overflow is a closed domain vocabulary.
type Overflow string

// Supported Overflow values.
const (
	OverflowError Overflow = "error"
	OverflowAllow Overflow = "allow"
)

var overflowValues = []string{string(OverflowError), string(OverflowAllow)}

// String returns the wire value.
func (v Overflow) String() string { return string(v) }

// MarshalText encodes a domain value.
func (v Overflow) MarshalText() ([]byte, error) { return []byte(v), nil }

// UnmarshalText rejects unknown values.
func (v *Overflow) UnmarshalText(b []byte) error {
	if !slices.Contains(overflowValues, string(b)) {
		return fmt.Errorf("unknown Overflow: %q", b)
	}
	*v = Overflow(b)
	return nil
}

// Values returns the supported wire values.
func (Overflow) Values() []string { return slices.Clone(overflowValues) }

// StartOn is a closed domain vocabulary.
type StartOn string

// Supported StartOn values.
const (
	StartAny StartOn = "any"
	StartOdd StartOn = "odd"
)

var startOnValues = []string{string(StartAny), string(StartOdd)}

// String returns the wire value.
func (v StartOn) String() string { return string(v) }

// MarshalText encodes a domain value.
func (v StartOn) MarshalText() ([]byte, error) { return []byte(v), nil }

// UnmarshalText rejects unknown values.
func (v *StartOn) UnmarshalText(b []byte) error {
	if !slices.Contains(startOnValues, string(b)) {
		return fmt.Errorf("unknown StartOn: %q", b)
	}
	*v = StartOn(b)
	return nil
}

// Values returns the supported wire values.
func (StartOn) Values() []string { return slices.Clone(startOnValues) }

// PartKind is a closed domain vocabulary.
type PartKind string

// Supported PartKind values.
const (
	PartSource   PartKind = "source"
	PartPage     PartKind = "page"
	PartDivider  PartKind = "divider"
	PartContents PartKind = "contents"
	PartPadding  PartKind = "padding"
)

var partKindValues = []string{string(PartSource), string(PartPage), string(PartDivider), string(PartContents), string(PartPadding)}

// String returns the wire value.
func (v PartKind) String() string { return string(v) }

// MarshalText encodes a domain value.
func (v PartKind) MarshalText() ([]byte, error) { return []byte(v), nil }

// UnmarshalText rejects unknown values.
func (v *PartKind) UnmarshalText(b []byte) error {
	if !slices.Contains(partKindValues, string(b)) {
		return fmt.Errorf("unknown PartKind: %q", b)
	}
	*v = PartKind(b)
	return nil
}

// Values returns the supported wire values.
func (PartKind) Values() []string { return slices.Clone(partKindValues) }
