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
| No recognizable PDF header | Refuse `source_not_pdf`; scanning follows the pinned parser, including displaced headers |
| Initial absent, denied or nonregular input | Invalid `source_input_invalid`; unexpected read errors remain failed |
| Source changes/disappears after inspection | Failed `source_changed` against the bytes/state actually used |
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
| Tagged structure | Preserve supported source semantics by default; explicit `structure: discard` removes claims and warns |
| Source page labels and output intents | Remove with warnings; authored numbering generates output labels; no PDF/A/PDF/X claim |
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

### Narrow classic-xref input tolerance

Original sources may contain an in-use classic xref entry for a positive object
number whose literal offset is zero. The modified strict reader accepts it only
when no original effective live object or effective trailer references that
object number, including references with another generation. It retains a
revision tombstone so an older definition cannot return. Each admitted entry
produces `source_xref_entry_ignored` with original xref/object evidence and
consuming items. Original bytes and hashes are unchanged; assembly repeats the
gate after rereading and checking the digest.

An exceptional hybrid/xref-stream chain is refused because the original-reference
proof is limited to exclusively classic revisions. Valid xref-stream and hybrid
inputs are supported. Other syntax, encryption, signature, content,
structure, resource and cancellation safeguards remain. Serialized output uses
ordinary strict reading with the exception disabled. This bounded exception does
not establish universal producer compatibility.
The source and provenance of the local reader are in
[`third_party/pdfcpu`](../third_party/pdfcpu/PROVENANCE.md).


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
Optional page-number overlay geometry is measured once for the union of selected
original pages, rather than reparsing each occurrence. Building independently
rereads and digest-checks every occurrence.

### Assembly details

Selected pages retain their effective inherited resources, page boxes and
rotation. Links, actions, named destinations and outlines are pruned within each
occurrence. An omitted parent outline target may retain a nonclickable container
for surviving descendants. Outline styles, actions and open/closed state survive
grafting beneath the nearest authored section; the composed hierarchy must fit
the supported outline limit.

Generated pages and page-number overlays share font resources. Repeated
generated pages may share content and resources while retaining separate
semantic identities. See [architecture](ARCHITECTURE.md) for construction and
resource ownership.

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
or tree loss. Original definition/binding sites are retained separately from mutable
objects. Equal legacy and name-tree declarations keep both sites. Selection
records removed bindings before later fitting/validation gates; output object
renumbering cannot overwrite those source addresses. Warning production follows
retained routes (including independent Next siblings), without fetching targets.
One traversal maps the final page references, then balanced name
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

Fitting compares each page's ordered displayed physical size to the fixed target.
Within .01 pt per side, the page keeps its actual geometry: no fit Form, box or
coordinate rewrite, rotation baking, UserUnit deletion, or annotation/structure
fitting. The interval endpoints use deterministic float64 rounding. Two-decimal
A4 keeps; integer 595 × 842 pt A4 transforms. A kept CropBox may retain a larger
MediaBox: the promise concerns displayed frames, not exact printer sheets.

Other pages scale proportionally into zero-origin target MediaBox/CropBox with
zero rotation and effective UserUnit one. A finite usable affine with supported placement accuracy is required before
Check succeeds. The same affine handles content, annotations and structure.
Static widgets may remain on proved kept owners while unrelated pages transform.
Transformed or unresolved/conflicting widget owners refuse. Partial-selection and
all general form, security, source and PDF2 compatibility rules still apply.

Before mutation, multiply bound annotation dictionaries refuse when any binding
transforms, including duplicate bindings on one page. All-keep sharing remains
subject to existing source policy. Ordinary shared appearance streams/resources
remain supported. This guard does not clone partial annotation graphs.

Transformed pages omit only their own present non-null BleedBox, TrimBox and
ArtBox references, with separate original-page/entry warnings describing expected
default printing bounds. Shared rectangle objects and kept entries are untouched;
original validation precedes omission. Production-print/imposition fidelity remains
unsupported. Reports expose actual source and expected output dimensions even on
later refusal; `output_size` does not mean that publication occurred.

The destination restrictions are:

| Destination | Fitting behavior |
| --- | --- |
| Explicit numeric position coordinates: XYZ left/top, FitH/FitBH top, FitV/FitBV left, or FitR rectangle coordinates | Refuse `fit_destination_unsupported` only when the retained TARGET page transforms |
| Whole-page Fit/FitB, null-only position coordinates, or zoom-only XYZ | Supported |
| Remote destinations, removed catalog actions, or omitted out-of-tree named definitions | Do not trigger the coordinate guard |

The coordinate guard covers retained named definitions, including unused ones,
legacy names, direct/indirect targets, links, outlines and chained local GoTo
actions. It follows target disposition, never the originating link page. Numeric
tails targeting kept pages remain unchanged apart from page-reference rebinding. Print-box, bleed and imposition fidelity beyond these
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

`fit_annotation_unsupported` refuses these specific TRANSFORMED selected-page cases:

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
Widgets use field flags; fitting requires proved kept widget ownership. ReadOnly does not
waive appearance requirements or clipping. Page-lifecycle actions alone do not
create an interactive area. Appearance clipping still applies to printed artwork. Supply reviewed normal appearances for the
appearance-related cases, or use original page sizes; crossing interactive regions
require original sizes. Ordinary contained uncaptioned Line, Ink, Polygon, PolyLine, Square,
and Circle annotations remain supported without appearances. Text icons, Carets and captioned Lines can still be composed at original page
sizes without appearances.
The guards apply equally in `check` and `build`, before output staging.

Transformed pages have UserUnit 1 and zero-origin target MediaBox/CropBox.
Kept pages retain their native origins and actual displayed dimensions.
Explicit generated canvases retain authored layout and overflow checks before
fitting; inherited canvases use the fit target.

#### Forward fitting accuracy

Finite coefficients and correctly sized target boxes alone do not establish
paint accuracy. Before Check can accept a transformation, the PDF adapter bounds
its represented affine's discrepancy from centered placement and floating-point
evaluation error over the entire visible source rectangle. The supported budget
is 64 binary64 ULPs at each requested target-axis extent, retaining all but six low
significand bits of target-coordinate precision. This allows ordinary arithmetic
rounding, not bit-exact arbitrary affine evaluation.

The uniform dot-product bound includes both multiplications and additions and
uses maximum absolute coordinates throughout the frame. A corner-only check
would miss interior cancellation. Subnormal-operation terms are included; an
unused determinant or inverse is not required. Ill-conditioned mappings that
cannot meet the bound refuse as `fit_geometry_unsupported` before publication,
even when the source is blank. Large-origin pages may therefore keep successfully
but require reviewed rebasing before fitting. Normal nonzero-origin fitting and
finite extreme scales remain supported when the forward bound is met.

This is an arithmetic admission boundary, not a source-coordinate limit, paper
match tolerance, rendering repair or verifier slack. Kept pages bypass fitting
entirely. Native tuples and displayed sizes are still verified exactly before
serialization and after strict reopening. The bound addresses the fitting map
within its visible frame; it does not certify arbitrary source operator/resource
semantics or every renderer's implementation.

#### Source graphics composition under fitting

A page-space affine certificate does not certify subsequent source graphics-state
execution. Transformed pages also undergo a bounded q/Q/cm observation before
Check can succeed and again before fitting mutation. The observer tracks intrinsic
source CTM and actual fitted CTM separately, restores both on Q, and certifies
geometry-consuming operations. It preserves source programs rather than rewriting
or rasterizing them. Kept pages do not run this transformation-specific gate.

At consumption, exact fixed-size rational arithmetic compares the represented
concatenated CTM with intrinsic source execution followed by the outer map. The
visible frame is pulled back through the intrinsic CTM to bound all relevant local
coordinates; uniform floating evaluation and propagated source error must fit the
same 64 target-axis ULP budget. A singular or unbounded local domain cannot be
certified. This uses an exact domain calculation, not a floating determinant or
inverse representability proxy. Conservative refusals are possible.

Invoked Forms are checked per incoming state, including their own Matrix and
implicit clip. An object reference is not a reusable global certificate. Tiling
and shading pattern matrices, retained appearance N/R/D states, soft-mask group
Forms and used font matrices/Type3 programs are also observed at their matrix
boundaries. Shared AP placement comes from the same mapping owner as wrapping.
Used Type3 fonts are conservatively checked across retained CharProcs rather than
adding a second glyph decoder. Missing or otherwise malformed resources remain
subject to existing source policy; this is not a universal PDF resource validator.

The observer uses the existing strict operand/inline-image parsing and decoded
stream limits. It bounds operands, graphics/recursive depth and repeated invocation
work, checks cancellation, and preserves original page/object/operator attribution.
A valid source may keep successfully while refusing fitting when its composition
accuracy cannot be established. Large cancelling translations that erase a fitting
offset are one example, not a special-case magnitude rule. Exact native geometry
checks and independent qpdf/Poppler fidelity controls remain separate safeguards.

#### Effective resources and execution roles

The fitting observer binds selected font objects at both Tf and ExtGState Font,
including size, and selected pattern objects at scn/SCN. q/Q and Form entry preserve
those bindings; a child dictionary's same-name alias cannot replace an inherited
selection. Pattern bindings retain the complete supported graphics state at entry to their
parent stream; pattern execution installs that state before its own matrix.
Type3 glyph resource fallback uses the page resource dictionary when the font has
no Resources entry. Names serve provenance and selection, not late paint lookup.

Resource execution follows paint channels. S/s consumes stroke, f/F/f* consumes
fill, combined path paints consume both, and sh does not consume selected colour
patterns. Non-Type3 text uses its rendering mode; invisible text consumes neither.
Type3 programs execute their own paint operators (except invisible mode), so a
parent pattern is not scanned merely because text is shown if the glyph replaces
that colour before painting. Colour resets and q/Q restore each channel.

Hidden annotations do not execute appearance-composition certification; independent
source, encoding, security and PDF2 checks remain. Later visible annotations are
still checked. FontMatrix belongs to the Type3 font dictionary in this observer;
a private same-name key in a Type1 font is not an executable transform. Likewise,
a Type3 CharProc is a glyph program, not a Form: private Matrix/Resources entries
in its stream dictionary do not acquire Form meanings. Form, appearance, pattern
and soft-mask matrix/clip roles retain their own checks. Shared parsing and work
budgets do not merge these distinct PDF contracts.

These role and state rules follow PDF32000-1:2008 sections8.4,8.7,8.10,9.3 and9.6;
see [the Adobe-hosted standard](https://opensource.adobe.com/dc-acrobat-sdk-docs/standards/pdfstandards/pdf/PDF32000_2008.pdf).

#### Text execution context

Type3 rendering certification composes FontMatrix, text scale/rise, current Tm
and the graphics CTM at each shown character. BT/ET owns the text/line matrices;
q/Q saves text parameters but does not restore those matrices. Td/TD, T*, quotes,
TJ adjustments and simple Type3 widths/spacing advance the observed cursor.
Raw character bytes remain font codes rather than being decoded as Unicode.

The observer does not duplicate other font interpreters. After their strings the
cursor is unknown; Tm or a known line position can establish it again. A later
visible Type3 show with an unknown cursor refuses fitting, rather than assuming
identity. Invisible Type3 text does not execute CharProcs but still advances the
cursor. Retained CharProcs are conservatively inspected at each known cursor.
This bounded observation does not change kept-page execution or source bytes.

Type3 codes outside FirstChar..LastChar have known zero glyph width, as defined
by PDF32000-1:2008 Table112. Character/word spacing and horizontal scale still
affect their cursor advance. This default requires readable, consistent font
metrics; missing entries within the declared interval and nonfinite values do
not become zero. Repeated metrics inspection shares the invocation work budget.

### Catalog and metadata

Source document identity is replaced by authored XMP, synchronized Info for PDF
1.7, and no Info for PDF 2.0. Dates are absent unless explicitly supplied;
[PLAN](PLAN.md#document-identity-time-and-structure) defines the contract. Metadata
attached to retained pages or other retained objects may remain. These operations
do not sanitize the document; see [SECURITY](../SECURITY.md).

The output catalog permits Type, Pages, AcroForm, Dests, Outlines, Version,
authored Metadata/Lang/ViewerPreferences/PageLabels, preserved
StructTreeRoot/MarkInfo, and Names containing only Dests. XFA is removed. Cleanup
covers catalog entries and internal caches so writing cannot recreate removed
name trees. Effective version fields are synchronized before validation and
writing: output is at least PDF 1.7, or PDF 2.0 if any source is 2.0.

### Output verification

The staged, serialized PDF is reopened with strict validation and without source
normalization. Its catalog and name-tree policy is checked before semantic
validation can change the document. Encryption, forbidden catalog entries, XFA,
broken page trees, reported repairs and changes to page/content identity are
rejected. Page count must match the resolved packet before publication. Reopened
metadata, timestamp, viewer preference, numbering spans, structure policy and
coverage must also match resolved expectations. This check is independent of
source admission policy, but uses the same PDF engine; the test suite also uses
qpdf and Poppler for independent structural and rendering evidence. Strict
validation is not a complete PDF conformance or arbitrary-input fidelity proof.

## Text and pruning

Text is shaped and measured with the original static OpenType program. ToUnicode
and ActualText preserve text extraction for glyph aliases and clusters. See
[PLAN](PLAN.md) for script, wrapping, placement and overflow rules.

### Font requirements

PDFConcat refuses fonts with restricted or bitmap-only embedding flags. TrueType
fonts are pruned when their permissions allow subsetting; a no-subsetting flag
requires embedding the original program in full. CFF1 fonts always retain every
glyph and are converted to explicit identity CIDs using go-sfnt. Empty text still
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
programs and body/contents/page-number resources.

### Glyph pruning

TrueType glyph pruning keeps IDs, metrics, glyph count, used outlines and composite
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
prefix and retain their original sanitized PostScript name. Full TrueType
programs retain exact bytes, including names that already resemble subset names.
TrueType name tables remain unchanged.

### Structure ownership

Original page/content references and all-live-object signature evidence are checked
before structure policy is applied. Explicit discard detaches structure/MarkInfo,
ownership keys and cached tagging state before backend semantic validation.
Only tag-exclusive objects unreachable from retained catalog/Info/page/resource/
Form/annotation graphs are removed. Shared artwork, text streams, navigation and
form protections remain intact.

With no effective StructTreeRoot, orphan MarkInfo/StructParent(s) and unbound
MCIDs do not supply preserved semantics. Located warnings explain their removal
or non-use. Untagged meaningful paint and annotations remain uncovered; blanks
and valid artifact-only content remain neutral. Form MCIDs always belong to the
Form's namespace, never the invoking page's; an enclosing semantic scope can
still cover the Form's artwork as a whole. This remains true when an untagged
source is combined with generated/tagged content and the output has a root.

A preserved declared hierarchy is validated before selection. Whole stale scalar
ParentTree keys may be removed only when no source object claims them and their
targets are valid retained elements, after all live forward/reverse ownership,
IDs, references and header bindings pass. An absent IDTree may be rebuilt from
unique validated forward IDs. Both corrections carry typed object/path warnings.
Live-key unused slots, duplicate claims/IDs, dangling/cyclic/conflicting indexes
and missing owners are not repaired. Reopened output uses strict integrity
checks without this source normalization; indexes are rebuilt for the output.

The adapter validates source parent/child links, ParentTree ownership, IDs,
role-map cycles and marked-content references before selection. Content-array
streams keep separate token boundaries and shared marked-content state. Repeated
sources get isolated graphs, role/class maps and IDs; table-header ID consumers
are renamed with the IDTree. Table `Headers` bindings, including applied class
attributes and indirect byte-string IDs, must resolve to retained standard `TH`
elements in original and reopened output. Selection that omits a referenced
header is refused before staging: include its pages, select the whole source,
or explicitly discard structure. Header associations are never silently removed
or repaired by importing omitted content. Selection prunes omitted semantic content.

On transformed pages, fitting moves page-owned MCIDs into the new Form namespace, preserves nested
Form namespaces and annotation OBJR ownership, and transforms scoped BBox
attributes, including class attributes. A bounding box without a resolvable page
scope refuses when a transformation needs it; use original sizes or a reviewed source with
explicit page association. Final indexes are rebuilt from retained semantic
ownership. Generated meaningful text is tagged, and contents links receive
Link/OBJR associations. Each title/label pair is emitted contiguously in physical
and logical reading order. Source languages belong on imported subtree
boundaries. No PDF/UA identifier is inherited.

Inline-image payloads remain opaque. Structure inspection accepts a unique
lexically valid EI boundary, with bounded lookahead, and refuses ambiguous
boundaries instead of guessing. Structure names, arrays and mapping targets are
resolved before occurrence remapping; reopened output must retain their
role/class bindings. Coverage distinguishes uncovered content from absent
semantic content, so blank and artifact-only inputs do not dilute a covered
packet or raise loss warnings. See
[PLAN](PLAN.md#document-identity-time-and-structure) for coverage.

Machine checks, text extraction and a successful build do not certify
accessibility. Review rendered pages, reading order, meaningful text and link
descriptions in the intended accessibility consumer. Structure-text extraction
can omit content in nested ActualText spans; use several observations rather
than treating one extractor as a complete accessibility oracle. Contributor
[machine PDF checks](../CONTRIBUTING.md#what-we-test-and-what-we-do-not) complement
this review; they do not establish spoken output, pronunciation or PDF/UA
certification.

### Deterministic serialization

The [plan contract](PLAN.md#document-identity-time-and-structure) defines which
inputs produce identical PDF bytes. Output contains only reachable objects and
omits optional unencrypted trailer IDs. Retained encoded streams are preserved;
fitting and overlays create their own content. See
[architecture](ARCHITECTURE.md) for object construction and serialization.

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

Decoded finite float64 model reals serialize as non-exponent round-trip decimals,
with a decimal marker for nonzero integral-valued reals and canonical numeric zero.
Model scalars and content operands use the same real emitter. Nonfinite output
scalars refuse. Real syntax is parsed before native integer conversion; oversized
integer tokens and out-of-range real values refuse rather than becoming zero or
rounded substitutes. This does not recover
arbitrary decimal precision already lost during parsing. Exact normalized native
MediaBox/CropBox, rotation, UserUnit and displayed sizes are verified before writing
and after strict reopening; the fitting match tolerance is not a verifier epsilon. PDFConcat inspects content
operators and may rebuild page streams for fitting. Document-level metadata is
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

