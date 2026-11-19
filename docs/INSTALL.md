# Install, update and uninstall the CLI

Install a standalone PDFConcat release for the current user, or
[build from source](../README.md#build-from-source). No administrator account,
background updater, scheduled task or file association is needed. Your PDFs,
plans and reports are outside the installer's scope.

Use an installer and payload from the same qualified build, with its matching
documentation. The [README quick install](../README.md#install) keeps no script
after a successful run. To inspect the installer before running it or keep it
for offline removal, save it using the instructions below.
Published installers link to this guide at their build's Git commit; source
installers link to the current `main` guide.

## Existing executable without an installer receipt

`Existing executable has no installer receipt` means a file already exists at
the printed path, but this installer has no record of managing it. It does not
establish how that file was installed. No program or PATH entry was changed by
this refusal.

**Replace the existing executable:** use the complete command printed by the
installer. It preserves your selected directory, explicit release pin and
no-PATH option. The original executable is kept in a recovery backup outside
the install directory; the installer prints its exact location. Keep that backup
until you have verified the replacement. Existing PATH entries and directories
are not adopted.

For the saved installer and default directory, the replacement commands are:

```sh
sh ./install-pdfconcat.sh --install-dir "$HOME/.local/bin" --replace-unmanaged
```

```powershell
powershell -NoProfile -ExecutionPolicy ByPass -File (Join-Path $HOME 'install-pdfconcat.ps1') -InstallDir "$env:LOCALAPPDATA\Programs\PDFConcat" -ReplaceUnmanaged
if ($LASTEXITCODE -ne 0) { throw "Installer failed ($LASTEXITCODE)." }
```

Use your printed directory if it differs, including when `XDG_BIN_HOME` is set.
Retain any `--version` / `-Version` and `--no-modify-path` / `-NoModifyPath`
choices. If you ran the installer through stdin or a script expression, first
save and review the matching installer using the download instructions below;
the fallback example is not an automatically located saved file.

**Keep the existing executable:** install into a different absolute directory
with `--install-dir` / `-InstallDir` and `--no-modify-path` / `-NoModifyPath`.
Use the new program's printed full path so the existing command remains selected.

After replacement, run the new executable's printed full path with `--version`
and `--help`. To restore the original later, first uninstall the new managed
copy using the same saved installer and install directory. Then move the exact
reported backup to the original executable path, after confirming that path is
vacant. Review PATH separately. Uninstall does not remove the recovery backup.

For an **uninstall refusal**, use the method that installed the existing file
to remove it. If its origin is unknown, preserve it and inspect the exact file
before deciding what to remove; installing a replacement is not required.

Replacement is install-only and cannot bypass changed managed files, corrupt
state, symlinks or inaccessible targets. If activation reports incomplete
restoration, preserve all reported recovery paths and inspect them before
retrying. See [ownership and recovery](#ownership-and-recovery) for details.

## Download, inspect and install

On macOS or Linux, save the latest installer, inspect it, then run it:

```sh
curl --proto '=https' --proto-redir '=https' -fL -o ./install-pdfconcat.sh https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh &&
  less ./install-pdfconcat.sh &&
  sh ./install-pdfconcat.sh
```

If curl is unavailable, use `wget --https-only -O ./install-pdfconcat.sh URL`
with the same URL and `&&` chain. The installer itself can use curl or wget and
requires standard shell tools, tar and a SHA-256 tool (`sha256sum`, `shasum` or
`openssl`). Use `less` to review the exact copy you will run.

On Windows, use Windows PowerShell 5.1 or PowerShell 7. Download the
installer to your home directory and display the saved script for review.

```powershell
& {
    $ErrorActionPreference = 'Stop'
    $installer = Join-Path $HOME 'install-pdfconcat.ps1'
    Invoke-WebRequest -UseBasicParsing -Uri https://github.com/resoltico/pdfconcat/releases/latest/download/install.ps1 -OutFile $installer
    Get-Content -LiteralPath $installer
}
```

After the download succeeds and you review the script, run it in a child session.
If you saved it elsewhere, substitute its full path here and in the examples
that reuse the saved installer.

```powershell
powershell -NoProfile -ExecutionPolicy ByPass -File (Join-Path $HOME 'install-pdfconcat.ps1')
if ($LASTEXITCODE -ne 0) { throw "Installer failed ($LASTEXITCODE)." }
```

Use `pwsh` in place of `powershell` to run the installer under PowerShell 7.
`ByPass` sets the child session's execution policy. Persistent settings stay
in place, and Group Policy takes precedence.

Keep the Unix `&&` chain or the equivalent download-success check in automation.
Make a nonzero installer exit fail the parent command as well.

Supported systems are macOS 15 or later on Apple Silicon and Intel, Linux on
x86-64 and ARM64, and Windows 10/11 on x64 and ARM64. Linux builds use
`CGO_ENABLED=0` and do not need a system C library; execution can still be limited
by the kernel or device policy. Native CPU selection also handles translated shells.

The default location is `$XDG_BIN_HOME/pdfconcat` when `XDG_BIN_HOME` is set,
otherwise `~/.local/bin/pdfconcat`. On Windows it is
`%LOCALAPPDATA%\Programs\PDFConcat\pdfconcat.exe`.
Choose another absolute folder with `--install-dir DIR` or `-InstallDir DIR`.

Open a new terminal after PATH changes, then verify:

```sh
pdfconcat --version
pdfconcat --help
```

For a quick functional check, run `pdfconcat build -o blank.pdf --blank --fit-to A4`.

The Windows command above runs in a child process, so its PATH changes do
not reach an already-open parent terminal. Running the script directly in the
current PowerShell session may also update that session's PATH. On macOS/Linux,
use the quoted full executable path printed by the installer while this terminal
keeps its current PATH, or open a new terminal after successful persistence.
`--no-modify-path` / `-NoModifyPath` leaves persistent and current-session PATH
unchanged.

## Update and choose a release

Repeat the latest download-and-install command to update an unchanged managed
copy. To pin the installer and payload together, choose a stable tag from
[Releases](https://github.com/resoltico/pdfconcat/releases) and replace `X.Y.Z`
with its version number. The download URL adds the tag's leading `v`.

```sh
version=X.Y.Z
curl --proto '=https' --proto-redir '=https' -fL -o ./install-pdfconcat.sh \
  "https://github.com/resoltico/pdfconcat/releases/download/v${version}/install.sh" &&
  less ./install-pdfconcat.sh
```

After a successful download and review, install the pinned release:

```sh
sh ./install-pdfconcat.sh --version "$version"
```

On Windows, set `$version` before downloading and reviewing the pinned installer:

```powershell
$version = 'X.Y.Z'
& {
    $ErrorActionPreference = 'Stop'
    $uri = "https://github.com/resoltico/pdfconcat/releases/download/v${version}/install.ps1"
    $installer = Join-Path $HOME 'install-pdfconcat.ps1'
    Invoke-WebRequest -UseBasicParsing -Uri $uri -OutFile $installer
    Get-Content -LiteralPath $installer
}
```

Once you have reviewed the successful download, run the pinned installer:

```powershell
powershell -NoProfile -ExecutionPolicy ByPass -File (Join-Path $HOME 'install-pdfconcat.ps1') -Version $version
if ($LASTEXITCODE -ne 0) { throw "Installer failed ($LASTEXITCODE)." }
```

Re-running these commands with the same pin reinstalls the selected release.

When no version is supplied, the installer resolves latest once to a stable
`vX.Y.Z` tag, then downloads both archive and `checksums.txt` from that release.
A failed download, checksum or candidate test happens before replacing the active
program. An update requires intact ownership state and every remaining managed
file to match its receipt. An intact receipt with a missing executable can be
repaired by reinstalling; remaining owned files must verify.

Alternatively, `PDFCONCAT_VERSION` supplies the release when no version argument
is given. Clear an environment pin to return to latest:

```sh
unset PDFCONCAT_VERSION
```

```powershell
Remove-Item Env:PDFCONCAT_VERSION -ErrorAction SilentlyContinue
```

Keep the reviewed installer with the matching release documentation for later
update or removal.

The installer stages files privately on the destination filesystem and uses a
per-product, per-directory exclusive lock. A held lock fails promptly; there are
no automatic lock retries. It attempts to restore the prior owned state if
activation fails. Errors describe any remaining state; an interruption or failed
PATH write is not a promise that nothing was installed. Inspect the reported
paths before trying again.

Unix PATH writes are part of activation: only
completed additions enter the active receipt. A failed transaction attempts to
restore the prior executable and receipt, including an executable's prior
absence. Rollback removes only this transaction's verified additions, preserving
later user content. If a partial write or concurrent edit prevents exact cleanup,
the diagnostic names retained staging evidence; keep it and inspect the reported
startup file before retrying. Later deliberate edits or removals of a successfully
recorded entry are preserved instead of silently re-added.

Unix uninstall and rollback preserve private recovery images before rewriting
startup files in place, retaining their symlink and hardlink identities. A failed
write or handled interruption attempts to restore only attributable changes.
When restoration is unsafe or fails, keep the reported staging directory: each
`rewrite.N` records the startup path and identity, exact `preimage` and intended
`postimage`, plus any captured `observed` contents and `restore-image`. These
files may contain private shell configuration. Inspect them privately against
the current startup file before restoring anything or removing recovery evidence;
a subsequent attempt refuses retained staging rather than guessing how to recover.

PowerShell records exact recovery paths before activation. Cancellation attempts
to undo only this transaction's files and new PATH entry; after a validated
commit, the new installation remains active. If restoration cannot be verified,
keep the retained transaction evidence named by the next attempt. Ctrl-C can
suppress console output, so a quiet cancellation is not proof of cleanup.
Uninstall and rollback preserve a user-edited PATH value type instead of deleting
that edited value merely because the install originally created it.

On Windows an update can retain one uniquely named, receipt-owned running
executable. Close processes using it before another update or uninstall if
cleanup is refused. The installer never adopts or deletes arbitrary `.old` files.

## Custom directory and PATH

Use the saved, reviewed installer with an absolute directory, for example:

```sh
sh ./install-pdfconcat.sh --install-dir "$HOME/my tools" --no-modify-path
```

```powershell
powershell -NoProfile -ExecutionPolicy ByPass -File (Join-Path $HOME 'install-pdfconcat.ps1') -InstallDir "$env:LOCALAPPDATA\My Tools" -NoModifyPath
if ($LASTEXITCODE -ne 0) { throw "Installer failed ($LASTEXITCODE)." }
```

Omit the no-PATH option to enable normal PATH setup. With it, use the full
executable path printed by the installer; neither persistent nor current-session
PATH changes. Reuse the same custom directory for update and uninstall.

## Ownership and recovery

Only the current receipt format and complete current file inventory are accepted.
An incompatible receipt is refused without changes; no conversion is provided.
Use its matching installer for removal, or choose a fresh directory.

The current installer uses `.pdfconcat-install` beside the executable
for ownership receipts and managed data. It verifies managed file hashes before
update or removal. It refuses unowned files by default and refuses changed
managed files. It does not adopt unowned installs from their filename, version output or PATH settings.

Never delete an entire shared bin directory, copy a receipt from another
directory or edit hashes to bypass an ownership refusal.

- A receiptless regular executable with no private installer state is
  refused by default. Explicit `--replace-unmanaged` / `-ReplaceUnmanaged` on
  install authorizes replacing only that executable. The installer reports its
  scope, verifies the new candidate and rechecks the target under its lock.
- The original executable is preserved in a unique recovery location in the
  install directory's parent, outside the bin directory being managed. Its exact
  path is printed. Keep it until you have verified the replacement. Normal
  uninstall does not delete it or treat it as another active tool that requires
  retaining a PATH entry. Pre-existing PATH entries and directories are not adopted.
- A valid receipt with only the managed executable missing can be repaired by
  re-running install, or cleaned up with uninstall. Remaining owned files must
  still verify. Repeating uninstall after complete cleanup is harmless.
- Changed managed files, corrupt or incomplete state, inaccessible files, and
  symlinks or other nonregular executable/state targets remain conservative refusals; the
  replacement option cannot bypass them. Inspect the exact path and cause in
  the diagnostic, preserve valuable files and use a fresh directory if needed.
- Unix directory aliases are accepted only when requested and recorded paths
  still resolve to the same physical directory. Windows accepts normalized
  spellings but refuses a reparse-point install directory. Moved receipts are not
  adopted; use the recorded path shown in a location-mismatch diagnostic.

For replacement commands and the choice to keep the existing executable, see
[existing executable without an installer receipt](#existing-executable-without-an-installer-receipt).

The flag is install-only, takes no stdin consent and does not authorize an
unmanaged uninstall. It does not execute the old executable to identify it.
If activation fails, the installer attempts to restore the original executable
and prior state, including prior absence. If restoration fails, keep all reported
recovery paths and inspect them before retrying; do not discard evidence based
on a successful earlier download. An operation reporting incomplete restoration
has not restored the prior installation.

Pre-existing directories may be shared with other tools. They are never removed
merely because they became empty. Unrelated files are preserved.

## Uninstall

Use the reviewed installer matching the installed release. If you used the
README quick install, get the release number from `pdfconcat --version` and
follow the matching [download and review steps](#update-and-choose-a-release).
Run the saved installer as shown below to remove an unchanged installation and
only the PATH entries it owns.

```sh
sh ./install-pdfconcat.sh --uninstall
```

```powershell
powershell -NoProfile -ExecutionPolicy ByPass -File (Join-Path $HOME 'install-pdfconcat.ps1') -Uninstall
if ($LASTEXITCODE -ne 0) { throw "Uninstall failed ($LASTEXITCODE)." }
```

Pass the same custom install directory used for installation, for example
`sh ./install-pdfconcat.sh --uninstall --install-dir '/absolute/my tools'` or
`powershell -NoProfile -ExecutionPolicy ByPass -File (Join-Path $HOME 'install-pdfconcat.ps1') -Uninstall -InstallDir 'C:\My Tools'`.
Removal does not need a download of the program. An offline saved copy of the installer can be
run with the same uninstall option.

Changed or unowned managed files are refused rather than silently deleted. For
manual cleanup, inspect the executable and its private `.pdfconcat-install`
directory, then remove only the files you intend to remove. An installer-created
directory is removed only if empty and never if it is a filesystem root.

Unix PATH cleanup uses the exact recorded startup path and generated line or Fish
entry for this install directory. It preserves other installations and user edits;
symlinked startup files remain symlinks. Windows removes only its exact added user
PATH entry, preserving raw text, empty segments, environment references and value
kind. A PATH entry can be conservatively retained with a warning if unrelated
files remain in the shared install directory; removing it could break other tools.
`NoModifyPath` also suppresses PATH cleanup during
uninstall and reports the entry left for manual removal.

Windows uninstall removes an owned PATH entry before deleting
the executable or receipt. A failed PATH write leaves those files intact for
retry. A later file-removal failure can leave partial progress; inspect the
reported paths before retrying or using the manual cleanup above.

Your PDFs, plans and reports are not installer-managed files.

## Options and mirrors

| macOS/Linux | Windows | Environment | Effect |
|---|---|---|---|
| `--version X.Y.Z` | `-Version X.Y.Z` | `PDFCONCAT_VERSION` | Choose a stable release; one leading `v` is allowed. |
| `--install-dir DIR` | `-InstallDir DIR` | `PDFCONCAT_INSTALL_DIR` | Choose an absolute directory. |
| `--no-modify-path` | `-NoModifyPath` | `PDFCONCAT_NO_MODIFY_PATH=1` | Do not change PATH or shell startup files. |
| `--replace-unmanaged` | `-ReplaceUnmanaged` | | Explicitly back up and replace a receiptless regular executable; install only. |
| `--uninstall` | `-Uninstall` | `PDFCONCAT_UNINSTALL=1` (Windows) | Remove unchanged owned files. |
| `--help` | `-Help` | | Show usage. |

Pass options to the saved, reviewed script as shown above. The Windows environment
variables also work when the script runs in the current PowerShell session.

`PDFCONCAT_DOWNLOAD_BASE` selects an HTTPS release mirror or an absolute local
directory containing the release's archive and `checksums.txt`. HTTPS is required
at every redirect hop. A mirror is explicitly trusted as the release source;
its checksums do not authenticate its publisher. Mirror URL bases must be directory
URLs without credentials, queries or fragments; use an absolute local release
directory if authenticated downloads are handled separately. A local snapshot archive can be tested without a stable release pin.

A PATH entry cannot represent a Unix colon or a Windows semicolon in a directory
name. The installer reports unsupported persistence rather than claiming PATH
was configured. Use the printed, correctly quoted full executable path or choose
another directory. Startup/registry write failures are reported.

## What the installer changes

- The executable at the selected install directory.
- The private `.pdfconcat-install` ownership directory. Its receipt is data, not
  an executable shell configuration file.
- An explicit receiptless replacement also leaves a recovery backup outside the
  install directory; its printed path is preserved by later uninstall.
- PDFConcat embeds its license notices (`pdfconcat licenses`) and schemas
  (`pdfconcat schema plan` / `pdfconcat schema result`); no external legal files
  are needed by the installed program.
- Only the exact startup entry or user PATH entry added by this installation.

The Unix lock is the sibling `.pdfconcat-install.lock`; a normal run removes its
own lock. Locks serialize cooperating installer operations, not hostile processes.
Do not remove a lock while another installer is running. The CLI does not require
installer receipts to run.

## Trust and verification

Both installer and program are code from this repository's release. SHA-256
verification detects damaged or incomplete downloads, not a compromised repository
or mirror. A candidate is executed as the current user only after checksum
verification. A receipt prevents accidental replacement/removal of other files;
it is not protection from a hostile process with your account's permissions.

To verify an asset's GitHub build provenance before running or extracting it,
substitute its local filename in this command. The installer performs its own
checksum check.

```sh
gh attestation verify ./install-pdfconcat.sh --repo resoltico/pdfconcat \
  --signer-workflow resoltico/pdfconcat/.github/workflows/release.yml
```

The scripts do not disable Gatekeeper, SmartScreen or managed-device policy.
Mac executables are ad hoc signed, without Developer ID or notarization; Windows
executables are unsigned. Browser downloads may carry internet markings and
system policy may block execution even after a matching checksum. Inspect the
candidate diagnostic shown by the installer before its temporary files are cleaned.

## Manual installation

For manual downloads, open [Releases](https://github.com/resoltico/pdfconcat/releases),
choose the native CPU archive on the matching release tag,
verify its entry in `checksums.txt`, then extract into a fresh directory. Keep the
archive's legal material with any copy you redistribute.

## Troubleshooting

If an ownership check fails, inspect the selected path and private directory.
Use the state-specific diagnosis under [ownership and recovery](#ownership-and-recovery);
a version string is not proof that this installer owns a file. If a lock remains after a crash,
first establish that no installer is using it before manual cleanup.

If the verified candidate cannot run, use its reported diagnostic to check the
CPU, minimum OS/runtime and device policy. Do not bypass checksum verification.
If PATH was not changed, use the quoted full path printed by the installer or
open a new terminal after successful persistence. For shell shadowing use
`command -v pdfconcat`; in PowerShell use `Get-Command pdfconcat` and remove a
conflicting installation using the method that installed it.

## Archive names

Choose the release tag first because the archive names are unversioned.

| System | Archive |
|---|---|
| macOS Apple Silicon | `pdfconcat_darwin_arm64.tar.gz` |
| macOS Intel | `pdfconcat_darwin_amd64.tar.gz` |
| Linux ARM64 | `pdfconcat_linux_arm64.tar.gz` |
| Linux x86-64 | `pdfconcat_linux_amd64.tar.gz` |
| Windows ARM64 | `pdfconcat_windows_arm64.zip` |
| Windows x64 | `pdfconcat_windows_amd64.zip` |

The native installers select the hardware CPU, including when the current shell
is translated. On a Mac, About This Mac shows a Chip entry for Apple Silicon and
an Intel Processor entry for Intel. Manual downloads must match that hardware.
