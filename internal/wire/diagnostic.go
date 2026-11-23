// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package result

import "github.com/resoltico/pdfconcat/internal/core/diag"

// Diagnostic is the result representation of a core finding.
type Diagnostic struct {
	Severity         diag.Severity           `json:"severity"`
	Code             diag.Code               `json:"code"`
	Message          string                  `json:"message"`
	Detail           string                  `json:"detail,omitempty"`
	PDFLocation      *PDFLocation            `json:"pdf_location,omitempty"`
	File             string                  `json:"file,omitempty"`
	Items            []string                `json:"items,omitempty"`
	ItemsOmitted     int                     `json:"items_omitted"`
	Location         *Location               `json:"location,omitempty"`
	SignatureDiscard *SignatureDiscardCounts `json:"signature_discard,omitempty"`
}

// PDFLocation locates evidence in the original PDF.
type PDFLocation struct {
	Object int    `json:"object,omitzero"`
	Page   int    `json:"page,omitzero"`
	Path   string `json:"path,omitempty"`
}

// Location locates evidence in the command line or plan.
type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Offset int64  `json:"offset"`
	Ref    string `json:"pointer"`
}

// SignatureDiscardCounts includes all seven counts even when zero.
type SignatureDiscardCounts struct {
	PopulatedSignatureFields int `json:"populated_signature_fields"`
	Widgets                  int `json:"widgets"`
	SignatureValues          int `json:"signature_values"`
	DocumentTimestamps       int `json:"document_timestamps"`
	PermissionStructures     int `json:"permission_structures"`
	ValidationStores         int `json:"validation_stores"`
	SignatureFlagEntries     int `json:"signature_flag_entries"`
}

func projectDiagnostics(findings []diag.Diagnostic) []Diagnostic {
	diagnostics := make([]Diagnostic, len(findings))
	for i, d := range findings {
		view := Diagnostic{
			Severity: d.Severity, Code: d.Code, Message: d.Message, Detail: d.Detail,
			PDFLocation: (*PDFLocation)(d.PDFLocation), File: d.File,
			Items: d.Items, ItemsOmitted: d.ItemsOmitted, Location: (*Location)(d.Location),
		}
		if counts, ok := d.SignatureDiscard(); ok {
			payload := SignatureDiscardCounts(counts)
			view.SignatureDiscard = &payload
		}
		diagnostics[i] = view
	}
	return diagnostics
}
