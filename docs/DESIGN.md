# Design and PDF policy

PDFConcat is a sequential in-memory packet assembler on unmodified upstream
pdfcpu. It owns plan/result contracts, generated text, raw source refusals,
per-occurrence destination handling and publication. It does not own another
PDF content parser, appearance engine, worker pool or verification platform.

## Packages and boundaries

| Package | Responsibility |
| --- | --- |
| cmd/pdfconcat | Signals, stdio, build identity and exit code |
| cli | Pure interleaved argv grammar and help |
| plan | Strict JSON v2 domain decoding, locations and editor schema |
| job | Styles, paths, sizes, layering and page-range resolution |
| typeset | Font validation, shaping, placement and stable-GID pruning |
| pdf | The only pdfcpu importer: inspect, generate, fit, merge and verify |
| inputfile | Named regular-file reads and bounded stream reads |
| atomicfile | Sibling staging, native no-replace rename/link and durability |
| result | Result/report v3, schema and capped stdout views |
| pipeline | Staged check/build execution and safe reporting |

Test-only pdffixture generates small PDFs; oracle reads them with qpdf/Poppler.
Stock depguard limits go-text to typeset and x/sys to atomicfile, keeps the pure
internal boundaries, and prevents fixture/oracle imports into product code.

## Strict decoding

The decoder uses jsontext grammar/duplicate checks and json/v2 scalar conversion
with domain-specific dispatch and immediate null/value guards. A full null scan
would mask an earlier unknown/type/value fault. One leading BOM is counted in
original byte locations; a well-formed v1 document gets its migration diagnostic
regardless of member order. Required/empty member and numeric-boundary mappings
use the existing code registry. Standard schema integers cannot enforce lexical
integer tokens, and unit/path/aggregate checks remain decoder responsibilities.
No second general JSON parser or schema vocabulary is introduced.

## Pipeline and input identity

Load a plan/operands; check paths/roles; load every selected font and validate
text policy before reading PDFs; inspect distinct sources in first-use order;
resolve ranges/sizes and form/destination policy; lay out each distinct generated
style/canvas. Check stops there, with an optional report.

Build re-reads each source occurrence and compares SHA-256 with inspected bytes.
It imports each occurrence separately, so links/widgets belong to their own
copy. Readable refusals retain known byte hashes/lengths while unknown pages stay
null. Named inputs are Stat-checked before open, then checked on the descriptor;
regular-file symlinks remain supported. Stdin is the stream exception. Its
closable read runs independently of cancellation, so the outcome does not wait
for a blocked Windows console close/read. The CLI exits after rendering it.

Every part is validated before upstream merge. Finalization rebuilds destinations,
prunes catalog/name caches, synchronizes effective version, optimizes, validates
and writes. Verification reopens the actual temp PDF and checks validation,
exact page count and the serialized catalog policy before publication.

Stages collect their diagnostics, then stop on errors. Interrupted wins over
failed, which wins over invalid. Saved reports are attempted after failed runs
too, but never after argv parse failure. Stdout caps diagnostics/consumers; reports
retain details. See [CLI](CLI.md) for publication facts and safe-report rules.

## PDF policy

| Source feature | Behavior |
| --- | --- |
| Encryption, including empty password | Refuse source_encrypted, including known pre-context password errors |
| Signature values, certification or document timestamp | Refuse source_signed; do not authenticate signatures |
| Empty unsigned signature fields, including null values | Keep |
| NeedsRendering true | Refuse source_dynamic_form |
| OCProperties | Refuse source_optional_content |
| Damaged page tree | Refuse source_damaged before validator repair |
| Parse/validation failure, panic or specific unsafe shape | Refuse source_invalid |
| Content, ordinary annotations and links | Keep through upstream merge, subject to the limits below |
| Static AcroForm fields | Keep only compatible resolved DR/DA/Q representations; otherwise source_form_unsupported |
| Later nonempty forms | Upstream may namespace them even without collisions; build reports actual mappings |
| NeedAppearances true | Keep request and warn that viewers may regenerate appearances |
| XFA alongside static fields | Remove XFA, retain static form, warn |
| Named/legacy destinations | Detach, rename collisions per occurrence and rebuild |
| Destination outside its source page tree | Omit with warning and remove unusable references/actions |
| Source outlines | Preserve, including direct/named/GoTo/Next targets per occurrence |
| Tags, page labels, output intents | Remove with a warning; no tagging/PDF/A/PDF/X claim |
| Catalog OpenAction/AA and JavaScript name tree | Remove with warning |
| Embedded-files index and catalog AF | Remove with warning; page FileAttachment annotations remain |
| Source Info/XMP/viewer preferences/page mode/layout | Replace/remove |
| Page/annotation actions and scripts | Keep; never execute, test network targets or sanitize |
| Catalog URI Base | Remove; warn that retained relative actions may resolve differently |

Raw walks check effective values, including indirect field/action names, flags
and nulls. A Page must be a nonempty dictionary without effective non-null Kids
(even an empty array is refused). Pages nodes need nonempty Kids and an exact
leaf Count; repeated/cyclic/missing nodes are refused. Page, field and destination
tree depth is bounded. Ordinary panic recovery is not an exhaustion sandbox.

## Destination byte identity

Named destinations are byte strings, not display text. Decode raw literal/hex
bytes, preserve them, compare/sort bytewise, and emit hex without text transcoding.
Collisions receive unique ASCII names. Rewrite local Dest and GoTo D references
through annotations, outlines, actions and Next chains; JavaScript references
are not rewritten. Detach both source structures/caches before merge, then map
recorded page indexes in one output-tree traversal and build balanced name trees.
This avoids upstream loss/misdirection and repeated root page lookups.

A key occurring in both the name tree and legacy dictionary with conflicting
resolved targets is refused rather than conflated. An omitted GoTo target also
loses that action's own Next chain; valid sibling entries in a Next array remain.

A real Poppler lookup caught a rebuild defect hidden by decoded qpdf JSON and
MuPDF normalization: UTF-16 tree keys did not match untouched first-use ASCII
link bytes. The corrected adapter preserves both encodings. Tests include byte
keys with identical display text, collision trees, repeated/distinct documents
and actual pdftohtml lookup alongside qpdf page-target checks. The external C++
probe is evidence only, not a build/CI dependency. See [ISO 32000 byte-string
destinations](https://opensource.adobe.com/dc-acrobat-sdk-docs/standards/pdfstandards/pdf/PDF32000_2008.pdf)
and [Poppler's bytestring API note](https://poppler.freedesktop.org/api/glib/PopplerDocument.html).

## Forms, fitting and catalog

Upstream does not generally merge form DR. Comparing resolved resource/default
representations is a conservative boundary that supports repeated/compatible
forms without recreating appearance/resource machinery. Nonemptiness uses Fields,
not the number of extracted nonempty names. Actual per-part mappings are read
from merged parent chains; check only predicts possible namespacing. An empty
form is replaced by the next nonempty form in upstream, including its resources;
qpdf/resource and rendering tests cover conflicting unused defaults.

The backend can require widget appearance dictionaries when writing PDF 2.0. A valid
PDF 1.7 form using viewer-regenerated appearances can therefore fail assembly
when another source raises the output to 2.0. PDFConcat does not generate those
appearances: provide sources with complete appearances or assemble the form
separately. This backend limitation produces `assembly_failed`, not the
`output_write_failed` reserved for actual destination I/O failures. `check` does
not serialize a merged document and may not detect this mixed-version limitation.

Fitting uses upstream proportional resize with forced target orientation and no
extra rotation. Widgets are refused. After resizing, local UserUnit is removed
so the physical sheet has unit 1. qpdf box/annotation geometry and exact Poppler
pixels match independently scaled-coordinate controls with crop, rotation and
mixed units. Upstream may retain a nonzero box origin; target side lengths are
the contract. Destination XYZ/FitH/FitV/FitR coordinates are not transformed.
Print/bleed/imposition fidelity beyond this boundary is not guaranteed.

The final catalog permits Type, Pages, AcroForm, Dests, Outlines, Version and Names
with only its Dests tree. XFA is removed. Forbidden entries are removed from both
catalog and context caches; reopening output verifies they were not recreated.
Effective version caches are synchronized before writing/validation: at least 1.7,
2.0 if any source is 2.0. A real version-selected validation rule is tested.

## Text and pruning

Existing shaping/wrapping/ink measurement and ActualText alias/cluster logic are
retained. See [PLAN](PLAN.md). Static glyf fonts must allow outline embedding;
bitmap-only/restricted permissions are refused, and no-subsetting retains original
bytes. Empty text still validates the effective selected font, without embedding
an unused font. Tiny plain pages work; unusable derived text width is refused.

The stable-GID pruner keeps metrics/glyph count, composite closure and used outline
bytes; rebuilds glyf/loca and checksums; reduces post to format 3; and drops layout
and cmap tables after shaping. Job-wide glyph union supplies identical program,
widths and ToUnicode to every generated item. One item shares content/resources
across counted pages with a balanced tree. Tests cover checksums, extraction,
embedding, deduplication, pixels and short-separator output growth.

The inspected [tdewolff/font Subset API](https://github.com/tdewolff/font/blob/master/sfnt_subset.go)
renumbers supplied glyphs without the required stable-GID pruning option. Passing
all GIDs retains unwanted outlines. The narrow owned pruner avoids adding a
mapping/dependency layer; it never shapes the reduced program.

## Files, tools and delivery

Targets follow umask; replacements retain their mode. Atomic publication records
visibility immediately after rename/link, before cleanup/directory sync. Abort
only removes its own temporary name. Report roles are rechecked after PDF
publication; unsafe reports are invalid refusals, attempted I/O failures are failed.
Unknown plan inputs force report no-replace even with overwrite. No private ACL,
recovery-copy, adversarial namespace-race or network-share guarantee is made.

Tool versions live in tools/go.mod. Build the pinned native linter once, then
run it under each target GOOS. The combined graph selects YAML v4 rc.6 through
gosec while actionlint needs incompatible rc.3; its demonstrated compile failure
is avoided by stock @version isolation, deriving the pin from the same module.
No fork/replace/installer or extra module is added. See [CONTRIBUTING](../CONTRIBUTING.md).

CI has lint, three native tests, a six-target build and Required CI. That context
remains bound to Actions integration 15368; no settings change was made. Snapshots
run only in isolated clones and are inspected. Readiness does not publish release
assets or change @latest; upstream drafts and the owner-controlled tag remain
separate outward actions. Module-only advisory
[GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) concerns unimported OpenPGP;
local govulncheck found zero affected symbols/imported packages.

## Published-baseline and process record

The rebuild follows accepted D1–D10 and WP0–WP10. Obsolete workflow/release config
was removed with its dependencies in WP1 and recreated in WP8. WP5 hosted native
acceptance waited for replacement CI; the single draft PR opened in WP8, and PR #1
was commented on and closed. The first six-job run, including Required CI, passed
at https://github.com/resoltico/pdfconcat/actions/runs/38682084085 . Final exact-head
checks still follow documentation changes.

Compared with published v0.2.0, repeated named-destination correctness and damaged-
tree refusal are preserved protections, not new fixes. Repeated legacy Dests is
new support. Forms, parser strictness, numeric identity and fitting guarantees
narrow as documented; source bookmarks and wider sheet grammar are additive.
Detailed checkpoint logs remain in the owner's external implementation evidence;
they are not runtime or clean-checkout dependencies.

## Preconditions (WP0)

On 2026-10-15, the working tree was clean. The foundation branch and its remote
both identified `56260859095ceac53c4455486f84e2ac3547ad76`; main identified
`a74cd6fbf6ffcb5934e034a54d071d496b5794f6`. Fetch and a second remote observation
confirmed those tips. Neither branch contains commits newer than the bundle's
snapshot, so there are no additional commits to classify. PR #1 remains open.
The latest existing CI run failed:
<https://github.com/resoltico/pdfconcat/actions/runs/38659639579>.

The app's chat inventory showed the previous PDFConcat writer unloaded and the
audit chat idle; this chat is the sole implementation writer. The branch is
`rebuild/upstream-pdfcpu`, based on the foundation tip. All owner decisions
D1–D10 apply: keep the project, plan v2/page, curated lint, umask permissions,
100,000 generated-page limit, remove progress/query/jobs/inline plans, supersede
PR #1, add the contributor-scope pointer, draft upstream issues only, and let the
owner tag the release.

Go 1.27.2 runs on macOS arm64. qpdf and Poppler are installed. The comparison
binary and diagnostic logs live outside the checkout at
`/Users/erst/Tools/pdfconcat-implementation-evidence/`. Owner artifacts `dist/`,
`bin/`, `.tools/`, `pdfconcat`, `plan.test`, and ignored filesystem metadata are
protected. No cleanup command may remove them.

Baseline verification: the old executable was built to the external evidence
directory; `go build ./...`, `go vet ./...`, and `go test ./...` passed on macOS
arm64 (23 package results, including one package without tests). No campaign or
opt-in scale test was requested. This local result does not resolve the existing
hosted CI failure or establish native Linux/Windows behavior.


## Deletion review (WP1)

Reviewed package responsibilities, exported boundaries, relevant tests, workflows
and release hooks against the approved deletion inventory. Git history retains
the removed implementations and regression sources for WP4 individual triage.

| Group removed | Protected property and replacement |
| --- | --- |
| Dependency forks, base archives and patches | Cancellation uses upstream context APIs; small PDF refusals/adapters and WP4 end-to-end regressions preserve relevant fidelity. Fork-specific resource budgets are explicitly retired. Licenses remain for WP9 review. |
| Capture and publish | Inspected/assembled byte identity becomes SHA-256 comparison; regular input checks and SameFile roles remain. Atomic publication retains no-clobber and durability. Native private ACLs, recovery copies, namespace-race and identity bookkeeping guarantees are intentionally removed. |
| Report | Stable outcomes and bounded stdout become result v3 plus a full report. Query language, continuations and retention machinery are removed. Report alias safety and late publication-state truth remain required. |
| App, assembly, plan, layout and CLI | Product contracts move to job, plan, pipeline and CLI. Path layering, origin pointers, first-fault locations, overflow and refusal behavior remain explicit acceptance cases. Worker scheduling and inline plans are removed. |
| PDF engine | Upstream merge/resize replaces the object pool, fitting parser and appearance engine. Named destinations, damage/signature/encryption refusals and catalog policy remain WP4 obligations; form and coordinate limitations require evidence and disclosure. |
| Progress, observation, native progress fixtures and command transport | Progress and its native console/backpressure guarantees are removed by D6. The executable uses ordinary streams and signal context. |
| Repository policy, documentation contract machinery and permission/subprocess helpers | Stock lint/depguard replaces repository policy. Small product documentation checks return in WP9; in-process CLI checks and two binary smoke tests replace subprocess coverage machinery. ACL fault guarantees are removed by D4. |
| Quality tools, mutation registry, installer and scale harness | Stock Go tooling replaces test discovery/supervision. Fuzz seeds remain ordinary tests; campaigns and mutation/coverage gates are removed. WP7 owns one opt-in measured scale case. |
| Workflows, composite actions, scripts, lint and release configuration | Their deleted dependencies make them obsolete. WP8 installs six-job CI, native tool invocation and isolated release snapshots. Required CI keeps its context. |
| Verification/closeout documents | Their verification-platform contracts are obsolete; CONTRIBUTING and delivered-state evidence replace them. Published changelog history remains. |

All generated-page writer tests were moved with the writer, rather than discarded.
All fixture, oracle and typesetting tests remain; the two oracle subprocess helper
calls use exec.CommandContext directly. Package names/imports follow the new
responsibilities. The minimal command intentionally refuses work until WP6.
Upstream pdfcpu v0.16.2 is still the latest stable module (requeried in WP1), so
there is no intervening changelog to reconcile. The initial configuration pins
that dependency through the actual library boundary.


## Known limitations and individual fork-fix triage

The fork regression sources were reviewed from the foundation commit, including
ambiguous parser/free-list/resource cases. The table below assigns every one of
the inventory's 45 behaviors. No dependency test suite or campaign was run.
Short product probes are in the external implementation evidence logs
`fork-triage-observations.log`, `fork-packed-freehead-observations.log`,
`wp4-valid-fork-regressions.log` and `wp4-fidelity-regressions.log`.

**Breaking parser/fidelity boundary:** upstream relaxed parsing may accept
material suffixes, malformed/overflow/underflow numeric tokens, decoded duplicate
keys (including null omissions), and normalize malformed free-head entries.
The small ordinary and packed-object probes demonstrated acceptance; acceptance
is not evidence of fidelity. We no longer promise the fork's strict lexical
refusal contract. Sources must be trusted local documents. Raw page-tree checks,
canonical-name refusals, resource-role refusals and policy checks remain explicit;
PDFConcat is neither a general repair detector nor a sanitizer. Finite PDF reals
are serialized by upstream to at most twelve fractional digits; bitwise numeric
round trips are not promised. Content streams are handled by upstream and fitting
may change them. Valid comment-separated references are accepted where upstream
supports them, or refused with source_invalid (one tested legal spelling is
unsupported). Upstream issue drafts must include these remaining limitations.

| Fork regression | Disposition / evidence |
| --- | --- |
| TestContextDecodedCopyInterruptsBufferedExpansionAndCapsWork | Retire fork-specific buffered-expansion work caps; context API cancellation remains. No hostile decompression guarantee. |
| TestContextCopyNChecksCancellationAtExactReadBoundary | Retire exact inner-read boundary guarantee; cancellation is passed to upstream and checked between parts. |
| TestContextPredictorReadFullChecksExactBoundaryCancellation | Retire predictor-loop checkpoint guarantee under trusted-input scope. |
| TestContextChecksASCIIAndRunLengthMaterializedScans | Retire materialized-scan work checkpoints; no hostile-input CPU budget. |
| TestContextTIFFChecksLargeInnerColorLoops | Retire inner-color-loop budget; no promise to interrupt every decoder iteration. |
| TestContextPaethWorkCounterChecksStridedInnerLoop | Retire Paeth work-count policy; upstream decoder owns this algorithm. |
| TestPredictorExactOutputCapExcludesPNGPrefixAndRejectsNextRowBeforeAppend | Retire fork-specific decoded-byte caps, including PNG predictor accounting. |
| TestCompleteSourceBodiesRejectMaterialSuffixAcrossPackedAndOrdinaryReaders | Short ordinary/packed probes accept suffixes; strict lexical refusal is explicitly retired and needs upstream follow-up. |
| TestCompleteSourceBodiesRetainCommentSeparatedReferencesAndCommentTails | Product regression preserves visible content for the supported split; the second split is source_invalid. Packed probe accepted the comment case. |
| TestPackedDictionaryPolicyMatchesStrictRefusalAndClassifiedRelaxedNotice | Product has one relaxed upstream mode, not the fork's strict/notice pair. Packed malformed suffix probe accepted without diagnostics; disclosed above. |
| TestMaintainedPrefixParserStillReturnsRemainingMaterial | Internal fork parser API has no product consumer after fitting/parser removal; no compatibility wrapper. |
| TestContextTypedEntryResolversRetainActualLazyParserLifetime | Fork-specific lazy-parser lifetime guarantee removed. Product materialization uses upstream context APIs; ordinary cancellation remains tested. |
| TestHexStringIgnoresPDFWhitespaceBetweenNibbles | Repeated-form product regression retains Hello World; qpdf independently reads the field value. |
| TestHexStringLargeCanonicalAndFormattedValues | Upstream handles the valid encoding; malformed/large-input work guarantees are outside scope. Whitespace/odd-nibble product cases retain field values. |
| TestHexLiteralRetainsCallerRemainderAndInvalidSkip | Prefix/remainder API is unused. Valid odd nibble preserves A@ in repeated-form output; malformed hex may follow upstream relaxed policy. |
| TestCanceledHexObjectRetainsContextRefusal | Cancellation now belongs to upstream context APIs, not the removed prefix parser; no per-token deadline guarantee. |
| TestNativeParserAndWriterPreserveCanonicalDictionaryIdentity | Refuse canonical literal-hash-plus-hex keys before validation; private CropBox counterexample reproduced. Ordinary hash control accepted. |
| TestDictionaryDecodedKeyUniquenessBeforeNullOmission | Upstream accepts tested decoded duplicates/null omission; malformed lexical refusal is explicitly retired. No second PDF parser added. |
| TestFiniteRealNativeRoundTripPreservesTypeAndBits | Product test records twelve-fraction-digit serialization and preserved visible text; bit identity is a documented breaking limitation. |
| TestNumericInstructionsNeverBecomeFabricatedZeroOrAbsentValue | Short malformed/overflow/underflow object probes are accepted upstream; no strict lexical guarantee. Raw page counts/physical geometry are checked separately. |
| TestParseObjectRejectsNonPDFNumbers | Same measured upstream policy, individually retired as a strict malformed-token refusal. |
| TestParseObjectBoundsIncompleteArray | Fork parser work/depth budget retired; raw page and field graph bounds remain. Panic recovery does not cover exhaustion/fatal runtime failures. |
| TestReadRejectsRetainedInUseFreeHeadAcrossClassicAndStreamEntries | Short classic in-use head probe was accepted/normalized upstream. Strict original-head rejection is retired; no raw xref parser is introduced. |
| TestReadPreservesKnownClassicFreeHeadNormalizationAndRepairs | Generation/offset/zero-head normalization probes accepted. Ordinary valid output is independently checked by qpdf. |
| TestReadPreservesActualRecycledFreeObjectInsertion | Product regression supplies a genuine free-list slot and rebuilds destinations, then checks qpdf and exact link targets. |
| TestProcessObjectRejectsMalformedNumericTokenStrict | Strict reader mode is no longer a public contract; the measured upstream relaxed numeric boundary applies. |
| TestHistoricalOCSPFixtureRejectsUnsignedTimeChange | Signature authentication is outside product scope; signed/certified/timestamped inputs are refused, unsigned signature fields remain accepted. |
| TestDictionaryCanonicalBytesGovernEveryOperation | Product avoids ambiguous canonical identities through the same pre-validation refusal, including name values. No fork dictionary API is exposed. |
| TestCanonicalNamesPreserveLiteralHashesInSerializationAndDiagnostics | Ambiguous hash-plus-hex shapes are refused rather than double-decoded; ordinary literal hashes remain supported. |
| TestContextFullDecodeBoundsCachedRawAndOpaqueResults | Retire fork-specific decoded/raw/opaque cache budgets; trusted inputs and OS limits are the documented boundary. |
| TestContextFullDecodeBoundsFilterChainAndPreservedTerminalStage | Retire custom filter-chain work/output limits; upstream owns filter decoding. |
| TestContextFullDecodeCancelsBufferedFlateWithoutPublishingContent | Retire buffered-flate inner checkpoint guarantee; product publication occurs only after complete verification. |
| TestContextPartialPrefixCannotExceedConfiguredCap | Removed content parser's bounded-prefix API has no consumer; no equivalent product option. |
| TestInfoPropertiesPreserveCanonicalPrivateNameBytes | Source Info is intentionally removed/replaced by catalog policy. Ambiguous canonical keys are refused before validation rather than promoted. |
| TestResourceValidationPreservesOtherDictionaryRoles | Shared annotation/field resource dictionaries are refused before upstream role pruning. Product regression exercises a shared resource/annotation object. |
| TestBoundedInlineBinaryIsNotTokenized | Removed benoitkugler parser has no consumer; source content/inline image handling belongs to upstream, with independent render acceptance. |
| TestBoundedFlateExpansionAndBoundary | Removed decoder's byte-cap API has no consumer; custom hostile-input decompression budgets are retired. |
| TestBoundedContainerOperandsTokensAndCancellation | Removed token/operand/depth/work configuration is not replaced. Product retains explicit page/field/name graph bounds and API cancellation. |
| TestBoundedNumericOriginalFloat64 | Old fitting analyser's original-number span API is unused. Upstream fitting is the accepted contract; exact numeric identity limitation is disclosed. |
| TestBoundedInlineMaintainedFilterMatrix | Removed inline-parser API is not exposed; valid source rendering uses upstream. No second image tokenizer/decoder is added. |
| TestBoundedInlineAggregateWorkAndRowOverflow | Custom aggregate inline-image budgets retired; trusted-input memory/CPU limits are external. |
| TestASCII85CapturedInvalidPrefixBeforeEOD | Removed skipper is unused. Malformed stream lexical refusal follows upstream, not the former tokenizer's EOD policy. |
| TestASCIIHexRejectsInvalidPrefixAndKeepsOddNibble | Removed skipper is unused; valid odd-nibble semantics have a product form-value regression. Invalid stream prefixes are not given a new sanitizer guarantee. |
| TestBoundedTokenizerConstructorLookaheadAndCancellation | Removed tokenizer constructor has no consumer. No lookahead-token work guarantee is carried forward. |
| TestBoundedTokenizerTokenProgramAndCountBoundaries | Removed tokenizer limits/options are retired rather than recreated around upstream. |

Additional published behaviors outside those 45 names have product regressions
for indirect DecodeParms and valid nonzero object generations. Source bookmarks,
named/direct links and action chains are tested per occurrence. Relative URI
links lose catalog Base with a per-source warning; action bytes remain intact.
Named destinations outside their source are omitted and dangling actions/links
are removed. Fitted destination coordinates are not transformed: page targets
remain correct, but scroll positions can be wrong. Form support is the narrow
resource/default-equivalence boundary above, with no owned appearance engine.

The trusted-input boundary does not guarantee recovery from memory exhaustion,
fatal stack overflow, every hostile decoder loop or concurrent namespace swaps.
It does not turn a successful validation into a claim that every source feature
survived. The feature policy and independent regression cases define that claim.

WP4 superseded writer cleanup: removed the old exported sequential Producer and
WriteProduced API, declared-font registry and producer-only tests. The product
now renders one already-laid-out item with shared resources. Inclusive geometry,
font boundaries, empty text, cancellation and the page limit remain tested via
WriteRepeated; missing job-wide resources are rejected. Font sharing/extraction
properties are covered by generated-item and cross-item deduplication oracles.
Producer callback failure/one-page-at-a-time shaping guarantees have no caller
in the accepted per-item pipeline and are intentionally retired. Original-font
Write remains for pixel comparison and the salvaged extraction/bounds tests.

The empty-form challenge passed without another adapter: upstream
`destinationFormFields` replaces an empty Fields form with the later nonempty
form, including its resources. The qpdf resource-key check and Poppler rendering
confirm this for conflicting unused defaults. Keep that regression; do not add
empty-form cleanup or another compatibility state. The initial UTF-16BE name-tree encoding experiment was superseded by the
byte-identity correction below. Non-BMP/BMP destination names retain their raw
encodings and have a repeated-source oracle case.

WP4 verification: `go build ./...`, `go vet ./...`, and
`PDFCONCAT_REQUIRE_ORACLE=1 go test ./...` pass on macOS arm64. Independent
checks cover repeated/distinct name-tree and legacy destinations, direct/named
outlines and GoTo/Next chains, 140 colliding names across balanced leaves,
Unicode destination names, outside-source destination omission, actual field
mappings (including distinct names), compatible/incompatible resources and
inherited defaults, empty-form replacement, catalog/name-cache cleanup after
serialization, XFA removal, relative/absolute URI controls, PDF 2.0 in either
source position, byte-change detection, raw tree/canonical/resource-role refusals,
unsigned signatures, both password cases, and library standard-stream silence.
Pruned/original glyph pixels match; distinct generated items share one embedded
font program and add less than 30000 bytes in the tested short-text job. Fitting
uses qpdf physical boxes, normalized UserUnit, annotation rectangle comparisons,
and exact Poppler pixels against independent scaled-coordinate controls with
crop/rotation and mixed units. Widget fitting is refused. Fuzz seeds ran as
ordinary tests; no fuzzing or scale campaign ran. Full command/publication
acceptance and native Linux/Windows results remain pending subsequent WPs/CI.


## Independent verification and scale (WP7)

The oracle uses qpdf for structure/targets and Poppler for extraction, pixels,
font embedding and byte-string destination lookup (pdftohtml). All are optional
locally unless PDFCONCAT_REQUIRE_ORACLE=1; Linux CI requires them. Removed the
unused MuPDF raster adapter after tracing its sole old consumer to the deleted
custom fitting tests. Poppler pixel controls now cover that fitting property;
the optional local MuPDF/Poppler probes remain external investigation evidence.
No additional native test toolchain is required.

The opt-in TestScale contains the two authorized workloads in one execution:
2000 two-page sources interleaved with 2000 distinct text pages (6000 output
pages), then one plain generated item with count 100000. Independent qpdf page
counts must match. Fixture generation is timed separately. Build timings include
inspection, layout, pruning, assembly, verification and publication. An external
/usr/bin/time measurement of the precompiled test binary records peak RSS for
the combined run, excluding compilation; it is not a per-workload RSS estimate.
BenchmarkBuild is a small opt-in benchmark and is not run by normal CI tests.

Measured once on macOS 27.0 (26A428), Mac16,10, arm64, 10 logical CPUs, 32 GiB
RAM, Go 1.27.2 and pdfcpu v0.16.2:

| Workload | Fixture time | Build/verify/publish | Source bytes | Output bytes | Pages |
| --- | ---: | ---: | ---: | ---: | ---: |
| 2000 two-page sources + 2000 distinct generated text pages | 0.273 s | 3.037 s | 1784000 | 1001831 | 6000 |
| One plain generated item, count 100000 | 0.001 s | 3.819 s | 0 | 1195430 | 100000 |

The precompiled test process took 7.67 s wall time including fixture generation,
independent counts and cleanup. Maximum resident set size was 478101504 bytes
(455.95 MiB) for the combined run; macOS getrusage documents this field in bytes.
This is not a per-case measurement. The command was `PDFCONCAT_SCALE=1
PDFCONCAT_REQUIRE_ORACLE=1 /usr/bin/time -l <pipeline-test-binary> -test.run
'^TestScale$' -test.v -test.timeout=10m`, after `go test -c` outside the timer.
Both qpdf counts matched. No second scale run or benchmark campaign was run.

These are observations on this workload/machine, not a universal time or memory
bound. Memory includes imported PDF object graphs and generated pages, not only
compressed source bytes; small-input/many-page jobs can use hundreds of MiB.
The implementation is in-memory. Image-heavy/compressed inputs and complicated
forms can need substantially more memory. Use OS resource limits for untrusted
or unusually large jobs; no hostile-input exhaustion guarantee is made.

WP7 verification: build/vet and `PDFCONCAT_REQUIRE_ORACLE=1 go test -race ./...`
pass on macOS arm64, including the complete independent oracle set and the exact
one-source/one-Unicode-separator extraction, embedding and <30000-byte growth
case. TestScale ran exactly once as recorded above; both workloads passed. Normal
and CI test runs skip it explicitly. BenchmarkBuild was compiled, not executed.
Native hosted oracle/test results remain pending WP8 CI.


## Comparison evidence

The repaired scripts/run-comparison.sh creates fresh work, preserves command
stdout/stderr and checks success/refusal codes, newly produced PDFs/reports,
independent page targets, Poppler byte-name lookup and extraction. Its fixtures
come from the authoritative pdffixture package via an explicit test export; no
fixture fork or unused 100000-page generation is included. A deliberate executable
exit 7 was preserved and rejected in a one-off control. The observed pinned CLI
kept 17 source pages but redirected the first named/legacy copy into the later
copy; PDFConcat kept each occurrence's targets. Helvetica core text kept only
Pielikums and the dash from the tested Unicode string. The damaged empty page
fixture was accepted by the CLI and returned three pages; it is not evidence
that every damaged tree drops a page. PDFConcat refused it. The short Unicode
separator grew output by approximately 11.3 kB; byte reproducibility is not a
contract, so small per-run byte differences are expected.

## Documentation checkpoint (WP9)

Build, vet and the complete oracle-required tests pass locally. All README/docs
JSON blocks decode as plan v2 or validate as result v3; the diagnostic table
matches all 63 registry entries in both directions. The notice test compares
the linked module union for six targets. The documented Python recipe executes
an actual built CLI with ordered Unicode/space and platform-supported quote/
newline filenames. Current relative documentation links resolve; published
changelog sections are byte-for-byte unchanged, including their historical
references. The repaired comparison and its one-off failure control pass.
No additional scale or fuzz campaign ran. Hosted checks for these documentation
changes will be observed on the updated PR head during final verification.

## Delivered-state verification (WP10)

Fresh-clone checks at 1f1aec024f1274822c213d378c48cb75f18891eb passed:
`go build ./...`, `go vet ./...`, oracle-required `go test -race ./...`, root/tools
module tidy diffs, pinned native lint for Linux/macOS/Windows targets, isolated
pinned actionlint, govulncheck, GoReleaser check/snapshot, archive content/checksum
inspection and the repaired comparison. A module install by that published branch
commit succeeded into an isolated GOBIN and its version command ran. This proves
source-module consumption; it does not change what @latest selects.

Hosted run https://github.com/resoltico/pdfconcat/actions/runs/38684560479 passed
all six jobs including Required CI for that commit. Final documentation/draft
record changes are checked again on the final PR head; the PR's checks and final
implementation report identify that last run rather than treating this earlier
run as proof of a later commit.

Owned Go at the delivered-code checkpoint: 6672 product lines, 2346 test-support
lines (fixtures/oracle), and 9513 test lines. Product code is within the expected
6–9k range; even including test support it remains below the 12k review threshold.
The single WP7 scale run was not repeated after tooling/diagnostic/documentation
changes. No mutation campaign, long CI scale run, fuzz campaign, native ACL or
console fault campaign ran. Additional CPU targets were cross-built, not natively
executed; network-share and hostile-exhaustion limits remain untested/unsupported.

Latest upstream remained v0.16.2. Duplicate searches and short repros finalized
the owner-only issue drafts. No issue was posted and no tag/release/settings/main
push or remote branch deletion occurred. Owner artifacts dist/, bin/, .tools/,
pdfconcat and plan.test remain present and untouched by cleanup/snapshot commands.

## Immutable setup-go archive repair

Final CI at c3cd20f failed before checkout: GitHub returned HTTP 404 for the
setup-go v7.0.0 commit archive, despite the official tag resolving to that SHA.
The v6.5.0 commit archive was also unavailable. CI and release now pin official
upstream commit `90ad2b35f69faf97585ad74d28fa006d2739b7af` from
[setup-go #794](https://github.com/actions/setup-go/pull/794), whose SHA archive
is available and whose upstream checks passed across the supported runner OSes.
It retains Node 24 and the version-file/cache inputs used here, and includes the
upstream cache 6.3.0 temporary-file isolation fix. This is an immutable upstream
pin, without a fork, mutable tag, alternate downloader or skipped check. Exact-head
product acceptance still requires all six CI jobs, including native macOS.
