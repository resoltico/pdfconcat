// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package scale_test

func descriptorLimitError(error) bool { return false }
func descriptorFactsHelper() int      { return helperExitUnknownMode }
