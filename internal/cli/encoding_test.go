// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package cli_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"
)

func TestInvalidUTF8ArgumentsRejectBeforeLossyEcho(t *testing.T) {
	t.Parallel()

	bad := string([]byte{0xff})

	cases := [][]string{
		{bad},
		{argBuild, bad},
		{argBuild, "-o", bad},
		{argBuild, "--report=" + bad},
		{argCheck, planOption, bad},
		{argCheck, argBaseDir, bad},
		{argCheck, argPlanJSON, bad},
		{argReport, bad},
		{argCheck, argFormat, "text", bad},
	}

	for _, args := range cases {
		_, err := cli.Parse(args)

		usage, ok := errors.AsType[*cli.UsageError](err)
		if !ok || len(usage.Diagnostics) != 1 || string(usage.Diagnostics[0].Code) != "usage_invalid_utf8" {
			t.Fatalf("Parse(%q): %v", args, err)
		}

		if strings.Contains(usage.Diagnostics[0].Message, bad) || strings.ContainsRune(usage.Diagnostics[0].Message, '�') {
			t.Fatal("raw invalid argument echoed lossily")
		}
	}
}
