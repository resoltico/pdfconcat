// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly

import "fmt"

// FitTarget is an explicitly requested portrait paper target. The empty value means no fitting.
type FitTarget string

const (
	// FitA4 selects ISO A4 portrait.
	FitA4 FitTarget = "A4"
	// FitLegal selects US Legal portrait.
	FitLegal FitTarget = "Legal"
)

// ParseFitTarget accepts only the two supported spellings; dimensions come from namedPageSizes.
func ParseFitTarget(text string) (FitTarget, error) {
	target := FitTarget(text)
	if target != FitA4 && target != FitLegal {
		return "", fmt.Errorf("%w: fit_to needs A4 or Legal, not %q", ErrInvalidPageSize, text)
	}

	return target, nil
}

// Dim resolves the authoritative named-paper dimensions.
func (t FitTarget) Dim() (PageDim, error) {
	if _, err := ParseFitTarget(string(t)); err != nil {
		return PageDim{}, err
	}

	size, err := ParsePageSize(string(t))

	return size.Dim, err
}
