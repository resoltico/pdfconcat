# Command line and result v5

```text
pdfconcat build [options] (--plan FILE|- | PDF... [--blank]...)
pdfconcat check [options] (--plan FILE|- | PDF... [--blank]...)
pdfconcat schema plan|result
pdfconcat version [--json]
pdfconcat help [COMMAND]
```

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
single-use; repeated `--blank` operands are allowed. There is no progress output,
worker-count option, report-query command or inline-plan JSON option.

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
See [PDF policy](PDF-POLICY.md#forms-fitting-and-catalog) for fitting refusals,
forms, destination handling and the effective output version.

Sections, dividers, contents, odd starts and page numbers are declared in
[PLAN](PLAN.md). The [composition example](../examples/plans/packet-composition.json)
combines them in one packet.

`--plan -` reads standard input with the plan byte limit. A read failure or
cancellation during acquisition returns `plan_unreadable` or `interrupted`
without writing a requested report.

## Output

Text is the default: stdout holds the result summary, and stderr holds one line
per diagnostic. With `--json`, stdout holds exactly one capped JSON result and
stderr is empty unless writing stdout fails. `schema result` covers results,
reports and JSON version objects. JSON member order is not a consumer contract.

Usage errors print diagnostics and a help hint on stderr, with empty stdout.
For build, check and version, an exact `--json` before the first `--` delimiter
selects JSON even when argument parsing fails. The first error and its location
are retained.

```json
{
  "format_version": 5,
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
  "page_numbers": null
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

Interrupted takes precedence over failed, then invalid. Errors precede warnings
in their output stream, in stage order. The summary shows at most 20 diagnostics, with
`diagnostics_omitted` for the rest. Each diagnostic shows at most 20 consumer
pointers, with `items_omitted`. A full report has no such caps.

Diagnostics include severity, stable code, free-English message and, when known,
file, consumer pointers and plan location. Locations have zero-based byte offset,
one-based byte line/column and JSON pointer. Stdin uses a virtual file label.
Messages may change; branch on code/status rather than matching their prose.

Text diagnostics include the source or font file, plan line/column and JSON
pointer when known, plus consuming item pointers. Paths and pointers are quoted;
control characters in paths and messages are escaped while printable Unicode
stays readable. The same 20-diagnostic and 20-consumer limits apply, with omission
counts and a `--report` hint. JSON and full reports preserve the original values.

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
mode. Publication uses a sibling temp, sync, no-replace rename (link fallback
when needed), and Unix directory sync. A trailing sync or temp-cleanup failure
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
  "format_version": 5,
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
  "sections": []
}
```

Source parts include full-input `input_pages`, hash/byte count, emitted `pages`
and compact `spans` such as `{"source":[2,4],"output":[10,12]}`. Unknown source mappings and non-source parts use `spans: null`; known source mappings are nonempty arrays. Divider parts
reference their section; padding records `reason: odd_start` and `before_item`.
Complete leaf ranges partition physical pages 1..N once. Nested section ranges
may overlap. Before resolution, emitted counts/spans/ranges and section targets
stay null; no padding is predicted in a failed layout. A nonnull total means the
physical sequence is complete.

Source parts include path, known byte hash/length and actual
`fields_renamed` records. Readable refused sources retain byte facts; unestablished
page counts/ranges remain null. Generated parts include authored size, count,
text block/ink bounds and overflow when known. Under fitting, the sheet is top-
level `fit_to`; generated size/text describe the authored layout.

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
  jq -n '{version: 4, items: ($ARGS.positional | map({source: .}))}' --args chapters/*.pdf > plan.json )
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
@{ version = 4; items = @($items | ForEach-Object { @{ source = $_ } }) } | ConvertTo-Json -Depth 5 | Set-Content plan.json -Encoding utf8
pdfconcat build --plan plan.json -o book.pdf
```


## Diagnostic codes

The registry in `internal/core/internal/diag/codes.go` is authoritative. Warning class is empty
(shown as —); warnings never select an error outcome.

<!-- codes -->

| Code | Severity | Class | Meaning |
| --- | --- | --- | --- |
| `source_selection_out_of_range` | error | invalid | selected page range exceeds the original source page count |
| `source_selection_form_unsupported` | error | invalid | partial page selection of a source with form fields or widgets is unsupported |
| `source_navigation_removed` | warning | — | navigation to omitted source pages was removed for affected occurrences |
| `source_outline_depth_unsupported` | error | invalid | authored sections and imported bookmarks exceed the supported outline hierarchy |
| `assembly_failed` | error | failed | PDF/text backend failed during assembly or shaping (including recovered panics) |
| `fit_destination_unsupported` | error | invalid | fitting internal page destinations with numeric position coordinates is unsupported |
| `source_output_incompatible` | error | invalid | retained annotations lack appearances required by the effective PDF 2.0 output |
| `fit_form_unsupported` | error | invalid | `fit_to` with a source that has form widgets |
| `font_invalid` | error | invalid | not a supported static TrueType (`glyf`) font, damaged, or its PostScript name cannot be preserved |
| `font_not_embeddable` | error | invalid | `OS/2.fsType` forbids embedding |
| `font_unreadable` | error | failed | font file could not be read |
| `interrupted` | error | interrupted | cancelled by a signal |
| `output_exists` | error | invalid | output exists and `--overwrite` not given |
| `output_invalid` | error | failed | the written output did not validate |
| `output_is_input` | error | invalid | output is (or aliases) a plan, font or source file |
| `output_not_regular` | error | invalid | output exists and is not a regular file (directory, symlink, device) |
| `output_page_count_mismatch` | error | failed | written page count ≠ expected |
| `output_publish_failed` | error | failed | the final rename failed (incl. no-replace unsupported and link fallback failed) |
| `output_write_failed` | error | failed | writing the staged output failed (disk full, permissions) |
| `page_size_required` | error | invalid | `inherit` with no source page to inherit from |
| `page_size_too_large` | error | invalid | inherited size above 14,400 pt; give an explicit size |
| `path_invalid` | error | invalid | empty path, NUL, Windows drive-relative (`C:x`) or rooted-without-drive (`\x`) |
| `plan_duplicate_member` | error | invalid | member repeated in one object |
| `plan_item_shape` | error | invalid | an item object without exactly one supported source, page, group or contents shape |
| `plan_limit` | error | invalid | nesting, entries, count or total generated pages over the limit |
| `plan_null` | error | invalid | `null` anywhere |
| `plan_syntax` | error | invalid | not one valid JSON object in UTF-8 (incl. trailing data, invalid UTF-8, comments) |
| `plan_too_large` | error | invalid | plan exceeds 64 MiB |
| `plan_type` | error | invalid | wrong JSON type |
| `plan_unknown_member` | error | invalid | member not defined at that place (case-sensitive) |
| `plan_unreadable` | error | failed | plan file or stdin could not be read |
| `plan_value` | error | invalid | value outside its grammar or range (length, colour, size, anchor, count, …) |
| `plan_version_unsupported` | error | invalid | required plan version is missing or unsupported |
| `report_exists` | error | invalid | report exists and `--overwrite` was not given, or plan inputs are unknown |
| `report_is_input` | error | invalid | report is (or aliases) an input |
| `report_is_output` | error | invalid | report and output are the same file |
| `report_not_regular` | error | invalid | same for the report |
| `report_write_failed` | error | failed | writing or publishing the report failed |
| `request_invalid` | error | invalid | invalid application request envelope |
| `report_inspection_failed` | error | failed | The report destination could not be inspected |
| `output_inspection_failed` | error | failed | The output destination could not be inspected |
| `working_directory_unavailable` | error | failed | The process working directory could not be determined |
| `source_changed` | error | failed | source bytes at merge time differ from the inspected bytes |
| `source_damaged` | error | invalid | broken page tree (missing/empty kid, wrong count) or content-replacing repair |
| `source_dynamic_form` | error | invalid | source requests NeedsRendering true |
| `source_encrypted` | error | invalid | encrypted, including empty-password encryption |
| `source_form_unsupported` | error | invalid | nonempty forms require identical resolved DR/DA/Q representations |
| `source_invalid` | error | invalid | unsupported unsafe PDF shape or parse/validation failure (including recovered panics) |
| `source_optional_content` | error | invalid | `/OCProperties` present |
| `source_signed` | error | invalid | signature value, certification (`/Perms`) or document timestamp |
| `source_unreadable` | error | failed | source file could not be read |
| `text_glyph_missing` | error | invalid | the font cannot render a character sequence |
| `text_overflow` | error | invalid | unusable derived text width, or measured block/ink/word overflow with overflow: error |
| `text_unsupported_character` | error | invalid | character outside the accepted scripts, right-to-left, control or format character |
| `usage_conflict` | error | invalid | `--plan` with PDF operands; `--base-dir` without `--plan -`; repeated single-use option |
| `usage_invalid_value` | error | invalid | option value not allowed (`--fit-to`, `schema` name, …) |
| `usage_missing_input` | error | invalid | neither `--plan` nor PDF operands |
| `usage_missing_output` | error | invalid | `build` without `-o` and without plan `output` |
| `usage_missing_value` | error | invalid | option needs a value |
| `usage_unknown_command` | error | invalid | first argument is not a command |
| `usage_unknown_option` | error | invalid | option not defined for the command |
| `destinations_dropped` | warning | — | named destinations whose target is not a page of the same document were omitted |
| `destinations_renamed` | warning | — | colliding named destinations were renamed for later occurrences |
| `form_appearance_regeneration_requested` | warning | — | a source requests `NeedAppearances`; viewers may redraw all fields |
| `form_fields_renamed` | warning | — | pdfcpu namespaces later nonempty forms even without collisions; build reports actual names |
| `source_attachments_removed` | warning | — | embedded-files name tree or catalog `/AF` removed (page attachments remain) |
| `source_document_actions_removed` | warning | — | `/OpenAction`, catalog `/AA` or document JavaScript removed |
| `source_output_intents_removed` | warning | — | output intents removed; output makes no PDF/A or PDF/X claim |
| `source_page_labels_removed` | warning | — | page labels removed |
| `source_structure_removed` | warning | — | tagged-PDF structure removed; output is not tagged |
| `source_uri_base_removed` | warning | — | source URI Base is removed; retained relative actions may resolve differently |
| `source_xfa_removed` | warning | — | XFA removed; the static AcroForm remains |
| `text_overflow_allowed` | warning | — | `overflow: allow` and measured text extends past the page or block |
