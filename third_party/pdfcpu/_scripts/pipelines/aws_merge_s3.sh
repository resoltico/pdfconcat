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

# Merge local PDFs and upload the resulting PDF to S3.
# No extra optimize step is needed because merge writes an optimized PDF by default.
#
# usage: INPUT_GLOB='reports/*.pdf' OUTPUT_URI=s3://bucket/merged.pdf ./aws_merge_s3.sh

if [ -z "$INPUT_GLOB" ] || [ -z "$OUTPUT_URI" ]; then
    echo "usage: INPUT_GLOB='reports/*.pdf' OUTPUT_URI=s3://bucket/merged.pdf ./aws_merge_s3.sh"
    exit 1
fi

# shellcheck disable=SC2086
pdfcpu merge - $INPUT_GLOB \
    | aws s3 cp - "$OUTPUT_URI"
