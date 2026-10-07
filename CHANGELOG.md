# Changelog

Notable changes to this project are documented in this file. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-10-07

### Added

- First public, source-only release of PDFConcat, an offline command-line tool for assembling PDFs in explicit order and inserting generated separator pages.
- JSON plans with reusable page appearance, Unicode text, and static TrueType fonts; direct operands support simple assemblies.
- `build`, `check`, `report`, `schema`, `version`, and `help` commands with JSON output by default and optional text output. Saved reports can be queried without reopening source PDFs.
- Verified output publication with no-clobber behavior by default, explicit overwrite, and actionable diagnostics and recovery information.

This release provides source code only. No executable packages, package checksums, or binary provenance attestations are published. [Build from source](https://github.com/resoltico/pdfconcat/tree/v0.1.0#install) using the Go toolchain specified in `go.mod`; local executables are created under `bin/`.

Fuzzing, mutation testing, and scale campaigns remain explicit manual checks; this release does not claim they ran.
