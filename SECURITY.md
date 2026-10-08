# Security policy

## Supported versions

Security fixes target the latest published release line and the maintained source branch unless a published advisory states otherwise. See [`CHANGELOG.md`](CHANGELOG.md) for release versions and publication status.

## Reporting a vulnerability

Do not open a public issue for a vulnerability that could expose users to malicious PDFs, filesystem attacks, or unsafe release artifacts. Use GitHub's private security-advisory reporting for this repository when available.

A useful report includes:

- affected PDFConcat version/commit;
- operating system and architecture;
- minimal reproduction steps;
- security impact;
- whether the input PDF can be synthetic and safely shared; and
- any relevant crash/error output with private paths or data removed.

Do not send confidential production PDFs when a synthetic reproducer can demonstrate the issue.

## Security boundaries

### What is trusted and what is not

- **Assembly instructions are trusted.** The user, or the agent acting for the user, decides which files are read and where output goes. A plan is not a sandbox: item paths, `dir` groups, font paths, `output`, and `--report` may name any file the process can access, including absolute paths and `..`. Directory groups only avoid repeating a prefix; they do not restrict access. Do not run a plan from an untrusted source with the privileges of an account that holds files you would not want read into a PDF or overwritten with `--overwrite`.
- **Source PDFs are untrusted data.** They are parsed and validated by the embedded pdfcpu, offline and stateless, and are never executed: no embedded JavaScript runs, no external program is invoked, no network connection is made by the assembly workflow, and nothing is read from or written to a user configuration directory. Parser panics on malformed files are converted to errors. Validation reduces accidental corruption but cannot eliminate parser vulnerabilities or resource exhaustion in the embedded backend, and PDFConcat is not a sandbox for adversarial documents: use ordinary operating-system isolation and resource limits (a separate account or container, memory and time limits) when processing hostile PDFs. Source size has no limit of its own; memory grows with the size of the imported documents.
- **Plan files and inline plans are untrusted syntax.** They are strict JSON, never executed or expanded: no globs, includes, environment variables, or shell syntax, and no schema is fetched. Declared bounds are charged while the plan is read: 64 MiB, nesting of 64 groups, 250,000 structural nodes, 100,000 flattened contributions, and 1,000,000 generated pages. These are resource bounds, not a promise of bounded memory for every job within them.
- **Font files are untrusted data.** A font must be a static TrueType file within 64 MiB whose embedding permissions allow embedding. It is parsed in-process by the pinned typesetting library with panics converted to errors, and malformed, variable, CFF, and collection fonts are rejected before any PDF is written. Text enters the output only as glyph identifiers and hexadecimal UTF-16 text-extraction maps, never as raw strings in a content stream, so text cannot inject PDF syntax.
- **Saved reports are untrusted files.** `pdfconcat report` rejects malformed JSON, unsupported versions, invalid references, duplicate identifiers, and impossible ranges with located errors, and enforces 256 MiB, nesting 64, and 2,000,000 decoded nodes before building any large structure. A query never reopens a PDF or modifies the report. A report describes the run that wrote it and says nothing about the current state of the files.
- **Reports contain absolute paths.** A saved report names the absolute path of every source, font, plan, output, and report file and the text of generated pages. Treat reports like the files they describe: do not share one publicly if paths or text are sensitive. Default diagnostics carry bounded path previews, explicitly marked when truncated; complete details retain full paths, and messages that echo a foreign error are length-bounded but not otherwise redacted.

### Retained PDF content and signatures

PDFConcat does not execute source scripts during assembly. Output is not sanitized: ordinary page/annotation/widget actions and page-local FileAttachment payloads can remain reachable. Catalog open/additional actions, associated-file (`/AF`) indices and non-destination name trees, including JavaScript and attachment indices, are removed; removing an index does not remove a payload still referenced by a retained annotation. Material keep/drop effects are recorded as warnings. A retained script's behavior is not verified or rewritten to follow qualified form names. Use a separate trusted viewer policy and active-content isolation when opening untrusted output.

Static forms preserve supported appearance and ordinary editing. Dynamic forms, unsupported rendering controls and unsupported form semantics fail instead of being flattened or silently altered. Actual signature values, certification/usage-rights signatures and document timestamps are refused because assembly rewrites signed bytes. Empty unsigned signature fields are distinct from signed inputs. PDFConcat does not authenticate a signer, evaluate certificate trust, strip signatures or sign output; assemble suitable unsigned inputs and sign the final document externally.

A passing check predicts known policy/layout effects. A warning does not imply that an unpublished job already removed content. A successful query can expose historical errors from a failed saved run; its own exit status says whether the query succeeded.

### Files PDFConcat writes

Source PDFs, fonts, and plan files are never intentionally modified. A job writes only its private workspace (beside the destination for `build`, in the system temporary directory for `check`), the output PDF, and the requested report; the workspace is removed on every exit path except one report-recovery file kept after a published PDF whose report could not be published.

Output paths that alias a source PDF, font, plan, report, or each other are rejected by filesystem identity, so hard links, symbolic links, and case aliases collide. New artifact names in one directory also undergo conservative Unicode normalization/case-fold comparison; this may reject distinct case-sensitive names. Actual identities are checked again before replacement. Symbolic-link and non-regular destinations are rejected instead of relying on platform-specific link replacement behavior. A failure report written before the job's files are all known is created only at a new path, whatever `--overwrite` says.

No-overwrite publication uses native no-clobber rename/move semantics: `renameat2(..., RENAME_NOREPLACE)` on Linux, `renameatx_np(..., RENAME_EXCL)` on macOS, and `MoveFileExW` without `MOVEFILE_REPLACE_EXISTING` on Windows. A destination created concurrently after the last check is therefore not silently replaced, and there is no weaker fallback. Explicit `--overwrite` authorizes replacement only of the named output artifacts and only after final verification. Atomic visibility is not crash durability; see [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md#the-pipeline) for exactly what is flushed.

## Concurrent path changes

PDFConcat copies each distinct source and font once into a private workspace, with a digest computed during the copy, and inspects and assembles only the copy, so the bytes that were validated are the bytes that were assembled. It does not lock files. The copy checks, on the open handle, that the source is a regular file (a FIFO or device substituted after a check cannot hang it), and compares the handle's size and modification time before and after the copy and with the number of bytes read; an ordinary change during the copy fails the job as `source_changed`. This is not a filesystem-wide atomic snapshot. A process that deliberately edits a source in place between those observations, replaces a parent directory while the job runs, or races changes to the plan file can still change which bytes are assembled. Treat source PDFs, fonts, and plan files as stable while PDFConcat is running. Two sources captured one after the other can reflect different moments, and a passing `check` does not predict a later `build`.

With explicit `--overwrite`, PDFConcat rechecks the destination immediately before replacement, but it does not claim to defend against a hostile local process racing changes to the destination entry or its parent directory. Do not use an attacker-controlled writable output directory when that threat model matters. The publication guarantees apply under ordinary local filesystem stability, not under hostile concurrent path mutation.

A job needs scratch space for a copy of every distinct source and font. Running out of space is reported as an actionable error and the workspace is cleaned up.

## Development tooling and supply chain

Development tools are pinned in `tools/versions.env` and installed by `go run ./tools/installtools`. The development linter and mutation tool are built from verified upstream modules with reviewed patches and unchanged upstream dependency files. GoReleaser and Gitleaks use official release archives checked against release manifests; Gitleaks also pins the manifest's SHA-256 because its upstream release is mutable and has no immutable-release attestation. Available release attestations are checked with the GitHub CLI and unavailable attestations are reported precisely. govulncheck and actionlint are built from source with Go checksum-database verification. These controls establish input identity and reduce tampering risk; they do not establish that third-party code is trustworthy.

The mandatory secret-hygiene CI gate uses the pinned Gitleaks default rules to scan intended source and reachable Git history, with full redaction. Inline allow comments and ignore fingerprints cannot bypass it; no secret-scanning exceptions are configured. Scanner detection is not proof that every possible secret format is absent. Keep credentials out of source and examples, use environment injection or platform secret stores, and rotate any credential that was exposed. Secret reports and audit evidence stay outside the repository.

GitHub Actions are pinned by full commit SHA, workflows request least-privilege permissions, checkout does not persist credentials, and source release verification depends on the reusable baseline CI including secret hygiene. CI also scans known vulnerabilities with govulncheck. Hosted execution is established by actual workflow results, not configuration alone.

The release workflow publishes tagged source and changelog notes after baseline CI verification. Executable packages require separate packaging and final-candidate verification; configured checksum and provenance controls do not establish that binary distribution has occurred. Fuzzing, mutation testing and scale acceptance require explicit campaign dispatch; baseline success does not establish those results.
