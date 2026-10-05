# Releasing

PDFConcat uses tagged GitHub releases and GoReleaser.

## Version source

Release version, commit, and commit date are injected into `cmd/pdfconcat` by GoReleaser ldflags. Local builds report `dev`, `none`, and `unknown` unless equivalent ldflags are supplied.

The release build uses `-trimpath`, the source commit timestamp for archive metadata, the commit date for embedded version metadata, and GoReleaser's module-proxy mode. These settings are intended to reduce build-path/time variability and keep dependency acquisition verifiable.

## Pre-release expectations

Before tagging a release on a development machine or CI environment:

1. review `CHANGELOG.md` and make the release narrative cumulative from the preceding normal release;
2. run the checks listed in [`CONTRIBUTING.md`](../CONTRIBUTING.md#checks) and commit any `go.mod`/`go.sum`/`tools` module changes they require;
3. verify macOS, Linux, and Windows CI is green;
4. verify `.goreleaser.yml` with the pinned GoReleaser tool (`goreleaser check` runs in CI);
5. confirm dependency/license notices still match the tidied application module graph and shipped binary obligations; and
6. confirm the release tag will remain available as corresponding source for the MPL-2.0 executable release.

Do not advertise unfinished or prerelease-only behavior as released capability.

## Tagging

Use semantic version tags:

```text
git tag -s vX.Y.Z -m "PDFConcat vX.Y.Z"
git push origin vX.Y.Z
```

The release workflow first calls the repository CI workflow as a release gate. Only after those checks pass does it invoke GoReleaser for macOS, Linux, and Windows amd64/arm64 artifacts and publish `checksums.txt`. GitHub's attestation action then consumes that checksum manifest so the released archive subjects receive build-provenance attestations.

## Corresponding source

PDFConcat is MPL-2.0. For every published executable release, the matching Git tag in `https://github.com/resoltico/pdfconcat` is the corresponding Source Code Form. Release notes and packaged `README.md` must continue to identify how recipients can obtain that source.

Do not delete or move a release tag/source in a way that would make the corresponding source unavailable to recipients of the executable release.

## Release contents

Each archive contains:

- the `pdfconcat` executable;
- `README.md`;
- `LICENSE`;
- `THIRD_PARTY_NOTICES.md`; and
- runtime third-party license texts.

Windows archives are ZIP; other targets use tar.gz.

The checksum manifest is published alongside the archives rather than embedded inside each archive.
