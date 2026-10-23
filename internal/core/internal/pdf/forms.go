// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type fieldRecord struct {
	name string
	dict types.Dict
}

func textOf(o types.Object) (string, error) {
	if o == nil {
		return "", nil
	}
	if name, ok := o.(types.Name); ok {
		return string(name), nil
	}
	text, err := types.StringOrHexLiteral(o)
	if err != nil {
		return "", err
	}
	if text == nil {
		return "", fmt.Errorf("expected a PDF text string, got %T", o)
	}
	return *text, nil
}

func textValue(s *model.Context, o types.Object) (string, error) {
	value, err := s.Dereference(o)
	if err != nil {
		return "", err
	}
	return textOf(value)
}
func nameValue(s *model.Context, o types.Object) (types.Name, error) {
	value, err := s.Dereference(o)
	if err != nil {
		return "", err
	}
	name, _ := value.(types.Name)
	return name, nil
}

func fields(c context.Context, s *model.Context) ([]fieldRecord, error) {
	cat, err := s.Catalog()
	if err != nil {
		return nil, err
	}
	form, err := s.DereferenceDict(cat["AcroForm"])
	if err != nil {
		return nil, err
	}
	roots, err := s.DereferenceArray(form["Fields"])
	if err != nil {
		return nil, err
	}
	var result []fieldRecord
	seen := map[types.IndirectRef]bool{}
	var visit func(types.Object, string, types.Object, int) error
	visit = func(o types.Object, prefix string, ft types.Object, depth int) error {
		if err := c.Err(); err != nil {
			return err
		}
		if depth > 64 {
			return refuse(diag.SourceInvalid, "Form field tree exceeds depth 64.")
		}
		if ref, ok := o.(types.IndirectRef); ok {
			if seen[ref] {
				return refuse(diag.SourceInvalid, "Form field tree repeats a node.")
			}
			seen[ref] = true
		}
		d, err := s.DereferenceDict(o)
		if err != nil {
			return err
		}
		own, err := textValue(s, d["T"])
		if err != nil {
			return err
		}
		name := prefix
		if own != "" {
			if name != "" {
				name += "."
			}
			name += own
		}
		ownType, e := s.Dereference(d["FT"])
		if e != nil {
			return e
		}
		if ownType != nil {
			ft = ownType
		}
		value, err := s.Dereference(d["V"])
		if err != nil {
			return err
		}
		if ft == types.Name("Sig") && value != nil {
			return refuse(diag.SourceSigned, "Signature values are not supported; unsigned signature fields are allowed.")
		}
		kids, err := s.DereferenceArray(d["Kids"])
		if err != nil {
			return err
		}
		hasFields := false
		for _, kid := range kids {
			child, err := s.DereferenceDict(kid)
			if err != nil {
				return err
			}
			childName, e := textValue(s, child["T"])
			if e != nil {
				return e
			}
			if childName != "" {
				hasFields = true
			}
			if err = visit(kid, name, ft, depth+1); err != nil {
				return err
			}
		}
		if (own != "" || depth == 0 && ft != nil) && !hasFields {
			result = append(result, fieldRecord{name: name, dict: d})
		}
		return nil
	}
	for _, root := range roots {
		if err = visit(root, "", nil, 0); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func qualifiedName(s *model.Context, d types.Dict) (string, error) {
	var names []string
	seen := map[types.IndirectRef]bool{}
	for depth := 0; ; depth++ {
		if depth > 64 {
			return "", errors.New("merged field parent chain exceeds depth 64")
		}
		name, err := textValue(s, d["T"])
		if err != nil {
			return "", err
		}
		if name != "" {
			names = append(names, name)
		}
		parent := d["Parent"]
		if parent == nil {
			break
		}
		if ref, ok := parent.(types.IndirectRef); ok {
			if seen[ref] {
				return "", errors.New("merged field parent cycle")
			}
			seen[ref] = true
		}
		d, err = s.DereferenceDict(parent)
		if err != nil {
			return "", err
		}
	}
	slices.Reverse(names)
	return strings.Join(names, "."), nil
}

// resourceIdentity refuses broader merging rather than repairing resources.
func resourceIdentity(s *model.Context, o types.Object) (string, error) {
	var b bytes.Buffer
	active := map[types.IndirectRef]bool{}
	var visit func(types.Object, int) error
	visit = func(o types.Object, depth int) error {
		if depth > 64 {
			return refuse(diag.SourceFormUnsupported, "Form default resources are nested too deeply for compatibility comparison.")
		}
		if ref, ok := o.(types.IndirectRef); ok {
			if active[ref] {
				return refuse(diag.SourceFormUnsupported, "Cyclic form default resources are unsupported for compatibility comparison.")
			}
			active[ref] = true
			resolved, err := s.Dereference(ref)
			if err != nil {
				return err
			}
			err = visit(resolved, depth+1)
			delete(active, ref)
			return err
		}
		switch v := o.(type) {
		case types.Dict:
			keys := make([]string, 0, len(v))
			for key := range v {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			b.WriteByte('{')
			for _, key := range keys {
				fmt.Fprintf(&b, "%q:", key)
				if err := visit(v[key], depth+1); err != nil {
					return err
				}
			}
			b.WriteByte('}')
		case types.Array:
			b.WriteByte('[')
			for _, child := range v {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
			b.WriteByte(']')
		case types.StreamDict:
			if err := visit(v.Dict, depth+1); err != nil {
				return err
			}
			fmt.Fprintf(&b, "stream:%x:%x;", v.Raw, v.Content)
		default:
			fmt.Fprintf(&b, "%T:%#v;", o, o)
		}
		return nil
	}
	if err := visit(o, 0); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(b.Bytes())), nil
}
