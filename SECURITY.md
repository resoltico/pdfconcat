# Security

## Reporting a security issue

Use GitHub's [private vulnerability report form](https://github.com/resoltico/pdfconcat/security/advisories/new)
if private reporting is available for this repository. If the form is unavailable,
open a [contact request](https://github.com/resoltico/pdfconcat/issues/new?template=security_contact.yml)
asking the maintainer to arrange a private channel. That request is public: include
only the request for contact, with no vulnerability details, private paths or PDFs.

Once a private channel is established, include the PDFConcat version, platform,
command and a minimal synthetic reproduction where possible. Keep confidential
PDFs out of public issues and pull requests.

## Installation and release trust

The [installers](docs/INSTALL.md) resolve latest once to a stable release tag,
download that release's archive and `checksums.txt` over HTTPS, verify one exact
SHA-256 entry, and test-run the program as the current user before installation.
Redirects must stay HTTPS. They need no administrator access. They write the
program, a sibling private ownership directory and, unless disabled, exact owned
shell startup or user PATH entries. Published v0.9.0 predates the receipt protocol;
use the matching release's documentation when inspecting an older installer.

A receipt records managed file hashes and PATH ownership. Install/update/removal
refuses unowned or changed managed files; no version-string or marker-based
adoption occurs. A per-directory lock serializes cooperating installers. Private
staging and rollback handling reduce accidental damage but do not promise crash
recovery or protection against a hostile process with the same account's rights.
User edits and unrelated files remain outside installer ownership. `NoModifyPath`
also suppresses uninstall PATH cleanup; shared Windows directories may retain an
entry rather than breaking other programs.
Because the checksums come from the same release as the archive, a match detects
a damaged download, not a compromised release or repository. Release archives,
`checksums.txt` and both installers carry GitHub build-provenance attestations
from the release workflow; `gh attestation verify` checks them. Per-executable
CycloneDX SBOMs list the linked modules for dependency and license audits.

Tag builds stage the final signed archives, installers, SBOMs and checksums once.
All seven native installer jobs qualify that set before the publisher can run.
The publisher checks the same downloaded bytes and manifest, attests them, then
creates the release without rebuilding or re-signing. Branch CI uses snapshot
versions; it does not publish. GoReleaser's direct publisher is disabled.

macOS executables have an [ad hoc signature](https://developer.apple.com/documentation/security/seccodesignatureflags/adhoc),
which does not identify the publisher. PDFConcat has no Apple Developer ID
signature or notarization, and Windows executables are not code-signed. The
installers do not disable Gatekeeper, SmartScreen or other protections. Manual
browser downloads carry the operating system's internet mark; the installation
guide describes clearing it only after the checksum has been checked. Managed
devices and features such as Windows Smart App Control may still refuse
execution.

## Input and PDF behaviour

PDFConcat is for trusted local files. It is not a sandbox or sanitizer. Malformed
or highly compressed PDFs can exhaust memory/CPU or trigger fatal runtime limits;
ordinary panic recovery cannot cover those failures. Use OS resource limits for
untrusted/large inputs. PDFConcat does not execute or verify network destinations
or active scripts embedded in source PDFs, but retained page/annotation actions
may execute in a PDF viewer. Removing document-level scripts/attachments is not
sanitization.

Encrypted sources (including empty-password encryption), signature values,
catalog permission structures, timestamps, dynamic forms, optional content and
known content-dropping page-tree shapes are refused. Empty
unsigned signature fields remain supported. Signatures are not authenticated.
Viewer directives are removed from imported catalogs after original-file checks;
this does not sanitize retained content. Serialized output is checked separately
with strict validation and without source normalization before publication.
Source parsing is strict and semantic compatibility validation remains relaxed.
Every original page-content stream must decode before selection, including
omitted pages. Truncated/corrupt Flate programs are refused using independent
EOF/checksum checks; encoded source data is not silently repaired. Decoder
per-stream, combined-page and cumulative limits apply; intermediate filter
buffers are conservatively charged too. This does not validate every image
payload or arbitrary content-operator grammar.
Page order and encoded content-stream identity are checked around validation;
these checks do not establish arbitrary PDF semantic or rendering equivalence.
Backend notices are incomplete evidence of repairs; their wording does not select
admission policy. Lower-level reader recovery can still occur; see the
[feature policy and limitations](docs/PDF-POLICY.md).

Page selection is visible-page composition, not redaction. It does not promise
removal of every private object or byte associated with omitted pages. Every
original source remains subject to whole-file safety checks and hashing. Footer
numbers are overlays and may cover existing content.

## Filesystem and saved outputs

Named input plans, fonts and PDFs must be regular files; input symlinks to regular
files are supported. Filesystem checks assume trusted local namespaces and do not
claim protection against concurrent adversarial path substitution.

Output/report targets cannot alias existing inputs or each other, cannot be
symlinks or other nonregular files, and require `--overwrite` for existing regular
targets. Missing inputs are protected only by cleaned path spelling; alternative
spellings of missing paths are not reserved. Reports are rechecked after PDF
publication. With undecoded plans, a report can only create a new path even if
overwrite was requested. A trailing error does not erase the fact that an output
has already been saved; do not blindly retry a result with `output.published: true`.

Files use ordinary umask permissions; replacements retain their old mode. No
private ACL or recovery-copy guarantee is made. Temporary files are siblings of
the target. Without overwrite, native no-replace rename has a hard-link fallback;
with overwrite, publication uses a replacing rename. Some network/share
filesystems do not support these semantics or directory durability. There is no
universal network-filesystem guarantee. A backend/output/report failure never
intentionally rolls back an already-published PDF. See
[saved-output and report behaviour](docs/CLI.md#reports-and-committed-state)
for the result fields that record these outcomes.
