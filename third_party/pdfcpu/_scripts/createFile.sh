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

# eg: ./createFile.sh /tmp/out

if [ $# -ne 1 ]; then
    echo "usage: ./createFile.sh outDir"
    exit 1
fi

out=$1
out1=$out/created.pdf

pdfcpu create ../pkg/testdata/json/create/textAnchored.json $out1 > $out/create.log 2>&1
if [ $? -ne 0 ]; then
    echo "create error: $out1"
    exit 1
else
    echo "create success: $out1"
fi
