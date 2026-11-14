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

// Wrap is the generated-text wrapping policy.
type Wrap string

// Supported Wrap values.
const (
	WrapWord      Wrap = "word"
	WrapEmergency Wrap = "emergency"
)

var wrapValues = []string{string(WrapWord), string(WrapEmergency)}

// String returns the wire value.
func (v Wrap) String() string { return string(v) }

// MarshalText encodes a domain value.
func (v Wrap) MarshalText() ([]byte, error) { return []byte(v), nil }

// UnmarshalText rejects unknown values.
func (v *Wrap) UnmarshalText(b []byte) error {
	if !slices.Contains(wrapValues, string(b)) {
		return fmt.Errorf("unknown Wrap: %q", b)
	}
	*v = Wrap(b)
	return nil
}

// Values returns the supported wire values.
func (Wrap) Values() []string { return slices.Clone(wrapValues) }

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

// NumberingMode selects the page-label counting policy.
type NumberingMode string

// Supported NumberingMode values.
const (
	NumberPhysical    NumberingMode = "physical"
	NumberConsecutive NumberingMode = "consecutive"
)

var numberingModeValues = []string{"physical", "consecutive"}

// String returns the wire value.
func (v NumberingMode) String() string { return string(v) }

// MarshalText encodes a domain value.
func (v NumberingMode) MarshalText() ([]byte, error) { return []byte(v), nil }

// UnmarshalText rejects unknown values.
func (v *NumberingMode) UnmarshalText(b []byte) error {
	if !slices.Contains(numberingModeValues, string(b)) {
		return fmt.Errorf("unknown NumberingMode: %q", b)
	}
	*v = NumberingMode(b)
	return nil
}

// Values returns supported wire values.
func (NumberingMode) Values() []string { return slices.Clone(numberingModeValues) }

// NumberPosition selects the physical page-number placement.
type NumberPosition string

// Supported NumberPosition values.
const (
	NumberTopLeft      NumberPosition = "top-left"
	NumberTopCenter    NumberPosition = "top-center"
	NumberTopRight     NumberPosition = "top-right"
	NumberBottomLeft   NumberPosition = "bottom-left"
	NumberBottomCenter NumberPosition = "bottom-center"
	NumberBottomRight  NumberPosition = "bottom-right"
)

var numberPositionValues = []string{"top-left", "top-center", "top-right", "bottom-left", "bottom-center", "bottom-right"}

// String returns the wire value.
func (v NumberPosition) String() string { return string(v) }

// MarshalText encodes a domain value.
func (v NumberPosition) MarshalText() ([]byte, error) { return []byte(v), nil }

// UnmarshalText rejects unknown values.
func (v *NumberPosition) UnmarshalText(b []byte) error {
	if !slices.Contains(numberPositionValues, string(b)) {
		return fmt.Errorf("unknown NumberPosition: %q", b)
	}
	*v = NumberPosition(b)
	return nil
}

// Values returns supported wire values.
func (NumberPosition) Values() []string { return slices.Clone(numberPositionValues) }

var structurePolicyValues = []string{"preserve", "discard"}

// String returns the wire value.
func (v StructurePolicy) String() string { return string(v) }

// MarshalText encodes a domain value.
func (v StructurePolicy) MarshalText() ([]byte, error) { return []byte(v), nil }

// UnmarshalText rejects unknown values.
func (v *StructurePolicy) UnmarshalText(b []byte) error {
	if !slices.Contains(structurePolicyValues, string(b)) {
		return fmt.Errorf("unknown StructurePolicy: %q", b)
	}
	*v = StructurePolicy(b)
	return nil
}

// Values returns supported wire values.
func (StructurePolicy) Values() []string { return slices.Clone(structurePolicyValues) }
