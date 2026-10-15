# Design and PDF policy

PDFConcat is a sequential, in-memory packet assembler built on unmodified pdfcpu. It owns plan and result contracts, generated text, source-policy checks, per-occurrence destinations and file publication.

## Packages and boundaries

| Package | Responsibility |
| --- | --- |
| cmd/pdfconcat | Signals, standard streams, build identity and exit code |
| cli | Interleaved argument grammar and help |
| plan | Strict plan decoding, located errors and editor schema |
| job | Styles, paths, sizes, layering and page ranges |
| typeset | Font validation, shaping, placement and stable-GID pruning |
| pdf | PDF inspection, generation, fitting, merging and verification |
| inputfile | Regular-file reads and bounded stream reads |
| atomicfile | Sibling staging, no-replace publication and durability |
| result | Result/report v3, schemas and capped stdout views |
| pipeline | Staged check/build execution and safe reporting |

Only `internal/pdf` imports pdfcpu; only `internal/typeset` imports go-text; only `internal/atomicfile` imports x/sys. The domain and typesetting packages depend only on the diagnostic package internally. Test-only `pdffixture` creates PDFs and `oracle` inspects them with qpdf and Poppler. Stock depguard enforces these boundaries.

## Strict decoding

Plan decoding uses jsontext grammar and duplicate-member checks, json/v2 scalar conversion, and immediate domain/null checks to preserve first-fault order. A leading UTF-8 BOM is included in original byte locations. Required members, lexical integer counts and aggregate limits are checked by the decoder; standard JSON Schema cannot enforce every one of these constraints. See [PLAN](PLAN.md) for syntax, paths, layering and error precedence.

## Pipeline and input identity

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

## Destination byte identity

Named destinations are byte strings rather than display text. The adapter preserves raw literal/hex bytes, compares and sorts bytewise, and emits hex without text transcoding. Collisions receive unique ASCII names. References in annotations, outlines, GoTo actions and Next chains are rewritten within their source occurrence; JavaScript references are not rewritten.

Source destination structures are detached from both catalogs and context caches before merging to prevent upstream name collisions or tree loss. One traversal maps the final page references, then balanced name trees and the legacy Dests dictionary are rebuilt. Names that display identically but have different byte encodings remain distinct.

Conflicting targets for the same key in a name tree and legacy dictionary are refused. A destination outside its source page tree is omitted. An unusable GoTo action also loses its own Next chain; valid siblings in a Next array remain.

## Forms, fitting and catalog

Upstream does not generally merge multiple form default-resource dictionaries. PDFConcat compares resolved `/DR`, form-level `/DA` and `/Q` representations and permits multiple nonempty forms only when those representations match. The Fields array determines nonemptiness. An empty form is replaced by a later nonempty form, including its resources. `check` predicts possible namespacing; `build` reads actual names from the merged parent chains.

The backend can require widget appearance dictionaries when writing PDF 2.0. A valid PDF 1.7 form using viewer-regenerated appearances can fail assembly when another source raises the output to 2.0. Provide sources with complete appearances or assemble the form separately. This produces `assembly_failed`; `output_write_failed` is reserved for actual destination I/O failures. `check` does not serialize a merged document and may not detect this combination.

Fitting uses upstream proportional resize with enforced target orientation and no extra rotation. Widget forms are refused. Resized pages have UserUnit 1 so the target dimensions describe physical sheets. Nonzero output box origins are permitted; target side lengths are the contract. Destination XYZ/FitH/FitV/FitR scroll coordinates are not transformed. Print-box, bleed and imposition fidelity beyond this boundary is not guaranteed. Explicit generated canvases retain authored layout/overflow checks before resizing; inherited canvases use the fit target.

The output catalog permits Type, Pages, AcroForm, Dests, Outlines, Version and Names containing only Dests. XFA is removed. Cleanup covers catalog entries and internal caches so writing cannot recreate removed name trees. Effective version fields are synchronized before validation and writing: output is at least PDF 1.7, or PDF 2.0 if any source is 2.0.

## Text and pruning

Text is shaped and measured with the original static TrueType program. ToUnicode and ActualText preserve text extraction for glyph aliases and clusters. Restricted and bitmap-only outline embedding are refused; no-subsetting permissions retain the original program. Empty text still validates its selected font without embedding an unused font. See [PLAN](PLAN.md) for script, wrapping, placement and overflow rules.

Glyph pruning keeps IDs, metrics, glyph count, used outlines and composite components; rebuilds glyf/loca and checksums; reduces post to format 3; and drops layout/cmap tables after shaping. Stable IDs allow Identity CID-to-GID mapping without a renumbering layer. The job-wide glyph union produces identical font programs, widths and ToUnicode in generated items so upstream can deduplicate them. Counted pages within one item share content/resources and use a balanced page tree.

## Parser and resource limits

Upstream relaxed parsing can accept malformed object suffixes, duplicate decoded keys including null omissions, malformed/overflowing/underflowing numeric tokens and normalized malformed free-head entries. Successful parsing or validation does not establish that arbitrary malformed input retains its meaning. Some legal comment-separated indirect-reference spellings are unsupported and return `source_invalid`.

Finite PDF real numbers are serialized by upstream to at most twelve fractional digits; bitwise numeric round trips are not promised. Upstream owns content-stream parsing and fitting may rewrite those streams. Source metadata is replaced/removed as specified in the policy table.

Assembly is in memory. Imported object graphs, decoded streams, repeated source occurrences and generated pages can require much more memory than compressed input sizes. There is no universal time or memory bound, hostile decoder-work budget, or guarantee to interrupt every inner decoding loop. Ordinary panic recovery does not cover memory exhaustion or fatal stack overflow. Use operating-system resource limits for unusually large or untrusted inputs. [SECURITY](../SECURITY.md) defines the trusted-input boundary; [CONTRIBUTING](../CONTRIBUTING.md#comparison-and-scale) describes optional measurements.

## Files, tools and delivery

Output and report files follow umask; replacements retain their mode. Publication records visibility immediately after rename/link, before cleanup/directory sync. Abort removes only its temporary name. Report roles are rechecked after PDF publication; unsafe targets are invalid refusals and attempted I/O failures are failed outcomes. An undecoded plan forces report no-replace even with overwrite. There is no private-ACL, recovery-copy, cross-file transaction, adversarial namespace-race or universal network-filesystem guarantee.

Tool versions are declared in `tools/go.mod`. The linter is built for the host before running it under each target GOOS. Actionlint runs with stock `@version` isolation because its YAML API is incompatible with the combined linter-tool dependency graph; the selected version is derived from that same module. Contributor commands, verification scope and isolated snapshot precautions are in [CONTRIBUTING](../CONTRIBUTING.md).

CI runs lint, native tests on Linux/macOS/Windows, a six-target cross-build and the `Required CI` aggregate. The release workflow requires these checks before publishing tagged binaries and provenance. Current upstream reproduction drafts are in [upstream issue drafts](https://github.com/resoltico/pdfconcat/blob/main/docs/upstream-pdfcpu-issues.md); posting them requires maintainer approval.
