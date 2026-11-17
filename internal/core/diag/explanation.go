// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package diag

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// FindingIdentity describes a semantic cause and its typed parameters. Rendered
// wording and opaque backend detail are deliberately outside this identity.
type FindingIdentity struct {
	Kind       string
	Phase      string
	Parameters [32]byte
}

// Explanation is a core-owned whole product explanation with named constructor parameters.
type Explanation struct {
	detail   string
	text     string
	identity FindingIdentity
}

func explain(kind, text string, parameters ...string) Explanation {
	hash := sha256.New()
	var size [8]byte
	for _, parameter := range parameters {
		binary.BigEndian.PutUint64(size[:], uint64(len(parameter)))
		hash.Write(size[:])
		hash.Write([]byte(parameter))
	}
	var fingerprint [32]byte
	copy(fingerprint[:], hash.Sum(nil))
	return Explanation{text: text, identity: FindingIdentity{Kind: kind, Parameters: fingerprint}}
}

// Report constructs a product finding from a concrete core explanation.
func Report(code Code, explanation Explanation, items ...string) Diagnostic {
	d := New(code, explanation.text, items...)
	d.Identity = explanation.identity
	d.Detail = explanation.detail
	return d
}

// ApplyExplanation changes rendering and its semantic cause together.
func ApplyExplanation(d *Diagnostic, e Explanation) { d.Message = e.text; d.Identity = e.identity }

// AttachEvidence records an independently typed cause/binding without using its
// rendered explanation as identity. Unattributable opaque evidence is not merged.
func AttachEvidence(d *Diagnostic, e Explanation) {
	d.Detail = e.text
	if d.Identity.Kind == "" || e.identity.Kind == "" {
		d.Identity = FindingIdentity{}
		return
	}
	phase := d.Identity.Phase
	d.Identity = explain(d.Identity.Kind, d.Message, e.identity.Kind, string(d.Identity.Parameters[:]), string(e.identity.Parameters[:])).identity
	d.Identity.Phase = phase
}

// TechnicalFailure keeps opaque library/native evidence as a named parameter of
// a complete sentence. It has no coalescing identity until a boundary supplies facts.
func TechnicalFailure(phase, reason string) Explanation {
	var text string
	switch phase {
	case "input":
		text = fmt.Sprintf("The input path could not be used: %s.", reason)
	case "typography":
		text = fmt.Sprintf("Generated text or font preparation failed: %s.", reason)
	case "interruption":
		text = fmt.Sprintf("The operation was interrupted: %s.", reason)
	case "publication":
		text = fmt.Sprintf("Publication inspection failed: %s.", reason)
	default:
		text = fmt.Sprintf("PDF processing failed: %s.", reason)
	}
	return Explanation{text: text, detail: reason}
}

// GeneratedTextFailure owns both whole-sentence variants of width attribution.
func GeneratedTextFailure(reason, widthOrigin string) Explanation {
	if widthOrigin == "" {
		return Explanation{text: fmt.Sprintf("Generated text could not be prepared: %s.", reason), detail: reason}
	}
	return Explanation{text: fmt.Sprintf("Generated text could not be prepared using width from %q: %s.", widthOrigin, reason), detail: reason}
}

// FittingAnnotationRefusal preserves the technical issue as a named parameter.
func FittingAnnotationRefusal(issue string) Explanation {
	return Explanation{text: fmt.Sprintf("%s Supply complete normal appearances and keep interactive hit areas inside the source frame, or use original page sizes.", issue)}
}
