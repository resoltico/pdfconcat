// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Package pdfengine is the pdfcpu adapter: the only package that imports pdfcpu. It inspects source
// PDFs and assembles the final document from them and from a generated-pages document. It knows nothing
// of plans, styles or layout; callers hand it facts (SourceInfo), a compact page order (Run) and file
// paths, and it returns failures as *Error values with stable Codes.
//
// # Operations
//
// Inspect reads one captured source once and reports SourceInfo: page count, effective version, whether
// pages carry page-local objects, whether a form or destinations exist, and the visible size of the
// first and last pages. Assemble takes the inspected sources, the generated resource document and the
// final order, and writes and verifies one PDF.
//
// # Visible page geometry
//
// A page's visible size is the effective CropBox clipped to the effective MediaBox (MediaBox alone when
// there is no CropBox), swapped in width and height for a rotation of 90 or 270 degrees, and multiplied
// by the page's UserUnit. MediaBox, CropBox and Rotate are inherited through the page tree, each
// independently; UserUnit is not inherited. Boxes may be written with any two opposite corners. Only the
// first and last pages are measured; a malformed box, an empty or non-overlapping CropBox, a rotation
// that is not a multiple of 90, or a non-positive UserUnit on those pages is an error. Sizes above any
// generated-page limit are returned unchanged: preserving a legal large page is the caller's rule to
// apply. The rule was checked against Poppler renderings (pdftoppm -cropbox) in the tests; Poppler
// ignores UserUnit, which the tests multiply in.
//
// # Assembly design
//
// Every document is merged into one pool with pdfcpu's exported merge primitive, then the pool's page
// tree is replaced in place by a flat tree of the final order. Page objects keep their identity, so
// links, destinations and /P back-references stay consistent, and fonts, images and content streams stay
// shared. pdfcpu's page-collection primitive was rejected: with repeated pages it leaves link
// destinations pointing at orphan page copies, shares annotation arrays between copies, and drops
// inherited CropBox (the tests keep a negative control showing the independent oracle detects this).
//
// The pool is built in one sequential pass and written once; the growing output is never rewritten.
// Memory grows with the total size of the imported documents plus a page dictionary per output page: the
// pool and the written file are both held in memory (the file again, when it is re-read to verify it).
// There is no streaming mode and no fixed memory bound; MaxOutputPages only bounds a request. The
// package benchmarks measure wall time, memory and descriptors.
//
// # Source policy
//
// A source whose pages have /Annots, /AA or /B, or that has an AcroForm or destinations, is "per
// occurrence": it is imported anew for every run that uses it, so each occurrence owns its annotations,
// widgets and destinations. Such a source must be used whole in each run (a partial range would leave
// links, fields or destinations pointing at dropped pages: CodePartialRange). A source with the legacy
// catalog /Dests dictionary may occur once per output (CodeLegacyDestsRepeated), because pdfcpu merges that
// dictionary by overwriting equal keys. Every other source is imported once; repeated pages get a cloned
// page dictionary and share everything else. Inherited MediaBox, CropBox, Rotate and Resources are copied
// onto each page, so reordering never changes a page's geometry, and the root of the output page tree
// carries none.
//
// # Backend behavior for document-level features
//
//   - Annotations and links: kept with the page, and for per-occurrence sources re-pointed to the
//     occurrence's own pages (direct /Dest, GoTo actions, page /AA actions, /P back-references).
//   - Forms: the AcroForm is kept. Fields of equal name in different documents or occurrences stay
//     separate fields; pdfcpu nests the fields of later documents under a generated numeric parent field,
//     so qualified names differ from the source's.
//   - Named destinations: the /Names /Dests tree is kept and merged. pdfcpu renames a colliding name in
//     a later occurrence (a control-character suffix) and its links follow the rename.
//   - Outlines/bookmarks, document information, XMP metadata, page labels, open action, viewer
//     preferences, permissions and other non-rendering catalog entries: dropped.
//     The output has a fresh /Info with pdfcpu's producer and the current date.
//   - Tagged-PDF structure (StructTreeRoot, MarkInfo, StructParents): dropped; output is not tagged.
//   - Attachments (embedded files), JavaScript and any name tree other than destinations: dropped.
//   - Optional content (OCProperties, OCG/OCMD and /OC references) and OutputIntents: rejected with
//     CodeUnsupportedRendering because document-wide layer/color configuration cannot be reconciled.
//   - Dynamic XFA forms and NeedsRendering true: rejected before validator repairs, including XFA-only
//     forms with no static fields. Ordinary static AcroForm widgets and NeedsRendering false are supported.
//   - Encryption: encrypted sources are rejected (CodeEncrypted), including those that open with an
//     empty password. Output is never encrypted.
//   - PDF version: the output is PDF 2.0 when any input is PDF 2.0, else PDF 1.7. A 2.0 source may appear
//     anywhere in the order.
//   - Digital signatures: not supported and not tested. Assembly rewrites the file, so any signature
//     is invalid in the output; signature fields in an AcroForm are not specially handled.
//
// Output is written by pdfcpu with a fresh creation date, so it is not byte-reproducible.
//
// # Robustness
//
// Sources are hostile input. pdfcpu panics on some malformed files and re-panics from its own recovery;
// Inspect and Assemble convert panics into errors. The context is checked between imports, while page
// clones are compiled, and before the write; pdfcpu checks it internally as well.
package pdfengine
