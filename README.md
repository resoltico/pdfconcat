# PDFConcat

Build a PDF packet with selected source pages, sections, title dividers, linked
contents and physical page numbers. Save the order and page settings in one JSON
plan, check it, then build the PDF. An optional report maps included source pages
to their places in the packet.

PDFConcat includes Noto Sans for Latin, Greek and Cyrillic generated text and uses
[pdfcpu](https://github.com/pdfcpu/pdfcpu) as its PDF engine. You need only the
PDFConcat executable to use the built-in font. It is intended for trusted local
documents.

## Use a published release

This documentation describes the current source tree: plan v3 and result v4.
For a published binary, use the matching documentation supplied with its archive.
A [source build](#build-from-source) follows the contracts documented here.

Download the archive matching your operating system and CPU from [published releases](https://github.com/resoltico/pdfconcat/releases), verify its checksum against `checksums.txt`, and extract it. The executable is at the archive root. From that directory, run `./pdfconcat version` on macOS/Linux or `.\pdfconcat.exe version` in Windows PowerShell. `version` reports the release version and build identity.

With Go at the version declared in [go.mod](go.mod), install the latest published
module tag into your Go binary directory:

```sh
go install github.com/resoltico/pdfconcat/cmd/pdfconcat@latest
pdfconcat version
```

Add that directory (`go env GOBIN`, or `bin` under `go env GOPATH` when GOBIN is empty) to PATH if needed. The examples below use `pdfconcat` on PATH; you can also use the extracted executable path.

## First packet

```sh
pdfconcat check cover.pdf --blank appendix.pdf
pdfconcat build -o packet.pdf cover.pdf --blank appendix.pdf --report packet.report.json
```

`--blank` inserts one plain page that inherits the neighboring source's displayed
size. Options may interleave with operands; `--` makes all following arguments
literal paths. Use `--overwrite` to replace an existing regular output/report
file. Known input, output and report collisions are refused.

For a divider with text, save this as `packet.json` next to the source PDFs:

```json
{
  "version": 4,
  "output": "packet-with-divider.pdf",
  "page_defaults": {
    "text": {
      "value": "Pielikums Ā ļ ķ — Ελληνικά",
      "size": 18
    }
  },
  "items": [
    {
      "source": "cover.pdf"
    },
    {
      "page": {}
    },
    {
      "source": "appendix.pdf"
    }
  ]
}
```

```sh
pdfconcat check --plan packet.json
pdfconcat build --plan packet.json --report packet-with-divider.report.json
```

Results are text by default. Add `--json` for one machine-readable result.
`--report` saves all diagnostics and per-part ranges, hashes, layout and actual
form-name mappings. `check` writes no PDF; it can write its requested report.
Passing check does not guarantee that a later build will succeed.

## Sections, selected pages and physical numbering

A source object can select ascending disjoint ranges, for example
`{"source":"report.pdf","pages":[[2,4],[7,7]]}`. A titled container creates a
bookmark and contents entry; its optional `divider` paints the same title.
Nested sections retain the original source bookmarks beneath them.

Put one `contents` item where you want the linked contents pages. Set
`start_on: "odd"` on a container for automatic blank backs, and add
`page_numbers: {}` at the root to print physical page numbers. All features use
the same final page positions. Padding counts but stays visually blank; authored
blank pages receive numbers. Footers may cover existing content; overlap is not
detected. Selection is packet composition, not redaction, and partial sources
with forms/widgets are refused.

See the [complete composition plan](examples/plans/packet-composition.json)
and [syntax, layout and limits](docs/PLAN.md).

## PDFConcat or the pdfcpu CLI?

For a simple merge, `pdfcpu merge` already covers the task. PDFConcat adds a
packet workflow on top of the same engine: one recipe coordinates selected
pages, sections, dividers, linked contents, odd-page starts and numbering. You
can check the complete recipe before building and keep a report of the final
page ranges and source identities.

pdfcpu can also create pages, insert blanks, manage bookmarks and resize PDFs.
PDFConcat's purpose is to coordinate those packet decisions in one plan.

| What you want to do | pdfcpu CLI | PDFConcat |
| --- | --- | --- |
| Join existing PDFs in order | `pdfcpu merge packet.pdf first.pdf second.pdf` | `pdfconcat build -o packet.pdf first.pdf second.pdf` |
| Rebuild a packet with excerpts, sections, dividers and numbering | Coordinate the PDF operations and generated pages in your script | Keep the packet choices together in one JSON plan |
| Use Latvian, Greek or Cyrillic divider text | Create pages with an installed font covering the text | Use the included Noto Sans without font installation |
| Check layout and track where packet pages came from | Validate the PDFs and track your script's assembly decisions | `check` predicts ranges and generated-text layout; `--report` records source hashes and page mappings |
| Split, extract, watermark, encrypt or fill forms | Use the corresponding pdfcpu tools | Use pdfcpu for these editing tasks |

PDFConcat applies a defined input policy. For example, it refuses encrypted PDFs
and PDFs containing signature values. It handles repeated internal named links
so each copy's supported links point into that copy. Forms must meet the supported
form rules. Fitting refuses interactive form fields and internal page
destinations with explicit numeric position coordinates, including destinations
used by links and bookmarks. See [PDF behavior and limits](docs/DESIGN.md).


## Build from source

With Go at the version declared in [go.mod](go.mod), build the current code from
`main`:

```sh
git clone --branch main https://github.com/resoltico/pdfconcat.git
cd pdfconcat
go build -o bin/pdfconcat ./cmd/pdfconcat
bin/pdfconcat version
```

The source build needs no PDF CLI, system font installation or runtime
configuration directory. Windows builds use `bin/pdfconcat.exe`.

The [release workflow](https://github.com/resoltico/pdfconcat/blob/main/.github/workflows/release.yml) publishes macOS, Linux and Windows archives for amd64/arm64, with checksums and provenance, when a release is tagged. Check [published releases](https://github.com/resoltico/pdfconcat/releases) for available artifacts and use the documentation supplied with the selected version. Building `main` uses the code and contracts documented here; `@latest` selects the latest published module tag.

## Limits and behavior

- Plan v3 declares `version: 3` and generated items with `page`; output uses result v4.
- At most 64 MiB of plan JSON, 64 container levels, 100,000 entries and 100,000
  generated pages. Each text value holds at most 10,000 characters.
- Generated text supports left-to-right Latin, Greek and Cyrillic with a static
  TrueType font. Missing glyphs and unsupported characters are errors. Overflow
  is refused by default; `overflow: allow` places measured overflow with a warning.
- Source policy refuses encryption, signature values, dynamic forms, optional
  content, damaged page trees and specific unsafe/unsupported shapes. Upstream
  relaxed parsing may accept other malformed syntax or repairs.
- Fitting refuses interactive form fields and internal page destinations with
  explicit numeric position coordinates. Use the original page sizes for those
  sources. Whole-page destinations and valid destinations without numeric
  position coordinates are supported.
- Static forms require compatible defaults/resources. Some form and annotation
  combinations are incompatible with PDF 2.0 output. Check reports the known
  appearance conflict; build can still fail for other assembly or writing reasons.
- Tags, some document settings and catalog information are removed. The output
  is not promised to retain PDF/A, PDF/X or accessibility conformance. See the
  full [PDF behavior and limits](docs/DESIGN.md).
- Assembly is in memory. PDF object graphs and generated pages can cost much more
  RAM than compressed input bytes. There is no universal memory or time bound.
- This is not a sanitizer: retained page/annotation actions can contain active
  content. No hostile-input memory/CPU sandbox is provided.

Exit codes: `0` ok, `2` invalid request/refusal, `1` environment/backend failure,
`130` interrupted. A PDF may already be published when a later report/durability
failure occurs; inspect `output.published` before retrying.

## Reference

[Plan and text](docs/PLAN.md) · [CLI, result and recipes](docs/CLI.md) ·
[Design and PDF policy](docs/DESIGN.md) · [Contributing](CONTRIBUTING.md) ·
[Security](SECURITY.md) · [Changelog](CHANGELOG.md) ·
[Third-party notices](THIRD_PARTY_NOTICES.md)

In-repository frontends share the [application core](docs/CORE.md). Contributor
setup and the whole-source architecture check are in [CONTRIBUTING](CONTRIBUTING.md).
