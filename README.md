# PDFConcat

PDFConcat combines PDFs and selected pages into one ordered document. Merge
whole files directly or use a JSON plan for selections, dividers, linked
contents and page numbers. Check the packet before building and save a report
with input SHA-256 hashes and source-to-output page mappings.

The standalone CLI uses bundled Noto Sans and runs without external PDF tools,
font installation or a configuration directory. It keeps no application state
between runs. See [temporary files and saved outputs](docs/CLI.md#reports-and-committed-state).

- [Understand the scope](#scope-and-boundaries)
- [Install and verify](#install)
- [Merge PDFs and insert blanks](#merge-pdfs)
- [Fit pages to A4](#fit-page-sizes)
- [Use a reusable plan and select pages](#keep-the-composition-in-a-plan)
- [Automate commands and save reports](#automation-and-reports)
- [Compare with the pdfcpu CLI](#pdfconcat-or-the-pdfcpu-cli)
- [Check the limits](#limits)

## Scope and boundaries

Plans can insert blanks for odd-page starts, organize sections with title
dividers and bookmarks, and add linked contents, page numbers and document
metadata. Generated pages can form the whole output. Each generated text page
holds one styled block with wrapping and overflow controls. Bundled Noto Sans
and supported static TrueType or OpenType CFF1 fonts render Latin, Greek and
Cyrillic. Linked contents can span several pages. Prepare richer layouts in
another authoring tool and include them as source PDFs.

Source pages keep their geometry unless you request fitting. Fitting transforms
pages outside the selected tolerance and remaps supported local destinations.
The source policy governs which links, bookmarks, tags and other PDF structures
can be carried into the output. PDFConcat removes proved-empty optional-content
settings by default. Plans can opt in to removing proved always-visible Form
controls or bare undeclared whole-page wrappers while retaining the artwork.
[PDF policy](docs/PDF-POLICY.md)

To extend an existing PDF, include it as the first source and choose a separate
output. Use separate source items to specify an interleaved order. PDF sources
and output are files. The CLI accepts a JSON plan on stdin.

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
  "version": 10,
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
  "version": 10,
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

Relative plan paths start beside the saved plan file. A top-level or nested
`dir` changes the scope for source paths inside it. CLI `-o` and `--report` paths
resolve from the working directory. The [plan reference](docs/PLAN.md#members-and-paths)
defines the path rules, including where external fonts resolve.

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

Print the current plan and result schemas with these commands.

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

PDFConcat uses [pdfcpu](https://github.com/pdfcpu/pdfcpu) with a
[documented local patch](third_party/pdfcpu/PROVENANCE.md). Compare its packet
workflows with the standalone [pdfcpu CLI v0.16.4](https://github.com/pdfcpu/pdfcpu/releases/tag/v0.16.4).
A command name identifies the standalone workflow. Some tasks use several
commands. A dash indicates no dedicated built-in workflow.

| Task | pdfcpu CLI v0.16.4 | PDFConcat |
| --- | --- | --- |
| Merge PDFs in a chosen order | [`merge`](https://pdfcpu.io/core/merge/) | Direct operands or plan |
| Append by updating an existing PDF | `merge --mode append` | — |
| Automatically alternate pages from two PDFs | `merge --mode zip` [1] | — |
| Read a PDF from stdin while merging | One input in `merge` create mode [1] | — |
| Write the merged PDF to stdout | `merge` create or zip mode [1] | — |
| Select, repeat or reorder source pages | [`collect`](https://pdfcpu.io/core/collect/), then `merge` for multiple inputs | Source ranges and separate occurrences in a plan [1] |
| Keep source pages at their original sizes | `merge` | Default assembly |
| Insert blank pages | [`pages insert`](https://pdfcpu.io/getting_started/usage/#page) or `merge --divider` | `--blank` or a plan page |
| Preserve imported source bookmarks | `merge` with bookmarks enabled [2] | Retains supported bookmarks in each occurrence [2] |
| Create bookmarks | `merge` or [`bookmarks import`](https://pdfcpu.io/bookmarks/bookmarks_import/) [2] | Titled plan sections [2] |
| Preserve supported source structure tags when merging | — [2] | Default structure policy [2] |
| Fit pages to a chosen paper size | [`resize`](https://pdfcpu.io/core/resize/) [3] | `--fit-to` or plan fitting [3] |
| Keep sheets automatically within a fitting tolerance | — | Per-side absolute tolerance [3] |
| Generate simple text pages from JSON | [`create`](https://pdfcpu.io/create/create/) | Plan text pages |
| Generate Latin, Greek and Cyrillic text with a bundled font | `create` with Roboto Regular from the configuration font store [4] | Bundled Noto Sans |
| Paint page numbers | [Text stamps](https://pdfcpu.io/core/stamp/) | Plan numbering with physical or consecutive counts |
| Describe sources, selections and assembly aids in one JSON plan | — | Plan |
| Define nested sections and title dividers in a plan | — | Plan groups and dividers |
| Automatically pad sections to start on odd pages | — | Plan odd starts |
| Generate linked contents from sections and resolved page numbers | — | Plan contents item |
| Validate source PDFs without writing PDF output | [`validate`](https://pdfcpu.io/getting_started/usage/#document) | `check` within the assembly policy [5] |
| Check a packet plan's text layout without writing PDF output | — | `check` |
| Report input SHA-256 hashes and source-to-output page ranges | — | `--report` [5] |
| Split a PDF into multiple output files in one command | [`split`](https://pdfcpu.io/core/split/) | — |
| Apply general text, image or PDF stamps and watermarks | [`stamp`](https://pdfcpu.io/core/stamp/) and `watermark` | — |
| Remove proved always-visible Form controls or bare undeclared whole-page wrappers | — | Plan opt-in [6] |
| Encrypt, decrypt and change PDF passwords | [Security commands](https://pdfcpu.io/getting_started/piping/) | — |
| Fill interactive forms | [`form fill`](https://pdfcpu.io/form/form_fill/) | — |

1. **Merge inputs and order.** pdfcpu zip alternates two file inputs below
   PDF 2.0 and emits the remainder of the longer input. PDFConcat uses ascending,
   non-overlapping ranges within each source item. Separate items can repeat,
   reorder or alternate selections. [pdfcpu merge and piping](https://pdfcpu.io/getting_started/piping/) ·
   [PDFConcat selection rules](docs/PLAN.md#source-selections)
2. **Bookmarks and structure.** pdfcpu file merges wrap source bookmarks under
   filenames by default. Use `--bookmarks --bookmark-mode preserve` to retain
   the input roots. Zip drops source outlines, and create mode with a stdin PDF
   carries only the first input's outline. pdfcpu merge omits source structure
   trees. PDFConcat places retained source outlines under the nearest authored
   section and preserves supported source tags under its default policy.
   [pdfcpu merge](https://pdfcpu.io/core/merge/) ·
   [pdfcpu stdin merge implementation](https://github.com/pdfcpu/pdfcpu/blob/v0.16.4/pkg/api/merge.go) ·
   [PDFConcat sections](docs/PLAN.md#sections-title-dividers-and-odd-starts) ·
   [PDFConcat structure policy](docs/PLAN.md#structure)
3. **Fitting.** pdfcpu resize can enforce a sheet orientation and can rotate the
   content for best fit. PDFConcat uses the requested target orientation,
   preserves the artwork's displayed orientation and remaps supported local
   destinations on transformed pages. [pdfcpu resize](https://pdfcpu.io/core/resize/) ·
   [PDFConcat fitting policy](docs/PDF-POLICY.md#fitting)
4. **Bundled fonts.** pdfcpu installs Roboto Regular into its filesystem
   configuration font store. Select `Roboto-Regular` for generated text and
   keep filesystem configuration enabled. PDFConcat embeds Noto Sans directly.
   [pdfcpu configuration](https://pdfcpu.io/getting_started/config_dir/#user-fonts)
5. **Checks and reports.** pdfcpu `info --json` reports document properties.
   PDFConcat `check` inspects a planned assembly, including generated-text
   layout, and can save the full assembly report. Results distinguish the check
   from successful publication. [pdfcpu piping and outputs](https://pdfcpu.io/getting_started/piping/) ·
   [PDFConcat results and reports](docs/CLI.md#reports-and-committed-state)
6. **Visibility layers.** Both PDFConcat plan policies remove proved-empty
   optional-content settings and report a warning. The `flatten-visible-content`
   opt-in also admits the two proved profiles listed in the table, including
   page wrappers with the supported graphics-state prelude.
   [PDFConcat visibility policy](docs/PDF-POLICY.md#optional-content)

## Limits

PDFConcat preserves supported source structure by default and tags meaningful
generated text. The result reports structural coverage. Blank and artifact-only
pages are neutral for coverage. Explicit structure discard produces untagged
output. See the [structure policy](docs/PLAN.md#structure).

- Use trusted local PDFs. Parsing and assembly can exhaust resources; plan limits are not a memory cap. See [security](SECURITY.md) and [plan limits](docs/PLAN.md#strictness-and-limits).
- Encrypted PDFs (including those opening without a password), populated signatures and certain unsafe structures are refused. Retained page actions can still run in a viewer. See the [source policy](docs/PDF-POLICY.md#source-policy).
- Zero-offset classic xref entries have a narrow exception for unreferenced entries and proved references from discarded structure indexes. See [input tolerance](docs/PDF-POLICY.md#narrow-classic-xref-input-tolerance).
- Page selection composes visible pages; it does not redact omitted content or guarantee removal of private objects. See [source selections](docs/PLAN.md#source-selections).
- Fitting checks destination targets, annotation/widget ownership and geometry, and placement accuracy. See [PDF compatibility](docs/PDF-POLICY.md#forms-fitting-and-catalog) before relying on interactive features.
- Page-number overlays may cover source content; there is no overlap detector. See [page numbers](docs/PLAN.md#page-numbering).
- PDF and report publication are separate operations, and a late failure can occur after a file becomes visible. Inspect [publication facts](docs/CLI.md#reports-and-committed-state) before retrying.

## Development and licensing

Contributor prerequisites, checks and release procedures are in
[CONTRIBUTING](CONTRIBUTING.md). The package map and execution pipeline are in
[ARCHITECTURE](docs/ARCHITECTURE.md).

Original code and documentation use [AGPL-3.0-only](LICENSE). See [licensing, source and contribution terms](PROJECT-NOTICE.md) and [commercial licensing status](LICENSE-COMMERCIAL.md). Third-party terms remain separate; `pdfconcat licenses` prints the project license and [third-party notices](internal/legal/THIRD_PARTY_NOTICES.md).
