// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import _ "embed" // Embeds the report JSON Schema.

var (
	//go:embed report.schema.json
	schema string

	//go:embed response.schema.json
	responseSchema string
)

// Schema returns the JSON Schema (draft 2020-12) of a saved report, format 2. It states the structural
// rules. Duplicate members, Unicode and byte-level rules, table references (source, style, and font
// indexes), ID uniqueness, range order and contiguity, count sums, status relations, and the byte,
// nesting, and node limits are enforced by Decode and Validate only.
func Schema() string {
	return schema
}

// ResponseSchema returns the local structured response union, format 2.
func ResponseSchema() string { return responseSchema }
