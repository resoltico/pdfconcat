// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println(`{"format_version":3,"command":"","status":"invalid","diagnostics":[{"severity":"error","code":"usage_missing_input","message":"The CLI is being rebuilt; no operation was performed."}]}`)
	os.Exit(2)
}
