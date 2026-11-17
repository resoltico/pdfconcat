# SPDX-License-Identifier: AGPL-3.0-only
# Copyright (c) 2026 Ervins Strauhmanis
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('sbom_provenance', Path(__file__).with_name('sbom-provenance.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
ROOT = Path(__file__).resolve().parents[2]


class SBOMProvenanceTest(unittest.TestCase):
    def test_local_identity_and_dependency_references(self):
        original = {'components': [{'name': './third_party/pdfcpu', 'bom-ref': 'local', 'purl': 'pkg:golang/./third_party/pdfcpu?goarch=arm64&goos=darwin'}], 'dependencies': [{'ref': 'main', 'dependsOn': ['local']}]}
        actual = module.stamp(original, ROOT)
        component = actual['components'][0]
        self.assertTrue(component['modified'])
        self.assertTrue(component['version'].startswith('v0.16.4-pdfconcat.'))
        self.assertEqual(actual['dependencies'][0]['dependsOn'], [component['bom-ref']])
        self.assertIn('goarch=arm64&goos=darwin', component['purl'])
        self.assertEqual(original['components'][0]['bom-ref'], 'local')

    def test_missing_replacement_identity_refused(self):
        with self.assertRaisesRegex(RuntimeError, 'exactly one linked local'):
            module.stamp({'components': []}, ROOT)
