# Changelog

All notable changes to PDFConcat are recorded here, against the latest published release. Versions follow [Semantic Versioning](https://semver.org/). There is no published release yet.

## [Unreleased]

### Added

- Assemble PDFs in an explicit order, inserting generated blank pages where `--blank` appears: `pdfconcat -o out.pdf a.pdf --blank b.pdf`. Blank pages take the displayed size of the neighboring source page, or an explicit size.
- Blank pages can carry a background color and text — standard PDF fonts, size, color, nine-point anchors with offsets, wrapping, and left/center/right/justify alignment — set for every blank with `--blank-*` options, for one blank with `--blank=TEXT`, or per blank in a plan. See [`docs/BLANK_PAGES.md`](docs/BLANK_PAGES.md).
- Versioned JSON plan files (`--plan FILE`, or `--plan -` for standard input) holding the output, blank defaults, directory groups, and per-blank style and count, with line/column and JSON-path errors. `--print-schema` prints the JSON Schema. See [`docs/PLAN.md`](docs/PLAN.md).
- `--dry-run` reports output page ranges and each blank's resolved size and style; `--json` makes dry-run and success reports machine-readable.
- Assembly scales to thousands of PDFs and thousands of blanks: one merge pass, each distinct blank rendered once, concurrent input validation that reports every failing input together, and blank styles checked before any PDF is read.
- Output safety: the result is validated and its page count verified before it is published; sources and plans are never modified; outputs that alias an input or plan, and symbolic-link outputs, are refused; without `--overwrite` publication uses the platform's native no-clobber rename on Linux, macOS, and Windows.
- macOS, Linux, and Windows builds for amd64 and arm64, using an embedded, stateless, offline pdfcpu v0.16.0 with no external PDF tools.

### Internal

- Release automation with GoReleaser, checksums, and provenance attestation; CI covering tests on three operating systems, the race detector, cross-compilation, `govulncheck`, and module hygiene.
- golangci-lint with every linter enabled and a single commented exception registry, enforced by tests.
- Licensed under MPL-2.0 with third-party notices; development tools isolated in `tools/go.mod`.
