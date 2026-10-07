// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Package repopolicy enforces the repository's quality policy. It reads the central exception
// registry (.quality-exceptions.yml) and checks that .golangci.yml carries exactly the lint
// exceptions the registry lists; applies the registry's coverage and mutation scopes; rejects
// inline suppression directives in Go source; and verifies derived copies such as the third-party
// notices, the tool versions and the workflow action pins. The tools/qualitygate command exposes
// these checks to developers and CI.
package repopolicy
