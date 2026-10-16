# Design and PDF policy

PDFConcat is a sequential, in-memory packet assembler built on unmodified pdfcpu. It owns plan and result contracts, generated text, source-policy checks, per-occurrence destinations and file publication.

## Packages and boundaries

| Package | Responsibility |
| --- | --- |
| cmd/pdfconcat | Signals, streams, cwd capability, linker override and exit |
| cli | Grammar/help, process stdin adapter and copied stdout/terminal views |
| plan | Strict plan decoding, located errors and editor schema |
| job | Styles, paths, sizes, layering and page ranges |
| typeset | Font validation, shaping, placement and stable-GID pruning |
| pdf | PDF inspection, generation, fitting, merging and verification |
| inputfile | Regular-file reads and bounded stream reads |
| publication | Native object identity, role reservations, staging and durability |
| result | Complete result/report v4, normalization, schemas and JSON encoding |
| core | Closed application facade; admission, preparation, composition, build and report |

Only `internal/pdf` imports pdfcpu; only `internal/typeset` imports go-text;
only `internal/publication` imports x/sys. Exact strict owner rules in
`.golangci.yml` cover the entire current package graph without grandfathering.
Owner directories are canonical and module-relative; root `.` selectors are
refused because their layer glob would not match canonical source filenames.
The tools-module architecture command invokes pinned depguard directly, fully
syntax-parsing all physical Go files including inactive OS/custom tags, hidden,
ignored, generated and testdata files. It rejects unclassified/overlapping/stale
owners, source links, unexpected module/workspace roots and discovery/config/syntax
errors. Root and tools are the supported modules. Tests have explicit allowances;
fixture/oracle production has no local imports. The core parity executable in
`internal/core/testdata/client` has an explicit owner permitting only core/std.
Import/ownership enforcement does not prove inactive-code type correctness or
behavior. Review semantic cohesion separately.
See [application contracts and ownership](CORE.md) and [contributor checks](../CONTRIBUTING.md).

## Strict decoding

Plan decoding uses jsontext grammar and duplicate-member checks, json/v2 scalar conversion, and immediate domain/null checks to preserve first-fault order. A leading UTF-8 BOM is included in original byte locations. Required members, lexical integer counts and aggregate limits are checked by the decoder; standard JSON Schema cannot enforce every one of these constraints. See [PLAN](PLAN.md) for syntax, paths, layering and error precedence.

## Declared tree and physical layout

Plan v3 keeps containers through resolution. Source objects hold normalized,
ascending disjoint original-page ranges. A single resolved layout supplies
compact mappings, authored section targets/ranges and all source, page, divider,
contents and padding spans. The result/report v4 leaf parts partition physical
pages 1..N; section ranges can overlap and can begin before their non-padding
target. Titles are stored once on sections and derived into divider text.

Contents titles wrap once against a fixed measured digit column sized by the
conservative selected-source/authored-page/divider/section/odd-boundary bound.
The exact contents count is known before final parity resolution; actual target
digits do not reflow titles. Whole-row logical and ink bounds fit beneath a
repeated heading and above the configured footer envelope in final fitted
geometry. Without contents its page count is zero.

Physical Arabic footers count every page but skip painting automatic parity
blanks. They are overlays, not page parts. Fresh page-local resource dictionaries
and q/Q wrapper streams isolate original conforming graphics state; original
stream objects and page/annotation geometry remain intact. Fitted preservation
is measured against the immediate post-fit baseline. Footer placement does not
inspect source content for overlaps. The output uses physical page numbers
without a PageLabels override.

## Execution and input identity

`check` and `build` load the plan or operands, check file roles, load selected fonts and validate text policy before reading PDFs, inspect each distinct source, resolve sizes/ranges and form/destination policy, then lay out generated text. `check` stops after layout and may write a report. It does not prove that merging or serialization will succeed.

`build` rereads every source occurrence and compares its SHA-256 with the inspected bytes. Each occurrence is imported separately so its links and widgets belong to that copy. Named inputs are checked with `Stat` before opening and on the opened descriptor; symlinks to regular input files are supported. Stdin is the stream exception. Cancellation is passed to pdfcpu and checked between parts. A pending stdin read or close does not block the interrupted CLI outcome; a second signal uses the process's default termination behavior.

Each part is validated before merging. Finalization rebuilds destinations, prunes catalog/name caches, synchronizes the effective PDF version, optimizes, validates and writes a sibling temporary file. Verification reopens that file and checks validation, exact page count and serialized catalog policy before publication.

Stages collect diagnostics and stop on errors. `interrupted` takes precedence over `failed`, then `invalid`. Safe reports are attempted after failures too, but never after argument parsing fails. Readable refused sources retain known byte hashes and lengths; unknown page counts stay null. See [CLI](CLI.md) for result caps, report rules and publication facts.

## PDF policy

| Source feature | Behavior |
| --- | --- |
| Encryption, including empty passwords | Refuse `source_encrypted`, including recognized password errors before a context is returned |
| Signature values, certification or document timestamps | Refuse `source_signed`; signatures are not authenticated |
| Empty unsigned signature fields, including null values | Keep |
| `NeedsRendering true` | Refuse `source_dynamic_form` |
| `OCProperties` | Refuse `source_optional_content` |
| Damaged page tree | Refuse `source_damaged` before validator repair |
| Parse/validation failure, ordinary panic or unsupported unsafe shape | Refuse `source_invalid` |
| Content, ordinary annotations and links | Keep through upstream merge, subject to the limits below |
| Static AcroForm fields | Require compatible resolved `/DR`, form-level `/DA` and `/Q`; otherwise `source_form_unsupported` |
| Later nonempty forms | Backend may namespace fields without collisions; build reports actual mappings |
| `NeedAppearances true` | Keep request and warn that viewers may regenerate appearances |
| XFA alongside static fields | Remove XFA, keep static form and warn |
| Named and legacy destinations | Detach, rename collisions per occurrence and rebuild |
| Destination outside its source page tree | Omit with warning and remove unusable references/actions |
| Source outlines | Preserve direct/named/GoTo/Next targets per occurrence |
| Tags, page labels and output intents | Remove with warnings; output makes no tagging/PDF/A/PDF/X claim |
| Catalog `OpenAction`/`AA` and JavaScript name tree | Remove with warning |
| Embedded-files index and catalog `AF` | Remove with warning; page FileAttachment annotations remain |
| Source Info/XMP, viewer preferences and page mode/layout | Replace/remove |
| Page/annotation actions and scripts | Keep; PDFConcat does not execute them or verify network targets |
| Catalog URI Base | Remove and warn that retained relative actions may resolve differently |

The raw page-tree walk requires nonempty dictionaries, valid Page/Pages types and an exact leaf count. A Page cannot have effective non-null Kids, including an empty array. Missing, repeated or cyclic nodes are refused. Page, field and destination walks have depth bounds and inspect effective indirect values.

Ambiguous canonical dictionary keys and name values containing a literal hash followed by hex digits are refused because upstream can decode their identity twice. Ordinary literal hashes remain supported. A resource dictionary shared with an annotation or field is refused because upstream validation can discard that other role. These checks do not constitute a general PDF repair detector.

## Selected sources and native construction

Inspection validates each whole path once, retaining full hash/byte count,
original page count/version, safety and conservative font-prefix facts. Each
requested occurrence gets an isolated ephemeral selected view; only selected
geometry/navigation/appearance facts drive composition compatibility. Partial
sources with fields/widgets refuse. Optional footer geometry is measured once
for the union of selected original pages, rather than reparsing each occurrence.
Building independently rereads and digest-checks every occurrence.

Projection materializes effective inheritable resources/boxes/rotation and
places selected original page references in a balanced tree, refreshing page
caches. Selected objects stay within their occurrence's native context.
Semantic links/actions/named destinations and native outline dictionaries are
pruned within the occurrence. Omitted parent outline targets may retain a
nonclickable container for surviving descendants. Native styles/actions and
open/closed state survive grafting beneath the nearest authored section; the
composed hierarchy is checked against the backend's actual outline limit.

Generated pages use a complete native pdfcpu context and the same font/content
builder for ordinary text, multi-block contents and footers. Generated contexts
merge directly. Pruning tests serialize the same builder with original and
pruned font programs. After catalog cleanup, structural reachability
frees orphan native objects so stale live trailing entries cannot leave an
incorrect serialized xref Size; this is separate from semantic navigation policy.

## Destination byte identity

Named destinations are byte strings rather than display text. The adapter preserves raw literal/hex bytes, compares and sorts bytewise, and emits hex without text transcoding. Collisions receive unique ASCII names. References in annotations, outlines, GoTo actions and Next chains are rewritten within their source occurrence; JavaScript references are not rewritten.

Source destination structures are detached from both catalogs and context caches before merging to prevent upstream name collisions or tree loss. One traversal maps the final page references, then balanced name trees and the legacy Dests dictionary are rebuilt. Names that display identically but have different byte encodings remain distinct.

Conflicting targets for the same key in a name tree and legacy dictionary are refused. A destination outside its source page tree is omitted. An unusable GoTo action also loses its own Next chain; valid siblings in a Next array remain.

## Forms, fitting and catalog

Upstream does not generally merge multiple form default-resource dictionaries. PDFConcat compares resolved `/DR`, form-level `/DA` and `/Q` representations and permits multiple nonempty forms only when those representations match. The Fields array determines nonemptiness. An empty form is replaced by a later nonempty form, including its resources. `check` predicts possible namespacing; `build` reads actual names from the merged parent chains.

The effective output version is the maximum of PDF 1.7 and every source version. Both preflight and assembly apply the same form and fitting compatibility rules. When individually valid sources combine into PDF 2.0 output with a missing required retained annotation appearance, `check` and `build` refuse with `source_output_incompatible`, identifying the affected source and the source requiring PDF 2.0. An invalid original source can fail whole-source validation earlier as `source_invalid`. Provide complete appearances or assemble the affected source separately. The upstream checker retains its Link/Popup/Projection and both-zero-size rectangle exceptions. `check` remains predictive: other merge or serialization failures can still produce `assembly_failed`; `output_write_failed` is reserved for actual destination I/O failures.

Fitting uses upstream proportional resize with enforced target orientation and no extra rotation. Widget forms are refused. Resized pages have UserUnit 1 so the target dimensions describe physical sheets. Nonzero output box origins are permitted; target side lengths are the contract. Fitting refuses internal destinations with explicit numeric position coordinates (`fit_destination_unsupported`): XYZ left/top, FitH/FitBH top, FitV/FitBV left and FitR rectangle coordinates. This includes retained named definitions, links, outlines and chained local GoTo actions. Whole-page Fit/FitB, null-only position coordinates and zoom-only XYZ remain supported. Remote destinations, removed catalog actions and omitted out-of-tree named definitions do not trigger the guard. Use original page sizes for sources with numeric local targets. Print-box, bleed and imposition fidelity beyond this boundary is not guaranteed. Explicit generated canvases retain authored layout/overflow checks before resizing; inherited canvases use the fit target.

The output catalog permits Type, Pages, AcroForm, Dests, Outlines, Version and Names containing only Dests. XFA is removed. Cleanup covers catalog entries and internal caches so writing cannot recreate removed name trees. Effective version fields are synchronized before validation and writing: output is at least PDF 1.7, or PDF 2.0 if any source is 2.0.

## Text and pruning

Text is shaped and measured with the original static TrueType program. ToUnicode and ActualText preserve text extraction for glyph aliases and clusters. Restricted and bitmap-only outline embedding are refused; no-subsetting permissions retain the original program. Empty text still validates its selected font without embedding an unused font. See [PLAN](PLAN.md) for script, wrapping, placement and overflow rules.

Fonts selected for generated text require complete consumed `post` metadata
(32-byte common header; formats 1, 2, 2.5 or 3) and `OS/2` metadata (version 0–5;
64-byte consumed prefix). The signed 16.16 `post` angle and nonzero fixed-pitch
value are independent of style. Italic style uses `fsSelection` bit 0 or, for
`OS/2` version 4 or later, oblique bit 9; names, angle and `head` flags are not
style fallbacks. Missing, short or unknown metadata is `font_invalid`; embedding
rights are enforced separately. Original traits feed shared native font
descriptors for full/pruned programs and body/contents/footer resources.

Glyph pruning keeps IDs, metrics, glyph count, used outlines and composite components; rebuilds glyf/loca and checksums; reduces post to format 3; and drops layout/cmap tables after shaping. Stable IDs allow Identity CID-to-GID mapping without a renumbering layer. The job-wide glyph union produces identical font programs, widths and ToUnicode in generated items so upstream can deduplicate them. Counted pages within one item share content/resources and use a balanced page tree.

Prepared fonts explicitly record whether they contain a subset. Before writing any generated item, the PDF adapter reserves six-uppercase-letter subset prefixes found in imported BaseFont and descriptor FontName entries and tag-like names of generated full fonts, then allocates unused prefixes deterministically in sorted font-byte identity order. Each pruned font uses one `AAAAAA+PostScriptName`-style name throughout the job; its Type0 wrapper, CIDFont and descriptor agree. Full programs receive no added prefix and retain their original sanitized PostScript name and exact bytes, including names that already resemble subset names. TrueType name tables remain unchanged. PDF name syntax is escaped when writing, so ordinary literal hashes remain literal. The canonical-name safety policy also applies to generated fonts: a PostScript name containing a literal hash followed by two hexadecimal digits is refused as `font_invalid` by both check and build, before publication. Select a font with a different PostScript name. Generated allocation avoids adding imported-tag collisions; it does not repair naming defects between sources.

## Parser and resource limits

Upstream relaxed parsing can accept malformed object suffixes, duplicate decoded keys including null omissions, malformed/overflowing/underflowing numeric tokens and normalized malformed free-head entries. Successful parsing or validation does not establish that arbitrary malformed input retains its meaning. Some legal comment-separated indirect-reference spellings are unsupported and return `source_invalid`.

Finite PDF real numbers are serialized by upstream to at most twelve fractional digits; bitwise numeric round trips are not promised. Upstream owns content-stream parsing and fitting may rewrite those streams. Source metadata is replaced/removed as specified in the policy table.

Assembly is in memory. Imported object graphs, decoded streams, repeated source occurrences and generated pages can require much more memory than compressed input sizes. There is no universal time or memory bound, hostile decoder-work budget, or guarantee to interrupt every inner decoding loop. Ordinary panic recovery does not cover memory exhaustion or fatal stack overflow. Use operating-system resource limits for unusually large or untrusted inputs. [SECURITY](../SECURITY.md) defines the trusted-input boundary; [CONTRIBUTING](../CONTRIBUTING.md#comparison-and-scale) describes optional measurements.

## Files, tools and delivery

Output and report files follow umask; replacements retain their mode. Publication records visibility immediately after rename/link, before cleanup/directory sync. Abort removes only its temporary name. Report roles are rechecked after PDF publication; unsafe targets are invalid refusals and attempted I/O failures are failed outcomes. An undecoded plan forces report no-replace even with overwrite. There is no private-ACL, recovery-copy, cross-file transaction, adversarial namespace-race or universal network-filesystem guarantee.

Tool versions are declared in `tools/go.mod`. The linter is built for the host before running it under each target GOOS. Actionlint runs with stock `@version` isolation because its YAML API is incompatible with the combined linter-tool dependency graph; the selected version is derived from that same module. Contributor commands, verification scope and isolated snapshot precautions are in [CONTRIBUTING](../CONTRIBUTING.md).

CI runs lint, native tests on Linux/macOS/Windows, a six-target cross-build and the `Required CI` aggregate. The release workflow requires these checks before publishing tagged binaries and provenance.
