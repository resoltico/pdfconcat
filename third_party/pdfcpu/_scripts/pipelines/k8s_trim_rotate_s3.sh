#!/bin/sh

# Trim pages and rotate the result in a stateless Kubernetes job.
# No final optimize step is added because trim/rotate already write processed PDFs.
#
# usage: INPUT_URI=s3://bucket/in.pdf PAGES=1-3 ROTATION=90 OUTPUT_URI=s3://bucket/out.pdf ./k8s_trim_rotate_s3.sh

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

if [ -z "$INPUT_URI" ] || [ -z "$PAGES" ] || [ -z "$ROTATION" ] || [ -z "$OUTPUT_URI" ]; then
    echo "usage: INPUT_URI=s3://bucket/in.pdf PAGES=1-3 ROTATION=90 OUTPUT_URI=s3://bucket/out.pdf ./k8s_trim_rotate_s3.sh"
    exit 1
fi

aws s3 cp "$INPUT_URI" - \
    | pdfcpu trim --pages "$PAGES" - - \
    | pdfcpu rotate - "$ROTATION" - \
    | aws s3 cp - "$OUTPUT_URI"
