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

# eg: ./formFile.sh /tmp/out

if [ $# -ne 1 ]; then
    echo "usage: ./formFile.sh outDir"
    exit 1
fi

out=$1
form=$out/form.pdf
locked=$out/form_locked.pdf
unlocked=$out/form_unlocked.pdf
reset=$out/form_reset.pdf

pdfcpu create ../pkg/testdata/json/form/checkbox.json $form > $out/form.log 2>&1
pdfcpu form list $form >> $out/form.log 2>&1
pdfcpu form export $form $out/form.json >> $out/form.log 2>&1
pdfcpu form lock $form $locked >> $out/form.log 2>&1
pdfcpu form unlock $locked $unlocked >> $out/form.log 2>&1
pdfcpu form reset $unlocked $reset >> $out/form.log 2>&1
if [ $? -ne 0 ]; then
    echo "form sample error"
    exit 1
else
    echo "form sample success: $reset"
fi
