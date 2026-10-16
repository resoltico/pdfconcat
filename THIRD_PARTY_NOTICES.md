# Third-party notices

PDFConcat application code is licensed under MPL-2.0. The executable also links
the following components under their own licenses. Dependency versions are
selected in `go.mod` and `go.sum`; this inventory records module identities and
licenses without duplicating those versions.

| Module | License | Purpose | License copy |
| --- | --- | --- | --- |
| `github.com/pdfcpu/pdfcpu` | Apache-2.0 | PDF reading, validation, merging, fitting and writing | [Apache-2.0](third_party/licenses/Apache-2.0.txt) |
| `github.com/go-text/typesetting` | Unlicense OR BSD-3-Clause (used under BSD-3-Clause) | TrueType parsing and text shaping | [go-text](third_party/licenses/go-text-typesetting-LICENSE.txt) |
| `github.com/clipperhouse/uax29/v2` | MIT | Unicode segmentation | [uax29](third_party/licenses/clipperhouse-uax29-MIT.txt) |
| `github.com/hhrutter/tiff` | BSD-3-Clause | PDF image decoding | [tiff](third_party/licenses/hhrutter-tiff-BSD-3-Clause.txt) |
| `github.com/mattn/go-runewidth` | MIT | Unicode display-width support | [go-runewidth](third_party/licenses/mattn-go-runewidth-MIT.txt) |
| `github.com/xdg-go/stringprep` | Apache-2.0 | PDF password-string handling in the backend | [stringprep](third_party/licenses/stringprep-Apache-2.0.txt) |
| `go.yaml.in/yaml/v3` | MIT AND Apache-2.0 | Backend configuration structures | [LICENSE](third_party/licenses/go-yaml-LICENSE.txt), [NOTICE](third_party/licenses/go-yaml-NOTICE.txt), [Apache-2.0](third_party/licenses/Apache-2.0.txt) |
| `golang.org/x/crypto` | BSD-3-Clause | Backend cryptographic support | [Go Authors](third_party/licenses/go-and-x-BSD-3-Clause.txt) |
| `golang.org/x/image` | BSD-3-Clause | Font fixed-point arithmetic and image support | [Go Authors](third_party/licenses/go-and-x-BSD-3-Clause.txt) |
| `golang.org/x/sys` | BSD-3-Clause | Native no-replace publication calls | [Go Authors](third_party/licenses/go-and-x-BSD-3-Clause.txt) |
| `golang.org/x/text` | BSD-3-Clause | Unicode direction and encoding support | [Go Authors](third_party/licenses/go-and-x-BSD-3-Clause.txt) |

Go standard-library/runtime code uses the same [Go Authors BSD-3-Clause
text](third_party/licenses/go-and-x-BSD-3-Clause.txt). Project-specific copyright
notices remain in the separate license copies. The go-yaml combined license and
NOTICE are both retained.

## Embedded font

The executable embeds Noto Sans Regular, Copyright 2022 The Noto Project Authors,
under the [SIL Open Font License 1.1](third_party/licenses/NotoSans-OFL-1.1.txt).
The same notice accompanies the asset in `internal/typeset/fontdata/OFL.txt`.
Generated PDFs normally embed a glyph-pruned copy with stable glyph IDs. A font
whose embedding permission prohibits subsetting is embedded unchanged. The
bundled font asset itself is unchanged; the font is not sold separately.

## Source-only test corpus

Source checkouts include Go Italic and Go Mono fonts, plus frozen PDFs with
embedded Noto Sans and Libertinus Serif font programs. Their credits, licenses
and authoring details are recorded in the [corpus notices](https://github.com/resoltico/pdfconcat/blob/main/internal/core/testdata/corpus/NOTICE.txt).
These test assets are not included in the executable or binary release archives.

## Development tools

Stock golangci-lint, govulncheck and actionlint are development dependencies pinned
in `tools/go.mod`; GoReleaser is pinned in the release workflow. They are not
linked into the executable or included in product archives. Their source and
licenses remain available through their upstream modules/releases. There are no
local dependency forks or development-tool patches.

A dependency update that changes the linked module set or license/NOTICE contents
can require a notice update. Version-only changes do not require duplicating a
new version number here.
