# Security

Report security issues privately through the repository owner's GitHub contact
rather than attaching confidential PDFs to a public issue. Include the version,
platform, command and a minimal synthetic reproduction where possible.

PDFConcat is for trusted local files. It is not a sandbox or sanitizer. Malformed
or highly compressed PDFs can exhaust memory/CPU or trigger fatal runtime limits;
ordinary panic recovery cannot cover those failures. Use OS resource limits for
untrusted/large inputs. No network destination or active script is executed or
verified by PDFConcat, but retained page/annotation actions may execute in a PDF
viewer. Removing document-level scripts/attachments is not sanitization.

Encrypted sources, signature values/certification/timestamps, dynamic forms,
optional content and known content-dropping page-tree shapes are refused. Empty
unsigned signature fields remain supported. Signatures are not authenticated.
Upstream relaxed parsing may accept other repairs/malformed syntax; see the
[feature policy and limitations](docs/DESIGN.md).

Page selection is visible-page composition, not redaction. It does not promise
removal of every private object or byte associated with omitted pages. Every
original source remains subject to whole-file safety checks and hashing. Footer
numbers are overlays and may cover existing content.

Named input plans, fonts and PDFs must be regular files; input symlinks to regular
files are supported. Filesystem checks assume trusted local namespaces and do not
claim protection against concurrent adversarial path substitution.

Output/report targets cannot alias inputs or each other, cannot be symlinks or
other nonregular files, and require `--overwrite` for existing regular targets.
Reports are rechecked after PDF publication. With undecoded plans, a report can
only create a new path even if overwrite was requested. Publication preserves
visibility facts through trailing errors; do not blindly retry a result with
`output.published: true`.

Files use ordinary umask permissions; replacements retain their old mode. No
private ACL or recovery-copy guarantee is made. Temporary files are siblings of
the target. Native no-replace rename has a hard-link fallback; some network/share
filesystems do not support these semantics or directory durability. There is no
universal network-filesystem guarantee. A backend/output/report failure never
intentionally rolls back an already-published PDF.
