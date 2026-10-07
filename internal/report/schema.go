// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report

import _ "embed" // Embeds the report JSON Schema.

//go:embed report.schema.json
var schema string

// Schema returns the JSON Schema (draft 2020-12) of a saved report, version 1. It states the structural
// rules. Duplicate members, Unicode and byte-level rules, table references (source, style, and font
// indexes), ID uniqueness, range order and contiguity, count sums, status relations, and the byte,
// nesting, and node limits are enforced by Decode and Validate only.
func Schema() string {
	return schema
}
