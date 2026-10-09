/*
Copyright 2026 The pdfcpu Authors.
Licensed under the Apache License, Version 2.0.
*/
package validate

import (
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestInfoPropertiesPreserveCanonicalPrivateNameBytes(t *testing.T) {
	for _, key := range []string{"A#42", "unfinished#", "bad#gg"} {
		version := model.V17
		xref := &model.XRefTable{HeaderVersion: &version, Properties: map[string]string{}}
		if err := handleProperties(xref, key, types.StringLiteral("metadata value"), 1); err != nil {
			t.Fatalf("legal canonical private key rejected: %q %v", key, err)
		}
		if len(xref.Properties) != 1 || xref.Properties[key] != "metadata value" {
			t.Fatalf("canonical Info property promoted/redecoded: %v", xref.Properties)
		}
	}
}
