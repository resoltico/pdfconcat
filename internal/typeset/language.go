// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package typeset

import "github.com/go-text/typesetting/language"

// Language is the shaping library value, supplied by the validated composition boundary.
type Language = language.Language

// ShapingLanguage adapts an already validated authored tag to the library's
// case-insensitive shaping value, without changing the stored authored spelling.
func ShapingLanguage(tag string) Language { return language.NewLanguage(tag) }
