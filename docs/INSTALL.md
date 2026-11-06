# Install, update and uninstall

PDFConcat is one self-contained program. The installer downloads it from this
project's GitHub releases, checks it and puts it in a folder in your user
account without an administrator password. The program needs no Go, Homebrew,
external PDF engine or font installation. The macOS/Linux installer uses standard
shell tools, `tar`, `curl` or `wget`, and a SHA-256 tool (`sha256sum`, `shasum` or
`openssl`). Windows uses PowerShell's download, archive and checksum commands.

- [macOS and Linux](#macos-and-linux)
- [Windows](#windows)
- [Update](#update)
- [Uninstall](#uninstall)
- [Options](#options)
- [What the installer changes](#what-the-installer-changes)
- [Trust and verification](#trust-and-verification)
- [Manual installation](#manual-installation)
- [Troubleshooting](#troubleshooting)

## macOS and Linux

Open Terminal and run:

```sh
curl -fsSL https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh | sh
```

If `curl` is missing, use `wget -qO- https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh | sh`.

The installer identifies the platform, downloads the matching archive, verifies
its checksum and test-runs the program before installation. It prints the selected
program path and any PATH command needed for the current terminal.

Then check it:

```sh
pdfconcat version
```

Supported systems: macOS 15 or later on Apple silicon or Intel, and Linux on
x86-64 or arm64. Linux archives are built with `CGO_ENABLED=0` and require no
system C library. The installer test-runs the binary before replacing an installed
copy; static linking does not guarantee compatibility with every kernel or device
policy.

## Windows

Open PowerShell (press Start, type **PowerShell**, press Enter) and run:

```powershell
powershell -ExecutionPolicy ByPass -c "irm https://github.com/resoltico/pdfconcat/releases/latest/download/install.ps1 | iex"
```

`-ExecutionPolicy ByPass` applies to the launched PowerShell session; it does not
change persistent settings or override Group Policy. The installer shows the
same steps as on macOS and Linux. Open a new terminal window afterwards, then check it:

```powershell
pdfconcat version
```

Supported systems: Windows 10 and 11 on x64 or Arm64.

## Update

Run the installation command again. Without a version override it downloads the
latest release, checks it and only then replaces the installed program, so a
failed download leaves your working copy in place. It reports the installed
version and selected path.
A running `pdfconcat` process continues using the executable it started with.

To install a specific release instead of the latest, pass its version:

```sh
curl -fsSL https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh | sh -s -- --version 0.9.0
```

```powershell
$env:PDFCONCAT_VERSION = '0.9.0'; irm https://github.com/resoltico/pdfconcat/releases/latest/download/install.ps1 | iex
```

An environment-variable version pin applies to subsequent installer commands in
that session. To return to the latest release, clear it first:

```sh
unset PDFCONCAT_VERSION
```

```powershell
Remove-Item Env:PDFCONCAT_VERSION -ErrorAction SilentlyContinue
```

## Uninstall

Uninstalling removes the program and the PATH setting the installer added.
Your PDFs, plans and reports are not touched.

On macOS and Linux:

```sh
curl -fsSL https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh | sh -s -- --uninstall
```

On Windows:

```powershell
powershell -ExecutionPolicy ByPass -c "& ([scriptblock]::Create((irm https://github.com/resoltico/pdfconcat/releases/latest/download/install.ps1))) -Uninstall"
```

If you installed into a custom folder, add `--install-dir DIR` (macOS and
Linux) or `-InstallDir DIR` (Windows) to the uninstall command too.

Without an internet connection, uninstall by hand:

- **macOS and Linux:** delete `pdfconcat` from the selected install directory:
  `XDG_BIN_HOME` when set, otherwise `~/.local/bin`, unless you supplied a custom
  directory. Remove only PATH lines ending in `# added by the PDFConcat installer`
  from the shell files it uses: `${ZDOTDIR:-$HOME}/.zshrc` for zsh; `~/.bashrc` and
  the selected existing `~/.bash_profile`, `~/.bash_login` or `~/.profile` for bash;
  `~/.profile` for other POSIX shells. For Fish, remove
  `${XDG_CONFIG_HOME:-$HOME/.config}/fish/conf.d/pdfconcat.fish` only when its first
  line is the PDFConcat installer marker.
- **Windows:** delete only `pdfconcat.exe` and an installer-created
  `pdfconcat.exe.old` from the selected install directory (by default,
  `%LOCALAPPDATA%\Programs\PDFConcat`). Remove the directory only when empty.
  When that folder is no longer needed, remove its user PATH entry through
  **Edit environment variables for your account → Path**. Preserve unrelated
  files and PATH entries in a shared custom directory.

## Options

| macOS and Linux | Windows | Environment variable | Effect |
| --- | --- | --- | --- |
| `--version X.Y.Z` | `-Version X.Y.Z` | `PDFCONCAT_VERSION` | Install that release instead of the latest. |
| `--install-dir DIR` | `-InstallDir DIR` | `PDFCONCAT_INSTALL_DIR` | Install into `DIR`. |
| `--no-modify-path` | `-NoModifyPath` | `PDFCONCAT_NO_MODIFY_PATH=1` | Leave PATH and shell startup files alone. |
| `--uninstall` | `-Uninstall` | `PDFCONCAT_UNINSTALL=1` (Windows only) | Remove PDFConcat. |
| `--help` | `-Help` | | Show the options. |

Pass macOS and Linux options after `sh -s --`, as in the examples above. On
Windows, set the environment variable before `irm ... | iex`, or pass options
through the script block form shown under [Uninstall](#uninstall).

`PDFCONCAT_DOWNLOAD_BASE` downloads release files from another location instead
of GitHub: an `https://` mirror or an absolute local folder holding the release's
archives and `checksums.txt`. The checksum check still applies.

## What the installer changes

| | macOS and Linux | Windows |
| --- | --- | --- |
| Program | `~/.local/bin/pdfconcat`, or `$XDG_BIN_HOME/pdfconcat` when `XDG_BIN_HOME` is set | `%LOCALAPPDATA%\Programs\PDFConcat\pdfconcat.exe` |
| PATH | If missing from PATH, marked entries in the login shell's startup files; `ZDOTDIR` and `XDG_CONFIG_HOME` overrides are honored. Fish uses its `conf.d/pdfconcat.fish` fragment. | If the folder is not already on PATH, it is added to the start of your user PATH. |

For bash, the installer updates both `~/.bashrc` and one login startup file.
It also creates a Fish fragment when Fish is the login shell or its configuration
directory exists. Each POSIX shell entry only adds the folder when missing and
ends with `# added by the PDFConcat installer`; the Fish fragment starts with
that marker. Uninstall uses these markers to identify its entries. Reinstalling
does not duplicate entries. Startup files that are symbolic links stay links.

Nothing else is changed: no administrator settings, background updater,
scheduled task or file association. Downloads go to a temporary folder that is
deleted when the installer finishes. The installed program carries its own
license notices (`pdfconcat licenses`) and schemas (`pdfconcat schema plan` and
`pdfconcat schema result`). The documentation and example plans are in this
repository at each release's tag.

## Trust and verification

The installer and the program both come from this project's GitHub releases,
over HTTPS. Before installing, the installer checks the download against the
SHA-256 checksums published with the same release, then runs the downloaded
program once to confirm that it works on your system and reports the expected
version. A checksum mismatch stops the installation. Because the checksums come
from the same place as the download, this catches damaged or incomplete
downloads, not a compromised release.

To check where a release came from, use GitHub's
[artifact attestations](https://docs.github.com/actions/security-for-github-actions/using-artifact-attestations):
every archive, `checksums.txt`, both installers and the SBOMs carry build
provenance from this repository's release workflow. With the
[GitHub CLI](https://cli.github.com/):

```sh
gh attestation verify pdfconcat_linux_amd64.tar.gz --repo resoltico/pdfconcat
```

Each release also publishes a [CycloneDX](https://cyclonedx.org/) software bill
of materials per executable, such as `pdfconcat_linux_amd64.cdx.json`, listing
the Go modules linked into it with their versions, hashes and detected licenses.

To read the installer before running it, download it, inspect it, then run the
saved copy:

```sh
curl -fsSLO https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh
less install.sh
sh install.sh
```

**Code signing.** The macOS program has an ad hoc signature, which Apple silicon
requires, but it is not signed with an Apple Developer ID or notarized by Apple.
The Windows program is not code-signed. The installer does not disable Gatekeeper,
SmartScreen or other operating-system protections. Download markings and managed
policies can affect execution; browser downloads commonly carry an internet mark.
See [Manual installation](#manual-installation). Windows 11 Smart App Control,
when enabled, may block unsigned programs such as PDFConcat.

See also [Security](../SECURITY.md#installation-and-release-trust).

## Manual installation

Every release has archives with stable names, so these links always point to the
latest release:

| System | Archive |
| --- | --- |
| macOS, Apple silicon | [pdfconcat_darwin_arm64.tar.gz](https://github.com/resoltico/pdfconcat/releases/latest/download/pdfconcat_darwin_arm64.tar.gz) |
| macOS, Intel | [pdfconcat_darwin_amd64.tar.gz](https://github.com/resoltico/pdfconcat/releases/latest/download/pdfconcat_darwin_amd64.tar.gz) |
| Linux, x86-64 | [pdfconcat_linux_amd64.tar.gz](https://github.com/resoltico/pdfconcat/releases/latest/download/pdfconcat_linux_amd64.tar.gz) |
| Linux, arm64 | [pdfconcat_linux_arm64.tar.gz](https://github.com/resoltico/pdfconcat/releases/latest/download/pdfconcat_linux_arm64.tar.gz) |
| Windows, x64 | [pdfconcat_windows_amd64.zip](https://github.com/resoltico/pdfconcat/releases/latest/download/pdfconcat_windows_amd64.zip) |
| Windows, Arm64 | [pdfconcat_windows_arm64.zip](https://github.com/resoltico/pdfconcat/releases/latest/download/pdfconcat_windows_arm64.zip) |

Download the archive and [checksums.txt](https://github.com/resoltico/pdfconcat/releases/latest/download/checksums.txt)
into the same folder, then check the archive from that folder:

```sh
# macOS
shasum -a 256 --check --ignore-missing checksums.txt
# Linux
sha256sum --check --ignore-missing checksums.txt
```

```powershell
# Windows: compare this with the archive's line in checksums.txt
(Get-FileHash .\pdfconcat_windows_amd64.zip -Algorithm SHA256).Hash.ToLower()
```

Extract the archive. It holds the program, `pdfconcat` or `pdfconcat.exe`, and
its license notices. Move the program into a folder on your PATH, or run it by
its full path; `pdfconcat licenses` prints the same notices.

Browser downloads commonly carry an internet mark:

- **macOS** may refuse to open a browser-downloaded PDFConcat because it is not
  notarized. After checking the checksum, clear the mark from the extracted
  program with `xattr -d com.apple.quarantine pdfconcat`. Alternatively, run it
  once, then choose **Open Anyway** in **System Settings → Privacy & Security**.
- **Windows** may show a SmartScreen warning. After checking the checksum, run
  `Unblock-File .\pdfconcat.exe` in PowerShell.

The installer automates download verification and installation; system policies
can still block execution.

## Troubleshooting

### `pdfconcat` is not found after installing

Open a new terminal window: PATH changes apply to terminals started after the
installation. On macOS and Linux you can instead run the `export PATH=...` line
the installer printed. If a new window still cannot find it, your shell may read
a startup file the installer did not edit. Add
`export PATH="$HOME/.local/bin:$PATH"` for the default directory. For an XDG or
custom directory, use the selected path and command printed by the installer.
You can also run that executable by its full path.

### A different `pdfconcat` runs

Another copy earlier on your PATH takes precedence; the installer warns when it
sees one. Find it with `command -v pdfconcat` (macOS and Linux) or
`Get-Command pdfconcat` (PowerShell), then remove that copy using the method
that installed it.

### The download fails

Check your internet connection and that
[github.com/resoltico/pdfconcat/releases](https://github.com/resoltico/pdfconcat/releases)
opens. On macOS/Linux, export lowercase `https_proxy` with your proxy URL; both
[`curl`](https://curl.se/libcurl/c/libcurl-env.html) and
[`wget`](https://www.gnu.org/software/wget/manual/html_node/Proxies.html) support it.
The documented Windows command uses Windows PowerShell's system proxy settings.
On networks that block GitHub, place the release files on an HTTPS mirror or in
an absolute local folder and point `PDFCONCAT_DOWNLOAD_BASE` at it.

### The checksum does not match

Run the installer again. If it still fails, do not work around the check;
[open an issue](https://github.com/resoltico/pdfconcat/issues) with the version
and the installer's output.

### The downloaded program does not run on this system

The installer tests the program before installing it and reports this when it
fails. Run the command it shows to see the reason. Common causes are a macOS
version older than 15, or a managed computer whose policy blocks programs that
are not notarized or signed. Ask the computer's administrator about such a
policy rather than disabling system protections.

### PowerShell refuses a saved installer script

When running a saved `install.ps1`, inspect the effective policies with
`Get-ExecutionPolicy -List`. Script-file restrictions differ from executing the
downloaded command text in the [Windows installation command](#windows).
Process-level `-ExecutionPolicy Bypass` does not change persistent settings and
cannot override Group Policy. Follow your organization's policy on managed
devices. See [PowerShell execution policies](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_execution_policies?view=powershell-5.1).
