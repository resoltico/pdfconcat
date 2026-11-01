# PDFConcat

Build a PDF packet from source pages, sections, title dividers, linked contents
and physical page numbers. Keep the choices in a JSON plan, check it, then build
the PDF. A full report records source identities and final page mappings.

PDFConcat is a Go CLI built on [pdfcpu](https://github.com/pdfcpu/pdfcpu). It
includes Noto Sans for generated Latin, Greek and Cyrillic text. The executable
needs no PDF tools, font installation or configuration directory at runtime.

This source tree uses plan v4 and result/report v5. Use the documentation supplied
with a published archive for that binary's contracts.

## Install

Download the archive for your operating system and CPU from
[published releases](https://github.com/resoltico/pdfconcat/releases), verify it
against the release's `checksums.txt`, and extract it. The executable is at the
archive root. Run `./pdfconcat version` on macOS/Linux or
`.\pdfconcat.exe version` in PowerShell.

With the Go version declared in [go.mod](go.mod), install the latest published tag:

```sh
go install github.com/resoltico/pdfconcat/cmd/pdfconcat@latest
pdfconcat version
```

Add `go env GOBIN`, or `bin` beneath `go env GOPATH` when GOBIN is empty, to PATH.

## First packet

```sh
pdfconcat check cover.pdf --blank appendix.pdf
pdfconcat build -o packet.pdf cover.pdf --blank appendix.pdf --report packet.report.json
```

`--blank` inserts one generated page with default style. Its size comes from a
source or `--fit-to SIZE`; for example, `pdfconcat check --blank --fit-to A4`
checks a source-free blank. Options may interleave with operands; `--` makes
following arguments literal paths. Text summaries go
to stdout and diagnostics to stderr. Add `--json` for one machine-readable result.

For a generated text page, save `packet.json` beside the source PDFs:

```json
{
  "version": 4,
  "output": "packet.pdf",
  "page_defaults": {
    "text": {
      "value": "Pielikums Ā ļ ķ — Ελληνικά",
      "size": 18
    }
  },
  "items": [
    { "source": "cover.pdf" },
    { "page": {} },
    { "source": "appendix.pdf" }
  ]
}
```

```sh
pdfconcat check --plan packet.json
pdfconcat build --plan packet.json --report packet.report.json
```

A source item can select page ranges. A titled group creates a section; a divider
can paint its title. Add a contents item, odd starts and page numbers as needed.
See the [composition example](examples/plans/packet-composition.json) and
[plan reference](docs/PLAN.md). The [CLI reference](docs/CLI.md) describes streams,
results, exit codes, reports and overwrite behavior.

## PDFConcat or the pdfcpu CLI?

Both concatenate existing PDFs. Use **pdfcpu** for a straightforward merge when
you already use its PDF tools, or when you need appending, interleaving, PDF
streams or general editing. Use **PDFConcat** to assemble a structured packet:
keep excerpts, sections, title pages, odd starts, linked contents and numbering
together in one reviewable plan, check its layout, and retain a source-to-output
report.

● means built-in support; an empty cell means the CLI does not provide that
capability. The table covers each tool's commands as a whole; pdfcpu can require
separate commands for selection, generation, merging and numbering.

| Capability | pdfcpu CLI | PDFConcat |
| --- | :---: | :---: |
| Concatenate PDFs in the supplied order | ● | ● |
| Append PDFs directly to an existing output file | ● | |
| Automatically interleave two PDFs: A1, B1, A2, B2, … | ● | |
| Read PDF input from stdin and write PDF output to stdout | ● | |
| Select, repeat or reorder source pages | ● | ● |
| Keep source pages at their original sizes | ● | ● |
| Insert blank pages | ● | ● |
| Preserve imported source bookmarks | ● | ● |
| Add custom PDF bookmarks | ● | ● |
| Fit pages to a chosen paper size | ● | ● |
| Generate text pages from JSON | ● | ● |
| Add physical page numbers | ● | ● |
| Describe the complete packet in one JSON assembly plan | | ● |
| Define nested sections and their title dividers in that plan | | ● |
| Automatically pad sections to start on odd pages | | ● |
| Automatically generate linked contents from sections and final page numbers | | ● |
| Check planned generated-text layout and overflow before writing a PDF | | ● |
| Report source SHA-256 identities and source-to-output page ranges | | ● |
| Generate Latin, Greek and Cyrillic text with a bundled font | | ● |
| Split a PDF into multiple output files in one command | ● | |
| Apply general text, image or PDF stamps and watermarks | ● | |
| Encrypt, decrypt and manage PDF passwords | ● | |
| Fill interactive forms | ● | |

See the [pdfcpu command reference](https://pdfcpu.io/) and
[PDFConcat plan reference](docs/PLAN.md) for options and limits. PDFConcat's
`check` predicts assembly; a later build or publication can still fail.

## Limits

- Plans bound input size, nesting, authored items and generated pages; see [plan limits](docs/PLAN.md#strictness-and-limits).
- PDFs are trusted local inputs; parsing and assembly can exhaust resources. See [security](SECURITY.md).
- Encryption, populated signatures and certain unsafe shapes are refused; see [source policy](docs/PDF-POLICY.md#source-policy).
- Page selection composes visible pages and does not redact omitted content; see [selections](docs/PLAN.md#source-selections).
- Forms and numeric internal destinations restrict fitting; see [PDF compatibility](docs/PDF-POLICY.md#forms-fitting-and-catalog).
- Retained page actions can run in a viewer; see [PDF policy](docs/PDF-POLICY.md#source-policy).
- Footer overlays may cover source content; see [page numbers](docs/PLAN.md#physical-footer-numbers).
- Publication has filesystem and durability limits; inspect [publication facts](docs/CLI.md#reports-and-committed-state) before retrying.

## Build from source

Use Go at the version in [go.mod](go.mod):

```sh
git clone https://github.com/resoltico/pdfconcat.git
cd pdfconcat
go build -o bin/pdfconcat ./cmd/pdfconcat
bin/pdfconcat version
```

Windows builds use `bin/pdfconcat.exe`. Contributor prerequisites, checks and
release procedures are in [CONTRIBUTING](CONTRIBUTING.md). The package map and
execution pipeline are in [ARCHITECTURE](docs/ARCHITECTURE.md).

Application code is [MPL-2.0](LICENSE). Bundled dependencies and fonts retain the
licenses listed in [third-party notices](THIRD_PARTY_NOTICES.md).
