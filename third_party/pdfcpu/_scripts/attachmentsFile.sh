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

# eg: ./attachmentsFile.sh pkg/testdata/go.pdf /tmp/out

if [ $# -ne 2 ]; then
    echo "usage: ./attachmentsFile.sh inFile outDir"
    exit 1
fi

f=${1##*/}
f1=${f%.*}
out=$2
work=$out/$f
attachment=../pkg/testdata/resources/test.wav
extractDir=$out/attachments

cp $1 $work
mkdir -p $extractDir

pdfcpu attachments add $work $attachment > $out/$f1.log 2>&1
if [ $? -ne 0 ]; then
    echo "attachment add error: $work"
    exit 1
fi

pdfcpu attachments list $work >> $out/$f1.log 2>&1
pdfcpu attachments extract $work $extractDir >> $out/$f1.log 2>&1
pdfcpu attachments remove $work test.wav >> $out/$f1.log 2>&1
if [ $? -ne 0 ]; then
    echo "attachment sample error: $work"
    exit 1
else
    echo "attachment sample success: $work"
fi
