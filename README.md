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

For a generated divider, save `packet.json` beside the source PDFs:

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

| Task | pdfcpu CLI | PDFConcat |
| --- | --- | --- |
| Join existing PDFs | `pdfcpu merge packet.pdf first.pdf second.pdf` | `pdfconcat build -o packet.pdf first.pdf second.pdf` |
| Combine excerpts, sections, dividers, contents and numbering | Coordinate PDF operations in a script | Keep packet choices in one plan |
| Generate Latvian, Greek or Cyrillic text | Install a suitable font | Use the included Noto Sans |
| Check layout and track included pages | Validate PDFs and track the script's choices | Check the plan and retain its full report |
| Split, watermark, encrypt or fill forms | Use the corresponding pdfcpu tools | Use pdfcpu for those editing tasks |

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
