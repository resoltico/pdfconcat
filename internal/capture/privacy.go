// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package capture

import (
	"errors"
	"strings"
)

var errPrivatePattern = errors.New("private temporary name must be an owned prefix followed by one terminal wildcard")

// Private creators accept the owned prefix followed by one terminal wildcard.
func privateTempPrefix(pattern string) (string, error) {
	prefix, found := strings.CutSuffix(pattern, "*")
	if !found || prefix == "" || strings.ContainsAny(prefix, "*/\\") {
		return "", errPrivatePattern
	}

	return prefix, nil
}
