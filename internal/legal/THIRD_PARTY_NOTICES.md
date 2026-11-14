# Third-party notices

PDFConcat application code is licensed under MPL-2.0; its source code is at
<https://github.com/resoltico/pdfconcat>, and `pdfconcat version` prints the
commit an executable was built from. The executable also contains the following
components under their own licenses. `pdfconcat licenses` prints this inventory
and every license text below. `go version -m pdfconcat` lists the exact module
versions linked into an executable.

| Module | License | Purpose | License copy |
| --- | --- | --- | --- |
| `github.com/pdfcpu/pdfcpu` | Apache-2.0; its `pkcs7` package MIT | PDF reading, validation, merging, fitting and object types | [Apache-2.0](licenses/Apache-2.0.txt), [pkcs7](licenses/pdfcpu-pkcs7-MIT.txt), [Adobe font metrics](licenses/Adobe-Core14-AFM-notice.txt) |
| `github.com/go-text/typesetting` | Unlicense OR BSD-3-Clause (used under BSD-3-Clause); its `harfbuzz` package MIT | OpenType parsing and text shaping | [go-text](licenses/go-text-typesetting-LICENSE.txt), [HarfBuzz port](licenses/go-text-harfbuzz-MIT.txt) |
| `github.com/clipperhouse/uax29/v2` | MIT | Unicode segmentation | [uax29](licenses/clipperhouse-uax29-MIT.txt) |
| `github.com/hhrutter/tiff` | BSD-3-Clause | PDF image decoding | [tiff](licenses/hhrutter-tiff-BSD-3-Clause.txt) |
| `github.com/mattn/go-runewidth` | MIT | Unicode display-width support | [go-runewidth](licenses/mattn-go-runewidth-MIT.txt) |
| `github.com/xdg-go/stringprep` | Apache-2.0 | PDF password-string handling in the backend | [stringprep](licenses/stringprep-Apache-2.0.txt) |
| `go.yaml.in/yaml/v3` | MIT AND Apache-2.0 | Backend configuration structures | [LICENSE](licenses/go-yaml-LICENSE.txt), [NOTICE](licenses/go-yaml-NOTICE.txt), [Apache-2.0](licenses/Apache-2.0.txt) |
| `golang.org/x/crypto` | BSD-3-Clause | Backend cryptographic support | [Go Authors](licenses/go-and-x-BSD-3-Clause.txt) |
| `golang.org/x/image` | BSD-3-Clause | Font fixed-point arithmetic and image support | [Go Authors](licenses/go-and-x-BSD-3-Clause.txt) |
| `golang.org/x/sys` | BSD-3-Clause | Native no-replace publication calls | [Go Authors](licenses/go-and-x-BSD-3-Clause.txt) |
| `golang.org/x/text` | BSD-3-Clause | Unicode direction and encoding support | [Go Authors](licenses/go-and-x-BSD-3-Clause.txt) |

| `seehuhn.de/go/sfnt` | BSD-3-Clause | Static CFF parsing and identity-CID conversion | [sfnt](licenses/seehuhn-sfnt-BSD-3-Clause.txt) |
| `seehuhn.de/go/postscript` | BSD-3-Clause | CFF dictionaries and glyph names | [postscript](licenses/seehuhn-postscript-BSD-3-Clause.txt) |
| `seehuhn.de/go/geom` | BSD-3-Clause | CFF geometry | [geom](licenses/seehuhn-geom-BSD-3-Clause.txt) |
| `seehuhn.de/go/membudget` | BSD-3-Clause | Bounded CFF parsing allocations | [membudget](licenses/seehuhn-membudget-BSD-3-Clause.txt) |
| `seehuhn.de/go/dag` | BSD-3-Clause | CFF dependency ordering | [dag](licenses/seehuhn-dag-BSD-3-Clause.txt) |

Go standard-library/runtime code uses the same [Go Authors BSD-3-Clause
text](licenses/go-and-x-BSD-3-Clause.txt), as do the LZW packages inside
`github.com/hhrutter/tiff` and `github.com/pdfcpu/pdfcpu`. Project-specific
copyright notices remain in the separate license copies. The go-yaml combined
license and NOTICE are both retained.

pdfcpu's `pkcs7` package carries its own MIT license, and go-text's `harfbuzz`
package, a port of HarfBuzz, carries the MIT license of its authors and of
HarfBuzz. pdfcpu also contains font metrics generated from Adobe's Core 14 AFM
files; the AFM files themselves are not included, and their notice is retained.

go-postscript embeds `agl-aglfn/aglfn.txt`, `agl-aglfn/glyphlist.txt` and
`agl-aglfn/zapfdingbats.txt` from Adobe's glyph lists, under the retained
[Adobe license](licenses/Adobe-Glyph-List-BSD-3-Clause.txt). Its
[third-party inventory](licenses/seehuhn-postscript-third-party.txt) is retained.

## Embedded fonts

The executable embeds Noto Sans Regular, Copyright 2022 The Noto Project Authors,
under the [SIL Open Font License 1.1](licenses/NotoSans-OFL-1.1.txt).
The same notice accompanies the asset in `internal/core/internal/typeset/fontdata/OFL.txt`.
Generated PDFs normally embed a glyph-pruned copy with stable glyph IDs. A font
whose embedding permission prohibits subsetting is embedded unchanged. The font
is not sold separately.

pdfcpu embeds Roboto Regular 2.137, Copyright 2011 Google Inc., under the
[Apache License 2.0](licenses/Apache-2.0.txt); Roboto is a trademark of Google.
PDFConcat does not use pdfcpu's bundled Roboto for generated text or form filling.
Source PDFs and user-supplied font files can independently contain Roboto.

## Source-only test corpus

Source checkouts include Go Italic and Go Mono fonts, plus frozen PDFs with
embedded Noto Sans and Libertinus Serif font programs. Their credits, licenses
and authoring details are recorded in the [corpus notices](https://github.com/resoltico/pdfconcat/blob/main/internal/core/testdata/corpus/NOTICE.txt).
Adobe Source Serif 4 static CFF is also included under its accompanying SIL Open Font License in `internal/core/testdata/fonts/source-serif`.
These test assets are not included in the executable or binary release archives.

## Development tools

Stock golangci-lint, govulncheck and actionlint are development dependencies pinned
in `tools/go.mod`; GoReleaser is pinned in the release workflow. They are not
linked into the executable or included in product archives. Their source and
licenses remain available through their upstream modules/releases. There are no
local dependency forks or development-tool patches.

A dependency update that changes the linked module set, a license or NOTICE
file of a linked package, or the assets a dependency embeds requires a notice
update; `cmd/pdfconcat/notices_test.go` checks those three. Version-only changes
do not require duplicating a new version number here.
