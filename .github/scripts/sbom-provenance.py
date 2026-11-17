#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-only
# Copyright (c) 2026 Ervins Strauhmanis
"""Run the pinned SBOM generator and identify the linked local reader honestly."""
import hashlib
import json
from pathlib import Path
import runpy
import subprocess
import sys


def stamp(document, root):
    root = Path(root)
    runpy.run_path(str(root / '.github/scripts/verify-dependency.py'))['verify'](root)
    record_bytes = (root / 'third_party/pdfcpu/PDFCONCAT-PATCH.json').read_bytes()
    identity = hashlib.sha256(record_bytes).hexdigest()
    candidates = [c for c in document.get('components', [])
                  if 'third_party/pdfcpu' in c.get('purl', '')
                  or c.get('name') in ('./third_party/pdfcpu', 'github.com/pdfcpu/pdfcpu')
                  or (c.get('name') == 'pdfcpu' and 'third_party' in c.get('group', ''))]
    if len(candidates) != 1:
        raise RuntimeError('SBOM must expose exactly one linked local pdfcpu replacement.')
    component = candidates[0]
    old_ref = component['bom-ref']
    version = 'v0.16.4-pdfconcat.' + identity[:12]
    old_purl = component.get('purl', '')
    qualifiers = '?' + old_purl.split('?', 1)[1] if '?' in old_purl else ''
    new_ref = 'pkg:golang/github.com/pdfcpu/pdfcpu@' + version + qualifiers

    def replace(value):
        if isinstance(value, dict):
            return {key: replace(child) for key, child in value.items()}
        if isinstance(value, list):
            return [replace(child) for child in value]
        return new_ref if value == old_ref else value

    document = replace(document)
    component = next(c for c in document['components'] if c['bom-ref'] == new_ref)
    component.pop('hashes', None)  # No released upstream archive hash identifies this modified source.
    component.update({'name': 'pdfcpu', 'group': 'github.com/pdfcpu', 'version': version,
                      'purl': new_ref, 'modified': True,
                      'licenses': [{'expression': 'Apache-2.0 AND MIT AND AGPL-3.0-only'}]})
    component['properties'] = component.get('properties', []) + [
        {'name': 'pdfconcat:upstream-version', 'value': 'v0.16.4'},
        {'name': 'pdfconcat:source-path', 'value': 'third_party/pdfcpu'},
        {'name': 'pdfconcat:patch-manifest-sha256', 'value': identity},
        {'name': 'pdfconcat:source-delivery', 'value': 'SOURCE.tar.gz; canonical replacement and verified patched vendor source'},
        {'name': 'pdfconcat:license-notices', 'value': 'THIRD_PARTY_NOTICES.md; third_party/pdfcpu/LICENSE.txt'},
    ]
    return document


if __name__ == '__main__':
    root = Path(__file__).resolve().parents[2]
    arguments = sys.argv[1:]
    subprocess.run([str(root / '.tools/bin/cyclonedx-gomod'), *arguments], check=True)
    output = Path(arguments[arguments.index('-output') + 1])
    document = stamp(json.loads(output.read_text(encoding='utf-8')), root)
    output.write_text(json.dumps(document, ensure_ascii=False, sort_keys=True, indent=2) + '\n', encoding='utf-8')
