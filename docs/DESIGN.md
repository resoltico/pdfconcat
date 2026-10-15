# Design and rebuild acceptance

PDFConcat assembles a declared packet of source PDFs and generated pages using
unmodified upstream pdfcpu. The rebuild follows WP0–WP10 from the owner-approved
2026-10-15 bundle, with the peer-review corrections. Acceptance below describes
work actually run; pending work is not a passing check.

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

## Sequencing decisions

WP1 removes obsolete CI, release and GoReleaser configurations when their
dependencies are removed; WP8 recreates them. Leaving broken configurations
until WP8 would contradict WP1's acceptance and permit obsolete campaigns.
Every WP still requires build, vet, tests and a commit.

WP5 native Linux/Windows acceptance remains pending until replacement CI runs.
WP8 opens the single authorized draft PR after committing replacement tooling,
observes its CI, then comments on and closes PR #1 as superseded. WP10 updates
that same PR and verifies the delivered state. All GoReleaser snapshots using
`--clean` run in isolated clones, preserving the owner's `dist/`.

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

## Contract decisions to verify before implementation

The peer audit supplies executable counterexamples outside this repository.
The following corrections must be reproduced before their affected changes.

WP2 reproduction: reran contract-evidence/json.go and nullguard.go with Go
1.27.2; their outputs in the external implementation evidence confirm null
acceptance, pre-scan precedence failure, lexical-integer rejection and the
integrated guard's ordering. The decoder uses jsontext's grammar/duplicate
checking and json/v2 scalar decoding, with domain-specific object dispatch and
immediate value validation. It never scans ahead for null. A well-formed v1 plan
has a deliberate version-diagnostic exception so member order cannot hide the
actionable migration message. Offsets are zero-based; lines/columns are one-based
bytes in the original input, including an accepted leading BOM.

- Integrate null rejection with strict decoding; a blanket null scan masks
  earlier faults. Accept one leading BOM and count its bytes in offsets. Detect
  v1 deliberately and explain both `version: 2` and `blank` → `page`.
- Required version omission maps to `plan_version_unsupported`; missing items
  to `plan_type`; empty items to `plan_value`. Wrong JSON types map to
  `plan_type`; noninteger/low count to `plan_value`; excessive count (including
  integer overflow) to `plan_limit`. Groups require dir and nonempty items.
  Warning classes are empty and cannot change outcome status.
- Standard JSON Schema cannot distinguish 3, 3.0 and 3e0. Lexical counts,
  duplicate members, encoding, converted dimensions, paths and aggregate limits
  remain decoder checks. Positive numeric font size/width bounds belong in
  schema too. Complete documentation examples must validate.
- Named plan/font/source inputs require Stat before open, then f.Stat before
  read. Follow input symlinks to regular files; stdin is the stream exception.
- Recheck report roles immediately before publication, including against an
  output that was initially absent. Unsafe report refusal remains invalid;
  attempted I/O failure is failed. Attempt safe reports after failed builds;
  never recursively report a report failure.
- Publication state is independent of Commit error. Set it after rename/link,
  before cleanup or directory sync, retain published bytes on later failure,
  and never let Abort undo publication.

## PDF boundaries to verify before implementation

- Walk raw page trees before validation, rejecting a Page with effective
  non-null Kids, including an empty array. Keep canonical dictionary-name
  identity protection with a narrow refusal where upstream double-decodes.
- Triage every fork regression individually. Preserve valid-input fidelity
  through small adapters/refusals; do not restore forks, appearance machinery
  or the fitting parser. Resource-exhaustion budgets are outside the accepted
  trusted-local-input threat model; ordinary API cancellation remains required.
- Prune forbidden name trees from both catalog and context caches. Reopen the
  serialized output and check its catalog. Close named/direct outlines, GoTo
  chains, repeated/distinct name trees and legacy destinations with oracles.
- Classify upstream password errors as source_encrypted even without a context.
  Synchronize cached effective version before validation/writing. Retain a
  warning for relative URI links whose catalog Base is removed.
- Upstream does not generally merge AcroForm DR and namespaces later fields
  even without collisions. Establish a narrow compatible boundary/refusal,
  report actual mappings, document limitations, and draft an upstream issue.
- Test minimal UserUnit normalization after Resize against physical geometry
  and pixels; otherwise explicitly refuse the unsupported case. Destination
  coordinates after fitting remain an explicit upstream limitation.
- Preserve bitmap-only font refusal, no-subsetting restrictions, and ActualText
  for glyph aliases and multi-glyph/multi-character clusters. Job-wide font
  identity includes widths and ToUnicode, not only the font program.

## Tooling and delivery boundaries

Build the pinned linter for the host once and run that binary under each target
GOOS; cross-building a module tool cannot execute it. Stock depguard also
enforces diag/job/typeset leaf boundaries. Keep six CI jobs and Required CI.
Compare fresh outputs with an evidence-preserving harness that asserts expected
exits; do not generate unused scale fixtures. Run only the authorized WP7 scale
case. Release readiness does not create tagged binaries or change @latest.
Changelog outcomes are relative to published v0.2.0: repeated named destinations
and damaged-tree refusal already shipped; repeated legacy Dests support is new.
Review actual linked licenses, NOTICE files and archive contents.

WP1 verification: `go build ./...`, `go vet ./...`, and `go test ./...` passed
on macOS arm64. All four salvaged package test suites ran; no tests were trimmed.
No replace directives remain. The forbidden-reference search is empty outside
history and documentation scheduled for WP9. Protected owner artifacts remain.

WP2 verification: `go build ./...`, `go vet ./...`, and `go test ./...` passed
on macOS arm64. Plan tests cover first-fault ordering, duplicate/null/unknown
members, escaped pointers, BOM rebasing, v1 member ordering, required/empty
members, lexical counts, schema-expressible negative cases, decoder-only count
lexemes, 64/65 group depth, 100000/100001 entries/counts, aggregate counts and
10000/10001 text length. All five example plans decode and validate against the
schema. Job tests cover lexical paths, units/colors, layering origins, inherited
first/last sizes, explicit sizes under fitting and int64 page ranges. Fuzz seeds
ran as ordinary tests; no fuzz campaign ran. Plan I/O/cancellation criteria belong
to the WP5/WP6 pipeline and remain pending there.

## Font embedding decision (WP3)

Keep the existing pure text layout, font validation, script policy and shaping
code. Its measured bounds support product overflow diagnostics rather than a
removed report-retention subsystem. Bitmap-only embedding refusal and
no-subsetting permissions remain required; glyph aliases and clusters still
require the salvaged ActualText logic in the PDF writer.

A small stable-GID pruner owns only glyf/loca rebuilding, composite closure,
selected-table copying, post format 3 and sfnt checksums. The existing go-text
parser remains the shaping source. The inspected alternative
[tdewolff/font Subset](https://github.com/tdewolff/font/blob/master/sfnt_subset.go)
renumbers the supplied glyphs and offers table selection but no stable-GID
pruning option. Passing every GID would retain the unwanted outlines. Adapting
that mechanism would add mapping/dependency complexity, so the narrow owned
pruner follows the accepted design. It never shapes the reduced font; layout
uses the original. fsType 0x0100 returns original bytes unchanged. Composite
closure preserves the outlines of components without adding text mappings for
components that the text itself does not use. One job-wide glyph union supplies
all PDF widths, ToUnicode and program bytes. Rendering/dedup/size acceptance
remains pending WP4/WP7; structural checks alone do not establish visual fidelity.

WP3 verification: build, vet and the full test suite pass on macOS arm64.
Pruning tests independently read table directories and loca offsets, compare
retained outline bytes and composite components, require unused outlines empty,
retain maxp/hmtx, check every table checksum and the whole-font 0xB1B0AFBA sum,
exercise invalid GID rejection, preserve no-subsetting bytes, and verify the
job-wide union is independent of page order/repetition. Existing text-policy,
wrap/alignment/anchor/overflow, glyph coverage and font-permission cases all
remain passing. No fuzz or scale campaign ran.

## PDF correction experiments and boundary (WP4 design pass)

Reran the audit's small upstream probes before implementing the affected code.
The raw Page/Kids fixture validates without a notice; the two different form DRs
merge successfully; removing catalog EmbeddedFiles leaves its cache populated;
setting catalog Version alone leaves effective cached version 1.7; resizing the
private Cr#6fpBox key fixture produces no extracted text. Outputs and exact
commands are retained in external evidence/pdf-probes/observed.log. These
reproduce the peer findings; none is dismissed as hostile-only.

The form boundary will allow multiple nonempty forms only when their complete
resolved DR dictionaries and form-level DA/Q defaults are structurally identical
(including stream bytes); absent resources/defaults must agree too. A single
form retains its own resources. This conservative compatibility check permits
repeated ordinary forms without implementing resource/appearance merging. Other
combinations receive source_form_unsupported with an actionable explanation.
Check warns that later nonempty forms may be namespaced, and build reads actual
field names before/after each merge for exact mappings. Distinct resources under
the same name and distinct names both require refusal. This narrows v0.2.0's form
support and must have a breaking note. Unsigned signature fields remain allowed.
Canonical names containing a literal hash followed by two hex digits receive a
narrow source_invalid refusal before validation; their identity cannot safely
survive upstream dictionary lookup. Ordinary literal hashes remain accepted.

The salvaged hostile fixture now fails the required raw tree precheck before
reaching upstream's panic: its root page-tree Type is malformed. Its specific
outcome is therefore source_damaged, rather than a recovered source_invalid.
This was observed in the WP4 regression run. The panic boundary remains in place;
a structurally valid page-tree fixture with a malformed widget rectangle tests
a narrow refusal separately. Upstream accepts that short rectangle in relaxed
mode, so the precheck rejects it before later consumers can index four entries.
The original panic is prevented before the backend, rather than reproduced
inside the guarded product. Recovery remains defense in depth. Generated-page tests that asserted
one flat Kids array now assert bounded fan-out and each leaf reference exactly
once; the old flat shape is intentionally replaced by the required balanced tree.
The producer boundary reflects the accepted 100000-page limit.

Fitting experiment: Resize preserves a source CropBox's lower-left origin in
its output MediaBox/CropBox (e.g. [10 20 310 520] for a 300×500-point target).
The target's physical width/height are the contract; fitted sources have no
clean-origin guarantee. Tests therefore assert the known origin, target side
lengths and UserUnit 1, then compare rendering with a physically equivalent
scaled-coordinate reference. Generated canvases retain their clean origin.

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

## Files and publication decisions (WP5)

Reran the report-alias and regular-file probes: initially absent targets through
symlinked parent directories compare unequal, then SameFile after PDF publication;
an unchecked report rename replaces PDF bytes. Stat accepts a symlink to a
regular input and identifies a FIFO before opening it. Role checks therefore run
again immediately before report publication. Report refusal is invalid; an
attempted environmental write/durability failure is failed. Commit returns
publication state independently from error and records visibility before cleanup
or directory sync. Abort only removes its own temporary name.

A small inputfile leaf shares named regular-file reading between plan, font and
PDF sources. This replaces the PDF package's reader rather than duplicating its
Stat/open/f.Stat/cancellation loop in the pipeline. A byte limit serves the real
64 MiB plan contract; fonts/PDFs use the existing unbounded trusted-input policy.
Stdin remains a separate stream exception. This additional responsibility-specific
package has no internal imports or native calls and adds no identity/race layer.

WP5 local verification: build, vet and the full Go test suite pass on macOS
arm64. Native publication tests cover late-target no-clobber, overwrite/mode,
umask, abort cleanup, real no-replace publication and real hard-link fallback.
Narrow state tests preserve published=true through post-link removal and
post-publication sync failures. Role tests cover hard/symbolic/case aliases and
the initially absent symlink-parent report alias; refusal stays invalid and PDF
bytes remain unchanged. Input tests reject directories, a device and a FIFO,
allow regular-file symlinks, and bound stream reading to the detection byte.
Linux/amd64 and Windows/amd64 atomicfile test binaries and all packages cross-
compile. Those binaries were not executed here. Native Linux/Windows acceptance
is pending the three-runner replacement CI, as resolved in the sequencing section.
The full failed-build/report/publication matrix remains pending WP6 integration.

## Pipeline integration decisions (WP6)

Safe reports are attempted once after either a successful or failed build/check;
refusals retain their invalid codes, while attempted report I/O failures are
failed. The final report role recheck happens after PDF publication. Both output
published and report written follow actual rename/link state even if durability
fails later; the saved report cannot predict its own trailing failure, so stdout
is authoritative then. No report follows argv parse failure.

For supported stdin (the process's os.File/pipe), cancellation closes the reader
to unblock a pending read, then returns interrupted. Ordinary in-memory readers
need no unblock action. Arbitrary nonclosable blocking Go readers are not a CLI
input mode. Tiny plain pages remain valid; nonempty text with a derived block
width below 1 point is text_overflow, even with overflow:allow. Give an explicit
valid width or a wider page. This is unusable layout geometry rather than a
measured overflow that allow can place.

Stdin challenge: Go's Windows fd.Close cancels pipe I/O but waits for references
held by a synchronous console ReadConsole call. Closing alone is therefore not
a sufficient interruption design. For closable stdin, read on one goroutine and
select completion versus context cancellation; request Close asynchronously on
cancellation. The outcome path does not wait for a blocked console close/read.
The CLI exits after rendering interrupted. Ordinary os.Pipe cancellation is a
native product test; a blocked-close control checks the independent outcome
path without console fault injection. No worker pool or native input API is added.

Integration checks exposed numeric error-class edges: unsupported numeric version
lexemes/overflow must be plan_version_unsupported; floating length/leading
overflow is plan_value rather than a wrong-JSON-type error. Domain-specific token
handling keeps that distinction without another parser. v1 diagnostics now locate
the actual version value. Path grammar validation is pure and separate from
resolution, avoiding a needless Getwd call for every parsed input path. Generated
inheritance below the writer's 1-point minimum is plan_value with an explicit-size
remedy; ordinary tiny source pages remain supported.

Integration regressions found two effective-value gaps: an indirect null signature
value was falsely classified as signed, and an indirect NeedsRendering=true was
missed. Policy checks must dereference those values; unsigned null remains
supported. A readable refused source also lost its already-known byte facts.
Compute its digest before parsing and retain SHA/byte length through refusal or
recovered panic; keep page counts null when they were not established. The new
negative/positive tests first reproduced all three failures.

The effective-value review reproduced further legal-indirection gaps: indirect
FT /Sig with a signature value lacking optional Type was accepted; indirect
Widget subtype and NeedAppearances were missed; indirect URI actions were not
warned; and indirect GoTo actions could cross source occurrences after naming.
The qpdf oracle was first taught to resolve action-type references so its GoTo
negative control observed the wrong target rather than ignoring the action.
Use ordinary context dereferencing for these values and text entries, retaining
raw canonical-key checks. Form nonemptiness is determined by Fields, not by how
many nonempty names happened to be extracted. This adds no PDF token parser,
resource merger or appearance engine.

When a destination points outside its source, its name and unusable references
are omitted. A GoTo action removed for such a target also loses its own Next
chain; sibling valid entries in a Next array remain. This narrow omission policy
avoids publishing dangling references without inventing replacement actions.

## Destination byte identity correction

A real Poppler lookup exposed a defect that decoded qpdf JSON and MuPDF's text
normalization did not detect: emitting every name-tree key as UTF-16 while leaving
first-use link strings in PDFDocEncoding made those links dangling to Poppler.
The short native Poppler probe and pdftohtml both resolved the source and failed
the rebuilt first-copy names. Logs are retained as poppler-named-before.log and
named-clickable-output-before.stderr in the external evidence.

[ISO 32000 named destinations](https://opensource.adobe.com/dc-acrobat-sdk-docs/standards/pdfstandards/pdf/PDF32000_2008.pdf)
use byte strings; [Poppler's API](https://poppler.freedesktop.org/api/glib/PopplerDocument.html)
explicitly distinguishes them from display strings. Preserve raw string/name
bytes, compare/sort bytewise, and write hex literals without text transcoding.
Renamed destinations use unique ASCII names rather than appending ASCII to an
opaque encoding. This also avoids repeated suffix searches. Conflicting targets
for the same raw name in the name tree and legacy dictionary are refused rather
than conflated. The qpdf target oracle remains useful for page membership/order;
pdftohtml adds actual byte-name lookup checks using the already-required Poppler
installation. The C++ probe remains external evidence, not a build/CI dependency.

The corrected output resolves chap1→page 1 and chap2→page 2 with native Poppler,
and pdftohtml produces no diagnostics. Raw ASCII and UTF-16BE keys that display
the same text also remain distinct, with valid links. The complete destination
fixture jobs now include real Poppler lookup checks alongside qpdf target checks.

WP6 verification: build, vet and the complete oracle-required test suite pass on
macOS arm64. In-process command tests cover JSON/text, every usage-code class,
0/1/2/130 exits, schema-valid golden stdout/full reports, optional check reports
without PDF writes, report refusals versus I/O failures, malformed/unreadable
plans with new/existing reports under both overwrite choices, failed builds with
safe reports, published PDF plus report failure, and the late symlink-parent
alias regression. Font/text refusal precedes PDF reads; named nonregular input
roles are rejected; regular source symlinks remain supported; tiny plain pages
work and unusable derived text width is refused. Native pipe cancellation and a
blocked-close control return interrupted. Two binary smoke tests execute version
and a build. The rebuilt generated-only example publishes five pages; qpdf check
reports no errors. Indirect policy/action values, unsigned null signatures,
readable-refusal byte facts and version-selected validation rules pass their
regressions. No real signal/console fault campaign, fuzz campaign or scale run
was performed. Linux/Windows execution remains pending replacement CI.
