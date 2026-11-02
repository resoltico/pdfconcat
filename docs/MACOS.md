# Install, update and uninstall on macOS

Use PDFConcat setup to manage the installation on your Mac. It chooses the
download for your processor and installs the `pdfconcat` command at
`/usr/local/bin/pdfconcat`. You do not need Go, Homebrew or an Apple developer
account.

- [Before you start](#before-you-start)
- [Install PDFConcat](#install-pdfconcat)
- [Update PDFConcat](#update-pdfconcat)
- [Uninstall PDFConcat](#uninstall-pdfconcat)
- [Try the examples](#try-the-examples)
- [Troubleshooting](#troubleshooting)
- [What setup changes](#what-setup-changes)

## Before you start

You need macOS 15 or later, an administrator account on the Mac, and an internet
connection to install or update. Once installed, PDFConcat itself and its
uninstall helper work without an internet connection.

**PDFConcat uses ad hoc code signing and is not notarized by Apple.** Ad hoc
signing does not verify the developer's identity or mean that Apple has reviewed
the software. Install it only if you trust the
[PDFConcat project on GitHub](https://github.com/resoltico/pdfconcat). Setup
checks the release download against the project's checksum manifest; both come
from that same project. More detail is in [Security](../SECURITY.md#macos-setup-and-release-trust).

Setup uses one fixed installation location. It does not change your Terminal
settings or offer a second installation location. If your account cannot perform
administrator operations, ask the Mac's administrator to install PDFConcat from
their account. The command will then be available to other accounts with the
normal macOS Terminal configuration.

## Install PDFConcat

### 1. Open Terminal

Press **Command–Space**, type **Terminal**, then press **Return**. Keep this
window open while setup runs.

### 2. Download and start setup

Copy the entire block below, paste it into Terminal, then press Return. Do not
add `sudo` at the beginning: setup downloads as your ordinary account and asks
for administrator permission only when it needs to install files.

```sh
/bin/bash -c '
set -eu
pdfconcat_setup_dir=$(/usr/bin/mktemp -d)
pdfconcat_cleanup() { /bin/rm -rf -- "$pdfconcat_setup_dir"; }
trap pdfconcat_cleanup EXIT
/usr/bin/curl -q --fail --show-error --location --proto "=https" --proto-redir "=https" \
  --connect-timeout 15 --max-time 120 --retry 2 --retry-delay 2 \
  --output "$pdfconcat_setup_dir/setup.sh" \
  "https://raw.githubusercontent.com/resoltico/pdfconcat/main/install/macos.sh"
/bin/bash "$pdfconcat_setup_dir/setup.sh"
'
```

This downloads the complete setup script before starting it. If the download
fails, it does not run a partial script. The temporary download is removed when
the command finishes.

### 3. Choose installation and confirm

Type `1` and press Return to choose **Install or update PDFConcat**. Setup finds
the latest stable release, downloads the archive for your Mac, and checks that
it can run and reports the expected version. It shows the version and destination
before asking you to continue. Type `y` and press Return
to confirm. Pressing Return without typing `y` cancels. You can also choose
**3 — Cancel** at the main menu if you do not want to proceed.

When Terminal asks for your password, enter the login password for your
administrator account and press Return. **Nothing appears as you type, not even
dots.** That is normal. You may not be asked again if macOS recently accepted
administrator authorization in that Terminal session.

Wait for `Installed PDFConcat` followed by the version and command location.
If setup reports an error, use
the relevant [troubleshooting section](#troubleshooting); do not assume that a
download alone means installation succeeded.

### 4. Check the installed command

Run the installed command by its full path:

```sh
/usr/local/bin/pdfconcat version
```

PDFConcat should print its version information. This full path selects the
setup-managed executable even when an alias, shell function or another program
uses the name `pdfconcat`. Setup leaves those shell settings alone; opening a
new Terminal window may load them again. See [command discovery](#terminal-runs-a-different-copy)
if the bare name behaves differently.

To check that it can create a PDF, run these commands in a writable folder where
`pdfconcat-blank.pdf` does not already exist:

```sh
/usr/local/bin/pdfconcat check --blank --fit-to A4
/usr/local/bin/pdfconcat build -o pdfconcat-blank.pdf --blank --fit-to A4
```

The second command creates a one-page blank A4 PDF in that folder. You can keep
or delete this test PDF yourself. It is not part of the installation.

## Update PDFConcat

Repeat [Download and start setup](#2-download-and-start-setup), then choose
**1 — Install or update PDFConcat** again. This obtains the current setup tool and
the latest stable release. Review the version shown before confirming.

Setup prepares and verifies a complete replacement before selecting it as the
installed version. A download or verification failure does not replace your
working executable. It checks the existing managed files before changing them;
if those files were modified or unrelated files were added, it stops for review.

After a successful update, run `/usr/local/bin/pdfconcat version` again.
Updating the program does not update or remove your PDFs, plans or reports.
Let an existing PDFConcat task finish before updating so that you can identify
which version produced its output.

## Uninstall PDFConcat

Uninstalling removes the program, its installed documentation and examples, the
setup helper and the installation records. **Your own PDFs, JSON plans, reports
and copied examples are not removed.** Uninstall does not search your home folder
for those files.

1. Let any PDFConcat task finish, then open Terminal.
2. Copy and run this command:

   ```sh
   /bin/bash /usr/local/libexec/pdfconcat/current/setup.sh uninstall
   ```

3. Read the removal information, then type `y` and press Return to confirm.
   Pressing Return without typing `y` cancels. Enter your administrator password
   if requested; the characters will not appear as you type.
4. Wait for `Removed the PDFConcat installation managed by this setup.`

This uses the installed helper, so removal does not need a download or an
internet connection. The helper is removed along with the installation; its
path will no longer exist after successful removal.

To check whether the managed locations remain, run:

```sh
if [ -e /usr/local/bin/pdfconcat ] || [ -L /usr/local/bin/pdfconcat ] ||
   [ -e /usr/local/libexec/pdfconcat ] || [ -L /usr/local/libexec/pdfconcat ]; then
  printf 'An installation path remains. Keep the setup result for review.\n'
else
  printf 'The managed installation is absent.\n'
fi
```

This checks the installation's paths, including dangling links. Setup leaves the
shared `/usr/local/bin` directory in place. A bare `pdfconcat` command can still
invoke an alias, function or another executable after successful removal. Use
[command discovery](#terminal-runs-a-different-copy) to identify it; its presence
does not establish that the managed installation remains.

If setup finds modified files, unknown files or a foreign command link, it stops
instead of deleting them. Follow [the preservation message below](#setup-reports-modified-or-unexpected-files).
If only the program file is missing, setup can still remove its other files.

### If the installed uninstall helper is missing

Run the [download command](#2-download-and-start-setup) again and choose
**2 — Uninstall PDFConcat**. That recovery step needs the internet. The current
setup tool checks any remaining installation; it reports an absent installation
or refuses a state it cannot safely identify. Do not delete a directory merely
because it has `pdfconcat` in its name.

## Try the examples

The release's documentation, schemas and example plans are installed beneath:

```text
/usr/local/libexec/pdfconcat/current/payload
```

Treat those installed files as read-only. Copy examples to your own working
folder before changing them or building their plans: relative output paths in
a plan are resolved beside that plan.

To try the generated-only plan without source PDFs:

1. In Finder, choose **Go → Go to Folder** and enter
   `/usr/local/libexec/pdfconcat/current/payload`.
2. Copy the `examples` folder to a new, writable working folder of your own.
   Keep the folder structure intact.
3. In Terminal, type `cd `, including the space, drag that working folder from
   Finder into Terminal, and press Return. The `examples` folder should now be
   directly inside the current directory.
4. Run:

   ```sh
   /usr/local/bin/pdfconcat check --plan examples/plans/generated-only.json
   /usr/local/bin/pdfconcat build --plan examples/plans/generated-only.json
   ```

The plan creates `Dividers.pdf` beside the copied plan, containing two text
pages and three blank pages. If that output already exists, choose a fresh
working folder or deliberately allow replacement with `--overwrite`.

The [examples guide](PLAN.md#examples) describes the other plans and their input
requirements. Your working copies remain when PDFConcat is updated or removed.

## Troubleshooting

### Terminal says command not found

If the bare name `pdfconcat` is not found, run the installed command by its full
path:

```sh
/usr/local/bin/pdfconcat version
```

If this works, the installation is present. Your shell's command search settings
do not select it. This can happen with a custom shell or PATH configuration.
Setup leaves those settings alone. The full path above remains usable while you
review that configuration with whoever maintains it.

If the full path also fails, keep the exact error message. Repeat setup to check
or repair its managed installation; do not add unrelated folders to PATH or
disable macOS protections to hide the error.

### Terminal runs a different copy

If `pdfconcat version` reports `ERROR Unknown option: version`, or help describes
a different interface such as BLK, pdfcpu or Ghostscript commands, inspect what
your current shell selects. Those messages alone do not identify a PDFConcat
release or establish that setup failed. In the same Terminal, run:

```sh
type -a pdfconcat
/usr/local/bin/pdfconcat version
/usr/local/bin/pdfconcat --help
```

The setup-managed command is `/usr/local/bin/pdfconcat`. `type -a` lists matching
aliases, shell functions and executable paths in that shell; compare them with
the full-path version and help when the managed installation exists. Setup runs
in a separate Bash process, which cannot determine every alias or function in
your calling shell. A new Terminal window can load the same definitions again.

After uninstall, the full path should be absent while `type -a` may still list
other definitions. Likewise, output from `command -v pdfconcat` can describe an
alias or function rather than a file path. Neither command establishes whether
the managed installation remains; check its fixed paths as described above.

Use the full path to work with the managed program. If you want to change a
different command, first identify who maintains it and how it was installed;
use that method's instructions. Setup does not edit shell definitions or
overwrite or uninstall software it cannot identify as its own.

### Setup found an existing installation it does not own

Setup may find a manually copied executable, a package-manager link or another
file at its destination. It leaves that file unchanged and reports its path.

If you previously installed PDFConcat another way, remove that installation
using its own instructions, then run this setup again. If you do not know how
the file arrived, ask the Mac's administrator or contact the project before
removing it. Do not use a blanket `sudo rm` command to clear the destination.

### Setup reports modified or unexpected files

The setup tool checks its installation records before an update or removal.
A file changed after installation, an unexpected link, or an unrelated file
inside the managed directory can cause it to stop and preserve the files.

Keep the complete error and the named paths. If you placed your own work inside
the managed directory, copy that work to your normal working folder before
planning any cleanup. Do not edit the installation records to make the check
pass. Ask for help if ownership of the remaining files is unclear.

### Password or administrator permission fails

Use the password for the administrator account that is running Terminal, not a
GitHub or Apple Account password. Nothing appears while typing it. If your
account is not allowed to perform administrator operations, ask the Mac's
administrator to install or remove PDFConcat from their account.

On a managed work or school Mac, ask the administrator about the applicable
policy. Repeatedly changing permissions or running the entire download as root
does not resolve that policy.

### A download or verification fails

Check your internet connection and the reported GitHub URL, then try setup
again. Setup will not install a partial download or an archive that does not
match the release checksum. A persistent mismatch should be reported to the
project with the version and error message; do not bypass verification.

### Another setup is already running

Let the existing install, update or uninstall finish, then retry. Do not delete
the setup lock file to force access. If the message persists after all setup
windows have finished, keep the exact error and ask for help.

### macOS refuses to run the command

Record the exact message and check that you used the documented setup command.
Ad hoc signing is not Apple notarization and does not override managed-device
or local execution policies. Setup does not remove quarantine attributes or
change Gatekeeper or System Integrity Protection settings.

Ask the Mac's administrator or the project about the specific failure. Do not
turn off system protections as a general installation step.

## What setup changes

| Location | Purpose and removal behavior |
| --- | --- |
| `/usr/local/bin/pdfconcat` | Command link to the selected managed executable; removed on successful uninstall. |
| `/usr/local/libexec/pdfconcat` | Verified release files, installed setup helper and ownership records; verified managed contents are removed on successful uninstall. |
| `/var/run/pdfconcat-setup.lock` | Empty coordination file used during setup. It may remain after setup or uninstall and is normally cleared at restart. Its presence does not mean setup is still running. |

Setup may create missing parent directories, but it preserves existing parent
ownership and permissions and leaves shared parent directories in place.
Do not remove the coordination file while another setup process could be using
it. It contains no user documents or configuration.

Setup uses private temporary folders for downloads and `/private/tmp/pdfconcat-stage.*`
for staging. It removes them on normal exit. A forced stop can leave temporary
files outside the managed installation; uninstall does not search for them,
and macOS temporary-file cleanup has no promised schedule.

Updates select a complete verified installation through the `current` link.
An interruption can leave a verified earlier generation; a later successful
update or uninstall checks such copies before cleaning them up. This is not a
promise of recovery from every filesystem or power failure.

Setup does not edit `.zprofile`, `.zshrc`, other shell startup files or system
PATH settings. It does not create a background updater, schedule tasks or change
your PDF file associations. PDFs, plans, reports, downloaded archives and examples
you copied outside the managed installation remain your files to keep or delete.

For developer verification and the remaining checks on a fresh Mac, see
[macOS setup verification](../CONTRIBUTING.md#macos-setup-verification).
