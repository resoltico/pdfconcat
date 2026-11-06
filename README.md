# PDFConcat

PDFConcat is a command-line tool for combining PDFs and selected pages into one
organized document. Merge whole files directly, or save a reusable JSON plan for
excerpts, nested sections, title pages, a linked table of contents and page numbers.
An optional assembly report records input file hashes and source-to-output page
mappings.

Source pages keep their original sizes unless you request fitting. Generated text
uses bundled Noto Sans for Latin, Greek and Cyrillic; the program needs no external
PDF tools, font installation or configuration directory at runtime.

- [Install and verify](#install)
- [Merge PDFs and insert blanks](#merge-pdfs)
- [Fit pages to A4](#fit-page-sizes)
- [Use a reusable plan and select pages](#keep-the-composition-in-a-plan)
- [Automate commands and save reports](#automation-and-reports)
- [Compare with the pdfcpu CLI](#pdfconcat-or-the-pdfcpu-cli)
- [Check the limits](#limits)

## Install

On macOS 15+ or Linux:

```sh
curl -fsSL https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh | sh
```

On Windows 10 or 11, in PowerShell:

```powershell
powershell -ExecutionPolicy ByPass -c "irm https://github.com/resoltico/pdfconcat/releases/latest/download/install.ps1 | iex"
```

The installer puts one self-contained program in your user account without an
administrator password and configures PATH when needed. On macOS/Linux, follow
any PATH command it prints for the current terminal. On Windows, open a new
terminal window. Then verify the installation:

```sh
pdfconcat version
```

Run the same installation command again to update. The
[installation guide](docs/INSTALL.md) covers supported architectures, uninstalling,
custom paths, manual downloads, verification and troubleshooting. Releases are not
notarized by Apple or code-signed for Windows; the guide explains what that means.

## Merge PDFs

Run these commands in a folder containing two PDFs of your own, `cover.pdf` and
`appendix.pdf`. Choose an unused output name:

```sh
pdfconcat check cover.pdf appendix.pdf
pdfconcat build -o packet.pdf cover.pdf appendix.pdf
```

Inputs appear in the order given. `check` reads and validates them without
creating a PDF; `build` creates the assembled document. Original inputs stay
unchanged. A successful check does not guarantee that a later build or file
publication will succeed.

To put a blank page between the documents and save an assembly report:

```sh
pdfconcat build -o separated.pdf cover.pdf --blank appendix.pdf --report separated.report.json
```

The blank inherits the preceding source page's displayed size. Use `--overwrite`
only when you intend to replace existing regular output/report files. Input
files are protected from replacement.

## Fit page sizes

To place all source pages on portrait A4 sheets:

```sh
pdfconcat build -o packet-a4.pdf --fit-to A4 cover.pdf appendix.pdf
```

Fitting preserves proportions and clips source artwork to its original visible
frame; it does not reflow text. Annotation appearances, interactive hit areas,
forms and internal destinations can restrict fitting. If fitting is refused,
follow the diagnostic and [fitting policy](docs/PDF-POLICY.md#fitting); some inputs
need reviewed appearances or their original page sizes. Omit `--fit-to` to retain
original sizes.

For a first run without preparing any input PDFs, create one blank A4 page in a
writable folder where `blank-a4.pdf` does not already exist:

```sh
pdfconcat check --blank --fit-to A4
pdfconcat build -o blank-a4.pdf --blank --fit-to A4
```

## Keep the composition in a plan

A plan keeps sources, page selections and generated pages together so you can
check and rebuild the same composition. Save this as `packet.json` beside your
two source PDFs:

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

This adds a linked table of contents and an A4 section title page, starts the
section on an odd physical page, and adds footer numbers. Section titles also
create PDF bookmarks. The A4 `page_defaults` style sets the section title page;
it does not resize source pages. To fit the whole packet, add `"fit_to": "A4"` at the plan's top level.

For an excerpt, replace the appendix source item with the following, using an
`appendix.pdf` with at least seven pages:

```json
{ "source": "appendix.pdf", "pages": [[2, 4], [7, 7]] }
```

This includes pages 2–4 and 7. Ranges within one item must ascend without overlaps;
use separate source items to repeat or reorder material. The whole source is
still validated, and omitted content is not guaranteed to be erased. See
[source selections](docs/PLAN.md#source-selections) for the limits of page selection.

Relative source and output paths in this plan resolve beside `packet.json`;
CLI `-o` and `--report` paths resolve from the current working directory. The
[plan reference](docs/PLAN.md) explains nested path scopes, layout and fonts.

The [repository examples](docs/PLAN.md#examples) include a
[generated-only plan](examples/plans/generated-only.json) needing no input PDFs
or external fonts, and a [composition plan](examples/plans/packet-composition.json)
with excerpts and nested sections. Examples are not included in binary archives.
Save a copy into your working folder before adapting it; plan-relative outputs
are created beside the saved plan.

## Automation and reports

Add `--json` to `check` or `build` for one machine-readable result. Without it,
stdout contains a text summary and stderr contains diagnostics. Options may
appear between inputs; `--` makes subsequent arguments literal paths. Quote
paths containing spaces in your shell.

`--report FILE` requests a full JSON assembly report from either command,
including when processing fails. Rejected command-line arguments produce no report.
It records input SHA-256 hashes, page mappings and uncapped diagnostics. PDF and report publication are separate operations;
inspect the final result before retrying after a failure. See
[results, reports and exit codes](docs/CLI.md#output).

Plans use format v4; results and reports use format v6. Print their schemas with:

```sh
pdfconcat schema plan
pdfconcat schema result
```

These instructions describe the current source. For a released binary, consult
the documentation at its matching release tag; `pdfconcat version` identifies
that version. Matching schema numbers alone do not establish support for every
field. The [CLI reference](docs/CLI.md) covers stdin plans, argument handling,
exit codes, diagnostics and publication facts.

## PDFConcat or the pdfcpu CLI?

PDFConcat uses [pdfcpu](https://github.com/pdfcpu/pdfcpu) as its PDF engine. Both
tools can merge documents, select pages, insert blanks, generate text, preserve
bookmarks, resize pages and add page numbers. Their workflows and limits differ.

Use **pdfcpu** for its broad range of PDF editing commands, appending, automatic
interleaving, PDF pipelines, encryption or form filling. Use **PDFConcat** when
you want to describe a packet's sources, sections and generated pages in one
plan, check its layout, and retain an assembly report.

The comparison below is against the released **[pdfcpu CLI v0.16.4](https://github.com/pdfcpu/pdfcpu/releases/tag/v0.16.4)**.
The PDFConcat column describes the current source.

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
| Validate PDF structure without creating a PDF [6] | ● | ● |
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
- Encrypted PDFs (including those opening without a password), populated signatures and certain unsafe structures are refused. Retained page actions can still run in a viewer. See the [source policy](docs/PDF-POLICY.md#source-policy).
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
licenses listed in [third-party notices](internal/legal/THIRD_PARTY_NOTICES.md); `pdfconcat licenses` prints them.
