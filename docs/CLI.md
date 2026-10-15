# Command line and result v3

```text
pdfconcat build [options] (--plan FILE|- | PDF... [--blank]...)
pdfconcat check [options] (--plan FILE|- | PDF... [--blank]...)
pdfconcat schema plan|result
pdfconcat version [--format json|text]
pdfconcat help [COMMAND]
```

`COMMAND --help`, `-h`, root `--help` and `--version` are also accepted. Help is
plain text. Options interleave with operands; `--` ends option parsing. A path
such as `./--blank` is always a source. Operand pointers are `argv:N`, zero-based
including the command but excluding the executable.

| Option | Meaning |
| --- | --- |
| `--plan FILE`, `--plan -` | Named plan or stdin; excludes source/blank operands |
| `-o FILE`, `--output FILE` | Output PDF; overrides plan output, relative to cwd |
| `--base-dir DIR` | Base for stdin plans only |
| `--fit-to SIZE` | Overrides plan fitting; paper name or dimensions |
| `--report FILE` | Full JSON report, relative to cwd |
| `--overwrite` | Permit replacement of existing regular output/report files |
| `--format json|text` | Stdout format, default JSON |
| `--help`, `-h` | Help |

Values accept `--option=value` or separate arguments. Each ordinary option is
single-use; repeated `--blank` operands are allowed. There is no progress output,
worker-count option, report-query command or inline-plan JSON option.

## Build, check and fitting

`build` needs output from `-o` or the plan. `check` does not need an output path;
when one is provided it also checks its role/existence. Both load and validate
fonts, inspect every distinct source, resolve sizes/ranges and lay out text.
`check` writes no PDF but may write its requested report. Passing check predicts
assembly; merge, pruning, fitting, I/O or publication can still fail later.

Fitting scales proportionally onto the declared sheet, preserving displayed
orientation without an extra best-fit rotation. Source widget forms are refused.
Explicit generated canvases retain their authored layout/overflow checks, then
resize; inherited canvases use the fit target. Link rectangles scale, while
coordinate destination scroll positions are not transformed. See the complete
[PDF policy and limits](DESIGN.md).

## Output

Stdout contains one result JSON object, text result, version object, help text or
schema. Stderr is quiet in normal operation. `schema result` covers result,
report and version. JSON member order is stable in the implementation but is not
a consumer contract.

```json
{
  "format_version": 3,
  "command": "build",
  "status": "ok",
  "output": {
    "path": "/abs/packet.pdf",
    "published": true,
    "sha256": "0000000000000000000000000000000000000000000000000000000000000000",
    "bytes": 11039
  },
  "pages": { "source": 1, "generated": 1, "total": 2 },
  "fit_to": null,
  "diagnostics": [],
  "diagnostics_omitted": 0,
  "report": null
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
in stdout, in stage order. Stdout shows at most 20 diagnostics, with
`diagnostics_omitted` for the rest. Each diagnostic shows at most 20 consumer
pointers, with `items_omitted`. A full report has no such caps.

Diagnostics include severity, stable code, free-English message and, when known,
file, consumer pointers and plan location. Locations have zero-based byte offset,
one-based byte line/column and JSON pointer. Stdin uses a virtual file label.
Messages may change; branch on code/status rather than matching their prose.

## Reports and committed state

A requested report is attempted once after either check/build success or failure.
No report follows argv parse failure. Targets cannot alias any plan/font/source
input or the output; existing targets must be regular files. Symlink targets,
directories and devices are refused. Input symlinks to regular files are allowed.
Roles are checked again before report publication, including an output that did
not exist initially.

When the plan could not be decoded, its inputs are unknown. Report publication
then always uses no-replace, even with `--overwrite`, so it cannot replace an
input hidden in a malformed plan. Refusals use `report_exists`,
`report_not_regular`, `report_is_input` or `report_is_output`; they do not invent
an environmental `report_write_failed` error.

Output/report files use ordinary umask permissions; replacements keep the old
mode. Publication uses a sibling temp, sync, no-replace rename (link fallback
when needed), and Unix directory sync. A trailing sync or temp-cleanup failure
can coexist with `output.published: true` or `report.written: true`. A report
failure never rolls back the PDF. The saved report cannot predict its own late
failure; stdout is authoritative for that final outcome. There are no recovery
copies or cross-file transaction guarantees.

A full report adds one part per flattened entry:

```json
{
  "format_version": 3,
  "command": "check",
  "status": "ok",
  "output": null,
  "pages": { "source": 0, "generated": 2, "total": 2 },
  "fit_to": null,
  "diagnostics": [],
  "diagnostics_omitted": 0,
  "report": { "path": "/abs/report.json", "written": true },
  "parts": [{
    "item": "/items/0", "kind": "page", "pages": 2, "count": 2,
    "output_pages": [1, 2], "size": { "width": 100, "height": 200 },
    "text": null
  }]
}
```

Source parts include path, known byte hash/length, known pages and actual
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

### Python

Run next to a `chapters` directory. This writes UTF-8 and returns PDFConcat's exit
code. Replace `glob` with `rglob` for recursive input.

```python
import json
import pathlib
import subprocess

files = sorted(path.as_posix() for path in pathlib.Path("chapters").glob("*.pdf"))
if not files:
    raise SystemExit("no PDF files in chapters/")
plan = {"version": 2, "items": files}
pathlib.Path("job.json").write_text(json.dumps(plan, ensure_ascii=False, indent=1) + "\n", encoding="utf-8")
raise SystemExit(subprocess.run(["pdfconcat", "build", "--plan", "job.json", "-o", "book.pdf"]).returncode)
```

### bash and zsh

Globs plus `jq --args` preserve quotes/newlines as arguments. `LC_ALL=C` fixes byte
order; bash `nullglob` avoids treating an unmatched pattern as a filename. In
zsh omit `shopt` (or use its `NULL_GLOB` option). Use Python for lists beyond OS
argument limits.

```sh
( export LC_ALL=C; shopt -s nullglob
  jq -n '{version: 2, items: $ARGS.positional}' --args chapters/*.pdf > job.json )
pdfconcat build --plan job.json -o book.pdf
```

### PowerShell

Ordinal sorting makes the order explicit. Force a string array even for one
file, and write an explicit UTF-8 file rather than relying on console pipeline
encoding. A leading BOM is accepted.

```powershell
$items = [string[]]@(Get-ChildItem chapters -File | Where-Object { $_.Extension -ieq '.pdf' } | ForEach-Object { 'chapters/' + $_.Name })
if ($items.Count -eq 0) { throw 'no PDF files in chapters/' }
[Array]::Sort($items, [StringComparer]::Ordinal)
@{ version = 2; items = $items } | ConvertTo-Json -Depth 3 | Set-Content job.json -Encoding utf8
pdfconcat build --plan job.json -o book.pdf
```

## Diagnostic registry

The registry in `internal/diag/codes.go` is authoritative. Warning class is empty
(shown as —); warnings never select an error outcome.

<!-- codes -->

| Code | Severity | Class | Meaning |
| --- | --- | --- | --- |
| `assembly_failed` | error | failed | PDF/text backend failed during assembly or shaping (including recovered panics) |
| `fit_form_unsupported` | error | invalid | `fit_to` with a source that has form widgets |
| `font_invalid` | error | invalid | not a static TrueType (`glyf`) font, a collection, variable, or damaged |
| `font_not_embeddable` | error | invalid | `OS/2.fsType` forbids embedding |
| `font_unreadable` | error | failed | font file could not be read |
| `interrupted` | error | interrupted | cancelled by a signal |
| `output_exists` | error | invalid | output exists and `--overwrite` not given |
| `output_invalid` | error | failed | the written output did not validate |
| `output_is_input` | error | invalid | output is (or aliases) a plan, font or source file |
| `output_not_regular` | error | invalid | output exists and is not a regular file (directory, symlink, device) |
| `output_page_count_mismatch` | error | failed | written page count ≠ expected |
| `output_publish_failed` | error | failed | the final rename failed (incl. no-replace unsupported and link fallback failed) |
| `output_write_failed` | error | failed | writing the temporary output failed (disk full, permissions) |
| `page_size_required` | error | invalid | `inherit` with no source page to inherit from |
| `page_size_too_large` | error | invalid | inherited size above 14,400 pt; give an explicit size |
| `path_invalid` | error | invalid | empty path, NUL, Windows drive-relative (`C:x`) or rooted-without-drive (`\x`) |
| `plan_duplicate_member` | error | invalid | member repeated in one object |
| `plan_item_shape` | error | invalid | an item object that is neither a generated page nor a group |
| `plan_limit` | error | invalid | nesting, entries, count or total generated pages over the limit |
| `plan_null` | error | invalid | `null` anywhere |
| `plan_syntax` | error | invalid | not one valid JSON object in UTF-8 (incl. trailing data, invalid UTF-8, comments) |
| `plan_too_large` | error | invalid | plan exceeds 64 MiB |
| `plan_type` | error | invalid | wrong JSON type |
| `plan_unknown_member` | error | invalid | member not defined at that place (case-sensitive) |
| `plan_unreadable` | error | failed | plan file or stdin could not be read |
| `plan_value` | error | invalid | value outside its grammar or range (length, colour, size, anchor, count, …) |
| `plan_version_unsupported` | error | invalid | `version` is not 2 (message explains v1 → v2 renames) |
| `report_exists` | error | invalid | report exists and `--overwrite` not given (or inputs unknown, see CLI.md) |
| `report_is_input` | error | invalid | report is (or aliases) an input |
| `report_is_output` | error | invalid | report and output are the same file |
| `report_not_regular` | error | invalid | same for the report |
| `report_write_failed` | error | failed | the report could not be written |
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
| `usage_invalid_value` | error | invalid | option value not allowed (`--format`, `--fit-to`, `schema` name, …) |
| `usage_missing_input` | error | invalid | neither `--plan` nor PDF operands |
| `usage_missing_output` | error | invalid | `build` without `-o` and without plan `output` |
| `usage_missing_value` | error | invalid | option needs a value |
| `usage_unknown_command` | error | invalid | first argument is not a command |
| `usage_unknown_option` | error | invalid | option not defined for the command |
| `destinations_dropped` | warning | — | named destinations whose target is not a page of the same document were omitted |
| `destinations_renamed` | warning | — | colliding named destinations were renamed for later occurrences |
| `form_appearances_regenerated` | warning | — | a source requests `NeedAppearances`; viewers may redraw all fields |
| `form_fields_renamed` | warning | — | pdfcpu namespaces later nonempty forms even without collisions; build reports actual names |
| `source_attachments_removed` | warning | — | embedded-files name tree or catalog `/AF` removed (page attachments remain) |
| `source_document_actions_removed` | warning | — | `/OpenAction`, catalog `/AA` or document JavaScript removed |
| `source_output_intents_removed` | warning | — | output intents removed; output makes no PDF/A or PDF/X claim |
| `source_page_labels_removed` | warning | — | page labels removed |
| `source_structure_removed` | warning | — | tagged-PDF structure removed; output is not tagged |
| `source_uri_base_removed` | warning | — | source URI Base is removed; retained relative actions may resolve differently |
| `source_xfa_removed` | warning | — | XFA removed; the static AcroForm remains |
| `text_overflow_allowed` | warning | — | `overflow: allow` and measured text extends past the page or block |
