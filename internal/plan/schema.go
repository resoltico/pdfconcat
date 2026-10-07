// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package plan

import _ "embed" // Embeds the plan JSON Schema.

//go:embed plan.schema.json
var schema string

// Schema returns the JSON Schema (draft 2020-12) describing plan format version 1. It states the
// structural rules; duplicate members, Unicode and byte-level rules, and unit-converted length bounds are
// enforced by Decode only.
func Schema() string {
	return schema
}
