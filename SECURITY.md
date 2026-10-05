# Security policy

## Supported versions

Until PDFConcat publishes its first normal release, only the current development branch is supported.

After normal releases begin, security fixes will target the latest supported release line unless a published advisory states otherwise.

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

PDFConcat treats PDF inputs as untrusted data and relies on the embedded pdfcpu parser/validator using its stateless/default resource limits. The pdfcpu configuration is explicitly offline for this workflow. PDFConcat does not execute embedded PDF content, invoke shell commands, call external PDF binaries, or intentionally initiate backend network activity.

Source PDFs and plan files are never intentionally modified. Plan files are strict JSON, size-limited (64 MiB), and never executed or expanded: no globs, includes, environment variables, or shell syntax. Text printed on generated blank pages is encoded to a fixed character set and escaped before it enters a PDF content stream. Final output is staged and verified before publication. Output paths that alias a source PDF or plan file are rejected. Existing symbolic-link destinations are also rejected instead of relying on platform-specific link replacement behavior.

No-overwrite publication uses native no-clobber rename/move semantics: `renameat2(..., RENAME_NOREPLACE)` on Linux, `renameatx_np(..., RENAME_EXCL)` on macOS, and `MoveFileExW` without `MOVEFILE_REPLACE_EXISTING` on Windows. A destination created concurrently after preflight is therefore not silently replaced. Explicit `--overwrite` authorizes replacement only after final verification.

## Concurrent path changes

PDFConcat validates source PDFs before assembly but does not lock source files for the duration of a run. Source PDFs and the plan file should therefore be treated as stable inputs while PDFConcat is running. A local process that deliberately replaces or mutates an input path concurrently may cause a failure or may change which bytes are assembled.

For no-overwrite publication, the native rename primitive itself enforces create-if-absent semantics. With explicit `--overwrite`, PDFConcat rechecks the destination immediately before replacement, but it does not claim to defend against a hostile local process racing changes to the destination entry or its parent directory. Do not use an attacker-controlled writable output directory when that threat model matters.

The verified-output publication guarantees apply to the result PDF under ordinary local filesystem stability, not to hostile concurrent path mutation.

PDFConcat is also not a sandbox for adversarial PDFs. Validation reduces accidental corruption but cannot eliminate parser vulnerabilities or resource-exhaustion risk in the embedded backend; use ordinary OS isolation/resource controls when processing hostile documents.
