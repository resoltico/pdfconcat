// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly

import (
	"fmt"
	"strings"
)

type (
	// TextAlign is the justification of lines within a text block.
	TextAlign uint8

	// Overflow is the policy for text that does not fit its box or page.
	Overflow uint8
)

const (
	// AlignLeft starts every line at the left edge of the block.
	AlignLeft TextAlign = 1
	// AlignCenter centers every line in the block.
	AlignCenter TextAlign = 2
	// AlignRight ends every line at the right edge of the block.
	AlignRight TextAlign = 3
	// AlignJustify stretches every line but the last of a paragraph to the block's width.
	AlignJustify TextAlign = 4

	// OverflowError rejects text that is too wide, too tall, or outside the page. It is the default.
	OverflowError Overflow = 1
	// OverflowAllow places the text as specified even when it extends beyond the page.
	OverflowAllow Overflow = 2
)

// AlignNames lists the alignment names.
func AlignNames() []string {
	return []string{AlignLeft.String(), AlignCenter.String(), AlignRight.String(), AlignJustify.String()}
}

// String returns the alignment's configuration name.
func (a TextAlign) String() string {
	switch a {
	case AlignLeft:
		return "left"
	case AlignCenter:
		return "center"
	case AlignRight:
		return "right"
	case AlignJustify:
		return "justify"
	default:
		return fmt.Sprintf("align(%d)", uint8(a))
	}
}

// ParseTextAlign resolves an alignment name (exact lower case).
func ParseTextAlign(name string) (TextAlign, error) {
	for _, align := range []TextAlign{AlignLeft, AlignCenter, AlignRight, AlignJustify} {
		if align.String() == name {
			return align, nil
		}
	}

	return 0, fmt.Errorf("%w %q for alignment: want %s", ErrUnknownName, name, strings.Join(AlignNames(), ", "))
}

// OverflowNames lists the overflow policy names.
func OverflowNames() []string {
	return []string{OverflowError.String(), OverflowAllow.String()}
}

// String returns the policy's configuration name.
func (o Overflow) String() string {
	switch o {
	case OverflowError:
		return "error"
	case OverflowAllow:
		return "allow"
	default:
		return fmt.Sprintf("overflow(%d)", uint8(o))
	}
}

// ParseOverflow resolves an overflow policy name (exact lower case).
func ParseOverflow(name string) (Overflow, error) {
	for _, policy := range []Overflow{OverflowError, OverflowAllow} {
		if policy.String() == name {
			return policy, nil
		}
	}

	return 0, fmt.Errorf("%w %q for overflow: want %s", ErrUnknownName, name, strings.Join(OverflowNames(), ", "))
}
