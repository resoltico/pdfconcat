# Third-party notices

PDFConcat source code is licensed under the Mozilla Public License 2.0. The
compiled program also incorporates Go standard-library code and may incorporate
code from modules in the application module graph under separate licenses.
Those components are not relicensed under MPL-2.0.

This inventory lists the modules linked into the release binaries for macOS, Linux, and Windows. A test fails if a linked module is missing from it, or if it lists a module that is not linked. Development-only tools from `tools/go.mod` are not
part of this runtime inventory.

| Component | Version | License | Purpose / relationship | License copy |
| --- | --- | --- | --- | --- |
| Go standard library | Go 1.27.1 toolchain | BSD 3-Clause | Runtime and standard-library code linked by the Go toolchain | `third_party/licenses/go-and-x-BSD-3-Clause.txt` |
| `github.com/pdfcpu/pdfcpu` | v0.16.0 | Apache-2.0 | PDF parsing, validation, merging, and standard-font metrics for generated blank-page text | `third_party/licenses/Apache-2.0.txt` |
| `github.com/clipperhouse/uax29/v2` | v2.7.0 | MIT | pdfcpu dependency graph | `third_party/licenses/clipperhouse-uax29-MIT.txt` |
| `github.com/hhrutter/tiff` | v1.0.6 | BSD 3-Clause | pdfcpu dependency graph | `third_party/licenses/hhrutter-tiff-BSD-3-Clause.txt` |
| `github.com/mattn/go-runewidth` | v0.0.30 | MIT | pdfcpu dependency graph | `third_party/licenses/mattn-go-runewidth-MIT.txt` |
| `go.yaml.in/yaml/v3` | v3.0.5 | MIT AND Apache-2.0 | pdfcpu dependency graph | `third_party/licenses/go-yaml-LICENSE.txt`; `third_party/licenses/Apache-2.0.txt`; `third_party/licenses/go-yaml-NOTICE.txt` |
| `golang.org/x/crypto` | v0.57.0 | BSD 3-Clause | pdfcpu dependency graph | `third_party/licenses/go-and-x-BSD-3-Clause.txt` |
| `golang.org/x/image` | v0.46.0 | BSD 3-Clause | pdfcpu dependency graph | `third_party/licenses/go-and-x-BSD-3-Clause.txt` |
| `golang.org/x/sys` | v0.48.0 | BSD 3-Clause | native no-clobber / replacement publication on supported operating systems and pdfcpu dependency graph | `third_party/licenses/go-and-x-BSD-3-Clause.txt` |
| `golang.org/x/text` | v0.42.0 | BSD 3-Clause | pdfcpu dependency graph | `third_party/licenses/go-and-x-BSD-3-Clause.txt` |

The Apache-2.0 text is shared by the Apache-licensed components above. go-yaml contains both MIT-covered and Apache-covered source files, so its upstream combined LICENSE and NOTICE are reproduced separately in addition to the shared Apache-2.0 text. The Go
standard library and listed `golang.org/x/*` modules use the same Go Authors BSD
3-Clause license text represented by the shared copy above. Components with
project-specific MIT or BSD notices retain those notices in separate files.

Dependency versions and this notice inventory must be reconciled before each
release whenever `go.mod` or `go.sum` changes.
