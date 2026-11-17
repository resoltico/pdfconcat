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

# Encrypt a PDF in a Kubernetes job and upload the encrypted PDF.
#
# usage: INPUT_URI=s3://bucket/in.pdf OUTPUT_URI=s3://bucket/encrypted.pdf OPW=owner-password UPW=user-password ./k8s_encrypt_s3.sh

if [ -z "$INPUT_URI" ] || [ -z "$OUTPUT_URI" ] || [ -z "$OPW" ] || [ -z "$UPW" ]; then
    echo "usage: INPUT_URI=s3://bucket/in.pdf OUTPUT_URI=s3://bucket/encrypted.pdf OPW=owner-password UPW=user-password ./k8s_encrypt_s3.sh"
    exit 1
fi

aws s3 cp "$INPUT_URI" - \
    | pdfcpu encrypt --opw "$OPW" --upw "$UPW" - - \
    | aws s3 cp - "$OUTPUT_URI"
