// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// Version is a PDF version encoded as major*10+minor: 14 is PDF 1.4 and 20 is PDF 2.0.
type Version int

// Versions the engine writes. Output is PDF 2.0 when any input is PDF 2.0, and PDF 1.7 otherwise.
const (
	Version17 Version = 17
	Version20 Version = 20

	// versionRadix is the factor between a version's major and minor digits in its encoding.
	versionRadix = 10
)

// String formats the version as "major.minor".
func (v Version) String() string {
	return fmt.Sprintf("%d.%d", v/versionRadix, v%versionRadix)
}

// versionOf converts a pdfcpu version, whose values count up from PDF 1.0 and end with PDF 2.0.
func versionOf(v model.Version) Version {
	if v == model.V20 {
		return Version20
	}

	return Version(versionRadix) + Version(v-model.V10)
}
