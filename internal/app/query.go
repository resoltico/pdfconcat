// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"errors"
	"slices"

	"github.com/resoltico/pdfconcat/internal/plan"
	result "github.com/resoltico/pdfconcat/internal/wire"
)

// SchemaKind selects a supported wire contract.
type SchemaKind string

// Schema selectors are the supported editor and result contracts.
const (
	PlanSchema   SchemaKind = "plan"
	ResultSchema SchemaKind = "result"
)

// Schema returns an owned copy of a supported wire contract.
func Schema(kind SchemaKind) ([]byte, error) {
	switch kind {
	case PlanSchema:
		return slices.Clone(plan.Schema), nil
	case ResultSchema:
		return slices.Clone(result.Schema), nil
	default:
		return nil, errors.New("unknown schema")
	}
}
