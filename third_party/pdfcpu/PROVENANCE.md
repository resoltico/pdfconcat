# PDFConcat's pdfcpu source patch

This is a modified local source distribution of `github.com/pdfcpu/pdfcpu`
`v0.16.4`, acquired from the Go module proxy using the reviewed module locks:

- Module sum: `h1:XW7q98xu3vNAimTSX93fIWnTDA2F0zz8mu88wiXoMjU=`
- go.mod sum: `h1:zMjQF5nYZ6A0yaG2d4GAj1QrzM5U8BZYDFHpuRwPuxQ=`
- Upstream: https://github.com/pdfcpu/pdfcpu/tree/v0.16.4
- Upstream license: [Apache-2.0](LICENSE.txt); upstream notices and source are retained.
- PDFConcat modifications: AGPL-3.0-only under [the project terms](../../PROJECT-NOTICE.md).

The root module replaces pdfcpu with this directory. It is **modified source**,
not the untouched released module. Build metadata exposes the local replacement;
scanners must retain that identity. `PDFCONCAT-PATCH.json` records the exact changed
source hashes and upstream hashes. This source tree is the canonical patch owner;
there is no second patch file or cache patching step.

The patch adds a typed, per-read `ZeroOffsetPolicy`, disabled by default. The
opt-in policy reserves effective zero-offset classic-xref entries as free
`tombstones`, retaining generation, revision and xref offset. The original typed
trailer and every effective live object's parsed syntax are checked before
`resolveObject` can consult indirect stream lengths, filters, or normalize values.
Dictionary/array references are checked by object number, including other
generations and unreachable active objects. Strings and stream payload are values,
not parsed reference syntax. Existing parser recursion, byte and cancellation
bounds remain in force. Ordinary strict output reads never enable the policy.

Exceptional hybrid or xref-stream revisions are refused. Those readers may parse
objects while constructing the combined xref table; this patch does not claim an
original-reference proof for that path. Ordinary valid hybrid/xref-stream sources
retain upstream processing. This is not a general Quartz compatibility claim.

Run the focused dependency test from this module:

```
go test ./pkg/pdfcpu -run TestZeroOffsetEntryPolicy -count=1
```

The first-party reader and application tests exercise the linked replacement,
including original-reference guards and independently checked serialized output.
The source archive check verifies that vendoring retains these exact modified
files, and an offline vendor build uses the same code without a patch step.

The patch also returns a typed `MissingAnnotationAppearanceError` from the
existing PDF 2.0 appearance checker. It changes no exception or graph rule;
PDFConcat uses the category and subtype facts rather than matching the English
error sentence. Other dictionary, shape and dereference failures remain errors.

The numeric extension emits finite model reals as non-exponent float64 round-trip
decimals, retaining a decimal marker for nonzero integral-valued reals and a
canonical zero. Content/operator emission uses this same owner. Nonfinite output
scalars refuse. Real syntax is dispatched before native integer conversion, so
large explicit reals do not hit an integer range error. Oversized integer tokens
and out-of-range real values refuse visibly; they never become zero or a rounded
real substitute. Ordinary integers and valid indirect-reference syntax retain
their native meaning. This does not recover precision lost during float64 parsing.

Additional dependency-owned qualification (also explicitly discovered by CI):

```
go test ./pkg/pdfcpu/types ./pkg/pdfcpu/model -run 'TestFloatPDFRoundTrip|TestRealLexicalRoundTripAndIntegerRangeRefusal' -count=1
```

The root serializer independently refuses nonfinite scalars before emitting PDF
bytes. Root tests reopen finite scalar witnesses through the actual parser and
verify numeric equality, including subnormals and the largest finite values.

The runtime-only `Configuration.RejectEncryptedInput` option defaults to false.
When enabled, a recognized encryption reference returns the existing typed
`ErrEncrypted` before encryption-dictionary dereference or key setup, with context
cancellation preserved. PDFConcat enables it for source admission/re-admission
and strict output reopening. Header/xref/trailer parsing still occurs first;
existing encryption-reference recognition and earlier malformed-input failures
are unchanged. No cryptographic algorithms or configuration-file schema change.

Dependency-owned controls explicitly exercise default encryption support, refusal
before dictionary I/O/key work, cancellation, cloning and the supported xref forms:

```
go test ./pkg/pdfcpu -run TestEncryptedInputReadPolicy -count=1
```


The reader refuses an occupied object-zero entry after resolving the effective
xref table and before free-list repair. Strict classic reads preserve the
original in-use classification of the effective row, including at offset
zero. Relaxed classic reads keep the prior zero-offset repair. A missing head
is still supplied by the existing reader across supported xref representations.

The effective-version reader checks the full identity of an indirect catalog
`Version` before caching the override. Undefined references fail at that
boundary. A live null contributes no override, and a valid catalog version
takes precedence only when it is later than the header. The general
dereference API retains its upstream behavior. PDFConcat's reader tests cover
admission and reopened output through this same backend path.

The runtime-only `Configuration.KeepDirectInfo` option retains a PDF 2.0
direct trailer Info dictionary for validation without allocating an object
number during the read. The default still materializes it. PDFConcat enables
the option for source admission and output verification, then discards source
Info when it installs authored packet metadata.

The reader records effective raw trailer fields for xref streams as well as
classic tables. For each key it keeps the first value encountered in the
newest-first traversal. PDFConcat uses the raw trailer as the root of its
strict reopened reference check.

The reader records disagreements between an effective xref identity and the
physical indirect-object header. PDFConcat checks that evidence along reachable
references in source and output reads, retaining the tolerance for unused alias
rows. The strict reader compares both identity components of effective
uncompressed rows at the physical offsets of parsed xref streams. A stream
without an effective row at its offset remains admissible. The reader also
checks headers of active object streams and requires each effective compressed
entry to match the object number in the selected directory slot before adopting
the compressed object. Superseded rows and unused directory entries do not
select objects. Relaxed object-stream reading retains the upstream behavior.

An optional parser observer exposes explicit null dictionary entries before
normalizing them to absent keys. It applies to classic and compressed objects
and leaves the returned dictionary and validation behavior intact. PDFConcat
uses this evidence when qualifying exact optional-content dictionary shapes.

The content reader shares exact inline-image framing with PDFConcat's
drawing-program scanners. For supported unfiltered device samples it computes
byte-aligned row lengths. PDF 2.0 explicit lengths also use the shared check,
while strict PDF 2.0 parsing still requires a length. The reader checks the
delimiter at the established boundary and retains fallback paths for other
encodings.

Required resource binding lookup is shared between page resource consolidation
and PDFConcat admission. Consolidation retains page Font and Properties
bindings when a legacy Form, including a nested Form, can borrow them from
the physical page scope. Page-only resource extraction cannot see the Form's
named uses.

A separate runtime read policy supports sources whose structure is explicitly
discarded. The original-reference guard can defer at most two matching
tombstone references in the top-level index slots. The reader validates the
effective typed structure root and its K hierarchy, then checks all original
live object syntax and the effective trailer for external aliases before
returning typed proof. The second pass uses the original object-read budget.
The source adapter removes those exact invalid edges before ordinary
structure discard. Preserve and serialized-output readers do not enable
this policy.

When the unreferenced-only source policy refuses a possible structure-index
reference, the guard reports a typed cause for an optional source-admission
probe. The cause indicates a top-level index slot on a typed dictionary and
a matching tombstone generation. The discard reader still establishes the
effective root, exclusive ownership and allowed uses independently.
