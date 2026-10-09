#!/bin/sh

# Copyright 2026 The pdfcpu Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#	http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# eg: ./annotationsFile.sh pkg/testdata/annotTest.pdf /tmp/out

if [ $# -ne 2 ]; then
    echo "usage: ./annotationsFile.sh inFile outDir"
    exit 1
fi

f=${1##*/}
f1=${f%.*}
out=$2
out1=$out/${f1}_no_text_annots.pdf

pdfcpu annotations list $1 > $out/$f1.log 2>&1
if [ $? -ne 0 ]; then
    echo "annotation listing error: $1"
    exit 1
fi

pdfcpu annotations remove $1 $out1 Text >> $out/$f1.log 2>&1
if [ $? -ne 0 ]; then
    echo "annotation removal error: $1 -> $out1"
    exit 1
else
    echo "annotation removal success: $1 -> $out1"
fi
