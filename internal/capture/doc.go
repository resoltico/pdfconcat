// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Package capture owns the private job workspace, the single-pass snapshot of source and font
// files, and filesystem-identity alias protection between the resources of one job.
//
// Snapshots make the bytes that were inspected exactly the bytes that are assembled. They are not
// a filesystem-wide atomic snapshot: a concurrent writer that edits a source in place can only be
// detected when the change is observable on the open handle (size or modification time moved, or
// the byte count read differs from the size first observed).
package capture
