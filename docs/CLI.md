# Command line and result v10

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

`pdfconcat COMMAND --help` (or `-h`) and `pdfconcat help COMMAND` show the same
command topic. Bare `help` and root `--help` list commands. `--version` is also
accepted. Help is plain text. Options interleave with operands. `--` ends
option parsing, and a path such as `./--blank` is always a source.

| Option | Meaning |
| --- | --- |
| `--plan FILE`, `--plan -` | Named plan or stdin; excludes source/blank operands |
| `--blank` | Insert one generated page; size comes from a source or `--fit-to SIZE` |
| `-o FILE`, `--output FILE` | Output PDF; overrides plan output, relative to cwd |
| `--base-dir DIR` | Base for stdin plans only |
| `--fit-to SIZE` | Replace the plan fitting policy with this target and the CLI tolerance |
| `--fit-tolerance LENGTH` | Set the absolute per-side tolerance. Requires `--fit-to` in the same invocation |
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

Argument diagnostics use `argv:N`, with the command at `argv:0`. Mixing a plan
and direct inputs points to the argument that creates the conflict. A disallowed
base directory points to `--base-dir`. For `schema`, the first selector must be
`plan` or `result`. The diagnostic for surplus selectors points to the first
extra argument.

Direct input arguments refuse populated signature state. If you deliver the
original separately and want an unsigned copy, set `"signatures": "discard"` in
a [plan](PLAN.md#signatures) and run `pdfconcat check --plan FILE` to see whether
the source qualifies. The refusal includes this guidance in text and JSON.
Build reports whether it published the PDF. See the
[signature policy](PDF-POLICY.md#signatures).

## Build, check and fitting

`build` needs output from `-o` or the plan. `check` does not need an output path.
When given a destination, `check` validates its role, existence and parent
directory. Both commands validate fonts, inspect distinct sources, resolve
page ranges and sizes, and lay out text. `check` can write a requested report.
Assembly, staged PDF verification and publication run only in `build` and can
still fail after a successful `check`.

A direct blank inherits the last selected page size of the nearest preceding
source, or the first selected page size of the first following source. With no
source, supply a fit target:

```sh
pdfconcat check --blank --fit-to A4
pdfconcat build --blank --fit-to A4 -o blank.pdf
```

`--fit-to` accepts the sheet grammar in [PLAN](PLAN.md#generated-pages-and-layering).
It replaces the complete policy after plan admission. With no `--fit-tolerance`,
the replacement uses .01 pt. With neither option, the plan supplies the policy.
Pages outside the tolerance are centered proportionally on the chosen sheet,
retaining their artwork clipping, pattern proportions and annotation geometry.
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

A stdout write failure, including a closed pipe reader, returns exit 1. A diagnostic
is written when stderr is available; no diagnostic delivery is guaranteed when
stderr is also broken. Any PDF or report already published remains saved. Its
saved processing facts do not imply that the final stdout summary was delivered.

In text mode, usage errors print diagnostics and a help hint on stderr, with
empty stdout. For build, check and version, an exact `--json` before the option
delimiter selects JSON even when argument parsing fails. A `--` consumed as an
option's required value is not that delimiter. The first error and its location
are retained.

```json
{
  "format_version": 10,
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
  "fitting": null,
  "diagnostics": [],
  "diagnostics_omitted": 0,
  "report": null,
  "page_numbers": null,
  "metadata": {},
  "timestamp": null,
  "structure": {
    "policy": "preserve",
    "coverage": "complete"
  },
  "numbering": []
}
```

This example assumes fully covered source and generated content. The digest is
illustrative; real digests are lowercase SHA-256 hex.
`output` is null until a path is known. `published` records actual namespace
publication; digest/bytes appear only when true. Page counts are null until
established by the corresponding completed stages. `fitting` contains the admitted
`target` dimensions and resolved `tolerance` in points, or null. An error never invents zero
pages. Warnings do not make an outcome invalid.

| Status | Exit | Meaning |
| --- | ---: | --- |
| `ok` | 0 | No errors; warnings possible |
| `invalid` | 2 | Usage, plan, policy, overflow or destination refusal |
| `failed` | 1 | Environment, backend, verification or publication failure |
| `interrupted` | 130 | Cancelled |

Processing status precedence is interrupted, failed, then invalid. If rendering
succeeds, the exit follows that status.
Diagnostic presentation keeps output-publication and report-writing failures
first, then interruption, other failures, invalid-input errors and warnings.
Stage/first-use order stays stable within each priority group. This keeps bounded
publication failures and the cause of the overall outcome visible under caps.
The summary shows at most 20 diagnostics, with
`diagnostics_omitted` for the rest. Each diagnostic shows at most 20 consumer
pointers, with `items_omitted`. A full report has no such caps. Omission hints
distinguish a saved report from an unavailable requested report; they do not
recommend reading a failed report.
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

Both input modes remove proved-empty optional-content settings. Use
`"optional_content": "flatten-visible-content"` in a plan to allow the other
[admitted optional-content profiles](PDF-POLICY.md#optional-content).
Fitted-destination and optional-content findings describe planned changes.
Use `output.published` to determine whether Build committed a PDF.

## Reports and committed state

Use `--report FILE` to request a full JSON report, including when `check` or
`build` fails. Reporting happens after application execution. A rejected
command line, an unavailable working directory, or a failed stdin plan read
produces no report. Report targets are checked against known plan, font and
source inputs and the output. Each destination needs an existing parent
directory. Existing targets
must be regular files. PDFConcat accepts input symlinks to regular files and
refuses symlinks, directories, and devices as destinations.

When the plan could not be decoded, its inputs are unknown. Choose a fresh
report path in that case. Report publication uses no-replace even with
`--overwrite` to protect inputs that the decoder could not identify.
Existing-target and known-collision refusals use `report_exists`,
`report_not_regular`, `report_is_input` or `report_is_output`.
An atomic no-replace collision uses the same exit 2 and destination code as an
existing target found during preflight (`output_exists` or `report_exists`).
It advises choosing a fresh path. Replacement advice requires preflight to
verify a regular destination distinct from all known inputs.

Existing files are compared by native identity, including hard links. If either
name is absent, PDFConcat compares their resolved parent directories and checks
leaf spellings conservatively. Names in the same directory that differ only in
case, canonical Unicode form, or ignorable or format characters can be refused
even on a filesystem that distinguishes them. Windows and recognized FAT and
SMB volumes also account for DOS spelling and potential short-name aliases when
checking reports or publishing a PDF. For a short-name conflict, choose
different extensions or directories. PDFConcat rechecks report destinations
after PDF publication and report serialization, including when the output was
initially absent. A destination can still fail to write after its parent passes
preflight.

Each invocation starts without saved PDFConcat settings, caches, or job
history. Large source PDFs use snapshots in the operating system's temporary
directory during inspection and assembly. Allow space for concurrent
source inspections and the largest individual assembly source. A
temporary-storage failure reports `source_unreadable` in either stage, with the
input and the scratch operation. Each spill stays bound to its original
directory for reopening and cleanup. The verifier reads the completed PDF
through the owned staging handle, including for destinations with no read
permission bits. It compares the staged length and SHA-256 with the assembler's
emitted bytes before publication.

On macOS and Linux, new output and report files use ordinary umask permissions.
The empty sibling stage takes an existing destination's permission bits before
data is written. Before publication, the final destination check supplies the
replacement's permission bits and the stage is synced with that mode. On
Windows, the mode setting controls the file's read-only attribute. PDFConcat
writes no custom access control list. Temporary filenames leave the full
filesystem filename limit available for the destination. PDF and report stages
stay bound to their original parent directory for cleanup. An observed parent
replacement prevents publication.

A crash or forced termination can leave source snapshots and sibling PDF or
report stages containing document bytes on disk. Later invocations leave these
files in place. For sensitive jobs, use a private per-job workspace for the
PDF and report destinations and the temporary directory. Before launching
PDFConcat, point `TMPDIR` on macOS and Linux or `TMP` on Windows to the
workspace's temporary directory. Each output or report stage stays beside its
own destination. After the process exits, retain the published files you need
and clean up the workspace.

Without `--overwrite`, publication uses a no-replace rename with a hard-link
fallback. With `--overwrite`, publication uses a replacing rename when the
destination rules permit replacement. Reports with unknown inputs still use
no-replace. On Unix, the containing directory is synced after publication. A
trailing sync or temp-cleanup failure can coexist with `output.published: true`
or `report.written: true`. PDF publication and report publication are separate
operations. Use the final CLI result to determine whether the report was written
and whether any trailing operation failed. PDFConcat creates no recovery copies.

A full report adds preorder `sections` with `item`, `title`, nullable `parent`,
`target_page` and `output_pages`. It records `page_numbers` once at packet level.
It includes one compact part per source occurrence, page item run, divider,
contents item or padding span:

```json
{
  "format_version": 10,
  "command": "check",
  "status": "ok",
  "output": null,
  "pages": {
    "source": 0,
    "generated": 2,
    "total": 2
  },
  "fitting": null,
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
      "output_size": {"width": 100, "height": 200},
      "text": null,
      "input_pages": null,
      "spans": null,
      "page_sizes": null,
      "fields_renamed": null
    }
  ],
  "page_numbers": null,
  "sections": [],
  "metadata": {},
  "timestamp": null,
  "structure": {
    "policy": "preserve",
    "coverage": "none"
  },
  "numbering": []
}
```

Source parts include full-input `input_pages`, hash/byte count, emitted `pages`
and compact `spans` such as `{"source":[2,4],"output":[10,12]}`. Unknown source
mappings and non-source parts use `spans: null`; known source mappings are
nonempty arrays. Divider parts reference their section; padding records
`reason: odd_start` and `before_item`.
Complete leaf ranges partition physical pages 1..N once. Nested section ranges
may overlap. Until the physical page sequence has been resolved, emitted
counts/spans/ranges and section targets stay null, and no padding is predicted.
A later compatibility, numbering, text-layout or build failure can retain a complete predicted sequence.
A nonnull total means the physical sequence is known; it does not establish
successful PDF publication.

Source parts include path, known byte hash/length and actual
`fields_renamed` records. Readable refused sources retain byte facts; unestablished
page counts/ranges remain null. Page items and dividers include `count` and, when
known, authored `size` and measured `text`. Contents and padding parts include
their authored canvas size but no text-layout object. Generated `output_size`
records the expected displayed frame after layout resolves. Source parts have
`output_size: null` and carry their geometry in `page_sizes`. Text bounds stay
in authored coordinates.

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
algorithm. Check uses null for unestablished mapping facts; [] means an observed empty mapping.

### Selected source page-size runs

Every part has nullable `page_sizes`. Non-source parts use null and retain the
existing authored meaning of `size`. Once complete validated selected geometry
and fit decisions are known, source parts contain nonempty runs:

`[{"source":[2,4],"source_size":{"width":595.28,"height":841.89},"output_size":{"width":595.28,"height":841.89}}]`

`source` is an inclusive original source-page range, not a selected or output
ordinal. `spans` separately owns physical output mapping. Runs combine only
adjacent original pages whose source AND output sizes are exactly equal; gaps and
near-but-distinct dimensions never merge. Dimensions are full-precision positive
finite physical points (1/72 inch), without generated canvas limits.

`source_size` is the pre-fit displayed frame. `output_size` is the resolved
EXPECTED displayed size, not evidence of PDF publication. Without fitting, or on
a kept page, it is the actual source size; on a transformed page it is the target.
Complete known runs survive later refusals and failures even if counts/spans
remain unresolved. Unknown/invalid selections or incomplete geometry use null,
never an empty array or a falsely complete prefix. Standard output may omit parts;
request the existing full report to inspect all runs.

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

Globs and `jq --args` preserve quotes and newlines within filenames.
`LC_ALL=C` requests byte order. The example uses bash. In zsh, replace
`shopt -s nullglob` with `setopt NULL_GLOB`. For lists beyond the OS argument
limit, use a program that writes each filename as a separate JSON element.

```sh
( export LC_ALL=C
  shopt -s nullglob
  chapters=()
  for chapter in chapters/*.pdf; do
    [[ -f "$chapter" ]] && chapters+=("$chapter")
  done
  if [[ ${#chapters[@]} -eq 0 ]]; then
    printf '%s\n' 'No PDF files in chapters/.' >&2
    exit 1
  fi
  jq -n '{version: 11, items: ($ARGS.positional | map({source: .}))}' --args "${chapters[@]}" > plan.json ) &&
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
@{ version = 11; items = @($items | ForEach-Object { @{ source = $_ } }) } | ConvertTo-Json -Depth 5 | Set-Content plan.json -Encoding utf8
pdfconcat build --plan plan.json -o book.pdf
```


## Diagnostic evidence

Core diagnostics supply the condition, explanation and applicable remedy.
The CLI can add command syntax to text output. JSON results and reports retain
the core explanation. `detail` can carry observations, measurements or phase
information. `pdf_location` can identify a positive `object` or `page` number,
or a dictionary `path`. Absent fields mean attribution is unavailable.
Source-page numbers refer to the original source, including after selection;
output-page numbers refer to the composed PDF. Source object numbers are captured
before selection and backend renumbering; they are not output object IDs. Paths
are anchored to that original object or its enclosing direct container. Document
collections, outline nodes and destination definitions omit `page` when they have
no owning page; a removed target's original page may instead appear in `detail`.
It is separate from `location`, which identifies plan text. Consumers should
branch on `code`, not backend text or a reported path. `source_validation_notice`
retains backend compatibility evidence, including phase and disposition in
`detail`. A skipped validation notice does not mean that content was removed;
explicitly reported repairs cause a separate `source_unsupported` refusal.

Navigation warnings identify changed bindings and their affected items. Build
combines equivalent inspection and assembly findings, using assembly evidence
when available. Distinct definitions and actions retain their own findings,
including matching entries in the destination dictionary and name tree.
`source_relative_action_context` identifies retained relative URI and remote
GoTo actions that may resolve differently at the packet location. A retained
relative URI with a nonempty source Base reports `source_uri_base_removed`.
Check reports these findings even without an output path. Their PDF locations
identify the action's original binding slot. See the [external action
policy](PDF-POLICY.md#external-action-context).

Check rename warnings identify known source scopes and definitions as
predictions. Build reports the original and assigned names. Layout findings
include a structured plan location when a specific authored value is implicated.
Each finding lists
distinct consuming items in first-seen order. Backend notices expose only
locations the backend supplies.

Warnings merge their consuming `items` only when their code, stable typed
cause/parameters, phase and file/site are equivalent. Message rendering and
opaque backend detail do not define identity; findings without sufficient stable
identity remain distinct. Different viewer entries therefore
remain different findings, including when a source is repeated. Full reports
retain all findings; stdout summaries still apply the documented caps.

`source_invalid` means parsing or validation failed. `source_unsupported` means
a supported-fidelity boundary was reached. `source_processing_failed` is a backend
failure, not proof of invalid input. Both invalid and unsupported input select
status `invalid`; a processing failure selects `failed`.

`source_xref_structure_reference_discarded` locates the original discarded
structure index and identifies the matching classic xref row. It accompanies
the structure-discard warning. Check and Build keep `source_invalid` and
exit 2 under preserve when a proved unwritten structure index prevents
admission. The refusal can name the plan discard setting and locate the
proved index. [Classic xref input policy](PDF-POLICY.md#narrow-classic-xref-input-tolerance)

## Diagnostic codes

The [diagnostic registry](../internal/core/diag/codes.go) is authoritative.
Warning class is empty (shown as —); warnings never select an error outcome.

<!-- codes -->

<!-- BEGIN GENERATED DIAGNOSTICS -->
| Code | Severity | Class | Meaning |
| --- | --- | --- | --- |
| `request_invalid` | error | invalid | invalid application request envelope |
| `source_selection_out_of_range` | error | invalid | selected page range exceeds the original source page count |
| `source_selection_form_unsupported` | error | invalid | partial page selection of a source with form fields or widgets is unsupported |
| `source_navigation_removed` | warning | — | navigation to omitted source pages was removed for affected occurrences |
| `source_outline_depth_unsupported` | error | invalid | authored sections and imported bookmarks exceed the supported outline hierarchy |
| `source_form_unsupported` | error | invalid | nonempty forms require identical resolved DR/DA/Q representations |
| `source_relative_action_context` | warning | — | a retained relative external action can resolve differently at the packet location |
| `source_uri_base_removed` | warning | — | source URI Base is removed; retained relative actions may resolve differently |
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
| `plan_limit` | error | invalid | nesting, entries, count, authored text or total generated pages over the limit |
| `path_invalid` | error | invalid | empty path, NUL, Windows drive-relative (`C:x`) or rooted-without-drive (`\x`) |
| `output_exists` | error | invalid | output exists and replacement was not authorized |
| `report_exists` | error | invalid | report exists and replacement was not authorized, or plan inputs are unknown |
| `output_parent_invalid` | error | invalid | output parent is absent or is not a directory |
| `report_parent_invalid` | error | invalid | report parent is absent or is not a directory |
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
| `source_unreadable` | error | failed | source read or temporary snapshot I/O failed |
| `source_changed` | error | failed | source bytes or readability at assembly differ from inspection |
| `source_structure_partial` | warning | — | retained source content has incomplete structural coverage |
| `source_not_pdf` | error | invalid | source has no recognizable PDF header |
| `source_input_invalid` | error | invalid | source input is absent, denied or nonregular |
| `plan_input_invalid` | error | invalid | plan input is absent, denied or nonregular |
| `font_input_invalid` | error | invalid | font input is absent, denied or nonregular |
| `source_xref_entry_ignored` | warning | — | unreferenced zero-offset classic xref entry ignored without changing original source bytes |
| `source_invalid` | error | invalid | PDF parsing or validation failed |
| `source_unsupported` | error | invalid | source structure cannot be composed with the supported fidelity guarantees |
| `source_processing_failed` | error | failed | unexpected inspection or PDF backend failure, including a recovered panic |
| `source_validation_notice` | warning | — | backend compatibility notice; skipped validation does not establish content removal |
| `source_viewer_settings_removed` | warning | — | source catalog PageLayout, PageMode or ViewerPreferences removed before validation |
| `source_damaged` | error | invalid | broken page tree, missing/malformed content streams, or validation changing page order or encoded contents |
| `source_encrypted` | error | invalid | encrypted, including empty-password encryption |
| `source_signature_discard_unsupported` | error | invalid | signature state cannot be discarded with proved ownership and appearance fidelity |
| `source_signature_discarded` | warning | — | supported source signature state is discarded during composition |
| `source_signed` | error | invalid | signature value, catalog permission structures (`/Perms`) or document timestamp |
| `source_dynamic_form` | error | invalid | source requests NeedsRendering true or contains XFA without a usable static form |
| `source_optional_content` | error | invalid | optional content is present outside the admitted policy |
| `source_optional_content_flattened` | warning | — | composition removes the proved layer's visibility controls and preserves the appearance of retained Form artwork |
| `fit_geometry_unsupported` | error | invalid | the required fitting affine lacks finite usable geometry or supported forward-placement accuracy |
| `fit_annotation_ownership_unsupported` | error | invalid | a multiply bound annotation dictionary belongs to at least one transformed page |
| `fit_print_box_omitted` | warning | — | fitting omits a page's optional printing box; output defaults apply |
| `fit_destination_unsupported` | error | invalid | a retained local destination cannot be mapped to its transformed target |
| `fit_destination_mapped` | warning | — | the fitting plan maps local destination coordinates to a fitted target |
| `source_structure_destination` | error | invalid | retained PDF structure destination cannot be preserved |
| `source_output_incompatible` | error | invalid | retained annotations lack appearances required by the effective PDF 2.0 output |
| `fit_annotation_unsupported` | error | invalid | fitting requires appearances for FreeText, Text icons, Caret, captioned Lines, rotated NoRotate or boundary-crossing artwork; interactive hit areas must stay inside the source frame |
| `fit_form_unsupported` | error | invalid | a widget transforms or its page ownership is unresolved or conflicting while other pages transform |
| `assembly_failed` | error | failed | PDF/text backend failed during assembly or shaping (including recovered panics) |
| `output_write_failed` | error | failed | writing the staged output failed (disk full, permissions) |
| `output_invalid` | error | failed | the staged PDF failed structural or byte verification |
| `output_page_count_mismatch` | error | failed | written page count ≠ expected |
| `output_publish_failed` | error | failed | output publication or staged cleanup failed |
| `report_write_failed` | error | failed | report writing, publication or staged cleanup failed |
| `interrupted` | error | interrupted | cancelled by a signal |
| `text_overflow_allowed` | warning | — | `overflow: allow` and measured text extends past the page or block |
| `form_fields_renamed` | warning | — | pdfcpu namespaces later nonempty forms even without collisions; build reports actual names |
| `form_appearance_regeneration_requested` | warning | — | a source requests `NeedAppearances`; viewers may redraw all fields |
| `destinations_renamed` | warning | — | colliding named destinations were renamed for later occurrences |
| `destinations_dropped` | warning | — | named destinations whose target is not a page of the same document were omitted |
| `source_xfa_removed` | warning | — | XFA removed; the static AcroForm remains |
| `source_structure_orphaned` | warning | — | tree-less source semantic remnants ignored and ownership removed |
| `source_structure_index_normalized` | warning | — | derived structure indexes normalized without changing retained semantics |
| `source_structure_removed` | warning | — | source structure claims or ownership discarded by explicit policy |
| `source_page_labels_removed` | warning | — | page labels removed |
| `source_output_intents_removed` | warning | — | output intents removed; output makes no PDF/A or PDF/X claim |
| `source_document_actions_removed` | warning | — | `/OpenAction`, catalog `/AA` or document JavaScript removed |
| `source_attachments_removed` | warning | — | embedded-files name tree or catalog `/AF` removed (page attachments remain) |
| `working_directory_unavailable` | error | failed | The process working directory could not be determined |
| `output_inspection_failed` | error | failed | The output destination could not be inspected |
| `report_inspection_failed` | error | failed | The report destination could not be inspected |
| `source_optional_content_empty_removed` | warning | — | composition removes proved-empty optional-content settings |
| `source_optional_content_wrappers_removed` | warning | — | composition removes proved undeclared whole-page visibility wrappers and preserves retained artwork |
| `source_xref_structure_reference_discarded` | warning | — | a discarded structure index referenced a proved zero-offset classic xref entry |
| `usage_unknown_command` | error | invalid | first argument is not a command |
| `usage_unknown_option` | error | invalid | option not defined for the command |
| `usage_missing_value` | error | invalid | option needs a value |
| `usage_invalid_value` | error | invalid | option value not allowed (`--fit-to`, `--fit-tolerance`, `schema` name, …) |
| `usage_conflict` | error | invalid | `--plan` with PDF or `--blank` operands; `--base-dir` without `--plan -`; repeated single-use option |
| `usage_missing_input` | error | invalid | neither `--plan` nor PDF or `--blank` operands |
<!-- END GENERATED DIAGNOSTICS -->
