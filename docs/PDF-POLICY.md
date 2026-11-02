# PDF policy

PDFConcat composes trusted local PDFs in memory using pdfcpu. Source policy is
checked before layout and again where it depends on the final packet. The trust
and filesystem boundary is in [SECURITY](../SECURITY.md).

## Source policy

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
| Later nonempty forms | Backend may namespace fields even when original names do not collide; build reports actual mappings |
| `NeedAppearances true` | Keep request and warn that viewers may regenerate appearances |
| XFA alongside static fields | Remove XFA, keep static form and warn |
| Named and legacy destinations | Detach, rename collisions per occurrence and rebuild |
| Destination outside its source page tree | Omit with warning and remove unusable references/actions |
| Source outlines | Preserve direct/named/GoTo/Next targets per occurrence |
| Tags, page labels and output intents | Remove with warnings; output makes no tagging/PDF/A/PDF/X claim |
| Catalog `OpenAction`/`AA` and JavaScript name tree | Remove with warning |
| Embedded-files index and catalog `AF` | Remove with warning; page FileAttachment annotations remain |
| Document Info and catalog-level XMP, viewer preferences and page mode/layout | Replace/remove; metadata attached to retained pages or other objects may remain |
| Page/annotation actions and scripts | Keep; PDFConcat does not execute them or verify network targets |
| Catalog URI Base | Remove and warn that retained relative actions may resolve differently |

### Structural rejection rules

The page tree must have nonempty dictionaries, valid Page/Pages types and an
exact leaf count. A Page cannot have effective non-null Kids, including an empty
array. Missing, repeated or cyclic nodes are refused. The raw page-tree check
runs before validation; page, field and destination walks have depth bounds and
inspect effective indirect values.

Ambiguous canonical dictionary keys and name values containing a literal hash
followed by hex digits are refused because upstream can decode their identity
twice. Ordinary literal hashes remain supported. A resource dictionary shared
with an annotation or field is refused because upstream validation can discard
that other role. These checks do not constitute a general PDF repair detector.

## Selected sources and native construction

### Selection and inspection

Page selection chooses which pages appear in the packet. It is not redaction and
does not promise to remove every private object or byte associated with omitted
pages. Partial selection of sources containing form fields or widgets is refused.

An occurrence is one use of a source in the packet. Inspection validates each
whole source path once and retains its full hash and byte count, original page
count and version, safety information and conservative font-prefix facts. Each
occurrence gets an isolated temporary view of its selected pages; only the
selected geometry, navigation and appearance facts drive composition compatibility.
Optional footer geometry is measured once for the union of selected original
pages, rather than reparsing each occurrence. Building independently rereads and
digest-checks every occurrence.

### Assembly details

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

Named destinations are byte strings rather than display text. Names that display
identically but have different byte encodings remain distinct. Collisions receive
unique ASCII names. References in annotations, outlines, GoTo actions and Next
chains are rewritten within their source occurrence; JavaScript references are
not rewritten.

Conflicting targets for the same key in a name tree and legacy dictionary are
refused. A destination outside its source page tree is omitted. An unusable GoTo
action also loses its own Next chain; valid siblings in a Next array remain.

The adapter preserves raw literal/hex bytes, compares and sorts bytewise, and
emits hex without text transcoding. Before merging, destination structures are
detached from both catalogs and context caches to prevent upstream name collisions
or tree loss. One traversal maps the final page references, then balanced name
trees and the legacy Dests dictionary are rebuilt.

## Forms, fitting and catalog

### Forms

Multiple nonempty forms are permitted only when their resolved `/DR`, form-level
`/DA` and `/Q` representations match. Upstream does not generally merge multiple
form default-resource dictionaries. The Fields array determines nonemptiness.
An empty form is replaced by a later nonempty form, including its resources.

`check` predicts possible field namespacing. `build` reads actual names from the
merged parent chains and reports the mappings.

### Output version and annotation appearances

The effective output version is the maximum of PDF 1.7 and every source version.
Both preflight and assembly apply the same form and fitting compatibility rules.

Individually valid sources can become incompatible when combined into PDF 2.0
output: a retained annotation may lack a required appearance. In that case,
`check` and `build` refuse with `source_output_incompatible`, identifying both the
affected source and the source requiring PDF 2.0. Provide complete appearances
or assemble the affected source separately.

An invalid original source can fail whole-source validation earlier as
`source_invalid`. The upstream checker retains its Link/Popup/Projection and
both-zero-size rectangle exceptions. `check` remains predictive: other merge or
serialization failures can still produce `assembly_failed`;
`output_write_failed` is reserved for actual destination I/O failures.

### Fitting

Fitting scales pages proportionally to the chosen paper size, enforces its
orientation and applies no extra content rotation. Sources with form widgets
are refused. For sources with numeric local navigation targets, use original
page sizes instead.

The destination restrictions are:

| Destination | Fitting behavior |
| --- | --- |
| Explicit numeric position coordinates: XYZ left/top, FitH/FitBH top, FitV/FitBV left, or FitR rectangle coordinates | Refuse `fit_destination_unsupported` |
| Whole-page Fit/FitB, null-only position coordinates, or zoom-only XYZ | Supported |
| Remote destinations, removed catalog actions, or omitted out-of-tree named definitions | Do not trigger the coordinate guard |

The coordinate guard covers retained named definitions, links, outlines and
chained local GoTo actions. Print-box, bleed and imposition fidelity beyond these
limits is not guaranteed.

Fitting uses upstream proportional resize. Resized pages have UserUnit 1 so the
target dimensions describe physical sheets. Nonzero output box origins are
permitted; target side lengths are the contract. Explicit generated canvases
retain authored layout and overflow checks before resizing; inherited canvases
use the fit target.

### Catalog and metadata

Document Info and catalog-level XMP metadata are replaced or removed. Metadata
attached to retained pages or other retained objects may remain. These operations
do not sanitize the document; see [SECURITY](../SECURITY.md).

The output catalog permits Type, Pages, AcroForm, Dests, Outlines, Version and
Names containing only Dests. XFA is removed. Cleanup covers catalog entries and
internal caches so writing cannot recreate removed name trees. Effective version
fields are synchronized before validation and writing: output is at least PDF
1.7, or PDF 2.0 if any source is 2.0.

## Text and pruning

Text is shaped and measured with the original static TrueType program. ToUnicode
and ActualText preserve text extraction for glyph aliases and clusters. See
[PLAN](PLAN.md) for script, wrapping, placement and overflow rules.

### Font requirements

PDFConcat refuses fonts with restricted or bitmap-only embedding flags. Fonts
with a no-subsetting flag retain their original program. Empty text still
validates its selected font without embedding an unused font.

Fonts selected for generated text must provide the metadata consumed by the
parser:

| Table | Required metadata |
| --- | --- |
| `post` | 32-byte common header; format 1, 2, 2.5 or 3 |
| `OS/2` | Version 0–5; 64-byte consumed prefix |

Missing, short or unknown metadata is `font_invalid`; embedding rights are
enforced separately. The signed 16.16 `post` angle and nonzero fixed-pitch value
are independent of style. Italic style uses `fsSelection` bit 0 or, for `OS/2`
version 4 or later, oblique bit 9; names, angle and `head` flags are not style
fallbacks. Original traits feed shared native font descriptors for full/pruned
programs and body/contents/footer resources.

### Glyph pruning

Glyph pruning keeps IDs, metrics, glyph count, used outlines and composite
components. It rebuilds glyf/loca and checksums, reduces post to format 3, and
drops layout/cmap tables after shaping. Stable IDs allow Identity CID-to-GID
mapping without a renumbering layer.

The packet-wide glyph union produces identical font programs, widths and
ToUnicode in generated items so upstream can deduplicate them. Counted pages
within one item share content/resources and use a balanced page tree.

### Font names

The canonical-name safety policy also applies to generated fonts: a PostScript
name containing a literal hash followed by two hexadecimal digits is refused as
`font_invalid` by both `check` and `build`, before publication. Select a font with
a different PostScript name. Ordinary literal hashes remain literal because PDF
name syntax is escaped when writing.

Prepared fonts explicitly record whether they contain a subset. Before writing
any generated item, the PDF adapter reserves six-uppercase-letter subset
prefixes found in imported BaseFont and descriptor FontName entries and tag-like
names of generated full fonts. It then allocates unused prefixes deterministically
in sorted font-byte identity order. Generated allocation avoids adding
imported-tag collisions; it does not repair naming defects between sources.

Each pruned font uses one `AAAAAA+PostScriptName`-style name throughout the packet;
its Type0 wrapper, CIDFont and descriptor agree. Full programs receive no added
prefix and retain their original sanitized PostScript name and exact bytes,
including names that already resemble subset names. TrueType name tables remain
unchanged.

## Parser and resource limits

### Parsing and numeric precision

Successful parsing or validation does not establish that arbitrary malformed
input retains its meaning. Upstream relaxed parsing can accept malformed object
suffixes, duplicate decoded keys including null omissions, malformed, overflowing
or underflowing numeric tokens, and normalized malformed free-head entries.
Some legal comment-separated indirect-reference spellings are unsupported and
return `source_invalid`.

Finite PDF real numbers are serialized by upstream to at most twelve fractional
digits; bitwise numeric round trips are not promised. Upstream owns content-stream
parsing and fitting may rewrite those streams. Document-level metadata is
replaced or removed as described under [catalog and metadata](#catalog-and-metadata);
this does not cover all metadata attached to retained pages or other objects.

### Memory and cancellation

Assembly is in memory. Imported object graphs, decoded streams, repeated source
occurrences and generated pages can require much more memory than compressed
input sizes. Upstream defaults limit individual and cumulative stream decoding
and some object-graph operations; sufficiently large or pathological inputs may
be refused.

These safeguards do not provide a universal time or memory bound or guarantee
interruption of every inner decoding loop. Ordinary panic recovery does not
cover memory exhaustion or fatal stack overflow. Use operating-system resource
limits for unusually large or untrusted inputs. [SECURITY](../SECURITY.md) defines
the trusted-input boundary; [CONTRIBUTING](../CONTRIBUTING.md#scale) describes
optional measurements.

