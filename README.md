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

**●** means built-in support for the task as described; **—** means no dedicated
built-in equivalent. The table covers each CLI's commands as a whole, so pdfcpu
may require several commands. A dash does not rule out an external script, and
dots do not imply identical defaults or preservation guarantees.

| Task | pdfcpu CLI v0.16.4 | PDFConcat |
| --- | :---: | :---: |
| Merge PDFs in a chosen order | ● | ● |
| Append PDFs to an existing output file | ● | — |
| Automatically interleave two PDFs: A1, B1, A2, B2, … | ● | — |
| Read PDF input from stdin and write PDF output to stdout [1] | ● | — |
| Select, repeat or reorder source pages [2] | ● | ● |
| Keep source pages at their original sizes | ● | ● |
| Insert blank pages | ● | ● |
| Preserve imported source bookmarks [3] | ● | ● |
| Create bookmarks [3] | ● | ● |
| Fit pages to a chosen paper size | ● | ● |
| Generate text pages from JSON | ● | ● |
| Generate Latin, Greek and Cyrillic text with a bundled font [4] | ● | ● |
| Add physical page numbers [5] | ● | ● |
| Describe the complete packet in one JSON assembly plan | — | ● |
| Define nested sections and title dividers in that plan | — | ● |
| Automatically pad sections to start on odd pages | — | ● |
| Generate linked contents from sections and final page numbers | — | ● |
| Check a packet plan's text layout without creating a PDF [6] | — | ● |
| Report input SHA-256 hashes and source-to-output page ranges [6] | — | ● |
| Split a PDF into multiple output files in one command | ● | — |
| Apply general text, image or PDF stamps and watermarks | ● | — |
| Encrypt, decrypt and manage PDF passwords | ● | — |
| Fill interactive forms | ● | — |

1. **PDF streams.** For pdfcpu merge, create mode accepts one stdin input; create
   and zip can write to stdout. Append and zip do not accept stdin, and append
   cannot write to stdout. PDFConcat accepts a JSON plan from stdin, but its PDF
   inputs and output are files.
2. **Page selection.** pdfcpu's `collect` command uses page expressions for
   selection, repetition and reordering. PDFConcat uses ascending, non-overlapping
   ranges within each source occurrence; repetition or reordering uses separate
   occurrences.
3. **Bookmarks.** pdfcpu normally wraps merged source bookmarks under filenames;
   `--bookmark-mode preserve` avoids those wrappers. Zip does not preserve source
   outlines, and stdin merge does not import later inputs' bookmark trees.
   Its `bookmarks import` command accepts explicit page targets and styles.
   PDFConcat creates bookmarks from titled plan sections, targeting their first
   non-padding page, and retains source bookmarks beneath the nearest authored
   section. It prunes local navigation to omitted pages and applies its
   [source policy](docs/PDF-POLICY.md#source-policy).
4. **Bundled fonts.** Both tools include a font for Latin, Greek and Cyrillic.
   pdfcpu automatically initializes Roboto Regular in its configuration font
   store; no separate font download is required. PDFConcat uses Noto Sans
   directly without a configuration/font directory. pdfcpu's `--conf disable`
   mode can avoid filesystem configuration for operations that do not need user
   fonts, but does not make that font store available.
5. **Page numbers.** pdfcpu text stamps support current/total page numbers and an
   offset. PDFConcat's footer numbers follow physical page positions; automatic
   padding counts in the sequence but has no printed footer.
6. **Plan checks and reports.** pdfcpu validates PDF structures and validates
   JSON during `create`; `info --json` reports document properties. PDFConcat
   provides a separate `check` command for planned layout and generated-text
   overflow, plus an assembly report with input hashes and page mappings.
   `check` can write a report when requested; a successful check does not
   guarantee a later build or publication.

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
