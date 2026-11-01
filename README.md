# PDFConcat

Assemble PDF documents and selected pages into a packet with sections, title
pages, linked contents and page numbers. Save the composition in a JSON plan,
check the planned layout, then build the PDF. An optional report records input
file hashes and where their pages appear in the result.

You can also concatenate whole PDFs and insert blank pages directly on the
command line. The executable includes its PDF engine and Noto Sans for generated
Latin, Greek and Cyrillic text; no external PDF tools, font installation or
configuration directory are needed at runtime.

## Install

Download the archive for your operating system and CPU from
[published releases](https://github.com/resoltico/pdfconcat/releases), verify it
against that release's `checksums.txt`, and extract it. The executable is at the
archive root. From the extracted directory, run:

```sh
# macOS or Linux
./pdfconcat version
```

```powershell
# Windows PowerShell
.\pdfconcat.exe version
```

To use the `pdfconcat` commands below from any directory, place the executable in
a directory on your `PATH`, or add its directory to `PATH`. Otherwise, replace
`pdfconcat` in each command with the executable's relative or absolute path.

### Install with Go

With the Go version declared in [go.mod](go.mod), install the latest published tag:

```sh
go install github.com/resoltico/pdfconcat/cmd/pdfconcat@latest
```

The executable goes into the directory printed by `go env GOBIN`. If that value
is empty, it goes into `bin` beneath the first directory printed by
`go env GOPATH`. Add that installation directory to `PATH`, then run
`pdfconcat version`.

This source tree uses plan v4 and result/report v5. For a published binary, use
the documentation and examples supplied in its release archive. Matching schema
version numbers alone do not establish support for every documented field.

## First packet

Use two PDFs of your own, named `cover.pdf` and `appendix.pdf` in the current
directory:

```sh
pdfconcat check cover.pdf --blank appendix.pdf
pdfconcat build -o packet.pdf cover.pdf --blank appendix.pdf --report packet.report.json
```

This places one blank page between the documents. Its displayed size is
inherited from the preceding source page. `check` reads the inputs and predicts
the assembly without creating a PDF; `build` creates the PDF and requested
report. A successful check does not guarantee that a later build or file
publication will succeed.

Choose unused output names, or add `--overwrite` to permit replacement of
existing regular output/report files. Text summaries go to stdout and diagnostics
to stderr. Add `--json` for one machine-readable result. Options may appear
between operands; `--` makes subsequent arguments literal paths.

To try the tool without supplying any PDFs, use the bundled
[generated-only example](examples/plans/generated-only.json). From an extracted
release archive or repository checkout:

```sh
pdfconcat check --plan examples/plans/generated-only.json
pdfconcat build --plan examples/plans/generated-only.json
```

It creates `Dividers.pdf` beside the plan, containing two text pages and three
blank pages.

## Keep the composition in a plan

Save this as `packet.json` beside your two source PDFs:

```json
{
  "version": 4,
  "output": "review-packet.pdf",
  "page_defaults": { "size": "A4", "text": { "size": 24 } },
  "page_numbers": {},
  "items": [
    { "source": "cover.pdf" },
    { "contents": {} },
    {
      "title": "Supporting documents",
      "start_on": "odd",
      "divider": {},
      "items": [{ "source": "appendix.pdf" }]
    }
  ]
}
```

```sh
pdfconcat check --plan packet.json
pdfconcat build --plan packet.json --report review-packet.report.json
```

The plan adds linked contents and an A4 section title page, starts the section on
an odd physical page, and adds footer numbers. Source pages retain their sizes
unless you request fitting. Relative source and output paths in this example
are resolved beside the plan; the CLI `--report` path is relative to the current
directory.

The [examples guide](docs/PLAN.md#examples) explains the supplied plans and the
inputs each needs. Start with the
[composition example](examples/plans/packet-composition.json) for excerpts and
nested sections. The [plan reference](docs/PLAN.md) covers the JSON format, text
layout and fonts; the [CLI reference](docs/CLI.md) covers commands, streams,
results, reports, exit codes and overwrite behavior.

## PDFConcat or the pdfcpu CLI?

PDFConcat uses [pdfcpu](https://github.com/pdfcpu/pdfcpu) as its PDF engine. Both
tools can merge documents, select pages, insert blanks, generate text, preserve
bookmarks, resize pages and add page numbers. Their workflows and limits differ.

Use **pdfcpu** for its broad range of PDF editing commands, appending, automatic
interleaving, PDF pipelines, encryption or form filling. Use **PDFConcat** when
you want to describe a packet's sources, sections and generated pages in one
plan, check its layout, and retain an assembly report.

The comparison below is against the released **[pdfcpu CLI v0.16.4](https://github.com/pdfcpu/pdfcpu/releases/tag/v0.16.4)**,
checked on **1 November 2026**. The PDFConcat column describes this source tree.
"No dedicated command" means no equivalent integrated CLI workflow in that
release; it does not rule out obtaining the result with several commands and an
external script.

| Task | pdfcpu CLI v0.16.4 | PDFConcat |
| --- | --- | --- |
| Merge PDFs in a chosen order | `merge`; optional filename sorting | Ordered operands or plan items |
| Append to an existing PDF; automatically interleave two PDFs | `merge --mode append`; `merge --mode zip` | No append or automatic interleave mode |
| Read/write PDF bytes through stdin/stdout | Supported with command/mode limits [1] | PDF inputs and output are files; a JSON plan may come from stdin |
| Select, repeat or reorder pages | `collect` with page expressions | Selected ranges in ordered source occurrences; repetition/reordering uses separate occurrences |
| Add blank pages | `pages insert`; `merge --divider` between documents | `--blank`, generated page items and automatic odd-start padding |
| Preserve imported bookmarks | File merges support filename wrappers or `--bookmark-mode preserve` [2] | Retained source bookmarks appear beneath the nearest authored section [2] |
| Author bookmarks | `bookmarks import` accepts a bookmark tree with explicit page targets and styles | Titled plan sections generate bookmarks to their first non-padding page |
| Keep original page sizes or fit to a paper size | Merge without resizing; `resize` when fitting is wanted | Keep source sizes by default; optional `fit_to` / `--fit-to` |
| Generate text pages from JSON | `create` also supports images, tables, headers/footers and forms | Generated page items and section title dividers |
| Use a bundled font for Latin, Greek and Cyrillic text | Roboto Regular, automatically initialized in the configuration font store [3] | Noto Sans Regular, used directly without a configuration/font directory |
| Add page numbers | Text stamps support current/total page numbers and an offset | Physical footer numbers integrated with the plan; padding counts in the sequence but has no printed footer |
| Assemble sources, nested sections and title dividers in one JSON plan | No dedicated packet assembly plan | Supported by plan v4 |
| Automatically start authored sections on odd pages | No dedicated section-alignment command | `start_on: "odd"` inserts padding when needed |
| Generate linked contents from sections and final page numbers | No dedicated command | A `contents` item derives entries and targets from the plan |
| Check planned text layout without producing an output PDF | No equivalent packet-plan preflight command [4] | `check` predicts layout and checks generated-text overflow |
| Report input SHA-256 identities and source-to-output page ranges | No equivalent assembly report; `info --json` inspects PDF properties | Full JSON assembly report with `--report` |
| Split PDFs; apply general stamps/watermarks; encrypt/decrypt; fill forms | Dedicated commands | Outside the tool's scope |

1. For pdfcpu merge, create mode accepts one stdin input; create and zip can
   write to stdout. Append and zip do not accept stdin; append does not write to
   stdout. This does not make every merge mode a full PDF pipeline.
2. pdfcpu normally wraps source bookmarks under filenames; `--bookmark-mode preserve`
   avoids those wrappers. Zip does not preserve source outlines, and
   stdin merge does not import later inputs' bookmark trees. PDFConcat prunes
   local navigation to omitted pages and applies its
   [source policy](docs/PDF-POLICY.md#source-policy). Support here does not promise
   identical navigation or preservation of every PDF feature.
3. pdfcpu includes Roboto; no separate font download is required for it. Its
   `--conf disable` mode can avoid filesystem configuration for operations that
   do not need user fonts, but does not make that font store available.
4. pdfcpu validates PDF structures and validates/renders JSON during `create`.
   PDFConcat's distinction is a separate assembly check, not exclusive access to
   validation. PDFConcat `check` can still write a report when requested.

Version-specific pdfcpu sources:
[document commands](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/cmd/pdfcpu/document.go),
[merge and streaming behavior](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/pkg/cli/document_exec.go),
[bookmark merge modes](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/pkg/pdfcpu/merge.go),
[page operations](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/cmd/pdfcpu/pages.go),
[stamps and bookmarks](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/cmd/pdfcpu/content_usage.go),
[font initialization](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/pkg/pdfcpu/model/configuration.go),
[JSON creation](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/pkg/api/create.go), and
[PDF info fields](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/pkg/pdfcpu/info.go).
The [pdfcpu documentation](https://pdfcpu.io/) provides usage examples; its live
pages may describe a later version.

## Limits

- Use trusted local PDFs. Parsing and assembly can exhaust resources; plan limits are not a memory cap. See [security](SECURITY.md) and [plan limits](docs/PLAN.md#strictness-and-limits).
- Encrypted PDFs, populated signatures and certain unsafe structures are refused. Retained page actions can still run in a viewer. See the [source policy](docs/PDF-POLICY.md#source-policy).
- Page selection composes visible pages; it does not redact omitted content or guarantee removal of private objects. See [source selections](docs/PLAN.md#source-selections).
- Forms and numeric internal destinations restrict fitting. See [PDF compatibility](docs/PDF-POLICY.md#forms-fitting-and-catalog) before relying on interactive features.
- Footer numbers may cover source content; there is no overlap detector. See [page numbers](docs/PLAN.md#physical-footer-numbers).
- PDF and report publication are separate operations, and a late failure can occur after a file becomes visible. Inspect [publication facts](docs/CLI.md#reports-and-committed-state) before retrying.

## Build from source

Use Go at the version in [go.mod](go.mod):

```sh
git clone https://github.com/resoltico/pdfconcat.git
cd pdfconcat
go build -o bin/pdfconcat ./cmd/pdfconcat
bin/pdfconcat version
```

On Windows, use `go build -o bin/pdfconcat.exe ./cmd/pdfconcat` and run
`.\bin\pdfconcat.exe version` in PowerShell. Contributor prerequisites, checks
and release procedures are in [CONTRIBUTING](CONTRIBUTING.md). The package map
and execution pipeline are in [ARCHITECTURE](docs/ARCHITECTURE.md).

Application code is [MPL-2.0](LICENSE). Bundled dependencies and fonts retain the
licenses listed in [third-party notices](THIRD_PARTY_NOTICES.md).
