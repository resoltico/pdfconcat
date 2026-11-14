// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import "strings"

// validLanguageTag checks RFC 5646 §§2.1, 2.2.5 and 2.2.6, without consulting
// the registry or canonicalizing away invalid/repeated subtags. Unknown but
// well-formed language/variant/extension values remain authored verbatim.
func validLanguageTag(value string) bool {
	// Check before Unicode case folding, which can turn non-ASCII characters
	// such as the Kelvin sign into ASCII letters.
	for _, c := range value {
		if c > 127 {
			return false
		}
	}
	tag := strings.ToLower(value)
	// RFC 5646's grandfathered set is fixed and cannot gain new registrations.
	switch tag {
	case "art-lojban", "cel-gaulish", "en-gb-oed", "i-ami", "i-bnn", "i-default", "i-enochian", "i-hak", "i-klingon", "i-lux", "i-mingo", "i-navajo", "i-pwn", "i-tao", "i-tay", "i-tsu", "no-bok", "no-nyn", "sgn-be-fr", "sgn-be-nl", "sgn-ch-de", "zh-guoyu", "zh-hakka", "zh-min", "zh-min-nan", "zh-xiang":
		return true
	}
	parts := strings.Split(tag, "-")
	for _, part := range parts {
		if len(part) < 1 || len(part) > 8 || !asciiSubtag(part, false) {
			return false
		}
	}
	if parts[0] == "x" {
		return len(parts) > 1
	}
	language := parts[0]
	if len(language) < 2 || !asciiSubtag(language, true) {
		return false
	}
	i := 1
	if len(language) <= 3 {
		for n := 0; n < 3 && i < len(parts) && len(parts[i]) == 3 && asciiSubtag(parts[i], true); n++ {
			i++
		}
	}
	if i < len(parts) && len(parts[i]) == 4 && asciiSubtag(parts[i], true) {
		i++
	}
	if i < len(parts) && (len(parts[i]) == 2 && asciiSubtag(parts[i], true) || len(parts[i]) == 3 && asciiDigits(parts[i])) {
		i++
	}
	variants := map[string]bool{}
	for i < len(parts) {
		part := parts[i]
		if len(part) < 5 && (len(part) != 4 || part[0] < '0' || part[0] > '9') {
			break
		}
		if variants[part] {
			return false
		}
		variants[part] = true
		i++
	}
	extensions := map[string]bool{}
	for i < len(parts) && len(parts[i]) == 1 && parts[i] != "x" {
		singleton := parts[i]
		if extensions[singleton] {
			return false
		}
		extensions[singleton] = true
		i++
		start := i
		for i < len(parts) && len(parts[i]) >= 2 {
			i++
		}
		if i == start {
			return false
		}
	}
	if i < len(parts) && parts[i] == "x" {
		return i+1 < len(parts)
	}
	return i == len(parts)
}

func asciiSubtag(s string, lettersOnly bool) bool {
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' {
			continue
		}
		if !lettersOnly && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}
func asciiDigits(s string) bool {
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
