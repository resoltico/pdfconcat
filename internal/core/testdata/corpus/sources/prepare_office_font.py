# SPDX-License-Identifier: MPL-2.0
# Copyright (c) 2026 Ervins Strauhmanis
"""Rename only font naming metadata for unambiguous office authoring selection.

The transient standalone derivative is not a corpus asset. Original Noto
copyright/license data and all outlines remain. See the export recipe.
"""
import argparse
from pathlib import Path
from fontTools.ttLib import TTFont
p=argparse.ArgumentParser();p.add_argument('output',type=Path);a=p.parse_args()
corpus=Path(__file__).resolve().parents[1]
f=TTFont(corpus.parents[2]/'typeset/fontdata/NotoSans-Regular.ttf')
for record in f['name'].names:
    names={1:'Corpus Noto Sans',2:'Regular',3:'Corpus Noto Sans Regular',4:'Corpus Noto Sans Regular',6:'CorpusNotoSans-Regular',16:'Corpus Noto Sans',17:'Regular'}
    if record.nameID in names:
        record.string=names[record.nameID].encode(record.getEncoding())
f.save(a.output)
