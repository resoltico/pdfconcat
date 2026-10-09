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

# Extract a single page from an S3 PDF and upload that page as a PDF.
#
# usage: INPUT_URI=s3://bucket/in.pdf PAGE=3 OUTPUT_URI=s3://bucket/page-3.pdf ./aws_extract_page_s3.sh

if [ -z "$INPUT_URI" ] || [ -z "$PAGE" ] || [ -z "$OUTPUT_URI" ]; then
    echo "usage: INPUT_URI=s3://bucket/in.pdf PAGE=3 OUTPUT_URI=s3://bucket/page-3.pdf ./aws_extract_page_s3.sh"
    exit 1
fi

aws s3 cp "$INPUT_URI" - \
    | pdfcpu extract --mode page --pages "$PAGE" - - \
    | aws s3 cp - "$OUTPUT_URI"
