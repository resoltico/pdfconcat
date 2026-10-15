# PDFConcat

Assemble a packet of PDFs in a declared order, with generated separator or blank
pages where you need them. One JSON plan describes the packet. `check` reads the
inputs and lays out generated text before `build` publishes a PDF.

PDFConcat uses unmodified [pdfcpu](https://github.com/pdfcpu/pdfcpu). It adds size
inheritance, built-in Unicode text, per-occurrence named destinations, explicit
source refusals and machine-readable results. Source bookmarks and supported
static forms are retained. It is a CLI for trusted local documents.

## Build from source

Build the current code from `main`:

```sh
git clone --branch main https://github.com/resoltico/pdfconcat.git
cd pdfconcat
go build -o bin/pdfconcat ./cmd/pdfconcat
bin/pdfconcat version
```

Install Go at the version declared in [go.mod](go.mod). The source build needs no
PDF CLI, system font installation or runtime configuration directory. Windows
builds use `bin/pdfconcat.exe`.

The [release workflow](https://github.com/resoltico/pdfconcat/blob/main/.github/workflows/release.yml) publishes macOS, Linux and Windows archives for amd64/arm64, with checksums and provenance, when a release is tagged. Check [published releases](https://github.com/resoltico/pdfconcat/releases) for available artifacts and use the documentation supplied with the selected version. Building `main` uses the code and contracts documented here; `@latest` selects the latest published module tag.

## First packet

```sh
bin/pdfconcat check cover.pdf --blank appendix.pdf
bin/pdfconcat build -o packet.pdf cover.pdf --blank appendix.pdf --report packet.report.json
```

`--blank` inserts one plain page that inherits the neighboring source's displayed
size. Options may interleave with operands; `--` makes all following arguments
literal paths. Output/report files require `--overwrite` to replace an existing
regular file and cannot alias an input or each other.

For a separator with text, save this as `packet.json` next to the source PDFs:

```json
{
  "version": 2,
  "output": "packet-with-separator.pdf",
  "page": { "text": { "value": "Pielikums Ā ļ ķ — Ελληνικά", "size": 18 } },
  "items": ["cover.pdf", { "page": {} }, "appendix.pdf"]
}
```

```sh
bin/pdfconcat check --plan packet.json
bin/pdfconcat build --plan packet.json --report packet-with-separator.report.json
```

Results are JSON by default. Use `--format text` for a human-readable result.
`--report` saves all diagnostics and per-part ranges, hashes, layout and actual
form-name mappings. `check` writes no PDF; it can write its requested report.

## PDFConcat and the pdfcpu CLI

The comparison uses the pdfcpu version selected in [go.mod](go.mod). The small
[same-input comparison](https://github.com/resoltico/pdfconcat/blob/main/scripts/run-comparison.sh)
requires a source checkout, uses fresh fixtures and checks command exits, results
and outputs. Broader fidelity cases use qpdf and Poppler
in the [product tests](CONTRIBUTING.md#what-we-test-and-what-we-do-not).

| Need | pdfcpu CLI | PDFConcat |
| --- | --- | --- |
| Join existing sources in order | `pdfcpu merge` | Ordered operands or one plan |
| One reviewed packet description | Merge filenames; separate `create` JSON for a generated document | One plan with sources, groups, generated pages and defaults |
| Latvian, Greek and Cyrillic separator text | The Helvetica comparison fixture retains `Pielikums` and the dash but drops the tested letters/words | Built-in Noto Sans shapes and extracts the complete tested text |
| Repeated sources with named destinations | First-copy named links target the later copy in the comparison fixture | Each copy's links stay within that copy |
| Repeated legacy `/Dests` | Later definitions redirect earlier links | Per-occurrence destinations are rebuilt correctly |
| Dry run of a packet and its text layout | `validate` checks existing PDFs | `check` predicts ranges and layout; optional report only |
| Forms from multiple sources | Backend namespaces later fields | Same namespacing, with actual mappings; differing resolved `/DR` or `/DA`/`/Q` representations are refused |
| Signature values | Merges the signed comparison fixture without refusal | Refused before assembly; no signature authentication is performed |
| Damaged page tree | Accepts and repairs the empty page dictionary in the comparison fixture | Refused as `source_damaged` |
| Uniform sheet size | Separate `resize` operation | `fit_to`/`--fit-to` uses upstream resize with unit normalization |
| Program-readable outcome | Human command output | Result v3, stable codes, source hashes and a full optional report |

Generated text uses glyph-pruned fonts shared across generated items. Fonts that prohibit subsetting remain whole; output size depends on the inputs and font permissions.

## Limits and behavior

- Plan v2 declares `version: 2` and generated items with `page`; output uses result v3.
- At most 64 MiB of plan JSON, 64 group levels, 100,000 entries and 100,000
  generated pages. Each text value holds at most 10,000 characters.
- Generated text supports left-to-right Latin, Greek and Cyrillic with a static
  TrueType font. Missing glyphs and unsupported characters are errors. Overflow
  is refused by default; `overflow: allow` places measured overflow with a warning.
- Source policy refuses encryption, signature values, dynamic forms, optional
  content, damaged page trees and specific unsafe/unsupported shapes. Upstream
  relaxed parsing may accept other malformed syntax or repairs.
- Fitting refuses widgets and does not transform destination scroll coordinates.
  Static forms require compatible defaults/resources. A form without widget
  appearances can fail assembly into PDF 2.0, including when another source
  raises the output version. [DESIGN](docs/DESIGN.md)
  lists the full policy and fidelity limits.
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
