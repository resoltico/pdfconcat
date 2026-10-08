// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package report is the agent-facing result boundary: the complete Report of a build or check, its compact
// Summary, targeted queries over a saved report, the untrusted-file decoder, the embedded JSON Schema, and
// the Builder that accumulates diagnostics in stable input order.
//
// A saved report never causes a PDF to be read. Queries describe the captured run only.
package report
