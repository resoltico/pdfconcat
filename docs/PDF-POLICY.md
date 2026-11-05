# PDF policy

PDFConcat composes trusted local PDFs in memory using pdfcpu. Source policy is
checked before layout and again where it depends on the final packet. The trust
and filesystem boundary is in [SECURITY](../SECURITY.md).

## Source policy

| Source feature | Behavior |
| --- | --- |
| Encryption, including empty passwords | Refuse `source_encrypted`, including recognized password errors before a context is returned |
| Signature values, catalog permission structures or document timestamps | Refuse `source_signed`; signatures are not authenticated |
| Empty unsigned signature fields, including null values | Keep |
| `NeedsRendering true` | Refuse `source_dynamic_form` |
| `OCProperties` | Refuse `source_optional_content` |
| Broken page tree or missing/malformed page contents | Refuse `source_damaged` before semantic validation |
| Parse/validation failure | Refuse `source_invalid`, with backend detail and object attribution when available |
| Unsupported structure or fidelity boundary | Refuse `source_unsupported` |
| Unexpected backend failure, including recovered panics | Fail `source_processing_failed`; this does not establish source invalidity |
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
| Catalog `PageLayout`, `PageMode`, `ViewerPreferences` | Remove before semantic validation, with `source_viewer_settings_removed` per entry |
| Document Info and catalog-level XMP | Replace/remove; metadata attached to retained pages or other objects may remain |
| Page/annotation actions and scripts | Keep; PDFConcat does not execute them or verify network targets |
| Catalog URI Base | Remove and warn that retained relative actions may resolve differently |

### Admission and viewer settings

Sources are parsed offline in strict mode and checked against whole-source
security and structural rules. PDFConcat explicitly removes the three viewer
entries above before pdfcpu's relaxed semantic validation. Original source bytes
and hashes remain unchanged.
Inspection and assembly use this same admission path; assembly rereads and checks
each original digest before admission. Findings already collected survive a later
validation failure.

Source viewer directives do not govern the composed document. Their catalog
entries are removed whether their values are valid, invalid, direct or indirect;
PDF null is equivalent to absence. Only the catalog references are removed, so
objects shared with retained content remain intact. This includes the nonstandard
`/PageLayout /PDLayoutDontCare`. Removal does not excuse parse failures or
whole-source refusals, including signatures in otherwise discarded objects.

Other discarded structures are validated according to their feature-specific
rules. Removing tags, output intents or actions has different consequences from
removing viewer settings; there is no general repair or sanitization mode.

Encrypted sources are refused even when an empty user password makes them
readable. Readability does not establish assembly permission. PDFConcat does not
override restrictions or implement an encrypted-output policy. Request an
unrestricted, unencrypted assembly copy from the supplier, or deliver originals
separately with a guide. Preparing a derivative with another tool requires its
own review; successful opening alone does not establish fidelity.

### Validation evidence and page preservation

Every source page, including omitted pages, must have finite positive displayed
physical dimensions. Declared MediaBox/CropBox rectangles accept either order of
opposite corners and are normalized before intersection. An empty or nonfinite
effective intersection, invalid UserUnit or overflowing physical dimension is
`source_invalid`, with the affected page/object under `Pages.EffectiveGeometry`.
This whole-source requirement applies equally with and without fitting.

Before semantic validation, each page's content must resolve to an existing
stream or an array of existing stream references. Page-tree and content-stream
references must match live object generations; missing/free objects, dangling
references, null array members and non-stream content are refused. Absent/null
Contents is a valid blank page, and an empty array contains no content to retain.

Source admission, final in-memory assembly validation and serialized output
verification record page order, content-stream reference order and hashes of
encoded stream bytes before validation, then check them again afterward. Repeated
or shared streams retain their multiplicity and order. This bounded check does
not compare all resources, forms, metadata, decoded caches or rendered pixels;
those representations may be normalized by the backend. It does not establish
semantic equivalence of arbitrary PDF objects.

Backend notices are preserved as `source_validation_notice` evidence, with phase
and disposition in `detail` and available PDF locations. Notice wording never
selects admission policy. A skipped validation divergence does not establish that
PDF content was removed: retained scripts can produce such notices. Explicitly
reported repairs are refused as `source_unsupported`. Notices are not a complete
repair log; the page/content checks operate independently of them.

### Page-content encodings

Before occurrence selection, every original page-content stream must decode,
including streams on omitted pages and every member of an array. Failures are
`source_invalid`, or `source_unsupported` for a recognized unsupported encoding,
with original source page/stream attribution. An unknown filter name is invalid
input; backend error wording does not choose its classification.

Generic byte filters use pdfcpu's decoder and predictor handling, one stage at a
time on the same source context. Each Flate stage also passes an independent
standard-library zlib read-to-EOF/checksum check; pdfcpu's deliberate truncated-
Flate recovery is not accepted for page programs. An initial Identity Crypt
filter, including its default Name, changes no bytes and remains supported;
encrypted files are still refused. Image-only filters are not treated as decoded
page commands. This check does not decode every image XObject payload.

Shared streams decode once per admission, while each array occurrence counts
toward its page's combined-content limit. Unfiltered bytes are bounded too.
Intermediate stage buffers conservatively count toward the existing cumulative
decoder allowance; no budget is reset for filter prefixes. PNG predictor framing
is checked without counting its per-row control byte as decoded page content.
Validated final caches are retained for backend reuse. Raw bytes, filter
declarations and dictionaries stay unchanged.

The staged serialized output receives the same encoding check before publication.
This establishes bounded encoding decodability and strict Flate integrity, not
complete content-operator grammar, resource validity, PDF conformance or universal
preflight prediction. Merge/optimization or later backend operations can still fail.

### Structural rejection rules

The page tree must have nonempty dictionaries, valid Page/Pages types and an
exact leaf count. A Page cannot have effective non-null Kids, including an empty
array. Missing, repeated or cyclic nodes are refused. The raw page-tree check
runs before validation; page, field and destination walks have depth bounds and
inspect effective indirect values. Field and outline walks also check reference
generations before dereferencing.

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

Fitting interprets annotation Rect and appearance BBox opposite corners in
either order, preserving their displayed orientation. It uses the effective source frame: CropBox intersected with MediaBox,
or MediaBox when CropBox is absent, including inherited boxes. That same frame
determines the scale, placement and clipping. Source-invisible page artwork
stays clipped in the new margins. Content remains vector/text inside a Form
XObject; hidden objects may remain in the file. This is not redaction. Staged
output verification checks Form stream encodings as well as page Contents.

Pattern scale, phase and orientation follow the source artwork. Original pattern
matrices and shared/nested resources are retained in their own coordinate spaces.
Annotation rectangles, QuadPoints, Line endpoints, Ink strokes, Polygon/PolyLine
vertices and FreeText callouts follow their displayed transform. Square, Circle,
Caret and FreeText RD insets are distances; their sides follow baked rotation. Border
widths/dashes and Line leader/caption offsets scale proportionally.

Normal, rollover and down appearances, including named states, retain their
mapping and orientation when page rotation is baked. Existing appearance streams
and their resources remain shared safely; wrappers preserve source clipping and
page-resource fallback. NoRotate (implicit for Text annotations) pivots about
the original upper-left corner; NoZoom remains a viewer magnification rule.
FreeText callouts and insets transform, but its default/rich-text style strings
remain authored. Subsequent appearance regeneration after editing is viewer-dependent.

`fit_annotation_unsupported` refuses these specific selected-page cases:

- FreeText without a usable normal appearance, whose text layout cannot be preserved by coordinate changes alone.
- A Text icon without a usable normal appearance, whose supplied Rect need not bound its viewer-generated icon and therefore cannot preserve source clipping.
- A Caret without a usable normal appearance, whose viewer-generated symbol cannot be preserved by rectangle and inset changes alone.
- A captioned Line (`Cap: true`) without a usable normal appearance, whose viewer-generated caption text layout cannot be preserved by coordinate changes alone.
- A rotated NoRotate annotation (including Text) without a usable normal appearance, whose viewer-generated bounds/pivot cannot be preserved reliably.
- An annotation without a usable normal appearance whose displayed rectangle crosses the source frame; page-content clipping cannot clip a viewer-generated appearance.
- An interactive annotation hit area crossing the source frame, including annotations with appearances; fitting must not activate a previously clipped region in a margin.

A usable normal appearance is an N stream or the stream selected by AS in an N
state dictionary. Hidden annotations do not trigger these guards. FreeText with
a zero-width or zero-height Rect has no display area and is retained without a
normal appearance. This exemption does not apply to Text icons or path annotations.

Interactive regions include Links, activation actions (A), and annotation AA
pointer/focus triggers E, X, D, U, Fo and Bl. NoView annotations have no on-screen
hit area. ReadOnly also disables user interaction for non-widget annotations;
Widgets use field flags and remain unsupported for fitting. ReadOnly does not
waive appearance requirements or clipping. Page-lifecycle actions alone do not
create an interactive area. Appearance clipping still applies to printed artwork. Supply reviewed normal appearances for the
appearance-related cases, or use original page sizes; crossing interactive regions
require original sizes. Ordinary contained uncaptioned Line, Ink, Polygon, PolyLine, Square,
and Circle annotations remain supported without appearances. Text icons, Carets and captioned Lines can still be composed at original page
sizes without appearances.
The guards apply equally in `check` and `build`, before output staging.

Fitted pages have UserUnit 1 so target dimensions describe physical sheets.
Nonzero output box origins are permitted; target side lengths are the contract.
Explicit generated canvases retain authored layout and overflow checks before
fitting; inherited canvases use the fit target.

### Catalog and metadata

Document Info and catalog-level XMP metadata are replaced or removed. Metadata
attached to retained pages or other retained objects may remain. These operations
do not sanitize the document; see [SECURITY](../SECURITY.md).

The output catalog permits Type, Pages, AcroForm, Dests, Outlines, Version and
Names containing only Dests. XFA is removed. Cleanup covers catalog entries and
internal caches so writing cannot recreate removed name trees. Effective version
fields are synchronized before validation and writing: output is at least PDF
1.7, or PDF 2.0 if any source is 2.0.

### Output verification

The staged, serialized PDF is reopened with strict validation and without source
normalization. Its catalog and name-tree policy is checked before semantic
validation can change the document. Encryption, forbidden catalog entries, XFA,
broken page trees, reported repairs and changes to page/content identity are
rejected. Page count
must match the resolved packet before publication. This check is independent of
source admission policy, but uses the same PDF engine; the test suite also uses
qpdf and Poppler for independent structural and rendering evidence. Strict
validation is not a complete PDF conformance or arbitrary-input fidelity proof.

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

Strict object reading rejects covered syntax defects, including duplicate keys
and non-name dictionary keys. It does not certify complete PDF conformance or
prove that no lower-level reader recovery occurred; malformed cross-reference
bookkeeping can still be normalized. Compatibility validation remains relaxed,
so ancillary producer differences do not become blanket source refusals. Some
legal comment-separated indirect-reference spellings remain unsupported and
return `source_invalid`. Successful parsing or validation does not establish that
arbitrary malformed input retains its meaning. Content-operator resource binding
is not exhaustively validated: a malformed source using an undefined font can be
accepted and render differently across readers or fitting. Supply a reviewed
valid source; composition does not reconstruct missing font bindings.

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

