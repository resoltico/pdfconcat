// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package plan

import _ "embed" // Embeds the plan JSON Schema.

//go:embed plan.schema.json
var schema string

// Schema returns the JSON Schema describing plan files.
func Schema() string {
	return schema
}
