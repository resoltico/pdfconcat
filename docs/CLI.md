# Command line and result v7

PDFConcat combines PDFs directly or follows a JSON assembly plan. Plans add page
selections, sections, navigation and simple text inserts; see the
[product scope](../README.md#scope-and-boundaries) and [plan reference](PLAN.md).

```text
pdfconcat build [options] (--plan FILE|- | ITEM...)
pdfconcat check [options] (--plan FILE|- | ITEM...)
pdfconcat schema plan|result
pdfconcat version [--json]
pdfconcat licenses
pdfconcat help [COMMAND]
```

`licenses` prints the license notices of PDFConcat and of the third-party code
and fonts inside the executable.

Each `ITEM` is a PDF path or `--blank`. Items retain their supplied order and may
contain only blanks when `--fit-to` supplies their size. A plan cannot be combined
with these direct items.

`COMMAND --help`, `-h`, root `--help` and `--version` are also accepted. Help is
plain text. Options interleave with operands; `--` ends option parsing. A path
such as `./--blank` is always a source. Operand pointers are `argv:N`, zero-based
including the command but excluding the executable.

| Option | Meaning |
| --- | --- |
| `--plan FILE`, `--plan -` | Named plan or stdin; excludes source/blank operands |
| `--blank` | Insert one generated page; size comes from a source or `--fit-to SIZE` |
| `-o FILE`, `--output FILE` | Output PDF; overrides plan output, relative to cwd |
| `--base-dir DIR` | Base for stdin plans only |
| `--fit-to SIZE` | Overrides plan fitting; paper name or dimensions |
| `--report FILE` | Full JSON report, relative to cwd |
| `--overwrite` | Permit replacement of existing regular output/report files |
| `--json` | Print one JSON result on stdout; text is the default |
| `--help`, `-h` | Help |

Values accept `--option=value` or separate arguments. Each ordinary option is
single-use; repeated `--blank` operands are allowed.

A separate required value cannot be a recognized option token (including
`--blank`); an omitted value is refused before input or report I/O. To name a
file like an option, use `--report=--json` or `--report ./--json`. A literal
`--` can be an option value; it ends options only when encountered between
options or operands.

## Build, check and fitting

`build` needs output from `-o` or the plan. `check` does not need an output path;
when one is provided it also checks its role/existence. Both load and validate
fonts, inspect every distinct source, resolve sizes/ranges and lay out text.
`check` writes no PDF but may write its requested report. Passing check predicts
assembly; merge, pruning, fitting, I/O or publication can still fail later.

A direct blank inherits the last selected page size of the nearest preceding
source, or the first selected page size of the first following source. With no
source, supply a fit target:

```sh
pdfconcat check --blank --fit-to A4
pdfconcat build --blank --fit-to A4 -o blank.pdf
```

`--fit-to` accepts the sheet grammar in [PLAN](PLAN.md#generated-pages-and-layering).
Fitting centers the effective CropBox–MediaBox intersection on the chosen sheet,
retaining its artwork clipping, pattern proportions and annotation geometry.
See [PDF policy](PDF-POLICY.md#fitting) for annotation appearance limits, fitting
refusals, forms, destination handling and the effective output version.

Sections, dividers, contents, odd starts and page numbers are declared in
[PLAN](PLAN.md). Its [examples index](PLAN.md#examples) lists ready-to-adapt plans
and their required inputs, including a plan that needs no source PDFs.

`--plan -` reads standard input with the plan byte limit. A read failure or
cancellation during acquisition returns `plan_unreadable` or `interrupted`
without writing a requested report.

PDF sources and output use regular files, not stdin/stdout. To extend an existing
PDF, pass it as the first source and choose a different output path. An output
cannot alias an input, even with `--overwrite`; replacement is not appending.

## Output

Text is the default: stdout holds the result summary, and stderr holds one line
per diagnostic. With `--json`, stdout holds exactly one capped JSON result and
stderr is empty unless writing stdout fails. `schema result` covers results,
reports and JSON version objects. JSON member order is not a consumer contract.

Usage errors print diagnostics and a help hint on stderr, with empty stdout.
For build, check and version, an exact `--json` before the option delimiter
selects JSON even when argument parsing fails. A `--` consumed as an option's
required value is not that delimiter. The first error and its location are
retained.

```json
{
  "format_version": 7,
  "command": "build",
  "status": "ok",
  "output": {
    "path": "/abs/packet.pdf",
    "published": true,
    "sha256": "0000000000000000000000000000000000000000000000000000000000000000",
    "bytes": 11039
  },
  "pages": {
    "source": 1,
    "generated": 1,
    "total": 2
  },
  "fit_to": null,
  "diagnostics": [],
  "diagnostics_omitted": 0,
  "report": null,
  "page_numbers": null,
  "metadata": {},
  "timestamp": null,
  "structure": {
    "policy": "preserve",
    "coverage": "partial"
  },
  "numbering": []
}
```

The digest above is illustrative. Real digests are lowercase SHA-256 hex.
`output` is null until a path is known. `published` records actual namespace
publication; digest/bytes appear only when true. Page counts are null until
established by the corresponding completed stages. An error never invents zero
pages. Warnings do not make an outcome invalid.

| Status | Exit | Meaning |
| --- | ---: | --- |
| `ok` | 0 | No errors; warnings possible |
| `invalid` | 2 | Usage, plan, policy, overflow or destination refusal |
| `failed` | 1 | Environment, backend, verification or publication failure |
| `interrupted` | 130 | Cancelled |

Interrupted takes precedence over failed, then invalid for status and exit.
Diagnostic presentation keeps output-publication and report-writing failures
first, then interruption, other failures, invalid-input errors and warnings.
Stage/first-use order stays stable within each priority group. This keeps bounded
publication failures and the cause of the overall outcome visible under caps.
The summary shows at most 20 diagnostics, with
`diagnostics_omitted` for the rest. Each diagnostic shows at most 20 consumer
pointers, with `items_omitted`. A full report has no such caps. Omission hints distinguish a saved report from
an unavailable requested report; they do not recommend reading a failed report.
A saved report holds uncapped processing details but cannot predict its own late
publication/durability error; the final CLI result remains authoritative.

Diagnostics include severity, stable code, free-form English message and, when known,
file, consumer pointers and plan location. Locations have zero-based byte offset,
one-based byte line/column and JSON pointer. Stdin uses a virtual file label.
Messages may change; branch on code/status rather than matching their prose.

Text diagnostics include the source or font file, plan line/column and JSON
pointer when known, plus consuming item pointers. Paths and pointers are quoted;
control characters in paths and messages are escaped while printable Unicode
stays readable. The same 20-diagnostic and 20-consumer limits apply, with omission
counts and report guidance based on actual report availability. JSON and full
reports preserve the original values.

Document metadata/timestamps, resolved numbering spans and structure coverage
are included in results and full reports. Unknown facts are null. Physical
`target_page` and output spans retain physical indexes regardless of numbering.
See the [plan contract](PLAN.md#document-identity-time-and-structure).

## Reports and committed state

Use `--report FILE` to request a full JSON report, including when `check` or
`build` fails. Reporting happens after application execution; argv rejection or
failure to establish a valid working directory produces no report. Report
targets are checked against known plan/font/source inputs and the output;
existing targets must be regular files. Symlink targets, directories and devices
are refused. Input symlinks to regular files are allowed. Roles are checked again
before report publication, including an output that did not exist initially.

When the plan could not be decoded, its inputs are unknown. Report publication
then always uses no-replace, even with `--overwrite`, so it cannot replace an
input hidden in a malformed plan. Existing-target and known-collision refusals
use `report_exists`, `report_not_regular`, `report_is_input` or
`report_is_output`.

Existing inputs and the output are protected by file identity. Missing inputs are protected only by cleaned path spelling; reports are rechecked after PDF publication.

Output/report files use ordinary umask permissions; replacements keep the old
mode. Publication writes and syncs a sibling temporary file first. Its bounded
name is independent of the destination basename, so staging does not reduce the
destination's filesystem filename limit. Without `--overwrite`, it uses a
no-replace rename, with a link fallback when necessary.
With `--overwrite`, it uses a replacing rename, except that a report whose plan
inputs are unknown still uses no-replace. On Unix it then syncs the containing
directory. A trailing sync or temp-cleanup failure
can coexist with `output.published: true` or `report.written: true`. A report
failure never rolls back the PDF. The saved report cannot predict its own late
failure; the final CLI result is authoritative for that outcome. There are no recovery
copies or cross-file transaction guarantees.

A full report adds preorder `sections` with `item`, `title`, nullable `parent`,
`target_page` and `output_pages`. It records `page_numbers` once at packet level.
It includes one compact part per source occurrence, page item run, divider,
contents item or padding span:

```json
{
  "format_version": 7,
  "command": "check",
  "status": "ok",
  "output": null,
  "pages": {
    "source": 0,
    "generated": 2,
    "total": 2
  },
  "fit_to": null,
  "diagnostics": [],
  "diagnostics_omitted": 0,
  "report": {
    "path": "/abs/report.json",
    "written": true
  },
  "parts": [
    {
      "item": "/items/0",
      "kind": "page",
      "pages": 2,
      "count": 2,
      "output_pages": [
        1,
        2
      ],
      "size": {
        "width": 100,
        "height": 200
      },
      "text": null,
      "input_pages": null,
      "spans": null
    }
  ],
  "page_numbers": null,
  "sections": [],
  "metadata": {},
  "timestamp": null,
  "structure": {
    "policy": "preserve",
    "coverage": "partial"
  },
  "numbering": []
}
```

Source parts include full-input `input_pages`, hash/byte count, emitted `pages`
and compact `spans` such as `{"source":[2,4],"output":[10,12]}`. Unknown source mappings and non-source parts use `spans: null`; known source mappings are nonempty arrays. Divider parts
reference their section; padding records `reason: odd_start` and `before_item`.
Complete leaf ranges partition physical pages 1..N once. Nested section ranges
may overlap. Until the physical page sequence has been resolved, emitted
counts/spans/ranges and section targets stay null, and no padding is predicted.
A later text-layout or build failure can retain a complete predicted sequence.
A nonnull total means the physical sequence is known; it does not establish
successful PDF publication.

Source parts include path, known byte hash/length and actual
`fields_renamed` records. Readable refused sources retain byte facts; unestablished
page counts/ranges remain null. Page items and dividers include `count` and, when
known, authored `size` and measured `text`. Contents and padding parts include
their generated size but no text-layout object. Under fitting, the final sheet
is top-level `fit_to`; generated size/text describe the authored layout.

Text `bounds` and `ink_bounds` are `[left, bottom, right, top]` in points
(1/72 inch), measured from the lower-left corner of the authored page before
fitting. `bounds` encloses the logical text block; `ink_bounds` encloses its
painted glyphs. For example, `bounds: [10, 20, 70, 50]` describes a block 60 pt
wide and 30 pt high, starting 10 pt from the left edge and 20 pt from the bottom.
It does not describe a width of 70 pt or a height of 50 pt, and fitting to a
different sheet does not convert these reported coordinates.

`text` is null when no generated-text layout is available, including empty text,
an unresolved layout, and source, contents or padding parts. A measured
whitespace-only block can have logical bounds and `ink_bounds: null` because it
paints no glyphs. `font_size` is in points; `font` is `default` for bundled Noto
Sans or the resolved path of the selected font file.

Ranges are inclusive and 1-based. Check ranges and rename warnings are predictions.
Only build records actual field-name mappings. The backend may namespace a later
nonempty form even without a collision; check does not copy its private prefix
algorithm. Check's empty mapping arrays mean no actual merge was performed.

```sh
# Part covering output page 12 (for a successful/published packet).
jq '.parts[] | select(.output_pages != null) | select(.output_pages[0] <= 12 and .output_pages[1] >= 12)' packet.report.json
# Every warning and its source file.
jq '.diagnostics[] | select(.severity == "warning") | {code,file,items}' packet.report.json
# Source identities that were established by inspection.
jq '.parts[] | select(.kind == "source") | {path,sha256,bytes}' packet.report.json
```

## Generate a plan from a directory

Declare the sort order and preserve filenames as array elements. Do not split
filenames on newlines. Plans require valid UTF-8 paths.

### bash and zsh

Globs plus `jq --args` preserve quotes/newlines as arguments. `LC_ALL=C` fixes byte
order; bash `nullglob` avoids treating an unmatched pattern as a filename. In
zsh omit `shopt` (or use its `NULL_GLOB` option). For lists beyond OS argument limits, generate the plan in batches or with a program that preserves filenames.

```sh
( export LC_ALL=C; shopt -s nullglob
  jq -n '{version: 5, items: ($ARGS.positional | map({source: .}))}' --args chapters/*.pdf > plan.json )
pdfconcat build --plan plan.json -o book.pdf
```

### PowerShell

Ordinal sorting makes the order explicit. Force a string array even for one
file, and write an explicit UTF-8 file rather than relying on console pipeline
encoding. A leading BOM is accepted.

```powershell
$items = [string[]]@(Get-ChildItem chapters -File | Where-Object { $_.Extension -ieq '.pdf' } | ForEach-Object { 'chapters/' + $_.Name })
if ($items.Count -eq 0) { throw 'no PDF files in chapters/' }
[Array]::Sort($items, [StringComparer]::Ordinal)
@{ version = 5; items = @($items | ForEach-Object { @{ source = $_ } }) } | ConvertTo-Json -Depth 5 | Set-Content plan.json -Encoding utf8
pdfconcat build --plan plan.json -o book.pdf
```


## Diagnostic evidence

Core diagnostics own the condition, explanation and terminology. The CLI adds
command syntax to text remedies; JSON and reports retain the core explanation.
`detail`, when present, contains backend evidence rather than a stable product
message. `pdf_location` can identify a positive `object` number, positive `page`
number, or dictionary `path`; absent fields mean attribution is unavailable.
It is separate from `location`, which identifies plan text. Consumers should
branch on `code`, not backend text or a reported path. `source_validation_notice`
retains backend compatibility evidence, including phase and disposition in
`detail`. A skipped validation notice does not mean that content was removed;
explicitly reported repairs cause a separate `source_unsupported` refusal.

Warnings merge their consuming `items` only when their code, file, message,
detail and both locations are equivalent. Different viewer entries therefore
remain different findings, including when a source is repeated. Full reports
retain all findings; stdout summaries still apply the documented caps.

`source_invalid` means parsing or validation failed. `source_unsupported` means
a supported-fidelity boundary was reached. `source_processing_failed` is a backend
failure, not proof of invalid input. Both invalid and unsupported input select
status `invalid`; a processing failure selects `failed`.

## Diagnostic codes

The registry in `internal/core/internal/diag/codes.go` is authoritative. Warning class is empty
(shown as —); warnings never select an error outcome.

<!-- codes -->

| Code | Severity | Class | Meaning |
| --- | --- | --- | --- |
| `request_invalid` | error | invalid | invalid application request envelope |
| `source_selection_out_of_range` | error | invalid | selected page range exceeds the original source page count |
| `source_selection_form_unsupported` | error | invalid | partial page selection of a source with form fields or widgets is unsupported |
| `source_navigation_removed` | warning | — | navigation to omitted source pages was removed for affected occurrences |
| `source_outline_depth_unsupported` | error | invalid | authored sections and imported bookmarks exceed the supported outline hierarchy |
| `source_form_unsupported` | error | invalid | nonempty forms require identical resolved DR/DA/Q representations |
| `source_uri_base_removed` | warning | — | source URI Base is removed; retained relative actions may resolve differently |
| `usage_unknown_command` | error | invalid | first argument is not a command |
| `usage_unknown_option` | error | invalid | option not defined for the command |
| `usage_missing_value` | error | invalid | option needs a value |
| `usage_invalid_value` | error | invalid | option value not allowed (`--fit-to`, `schema` name, …) |
| `usage_conflict` | error | invalid | `--plan` with PDF or `--blank` operands; `--base-dir` without `--plan -`; repeated single-use option |
| `usage_missing_input` | error | invalid | neither `--plan` nor PDF or `--blank` operands |
| `output_required` | error | invalid | build requires an output destination |
| `plan_unreadable` | error | failed | plan file or stdin could not be read |
| `plan_too_large` | error | invalid | plan exceeds 64 MiB |
| `plan_syntax` | error | invalid | not one valid JSON object in UTF-8 (incl. trailing data, invalid UTF-8, comments) |
| `plan_version_unsupported` | error | invalid | required plan version is missing or unsupported |
| `plan_unknown_member` | error | invalid | member not defined at that place (case-sensitive) |
| `plan_duplicate_member` | error | invalid | member repeated in one object |
| `plan_null` | error | invalid | `null` anywhere |
| `plan_type` | error | invalid | wrong JSON type |
| `plan_value` | error | invalid | value outside its grammar or range (length, colour, size, anchor, count, …) |
| `plan_item_shape` | error | invalid | an item object without exactly one supported source, page, group or contents shape |
| `plan_limit` | error | invalid | nesting, entries, count or total generated pages over the limit |
| `path_invalid` | error | invalid | empty path, NUL, Windows drive-relative (`C:x`) or rooted-without-drive (`\x`) |
| `output_exists` | error | invalid | output exists and replacement was not authorized |
| `report_exists` | error | invalid | report exists and replacement was not authorized, or plan inputs are unknown |
| `output_not_regular` | error | invalid | output exists and is not a regular file (directory, symlink, device) |
| `report_not_regular` | error | invalid | same for the report |
| `output_is_input` | error | invalid | output is (or aliases) a plan, font or source file |
| `report_is_input` | error | invalid | report is (or aliases) an input |
| `report_is_output` | error | invalid | report and output are the same file |
| `font_unreadable` | error | failed | font file could not be read |
| `font_invalid` | error | invalid | unsupported/damaged static TrueType or CFF1 font, or unsafe PostScript name |
| `font_not_embeddable` | error | invalid | `OS/2.fsType` forbids embedding |
| `text_unsupported_character` | error | invalid | character outside the accepted scripts, right-to-left, control or format character |
| `text_glyph_missing` | error | invalid | the font cannot render a character sequence |
| `text_overflow` | error | invalid | unusable derived text width, or measured block/ink/word overflow with overflow: error |
| `page_size_required` | error | invalid | `inherit` with no source page to inherit from |
| `page_size_too_large` | error | invalid | inherited size above 14,400 pt; give an explicit size |
| `source_unreadable` | error | failed | source file could not be read |
| `source_changed` | error | failed | source bytes or readability at assembly differ from inspection |
| `source_not_pdf` | error | invalid | source has no recognizable PDF header |
| `source_input_invalid` | error | invalid | source input is absent, denied or nonregular |
| `plan_input_invalid` | error | invalid | plan input is absent, denied or nonregular |
| `font_input_invalid` | error | invalid | font input is absent, denied or nonregular |
| `source_structure_partial` | warning | — | retained source content has incomplete structural coverage |
| `source_invalid` | error | invalid | PDF parsing or validation failed |
| `source_unsupported` | error | invalid | source structure cannot be composed with the supported fidelity guarantees |
| `source_processing_failed` | error | failed | unexpected PDF backend failure, including a recovered panic |
| `source_validation_notice` | warning | — | backend compatibility notice; skipped validation does not establish content removal |
| `source_viewer_settings_removed` | warning | — | source catalog PageLayout, PageMode or ViewerPreferences removed before validation |
| `source_damaged` | error | invalid | broken page tree, missing/malformed content streams, or validation changing page order or encoded contents |
| `source_encrypted` | error | invalid | encrypted, including empty-password encryption |
| `source_signed` | error | invalid | signature value, catalog permission structures (`/Perms`) or document timestamp |
| `source_dynamic_form` | error | invalid | source requests NeedsRendering true |
| `source_optional_content` | error | invalid | `/OCProperties` present |
| `fit_destination_unsupported` | error | invalid | fitting internal page destinations with numeric position coordinates is unsupported |
| `source_output_incompatible` | error | invalid | retained annotations lack appearances required by the effective PDF 2.0 output |
| `fit_annotation_unsupported` | error | invalid | fitting requires appearances for FreeText, Text icons, Caret, captioned Lines, rotated NoRotate or boundary-crossing artwork; interactive hit areas must stay inside the source frame |
| `fit_form_unsupported` | error | invalid | `fit_to` with a source that has form widgets |
| `assembly_failed` | error | failed | PDF/text backend failed during assembly or shaping (including recovered panics) |
| `output_write_failed` | error | failed | writing the staged output failed (disk full, permissions) |
| `output_invalid` | error | failed | the written output did not validate |
| `output_page_count_mismatch` | error | failed | written page count ≠ expected |
| `output_publish_failed` | error | failed | the final rename failed (incl. no-replace unsupported and link fallback failed) |
| `report_write_failed` | error | failed | writing or publishing the report failed |
| `interrupted` | error | interrupted | cancelled by a signal |
| `text_overflow_allowed` | warning | — | `overflow: allow` and measured text extends past the page or block |
| `form_fields_renamed` | warning | — | pdfcpu namespaces later nonempty forms even without collisions; build reports actual names |
| `form_appearance_regeneration_requested` | warning | — | a source requests `NeedAppearances`; viewers may redraw all fields |
| `destinations_renamed` | warning | — | colliding named destinations were renamed for later occurrences |
| `destinations_dropped` | warning | — | named destinations whose target is not a page of the same document were omitted |
| `source_xfa_removed` | warning | — | XFA removed; the static AcroForm remains |
| `source_structure_removed` | warning | — | tagged-PDF structure removed by explicit discard policy |
| `source_page_labels_removed` | warning | — | page labels removed |
| `source_output_intents_removed` | warning | — | output intents removed; output makes no PDF/A or PDF/X claim |
| `source_document_actions_removed` | warning | — | `/OpenAction`, catalog `/AA` or document JavaScript removed |
| `source_attachments_removed` | warning | — | embedded-files name tree or catalog `/AF` removed (page attachments remain) |
| `working_directory_unavailable` | error | failed | The process working directory could not be determined |
| `output_inspection_failed` | error | failed | The output destination could not be inspected |
| `report_inspection_failed` | error | failed | The report destination could not be inspected |
