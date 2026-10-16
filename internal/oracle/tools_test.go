// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package oracle

import (
	"errors"
	"strings"
	"testing"
)

func TestQPDFVersionRequirement(t *testing.T) {
	for _, version := range []string{"12.4.2", "12.4.3", "12.4.10", "12.5.0", "13.0.0"} {
		t.Run(version, func(t *testing.T) {
			if err := checkQPDFVersion("qpdf version " + version + "\nRun qpdf --copyright to see copyright and license information.\n"); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, output := range []string{
		"qpdf version 11.9.0", "qpdf version 12.4.1", "qpdf version 12.3.99",
		"", "other version 12.4.2", "qpdf version", "qpdf version 12.4",
		"qpdf version 12.4.2-dev", "qpdf version 12.4.2.1", "qpdf version -12.4.2",
		"qpdf version 12.+4.2", "qpdf version 12.04.2", "qpdf version 999999999999999999999999.0.0",
	} {
		t.Run(output, func(t *testing.T) {
			err := checkQPDFVersion(output)
			if !errors.Is(err, errQPDFVersion) || !strings.Contains(err.Error(), "install qpdf "+strings.TrimSpace(qpdfMinimumVersion)+" or newer") {
				t.Fatalf("unsupported version must give upgrade advice: %v", err)
			}
		})
	}
}
