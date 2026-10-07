# Third-party notices

PDFConcat application code is licensed under the Mozilla Public License 2.0. The development-only tool patches retain their upstream licenses as described below. The compiled program also incorporates Go standard-library code and may incorporate code from modules in the application module graph under separate licenses. Those components are not relicensed under MPL-2.0.

This inventory lists the modules linked into the release binaries for macOS, Linux, and Windows. A test fails if a linked module is missing from it, if it lists a module that is not linked, if a version differs from the one `go.mod` selects, or if a listed license copy does not reproduce the license files of the linked module version. Development tools (pinned in `tools/versions.env`) are not part of this runtime inventory.

| Component | Version | License | Purpose / relationship | License copy |
| --- | --- | --- | --- | --- |
| Go standard library | Go 1.27.1 toolchain | BSD 3-Clause | Runtime and standard-library code linked by the Go toolchain | `third_party/licenses/go-and-x-BSD-3-Clause.txt` |
| `github.com/pdfcpu/pdfcpu` | v0.16.1 | Apache-2.0 | PDF parsing, validation, merging, and writing of the assembled output (the text of generated pages is shaped with go-text/typesetting and written by PDFConcat's own page writer) | `third_party/licenses/Apache-2.0.txt` |
| `github.com/clipperhouse/uax29/v2` | v2.7.0 | MIT | pdfcpu dependency graph | `third_party/licenses/clipperhouse-uax29-MIT.txt` |
| `github.com/go-text/typesetting` | v0.3.5 | Unlicense OR BSD 3-Clause | text shaping (HarfBuzz port) and TrueType parsing for generated-page text; used under the BSD 3-Clause terms | `third_party/licenses/go-text-typesetting-LICENSE.txt` |
| `github.com/hhrutter/tiff` | v1.0.7 | BSD 3-Clause | pdfcpu dependency graph | `third_party/licenses/hhrutter-tiff-BSD-3-Clause.txt` |
| `github.com/mattn/go-runewidth` | v0.0.30 | MIT | pdfcpu dependency graph | `third_party/licenses/mattn-go-runewidth-MIT.txt` |
| `go.yaml.in/yaml/v3` | v3.0.5 | MIT AND Apache-2.0 | pdfcpu dependency graph | `third_party/licenses/go-yaml-LICENSE.txt`; `third_party/licenses/Apache-2.0.txt`; `third_party/licenses/go-yaml-NOTICE.txt` |
| `golang.org/x/crypto` | v0.57.0 | BSD 3-Clause | pdfcpu dependency graph | `third_party/licenses/go-and-x-BSD-3-Clause.txt` |
| `golang.org/x/image` | v0.46.0 | BSD 3-Clause | fixed-point arithmetic for text shaping and pdfcpu dependency graph | `third_party/licenses/go-and-x-BSD-3-Clause.txt` |
| `golang.org/x/sys` | v0.48.0 | BSD 3-Clause | native no-clobber / replacement publication on supported operating systems and pdfcpu dependency graph | `third_party/licenses/go-and-x-BSD-3-Clause.txt` |
| `golang.org/x/text` | v0.42.0 | BSD 3-Clause | Unicode artifact-name matching and text direction; pdfcpu encodings | `third_party/licenses/go-and-x-BSD-3-Clause.txt` |
| `golang.org/x/term` | v0.46.0 | BSD 3-Clause | detecting an interactive terminal on standard error, so progress is shown only there | `third_party/licenses/go-and-x-BSD-3-Clause.txt` |

The Apache-2.0 text is shared by the Apache-licensed components above. go-yaml contains both MIT-covered and Apache-covered source files, so its upstream combined LICENSE and NOTICE are reproduced separately in addition to the shared Apache-2.0 text. The Go standard library and listed `golang.org/x/*` modules use the same Go Authors BSD 3-Clause license text represented by the shared copy above. Components with project-specific MIT or BSD notices retain those notices in separate files.

Dependency versions and this notice inventory must be reconciled before each release whenever `go.mod` or `go.sum` changes.

## Embedded font

The executable embeds the Noto Sans Regular font (version 2.015, unhinted TrueType, 431,364 bytes, SHA-256 `f3961a9cde016d41a4879aecda1474d3a36d6bf54fa0e4643de029cc2248b0e8`) as the default font of generated pages, and writes it, whole, into every PDF that uses it. The font is Copyright 2022 The Noto Project Authors and is licensed under the SIL Open Font License 1.1, reproduced in `third_party/licenses/NotoSans-OFL-1.1.txt` (the same text accompanies the font file as `internal/typeset/fontdata/OFL.txt`). The font is not sold by itself and is not modified.


## Development linter source

The development-only golangci-lint is built from checksum-verified upstream source with the reviewed physical-source attribution patch under `tools/lint-patches`. Its upstream GPLv3 license and the corresponding patch/source instructions are kept there, separately from the runtime license inventory and release archives. Upstream dependencies retain their own licenses; the installer verifies the unchanged upstream module graph. The linter is not linked into PDFConcat, and the runtime remains licensed as stated above. The build/cache identity derives from the upstream version and patch digest; the source-tag epoch in its metadata is not a claim about build time.

## Development mutation tool source

The development-only Gremlins is built from checksum-verified upstream source with the reviewed executor patch under `tools/mutation-patches`. Its upstream Apache License 2.0 and corresponding source/patch instructions are kept there, separately from the runtime license inventory and release archives. Upstream dependencies retain their own licenses, and the installer verifies that their original module graph remains unchanged. Gremlins is not linked into PDFConcat. Its distinct version binds the upstream version and full patch digest; source provenance does not claim an actual build time.
