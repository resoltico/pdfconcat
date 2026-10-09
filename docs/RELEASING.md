# Releasing

The source-release workflow publishes tagged source and changelog notes. Executable distribution uses the separate configured packaging path and requires verification against its final candidate. The publication status of each release is recorded in [`CHANGELOG.md`](../CHANGELOG.md); configured workflows and archives do not establish published assets or native execution.

## Before tagging

1. Set the intended version in `internal/app/version.txt` and add its dated section to `CHANGELOG.md`, retaining `Unreleased`. Describe the net release outcome rather than implementation history.
2. Run the baseline checks in [`CONTRIBUTING.md`](../CONTRIBUTING.md#gates), including the secret scan. Review the staged inventory so ignored build outputs, local credentials, work bills and execution evidence do not enter the source commit.
3. Run `go run ./tools/qualitygate release-version -tag vX.Y.Z` and extract notes with `go run ./tools/qualitygate release-notes -tag vX.Y.Z -output /outside/checkout/release-notes.md`. The output path must be new. Check the actual notes; the matching changelog section must exist exactly once and contain release content.
4. Push the final candidate commit and wait for its baseline CI to pass. Workflow configuration alone is not hosted execution evidence. Fuzzing, mutation testing, mutation negative controls and scale acceptance are separate manually dispatched campaigns; their deferral must not be described as passing them.
5. Confirm notices and license texts are current and that the release tag will remain available as corresponding source.

## Tagging and publication

The version file contains a canonical Semantic Version without a `v` prefix or build metadata; a prerelease suffix is allowed. Major, minor and patch fit unsigned 64-bit integers, matching the packaging parser. The source version is limited to 175 ASCII characters so a snapshot archive name fits a 255-byte filename component even with the longest target suffix and a 40-character Git abbreviation. Its producer identity also fits the report format.

The file lives beside the application code so ordinary `go build` can embed it directly. Release tooling reads the same file. A general project configuration adds parsing without another required setting; a root version file requires a separate embedding package because Go cannot embed a parent path.

Create an annotated tag on the verified candidate; use `-s` instead of `-a` when a signing key is configured:

```text
git tag -a vX.Y.Z -m "PDFConcat vX.Y.Z"
git push origin vX.Y.Z
```

The tag workflow runs baseline CI on the triggering commit, validates the tag against the version file, and extracts the release notes from `CHANGELOG.md`. Before publishing, it checks that the remote tag still resolves to that same commit. It creates the release with `--verify-tag` and `--notes-file`, without asset arguments. It does not invoke GoReleaser or upload application packages. Only the publication job needs `contents: write`; checks use read permissions and checkout does not persist credentials. No personal access token is stored as a repository secret.

The notes are the matching version section of `CHANGELOG.md`, not GitHub-generated commit notes or a separate maintained release narrative. The extractor rejects missing or duplicate sections and empty notes.

## Version metadata

Application, plan, and output-format versions identify different contracts. The application version comes from `internal/app/version.txt`; the plan version comes from `internal/plan.Version`; the shared response and saved-report `format_version` comes from `internal/report.Version`. They need not have matching numbers. Published `v0.1.0` used plan version 1 and `report_version: 1`; the next output contract uses `format_version: 2`, while plans remain version 1.

Allocate a new format number for an incompatible change to a published contract, including a published prerelease. Consolidate changes made before that next contract is published under its one pending number. Development iterations do not consume format numbers. Keep published tags, reports, and release history unchanged; an unsupported historical report is evidence to preserve, not a file to relabel or migrate.

Ordinary source builds embed `internal/app/version.txt` and retain the Go toolchain's VCS revision, commit time and dirty-tree marker. `pdfconcat version` prints this metadata. A plain source archive without `.git` has unknown commit provenance; that is expected and does not change the configured application version. Module pseudo-versions do not replace the application version.

GoReleaser snapshots derive their version from the same file and append `-SNAPSHOT-` plus Git's short commit. The packaging build sets `main.version` at link time and retains recorded linker arguments; it deliberately omits `-trimpath` because Go otherwise suppresses those arguments. Executables can contain compiler source/build paths, so byte reproducibility also requires a stable checkout path.

`qualitygate archives` inspects all six packaging targets, checks checksums and the explicit runtime-content inventory, executable headers, source/toolchain/module metadata and the consumed linker version. It executes only the native host target. `qualitygate release-bytes` can compare a binary draft's actual remote bytes with verified local artifacts. These controls must be exercised against the final candidate before executable publication; configured or cross-compiled targets do not establish native behavior.

## Corresponding source

The application is MPL-2.0; third-party runtime components and development tooling retain their own licenses. Keep `LICENSE`, `THIRD_PARTY_NOTICES.md`, `third_party/licenses/` and development-tool license notices available in the source repository. A published executable must identify the matching source tag at `https://github.com/resoltico/pdfconcat`; do not delete or move published source tags.
