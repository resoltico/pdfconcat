#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-only
# Copyright (c) 2026 Ervins Strauhmanis
"""Capture stable tracked working files and vendored modules for a binary release."""

import gzip
import io
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile


def git(root, *args):
    return subprocess.check_output(["git", *args], cwd=root)


def create_archive(root, output):
    if git(root, "ls-files", "--others", "--exclude-standard", "-z"):
        raise RuntimeError("Untracked source files exist; add intended files to Git's index before packaging.")
    entries = []
    index = git(root, "ls-files", "--stage", "-z")
    for entry in index.split(b"\0"):
        if not entry:
            continue
        meta, name = entry.split(b"\t", 1)
        mode, _, stage = meta.split()
        if stage != b"0" or mode not in (b"100644", b"100755"):
            raise RuntimeError("Source packaging requires resolved, regular tracked files.")
        entries.append((os.fsdecode(name), int(mode[-3:], 8)))
    timestamp = int(git(root, "show", "-s", "--format=%ct", "HEAD"))
    with tempfile.TemporaryDirectory(prefix="pdfconcat-source-") as directory:
        source = Path(directory) / "pdfconcat-source"
        source.mkdir()
        for name, mode in entries:
            original, target = root / name, source / name
            if original.is_symlink() or not original.is_file():
                raise RuntimeError(f"Tracked source is missing or not regular: {name}")
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(original, target)
            target.chmod(mode)
        subprocess.run(["go", "mod", "vendor", "-o", str(source / "vendor")], cwd=root, check=True)
        for name, _ in entries:
            if (root / name).read_bytes() != (source / name).read_bytes():
                raise RuntimeError(f"Source changed during capture: {name}")
        if git(root, "ls-files", "--others", "--exclude-standard", "-z"):
            raise RuntimeError("Untracked files appeared during source capture.")
        if git(root, "ls-files", "--stage", "-z") != index:
            raise RuntimeError("Git index changed during source capture.")
        output.parent.mkdir(parents=True, exist_ok=True)
        # Publish only a complete deterministic archive. No host paths, wall
        # clock times, user/group IDs or local file ownership are recorded.
        with tempfile.NamedTemporaryFile(dir=output.parent, delete=False) as staged:
            staged_path = Path(staged.name)
        try:
            with staged_path.open("wb") as raw:
                with gzip.GzipFile(filename="", fileobj=raw, mode="wb", mtime=timestamp) as compressed:
                    with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as archive:
                        for path in sorted(source.rglob("*")):
                            if not path.is_file():
                                continue
                            if path.is_symlink():
                                raise RuntimeError(f"Source archive contains a symbolic link: {path}")
                            data = path.read_bytes()
                            info = tarfile.TarInfo(str(path.relative_to(source.parent)))
                            info.size, info.mtime = len(data), timestamp
                            info.mode = 0o755 if path.stat().st_mode & 0o111 else 0o644
                            archive.addfile(info, io.BytesIO(data))
            os.replace(staged_path, output)
        finally:
            staged_path.unlink(missing_ok=True)


if __name__ == "__main__":
    repository = Path(__file__).resolve().parents[2]
    create_archive(repository, repository / ".tools" / "SOURCE.tar.gz")
