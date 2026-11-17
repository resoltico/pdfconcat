# SPDX-License-Identifier: AGPL-3.0-only
# Copyright (c) 2026 Ervins Strauhmanis
import importlib.util
from pathlib import Path
import shutil
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('verify_dependency', Path(__file__).with_name('verify-dependency.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
ROOT = Path(__file__).resolve().parents[2]


class DependencySourceTest(unittest.TestCase):
    def test_canonical_source(self):
        module.verify(ROOT)

    def test_missing_replacement_and_unpatched_vendor_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            shutil.copyfile(ROOT / 'go.mod', root / 'go.mod')
            shutil.copytree(ROOT / 'third_party/pdfcpu', root / 'third_party/pdfcpu')
            (root / 'go.mod').write_text('module synthetic\n')
            with self.assertRaisesRegex(RuntimeError, 'exact local pdfcpu replacement'):
                module.verify(root)
            shutil.copyfile(ROOT / 'go.mod', root / 'go.mod')
            with self.assertRaisesRegex(RuntimeError, 'lost the canonical patch'):
                module.verify(root, root / 'vendor')
            (root / 'third_party/pdfcpu/pkg/pdfcpu/read.go').write_text('// removed patch\n')
            with self.assertRaisesRegex(RuntimeError, 'Canonical pdfcpu patch source differs'):
                module.verify(root)
