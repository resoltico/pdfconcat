// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Package genpage writes the generated-page resource document: one PDF whose page i is the i-th
// distinct generated page specification.
//
// Every distinct font of the pages is embedded once, whole, as a Type0/CIDFontType2 composite font
// with Identity-H encoding and shared by all pages. Text extraction works through /ToUnicode and,
// for clusters that are not a single character, /ActualText. Pages have a clean-origin MediaBox and
// no UserUnit (1 point per unit). The document is PDF 1.7, written in one pass to an [io.Writer] with
// memory bounded by one page's content plus the object offset table.
package genpage
