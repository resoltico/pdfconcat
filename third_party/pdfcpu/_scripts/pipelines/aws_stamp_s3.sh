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

# Add a text stamp to an S3 PDF and upload the stamped PDF.
# No extra optimize step is needed because stamp writes an optimized PDF.
#
# usage: INPUT_URI=s3://bucket/in.pdf OUTPUT_URI=s3://bucket/stamped.pdf STAMP_TEXT=CONFIDENTIAL ./aws_stamp_s3.sh

if [ -z "$INPUT_URI" ] || [ -z "$OUTPUT_URI" ] || [ -z "$STAMP_TEXT" ]; then
    echo "usage: INPUT_URI=s3://bucket/in.pdf OUTPUT_URI=s3://bucket/stamped.pdf STAMP_TEXT=CONFIDENTIAL ./aws_stamp_s3.sh"
    exit 1
fi

STAMP_DESC=${STAMP_DESC:-"pos:tr, scale:.35 abs, op:.6"}

aws s3 cp "$INPUT_URI" - \
    | pdfcpu stamp add "$STAMP_TEXT" "$STAMP_DESC" - - \
    | aws s3 cp - "$OUTPUT_URI"
