#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-only
# Copyright (c) 2026 Ervins Strauhmanis
"""Verify the canonical local reader patch and the exact source vendoring uses."""
import hashlib
import json
from pathlib import Path


def verify(root, vendor=None):
    root = Path(root)
    replacement = 'replace github.com/pdfcpu/pdfcpu => ./third_party/pdfcpu'
    if replacement not in (root / 'go.mod').read_text(encoding='utf-8').splitlines():
        raise RuntimeError('Missing exact local pdfcpu replacement; upstream cannot substitute for the reader patch.')
    fork = root / 'third_party/pdfcpu'
    record = json.loads((fork / 'PDFCONCAT-PATCH.json').read_text(encoding='utf-8'))
    if not record['modified'] or record['upstream_version'] != 'v0.16.4':
        raise RuntimeError('Local pdfcpu patch provenance is inconsistent.')
    for entry in record['files']:
        relative = Path(entry['path'])
        if relative.is_absolute() or '..' in relative.parts:
            raise RuntimeError('Unsafe dependency provenance path.')
        path = fork / relative
        if path.is_symlink() or hashlib.sha256(path.read_bytes()).hexdigest() != entry['sha256']:
            raise RuntimeError(f'Canonical pdfcpu patch source differs: {relative}')
        if vendor is not None and not relative.name.endswith('_test.go'):
            shipped = Path(vendor) / 'github.com/pdfcpu/pdfcpu' / relative
            if not shipped.is_file() or shipped.read_bytes() != path.read_bytes():
                raise RuntimeError(f'Vendored pdfcpu lost the canonical patch: {relative}')
    if not (fork / 'LICENSE.txt').is_file() or not (fork / 'PROVENANCE.md').is_file():
        raise RuntimeError('Missing dependency license or provenance.')


if __name__ == '__main__':
    verify(Path(__file__).resolve().parents[2])
