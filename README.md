# PDFConcat

PDFConcat combines existing PDFs and selected pages into one ordered document.
Merge whole files directly, or use a reusable JSON plan to select pages and add
separators, simple text inserts, navigation and page numbers. Check a proposed
assembly before building and save a report with input file hashes and
source-to-output page mappings.

Source pages keep their original sizes unless you request fitting. Generated text
uses bundled Noto Sans or a supplied static TrueType/OpenType CFF1 font for Latin,
Greek and Cyrillic. The program needs no external PDF tools, font installation or
configuration directory at runtime.

- [Understand the scope](#scope-and-boundaries)
- [Install and verify](#install)
- [Merge PDFs and insert blanks](#merge-pdfs)
- [Fit pages to A4](#fit-page-sizes)
- [Use a reusable plan and select pages](#keep-the-composition-in-a-plan)
- [Automate commands and save reports](#automation-and-reports)
- [Compare with the pdfcpu CLI](#pdfconcat-or-the-pdfcpu-cli)
- [Check the limits](#limits)

## Scope and boundaries

The core job is to select, order and combine existing PDF pages into one output.
Plans, checks and reports support repeatable, inspectable assembly. Handling page
geometry, links, bookmarks and incompatible inputs is part of correct merging,
subject to the [PDF policy](docs/PDF-POLICY.md). Merging does not promise lossless
preservation of every source document feature.

Assembly aids belong in PDFConcat when they organize the material, help readers
navigate it or prepare its presentation:

- Blanks and odd-page starts separate material and control physical page order.
- Sections, bookmarks, title dividers, linked contents, selective page numbering and authored metadata
  identify material and help readers navigate the combined document.
- Fitting moves pages outside the absolute tolerance onto the target sheet.
  Matching pages keep their native frames. Transformed pages have additional
  [compatibility limits](docs/PDF-POLICY.md#fitting).
- Simple text pages supply covers, separators, explanatory notes and other
  inserts. They support custom fonts, colors, backgrounds, positioning,
  alignment, wrapping and overflow controls.

Text-page authoring is limited to one styled text block on each declared page.
Plans containing only generated pages are supported; source PDFs are optional.
Text wraps within its block but does not flow onto additional pages. Linked
contents has its own automatic pagination. Rich text, tables, image placement,
multiple text columns, general-purpose document templates, Markdown/HTML
conversion and editing or reflowing source-page text are outside the product's
scope. Assemble PDFs prepared in another authoring tool when you need those layouts.

Appending, automatic interleaving and PDF stdin/stdout are relevant merge
workflows. PDFConcat can combine an existing PDF and additional inputs into a
different output, and plans can specify an interleaved order explicitly. It does
not update input files, interleave automatically or read/write PDF streams. See
the [comparison](#pdfconcat-or-the-pdfcpu-cli) for these workflow differences.

PDFConcat's purpose does not include general PDF editing, OCR, redaction, repair,
sanitization, encryption management or form filling. Source validation supports
assembly; it is not a universal PDF preflight service. See [limits](#limits) for
the current input, fidelity and publication constraints.

## Install

Install a standalone release for your user account; no Go installation is needed.
On macOS 15+ or Linux (Apple Silicon/ARM64 or Intel/x86-64):

```sh
sh -c '
    installer=$(curl --proto "=https" --proto-redir "=https" -fsSL https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh) &&
        printf "%s\n" "$installer" | sh
'
```

On Windows 10/11 (x64 or ARM64), use PowerShell.

```powershell
& {
    $ErrorActionPreference = 'Stop'
    $directory = New-Item -ItemType Directory -Path ([IO.Path]::GetTempPath()) -Name ('pdfconcat-install-' + [guid]::NewGuid())
    $installer = Join-Path $directory.FullName 'install-pdfconcat.ps1'
    $keepInstaller = $false
    try {
        Invoke-WebRequest -UseBasicParsing -Uri https://github.com/resoltico/pdfconcat/releases/latest/download/install.ps1 -OutFile $installer
        $keepInstaller = $true
        powershell -NoProfile -ExecutionPolicy ByPass -File $installer
        if ($LASTEXITCODE -ne 0) { throw "Installer failed ($LASTEXITCODE)." }
        $keepInstaller = $false
    } finally {
        if ($keepInstaller) {
            Write-Warning "Installer kept for retry at $installer"
        } else {
            Remove-Item -LiteralPath $directory.FullName -Recurse -Force -ErrorAction Continue
        }
    }
}
```

These commands download and execute a release installer. If Windows installation
fails after download, the command prints the temporary script path for retry.
To inspect or keep a copy for offline uninstall, pin a release, choose a custom
directory or avoid PATH changes, follow the [installation guide](docs/INSTALL.md).
Open a new terminal after installation, then verify:

```sh
pdfconcat version
pdfconcat --help
```

Re-run the download-and-install command to update an unchanged installer-managed
copy. Receiptless executables require the
[replacement or keep-existing steps](docs/INSTALL.md#existing-executable-without-an-installer-receipt).

Use documentation matching the installed executable's
[release tag](https://github.com/resoltico/pdfconcat/releases).

### Build from source

Use Go at the version in [go.mod](go.mod). On macOS or Linux:

```sh
git clone https://github.com/resoltico/pdfconcat.git
cd pdfconcat
go build -o bin/pdfconcat ./cmd/pdfconcat
bin/pdfconcat version
```

On Windows, run the same clone and directory commands, then use PowerShell:

```powershell
go build -o bin/pdfconcat.exe ./cmd/pdfconcat
.\bin\pdfconcat.exe version
```

Put the checkout's `bin` directory on PATH or replace `pdfconcat` in the examples
with the executable's full path. This build does not create installer ownership
receipts. The [installation guide](docs/INSTALL.md) describes supported systems,
release installers, custom paths, removal, verification and troubleshooting.

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
only when you intend to replace existing regular output/report files. Source
PDFs, the plan and consumed external fonts are protected from replacement,
including a font used only by linked contents.

## Fit page sizes

Fit pages outside the default tolerance to portrait A4:

```sh
pdfconcat build -o packet-a4.pdf --fit-to A4 cover.pdf appendix.pdf
```

Fitting compares the displayed width and height with an absolute tolerance of
0.01 pt per side by default. Use `--fit-tolerance 1pt` with `--fit-to A4` to keep
integer-rounded A4 sheets, or `--fit-tolerance 0pt` to require exact dimensions.
Matching pages keep their native geometry. Other pages are scaled proportionally
and centered on the target, preserving visible clipping and the text layout.
Annotation appearances, interactive hit areas, forms and internal destinations
can restrict transformations. For remedies, see the
[fitting policy](docs/PDF-POLICY.md#fitting). Omit `--fit-to` in direct mode to
keep the source sizes.

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
  "version": 8,
  "output": "review-packet.pdf",
  "metadata": { "title": "Review packet", "language": "en" },
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
create PDF bookmarks. `metadata.language` selects glyph shaping for generated body
text, divider titles and contents; absent or unmatched tags use deterministic
neutral shaping. See [authored language](docs/PLAN.md#authored-language).

The A4 `page_defaults` style sets the section title page; it does not resize source
pages. To fit the whole packet, add `"fitting": {"target": "A4"}` at the plan's top level.

Page numbering supports physical or consecutive counting, start offsets,
per-item suppression and six positions. For example,
`"page_numbers": { "mode": "consecutive", "kinds": ["source"], "position": "top-right" }`
numbers only source pages. The [plan reference](docs/PLAN.md#page-numbering)
defines counting, contents labels and placement limits.

For an excerpt, save this complete plan as `excerpt.json`, using an
`appendix.pdf` with at least seven pages:

```json
{
  "version": 8,
  "output": "excerpt.pdf",
  "items": [{ "source": "appendix.pdf", "pages": [[2, 4], [7, 7]] }]
}
```

```sh
pdfconcat check --plan excerpt.json
pdfconcat build --plan excerpt.json
```

This includes pages 2–4 and 7. Ranges within one item must ascend without overlaps;
use separate source items to repeat or reorder material. The whole source is
still validated, and omitted content is not guaranteed to be erased. See
[source selections](docs/PLAN.md#source-selections) for the limits of page selection.

Relative source and output paths resolve beside the saved plan file;
CLI `-o` and `--report` paths resolve from the current working directory. The
[plan reference](docs/PLAN.md) explains nested path scopes, layout and fonts.

The [repository examples](docs/PLAN.md#examples) include a
[generated-only plan](examples/plans/generated-only.json) needing no input PDFs
or external fonts, and a [composition plan](examples/plans/packet-composition.json)
with excerpts and nested sections. Examples are not included in binary archives.
Save a copy into your working folder before adapting it; plan-relative outputs
are created beside the saved plan.

## Automation and reports

Add `--json` to `check` or `build` for one machine-readable result. Text mode
writes the summary to stdout and diagnostics to stderr. Options may appear
between inputs. Put `--` between complete options or input arguments to treat
all following arguments as paths. Quote paths containing spaces in your shell.

`--report FILE` requests a full JSON assembly report from either command. When
destination rules permit, a failed check or build can also save a report. Rejected
command-line arguments produce no report. Reports record input SHA-256 hashes,
page mappings and uncapped diagnostics. Their `page_sizes` runs contain selected
original page ranges, observed source dimensions and expected output dimensions.
See [source page-size reports](docs/CLI.md#selected-source-page-size-runs).

PDF and report publication are separate operations. A stdout write failure returns
exit 1 and leaves already-published files saved; inspect publication facts before
retrying. See [results, reports and exit codes](docs/CLI.md#output).

Plans use format v8; results and reports use format v10. Print their schemas with:

```sh
pdfconcat schema plan
pdfconcat schema result
```

The same executable and effective plan, source and font bytes produce identical
PDF bytes across rebuilds. Output paths and the current clock do not affect
them. Dates are absent unless supplied by `timestamp` or the effective
`SOURCE_DATE_EPOCH`; see [document identity and time](docs/PLAN.md#document-identity-time-and-structure).
The [CLI reference](docs/CLI.md) covers stdin plans, argument handling, exit codes,
diagnostics and publication facts.

## PDFConcat or the pdfcpu CLI?

PDFConcat assembles packets with plans, layout checks and source-to-output
reports. Its engine is [pdfcpu](https://github.com/pdfcpu/pdfcpu) with a
[documented local patch](third_party/pdfcpu/PROVENANCE.md). The standalone
[pdfcpu CLI v0.16.4](https://github.com/pdfcpu/pdfcpu/releases/tag/v0.16.4)
offers merge modes and broader PDF editing commands.

**●** marks built-in support, which can involve several commands and is subject
to the limits below. **—** marks the absence of a dedicated built-in equivalent.

| Task | pdfcpu CLI v0.16.4 | PDFConcat |
| --- | :---: | :---: |
| Merge PDFs in a chosen order | ● | ● |
| Append PDFs by updating the existing destination [1] | ● | — |
| Automatically interleave two PDFs: A1, B1, A2, B2, … [2] | ● | — |
| Read PDF input from stdin for merging [1] | ● | — |
| Write merged PDF output to stdout [1] | ● | — |
| Select, repeat or reorder source pages [2] | ● | ● |
| Keep source pages at their original sizes | ● | ● |
| Insert blank pages | ● | ● |
| Preserve imported source bookmarks [3] | ● | ● |
| Create bookmarks [3] | ● | ● |
| Fit pages to a chosen paper size | ● | ● |
| Keep near-target source sheets unchanged when fitting | — | ● |
| Generate simple text pages from JSON | ● | ● |
| Generate Latin, Greek and Cyrillic text with a bundled font [4] | ● | ● |
| Add physical page numbers [5] | ● | ● |
| Describe sources, selections and assembly aids in one JSON plan | — | ● |
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

1. **Append and PDF streams.** pdfcpu `merge --mode append` updates a file
   destination. Create mode accepts one PDF from stdin and can write to stdout.
   Zip accepts two file inputs and can write to stdout. PDFConcat accepts JSON
   plans on stdin and uses files for PDF inputs and output. To extend a PDF with
   PDFConcat, list it first and choose a different output path.
2. **Page selection and interleaving.** pdfcpu's `collect` uses page expressions
   for selection, repetition and reordering. Its zip mode interleaves two inputs
   below PDF 2.0 and appends any remaining pages after one input runs out.
   PDFConcat uses ascending, non-overlapping ranges within each source item.
   Use separate items to repeat, reorder or explicitly alternate pages. The
   [selection limits](docs/PLAN.md#source-selections), including partial selection
   of forms, still apply.
3. **Bookmarks.** With default configuration, pdfcpu file merges wrap source
   bookmarks under filenames. Use `--bookmarks --bookmark-mode preserve` to
   keep source bookmark roots. Zip drops source bookmarks. When merging stdin,
   pdfcpu retains the first input's outline and omits later input outlines.
   The `bookmarks import` command accepts explicit page targets and styles.
   PDFConcat creates bookmarks from titled plan sections, targeting their first
   non-padding page, and retains source bookmarks beneath the nearest authored
   section. It prunes local navigation to omitted pages and applies its
   [source policy](docs/PDF-POLICY.md#source-policy).
4. **Bundled fonts.** Both tools include a font for Latin, Greek and Cyrillic.
   pdfcpu installs Roboto Regular into its filesystem configuration font store.
   Select `Roboto-Regular` for generated text and keep filesystem configuration
   enabled. PDFConcat uses bundled Noto Sans directly.
5. **Page numbers.** pdfcpu text stamps support current/total page numbers and an
   offset. PDFConcat supports physical or consecutive counting, start offsets,
   kind filters, inherited item suppression and six positions. Padding is never
   painted; it occupies a physical position but consumes no consecutive number.
   Contents labels and PDF PageLabels use the same resolved numbering.
6. **Plan checks and reports.** pdfcpu validates PDF structures and validates
   JSON during `create`; `info --json` reports document properties. PDFConcat
   provides a separate `check` command for planned layout and generated-text
   overflow, plus an assembly report with input hashes and page mappings.
   `check` can write a report when requested; a successful check does not
   guarantee a later build or publication.

pdfcpu v0.16.4 sources include
[document commands](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/cmd/pdfcpu/document.go),
[merge streams](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/pkg/cli/document_exec.go),
[merge inputs and zip restrictions](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/pkg/api/merge.go),
[bookmark merge modes](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/pkg/pdfcpu/merge.go),
[page operations](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/cmd/pdfcpu/pages.go),
[resize parameters](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/pkg/pdfcpu/model/resize.go),
[stamps and bookmarks](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/cmd/pdfcpu/content_usage.go),
[font initialization](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/pkg/pdfcpu/model/configuration.go),
[JSON creation](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/pkg/api/create.go),
[form filling](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/cmd/pdfcpu/forms.go),
[encryption and passwords](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/cmd/pdfcpu/security.go) and
[PDF info fields](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/pkg/pdfcpu/info.go).

## Limits

Supported source structure is preserved by default and meaningful generated text
is tagged. Untagged inputs remain usable, with honest coverage diagnostics;
blank and artifact-only pages are neutral. Explicit structure discard produces
untagged output. This does not certify PDF/UA. See
[structure policy](docs/PLAN.md#document-identity-time-and-structure).

- Use trusted local PDFs. Parsing and assembly can exhaust resources; plan limits are not a memory cap. See [security](SECURITY.md) and [plan limits](docs/PLAN.md#strictness-and-limits).
- Encrypted PDFs (including those opening without a password), populated signatures and certain unsafe structures are refused. Retained page actions can still run in a viewer. See the [source policy](docs/PDF-POLICY.md#source-policy).
- A narrow strict-reader exception admits unreferenced zero-offset classic xref entries with located warnings. It does not establish universal producer compatibility. See [input tolerance](docs/PDF-POLICY.md#narrow-classic-xref-input-tolerance).
- Page selection composes visible pages; it does not redact omitted content or guarantee removal of private objects. See [source selections](docs/PLAN.md#source-selections).
- Fitting checks destination targets, annotation/widget ownership and geometry, and placement accuracy. See [PDF compatibility](docs/PDF-POLICY.md#forms-fitting-and-catalog) before relying on interactive features.
- Page-number overlays may cover source content; there is no overlap detector. See [page numbers](docs/PLAN.md#page-numbering).
- PDF and report publication are separate operations, and a late failure can occur after a file becomes visible. Inspect [publication facts](docs/CLI.md#reports-and-committed-state) before retrying.

## Development and licensing

Contributor prerequisites, checks and release procedures are in
[CONTRIBUTING](CONTRIBUTING.md). The package map and execution pipeline are in
[ARCHITECTURE](docs/ARCHITECTURE.md).

Original code and documentation use [AGPL-3.0-only](LICENSE). See [licensing, source and contribution terms](PROJECT-NOTICE.md) and [commercial licensing status](LICENSE-COMMERCIAL.md). Third-party terms remain separate; `pdfconcat licenses` prints the project license and [third-party notices](internal/legal/THIRD_PARTY_NOTICES.md).
