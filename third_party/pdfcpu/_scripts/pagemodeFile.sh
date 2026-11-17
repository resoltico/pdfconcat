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

# eg: ./pagemodeFile.sh pkg/testdata/go.pdf /tmp/out

if [ $# -ne 2 ]; then
    echo "usage: ./pagemodeFile.sh inFile outDir"
    exit 1
fi

f=${1##*/}
f1=${f%.*}
out=$2
out1=$out/${f1}_mode.pdf
out2=$out/${f1}_mode_reset.pdf

pdfcpu pagemode list $1 > $out/$f1.log 2>&1
pdfcpu pagemode set $1 UseOutlines $out1 >> $out/$f1.log 2>&1
pdfcpu pagemode reset $out1 $out2 >> $out/$f1.log 2>&1
if [ $? -ne 0 ]; then
    echo "page mode sample error: $1"
    exit 1
else
    echo "page mode sample success: $1 -> $out2"
fi
