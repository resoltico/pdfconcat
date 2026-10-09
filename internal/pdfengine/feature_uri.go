// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"
	"net/url"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const (
	keyURI     = "URI"
	keyURIBase = "Base"
)

// catalogURIBase captures binding presence before validation or assembly drops the catalog key.
// No URI is resolved and no action bytes are rewritten.
func (o *featureObserver) catalogURIBase(ctx context.Context) error {
	dict, err := o.pdf.DereferenceDictContext(ctx, o.pdf.RootDict[keyURI])
	if err != nil {
		return fmt.Errorf("catalog /URI: %w", err)
	}

	base, err := o.pdf.DereferenceStringEntryBytesContext(ctx, dict, keyURIBase)
	if err != nil {
		return fmt.Errorf("catalog /URI /Base: %w", err)
	}

	o.uriBase = len(base) > 0

	return nil
}

func (o *featureObserver) baseDependentURI(ctx context.Context, action types.Dict) error {
	target, err := o.pdf.DereferenceStringEntryBytesContext(ctx, action, keyURI)
	if err != nil {
		return fmt.Errorf("action /URI: %w", err)
	}

	if len(target) > 0 && !absoluteURI(target) {
		o.found[FeatureURIBase] = true
	}

	return nil
}

// absoluteURI checks syntax and scheme presence only, without network or document-location resolution.
func absoluteURI(target []byte) bool {
	parsed, err := url.Parse(string(target))

	return err == nil && parsed.IsAbs()
}
