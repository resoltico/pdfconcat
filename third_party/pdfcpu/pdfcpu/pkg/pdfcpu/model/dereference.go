/*
Copyright 2021 The pdfcpu Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package model

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/pdfcpu/pdfcpu/internal/contextutil"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// ErrExpectedDict signals that a PDF object is not a dictionary.
var ErrExpectedDict = errors.New("expected types.Dict")

func processDictRefCounts(xRefTable *XRefTable, d types.Dict, depth int) error {
	if err := xRefTable.CheckRecursionDepth("reference count traversal", depth); err != nil {
		return err
	}
	for _, key := range slices.Sorted(maps.Keys(d)) {
		e := d[key]
		switch o1 := e.(type) {
		case types.IndirectRef:
			xRefTable.IncrementRefCount(&o1)
		case types.Dict:
			if err := processRefCounts(xRefTable, o1, depth+1); err != nil {
				return fmt.Errorf("dict entry %s: %w", key, err)
			}
		case types.Array:
			if err := processRefCounts(xRefTable, o1, depth+1); err != nil {
				return fmt.Errorf("dict entry %s: %w", key, err)
			}
		}
	}
	return nil
}

func processArrayRefCounts(xRefTable *XRefTable, a types.Array, depth int) error {
	if err := xRefTable.CheckRecursionDepth("reference count traversal", depth); err != nil {
		return err
	}
	for _, e := range a {
		switch o1 := e.(type) {
		case types.IndirectRef:
			xRefTable.IncrementRefCount(&o1)
		case types.Dict:
			if err := processRefCounts(xRefTable, o1, depth+1); err != nil {
				return err
			}
		case types.Array:
			if err := processRefCounts(xRefTable, o1, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func processRefCounts(xRefTable *XRefTable, o types.Object, depth int) error {
	switch o := o.(type) {
	case types.Dict:
		return processDictRefCounts(xRefTable, o, depth)
	case types.StreamDict:
		return processDictRefCounts(xRefTable, o.Dict, depth)
	case types.Array:
		return processArrayRefCounts(xRefTable, o, depth)
	}
	return nil
}

// ProcessRefCountsWithError processes reference counts and returns an error.
func ProcessRefCountsWithError(xRefTable *XRefTable, o types.Object) error {
	return processRefCounts(xRefTable, o, 0)
}

// ProcessRefCounts processes reference counts.
func ProcessRefCounts(xRefTable *XRefTable, o types.Object) {
	_ = ProcessRefCountsWithError(xRefTable, o)
}

func (xRefTable *XRefTable) indRefToObject(ir *types.IndirectRef, decodeLazy bool) (types.Object, int, error) {
	return xRefTable.indRefToObjectContext(context.Background(), ir, decodeLazy)
}

func (xRefTable *XRefTable) indRefToObjectContext(c context.Context, ir *types.IndirectRef, decodeLazy bool) (types.Object, int, error) {
	if err := c.Err(); err != nil {
		return nil, 0, err
	}
	if ir == nil {
		return nil, 0, errors.New("input argument is nil")
	}

	// 7.3.10
	// An indirect reference to an undefined object shall not be considered an error by a conforming reader;
	// it shall be treated as a reference to the null object.
	entry, found := xRefTable.FindTableEntryForIndRef(ir)
	if !found || entry.Free {
		return nil, 0, nil
	}

	if l, ok := entry.Object.(types.LazyObjectStreamObject); ok && decodeLazy {
		ob, err := l.DecodedObject(c)
		if err != nil {
			return nil, 0, err
		}

		if err := ProcessRefCountsWithError(xRefTable, ob); err != nil {
			return nil, 0, err
		}
		if err := c.Err(); err != nil {
			return nil, 0, err
		}
		entry.Object = ob
	}

	// return dereferenced object and increment nr.
	return entry.Object, entry.Incr, nil
}

// Dereference resolves an indirect object and returns the resulting PDF object.
func (xRefTable *XRefTable) Dereference(o types.Object) (types.Object, error) {
	return xRefTable.DereferenceContext(context.Background(), o)
}

// DereferenceContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceContext(c context.Context, o types.Object) (types.Object, error) {
	if err := c.Err(); err != nil {
		return nil, err
	}
	ir, ok := o.(types.IndirectRef)
	if !ok {
		// Nothing do dereference.
		return o, nil
	}

	obj, _, err := xRefTable.indRefToObjectContext(c, &ir, true)
	return obj, err
}

// DereferenceWithIncr dereferences obj and increments reference counts.
func (xRefTable *XRefTable) DereferenceWithIncr(o types.Object) (types.Object, int, error) {
	return xRefTable.DereferenceWithIncrContext(context.Background(), o)
}

// DereferenceWithIncrContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceWithIncrContext(c context.Context, o types.Object) (types.Object, int, error) {
	ir, ok := o.(types.IndirectRef)
	if !ok {
		// Nothing do dereference.
		return o, 0, nil
	}

	return xRefTable.indRefToObjectContext(c, &ir, true)
}

// DereferenceForWrite dereferences obj for writing.
func (xRefTable *XRefTable) DereferenceForWrite(o types.Object) (types.Object, error) {
	return xRefTable.DereferenceForWriteContext(context.Background(), o)
}

// DereferenceForWriteContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceForWriteContext(c context.Context, o types.Object) (types.Object, error) {
	ir, ok := o.(types.IndirectRef)
	if !ok {
		// Nothing do dereference.
		return o, nil
	}

	obj, _, err := xRefTable.indRefToObjectContext(c, &ir, false)
	return obj, err
}

// DereferenceBoolean resolves and validates a boolean object, which may be an indirect reference.
func (xRefTable *XRefTable) DereferenceBoolean(o types.Object, sinceVersion Version) (*types.Boolean, error) {
	return xRefTable.DereferenceBooleanContext(context.Background(), o, sinceVersion)
}

// DereferenceBooleanContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceBooleanContext(c context.Context, o types.Object, sinceVersion Version) (*types.Boolean, error) {
	o, err := xRefTable.DereferenceContext(c, o)
	if err != nil || o == nil {
		return nil, err
	}

	b, ok := o.(types.Boolean)
	if !ok {
		return nil, fmt.Errorf("wrong type <%v>", o)
	}

	// Version check
	if err = xRefTable.ValidateVersion("DereferenceBoolean", sinceVersion); err != nil {
		return nil, err
	}

	return &b, nil
}

// DereferenceBooleanEntry resolves a direct or indirect boolean dictionary entry.
func (xRefTable *XRefTable) DereferenceBooleanEntry(d types.Dict, key string) (*types.Boolean, bool, error) {
	return xRefTable.DereferenceBooleanEntryContext(context.Background(), d, key)
}

// DereferenceBooleanEntryContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceBooleanEntryContext(c context.Context, d types.Dict, key string) (*types.Boolean, bool, error) {
	o, found, objNr, err := xRefTable.dereferenceEntry(c, d, key)
	if err != nil || !found || o == nil {
		return nil, found, err
	}

	b, ok := o.(types.Boolean)
	if !ok {
		return nil, true, scalarEntryTypeError(key, "boolean", o, objNr)
	}

	return &b, true, nil
}

func (xRefTable *XRefTable) dereferenceEntry(c context.Context, d types.Dict, key string) (types.Object, bool, int, error) {
	if err := c.Err(); err != nil {
		return nil, false, 0, err
	}
	o, found := d.Find(key)
	if !found {
		return nil, false, 0, nil
	}
	if o == nil {
		return nil, true, 0, nil
	}

	objNr := 0
	if ir, ok := o.(types.IndirectRef); ok {
		objNr = ir.ObjectNumber.Value()
		entry, found := xRefTable.FindTableEntryForIndRef(&ir)
		if !found || entry == nil || entry.Free {
			err := fmt.Errorf("entry=%s: missing indirect target", key)
			return nil, true, objNr, WithValidationErrorObject(err, objNr)
		}
	}

	o, err := xRefTable.DereferenceContext(c, o)
	if err != nil {
		err = fmt.Errorf("entry=%s: %w", key, err)
		return nil, true, objNr, WithValidationErrorObject(err, objNr)
	}

	return o, true, objNr, nil
}

func scalarEntryTypeError(key, want string, o types.Object, objNr int) error {
	err := fmt.Errorf("entry=%s: expected %s, got %T", key, want, o)
	return WithValidationErrorObject(err, objNr)
}

// DereferenceInteger resolves and validates an integer object, which may be an indirect reference.
func (xRefTable *XRefTable) DereferenceInteger(o types.Object) (*types.Integer, error) {
	return xRefTable.DereferenceIntegerContext(context.Background(), o)
}

// DereferenceIntegerContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceIntegerContext(c context.Context, o types.Object) (*types.Integer, error) {
	o, err := xRefTable.DereferenceContext(c, o)
	if err != nil || o == nil {
		return nil, err
	}

	i, ok := o.(types.Integer)
	if !ok {
		return nil, fmt.Errorf("wrong type <%v>", o)
	}

	return &i, nil
}

// DereferenceIntegerEntry resolves a direct or indirect integer dictionary entry.
func (xRefTable *XRefTable) DereferenceIntegerEntry(d types.Dict, key string) (*types.Integer, bool, error) {
	return xRefTable.DereferenceIntegerEntryContext(context.Background(), d, key)
}

// DereferenceIntegerEntryContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceIntegerEntryContext(c context.Context, d types.Dict, key string) (*types.Integer, bool, error) {
	o, found, objNr, err := xRefTable.dereferenceEntry(c, d, key)
	if err != nil || !found || o == nil {
		return nil, found, err
	}

	i, ok := o.(types.Integer)
	if !ok {
		return nil, true, scalarEntryTypeError(key, "integer", o, objNr)
	}

	return &i, true, nil
}

// DereferenceNumber resolves a number object, which may be an indirect reference and returns a float64.
func (xRefTable *XRefTable) DereferenceNumber(o types.Object) (float64, error) {
	return xRefTable.DereferenceNumberContext(context.Background(), o)
}

// DereferenceNumberContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceNumberContext(c context.Context, o types.Object) (float64, error) {
	var (
		f   float64
		err error
	)

	o, err = xRefTable.DereferenceContext(c, o)
	if err != nil {
		return 0, err
	}

	switch o := o.(type) {

	case types.Integer:
		f = float64(o.Value())

	case types.Float:
		f = o.Value()

	default:
		err = fmt.Errorf("wrong type <%v>", o)

	}

	return f, err
}

// DereferenceNumberEntry resolves a direct or indirect number dictionary entry.
func (xRefTable *XRefTable) DereferenceNumberEntry(d types.Dict, key string) (*float64, bool, error) {
	return xRefTable.DereferenceNumberEntryContext(context.Background(), d, key)
}

// DereferenceNumberEntryContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceNumberEntryContext(c context.Context, d types.Dict, key string) (*float64, bool, error) {
	o, found, objNr, err := xRefTable.dereferenceEntry(c, d, key)
	if err != nil || !found || o == nil {
		return nil, found, err
	}

	var f float64
	switch o := o.(type) {
	case types.Integer:
		f = float64(o.Value())
	case types.Float:
		f = o.Value()
	default:
		return nil, true, scalarEntryTypeError(key, "number", o, objNr)
	}

	return &f, true, nil
}

// DereferenceName resolves and validates a name object, which may be an indirect reference.
func (xRefTable *XRefTable) DereferenceName(o types.Object, sinceVersion Version, validate func(string) bool) (n types.Name, err error) {
	return xRefTable.DereferenceNameContext(context.Background(), o, sinceVersion, validate)
}

// DereferenceNameContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceNameContext(c context.Context, o types.Object, sinceVersion Version, validate func(string) bool) (n types.Name, err error) {
	o, err = xRefTable.DereferenceContext(c, o)
	if err != nil || o == nil {
		return n, err
	}

	n, ok := o.(types.Name)
	if !ok {
		return n, fmt.Errorf("wrong type <%v>", o)
	}

	// Version check
	if err = xRefTable.ValidateVersion("DereferenceName", sinceVersion); err != nil {
		return n, err
	}

	// Validation
	if validate != nil && !validate(n.Value()) {
		return n, fmt.Errorf("invalid <%s>", n.Value())
	}

	return n, nil
}

// DereferenceNameEntry resolves a direct or indirect name dictionary entry.
func (xRefTable *XRefTable) DereferenceNameEntry(d types.Dict, key string) (*types.Name, bool, error) {
	return xRefTable.DereferenceNameEntryContext(context.Background(), d, key)
}

// DereferenceNameEntryContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceNameEntryContext(c context.Context, d types.Dict, key string) (*types.Name, bool, error) {
	o, found, objNr, err := xRefTable.dereferenceEntry(c, d, key)
	if err != nil || !found || o == nil {
		return nil, found, err
	}

	n, ok := o.(types.Name)
	if !ok {
		return nil, true, scalarEntryTypeError(key, "name", o, objNr)
	}

	return &n, true, nil
}

// DereferenceStringLiteral resolves and validates a string literal object, which may be an indirect reference.
func (xRefTable *XRefTable) DereferenceStringLiteral(o types.Object, sinceVersion Version, validate func(string) bool) (s types.StringLiteral, err error) {
	return xRefTable.DereferenceStringLiteralContext(context.Background(), o, sinceVersion, validate)
}

// DereferenceStringLiteralContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceStringLiteralContext(c context.Context, o types.Object, sinceVersion Version, validate func(string) bool) (s types.StringLiteral, err error) {
	o, err = xRefTable.DereferenceContext(c, o)
	if err != nil || o == nil {
		return s, err
	}

	s, ok := o.(types.StringLiteral)
	if !ok {
		return s, fmt.Errorf("wrong type <%v>", o)
	}

	// Ensure UTF16 correctness.
	s1, err := types.StringLiteralToString(s)
	if err != nil {
		return s, err
	}

	// Version check
	if err = xRefTable.ValidateVersion("DereferenceStringLiteral", sinceVersion); err != nil {
		return s, err
	}

	// Validation
	if validate != nil && !validate(s1) {
		return s, fmt.Errorf("invalid <%s>", s1)
	}

	return s, nil
}

// DereferenceStringOrHexLiteral resolves and validates a string or hex literal object, which may be an indirect reference.
func (xRefTable *XRefTable) DereferenceStringOrHexLiteral(obj types.Object, sinceVersion Version, validate func(string) bool) (s string, err error) {
	return xRefTable.DereferenceStringOrHexLiteralContext(context.Background(), obj, sinceVersion, validate)
}

// DereferenceStringOrHexLiteralContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceStringOrHexLiteralContext(c context.Context, obj types.Object, sinceVersion Version, validate func(string) bool) (s string, err error) {
	o, err := xRefTable.DereferenceContext(c, obj)
	if err != nil || o == nil {
		return "", err
	}

	switch str := o.(type) {

	case types.StringLiteral:
		// Ensure UTF16 correctness.
		if s, err = types.StringLiteralToString(str); err != nil {
			return "", err
		}

	case types.HexLiteral:
		// Ensure UTF16 correctness.
		if s, err = types.HexLiteralToString(str); err != nil {
			return "", err
		}

	default:
		return "", fmt.Errorf("wrong type %T", obj)

	}

	// Version check
	if err = xRefTable.ValidateVersion("DereferenceStringOrHexLiteral", sinceVersion); err != nil {
		return "", err
	}

	// Validation
	if validate != nil && !validate(s) {
		return "", fmt.Errorf("invalid <%s>", s)
	}

	return s, nil
}

// DereferenceStringEntry resolves a direct or indirect literal or hexadecimal string dictionary entry.
func (xRefTable *XRefTable) DereferenceStringEntry(d types.Dict, key string) (*string, bool, error) {
	return xRefTable.DereferenceStringEntryContext(context.Background(), d, key)
}

// DereferenceStringEntryContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceStringEntryContext(c context.Context, d types.Dict, key string) (*string, bool, error) {
	o, found, objNr, err := xRefTable.dereferenceEntry(c, d, key)
	if err != nil || !found || o == nil {
		return nil, found, err
	}

	switch o.(type) {
	case types.StringLiteral, types.HexLiteral:
	default:
		return nil, true, scalarEntryTypeError(key, "string or hex literal", o, objNr)
	}

	s, err := Text(o)
	if err != nil {
		err = fmt.Errorf("entry=%s: %w", key, err)
		return nil, true, WithValidationErrorObject(err, objNr)
	}

	return &s, true, nil
}

// Text returns a string based representation for String and Hexliterals.
func Text(o types.Object) (string, error) {
	switch obj := o.(type) {
	case types.StringLiteral:
		return types.StringLiteralToString(obj)
	case types.HexLiteral:
		return types.HexLiteralToString(obj)
	default:
		return "", fmt.Errorf("corrupt text: %v", obj)
	}
}

// DereferenceText resolves and validates a string or hex literal object to a string.
func (xRefTable *XRefTable) DereferenceText(o types.Object) (string, error) {
	return xRefTable.DereferenceTextContext(context.Background(), o)
}

// DereferenceTextContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceTextContext(c context.Context, o types.Object) (string, error) {
	o, err := xRefTable.DereferenceContext(c, o)
	if err != nil {
		return "", err
	}
	return Text(o)
}

// CSVSafeString returns obj as a CSV-safe string.
func CSVSafeString(s string) string {
	return strings.Replace(s, ";", ",", -1)
}

// DereferenceCSVSafeText resolves and validates a string or hex literal object to a string.
func (xRefTable *XRefTable) DereferenceCSVSafeText(o types.Object) (string, error) {
	return xRefTable.DereferenceCSVSafeTextContext(context.Background(), o)
}

// DereferenceCSVSafeTextContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceCSVSafeTextContext(c context.Context, o types.Object) (string, error) {
	s, err := xRefTable.DereferenceTextContext(c, o)
	if err != nil {
		return "", err
	}
	return CSVSafeString(s), nil
}

// DereferenceArray resolves and validates an array object, which may be an indirect reference.
func (xRefTable *XRefTable) DereferenceArray(o types.Object) (types.Array, error) {
	return xRefTable.DereferenceArrayContext(context.Background(), o)
}

// DereferenceArrayContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceArrayContext(c context.Context, o types.Object) (types.Array, error) {
	o, err := xRefTable.DereferenceContext(c, o)
	if err != nil || o == nil {
		return nil, err
	}

	a, ok := o.(types.Array)
	if !ok {
		return nil, fmt.Errorf("wrong type %T <%v>", o, o)
	}

	return a, nil
}

// DereferenceDict resolves and validates a dictionary object, which may be an indirect reference.
func (xRefTable *XRefTable) DereferenceDict(o types.Object) (types.Dict, error) {
	return xRefTable.DereferenceDictContext(context.Background(), o)
}

// DereferenceDictContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceDictContext(c context.Context, o types.Object) (types.Dict, error) {
	rawObject := o
	o, err := xRefTable.DereferenceContext(c, o)
	if err != nil || o == nil {
		return nil, err
	}

	d, ok := o.(types.Dict)
	if !ok {
		return nil, dereferenceDictTypeError(rawObject, o)
	}

	return d, nil
}

func dereferenceDictTypeError(rawObject, resolvedObject types.Object) error {
	if ir, ok := rawObject.(types.IndirectRef); ok {
		return fmt.Errorf(
			"obj#%d gen#%d: %w, got %T",
			ir.ObjectNumber.Value(),
			ir.GenerationNumber.Value(),
			ErrExpectedDict,
			resolvedObject,
		)
	}
	return fmt.Errorf("%w, got %T", ErrExpectedDict, resolvedObject)
}

// DereferenceDictWithIncr resolves and validates a dictionary object, which may be an indirect reference.
// It also returns the number of the written PDF Increment this object is part of.
// The higher the increment number the older the object.
func (xRefTable *XRefTable) DereferenceDictWithIncr(o types.Object) (types.Dict, int, error) {
	return xRefTable.DereferenceDictWithIncrContext(context.Background(), o)
}

// DereferenceDictWithIncrContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceDictWithIncrContext(c context.Context, o types.Object) (types.Dict, int, error) {
	rawObject := o
	o, incr, err := xRefTable.DereferenceWithIncrContext(c, o)
	if err != nil || o == nil {
		return nil, 0, err
	}

	d, ok := o.(types.Dict)
	if !ok {
		return nil, 0, dereferenceDictTypeError(rawObject, o)
	}

	return d, incr, nil
}

// DereferenceFontDict returns the font dict referenced by indRef.
func (xRefTable *XRefTable) DereferenceFontDict(indRef types.IndirectRef) (types.Dict, error) {
	return xRefTable.DereferenceFontDictContext(context.Background(), indRef)
}

// DereferenceFontDictContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceFontDictContext(c context.Context, indRef types.IndirectRef) (types.Dict, error) {
	d, err := xRefTable.DereferenceDictContext(c, indRef)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, nil
	}

	if xRefTable.ValidationMode == ValidationStrict {
		t, _, err := xRefTable.DereferenceNameEntryContext(c, d, "Type")
		if err != nil {
			return nil, fmt.Errorf("font dict Type: %w", err)
		}
		if t == nil {
			return nil, fmt.Errorf("missing dict type %s", indRef)
		}

		if t.Value() != "Font" {
			return nil, fmt.Errorf("expected Type=Font, unexpected Type: %s", t.Value())
		}
	}

	return d, nil
}

func (xRefTable *XRefTable) dereferencePageNodeDictType(c context.Context, indRef types.IndirectRef) (types.Dict, *types.Name, error) {
	d, err := xRefTable.DereferenceDictContext(c, indRef)
	if err != nil {
		return nil, nil, err
	}
	if d == nil {
		return nil, nil, errors.New("missing page node dict")
	}

	dictType, _, err := xRefTable.DereferenceNameEntryContext(c, d, "Type")
	if err != nil {
		return nil, nil, err
	}
	if dictType == nil {
		return nil, nil, errors.New("missing dict type")
	}

	return d, dictType, nil
}

// DereferencePageNodeDict returns the page node dict referenced by indRef.
func (xRefTable *XRefTable) DereferencePageNodeDict(indRef types.IndirectRef) (types.Dict, error) {
	return xRefTable.DereferencePageNodeDictContext(context.Background(), indRef)
}

// DereferencePageNodeDictContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferencePageNodeDictContext(c context.Context, indRef types.IndirectRef) (types.Dict, error) {
	d, dictType, err := xRefTable.dereferencePageNodeDictType(c, indRef)
	if err != nil {
		return nil, err
	}

	if dictType.Value() != "Pages" && dictType.Value() != "Page" {
		return nil, fmt.Errorf("unexpected Type: %s", dictType.Value())
	}

	return d, nil
}

func (xRefTable *XRefTable) dereferenceDestArray(c context.Context, o types.Object) (types.Array, error) {
	o, err := xRefTable.DereferenceContext(c, o)
	if err != nil || o == nil {
		return nil, err
	}
	switch o := o.(type) {
	case types.Array:
		return o, nil
	case types.Dict:
		o1, err := xRefTable.DereferenceDictEntryContext(c, o, "D")
		if err != nil {
			return nil, err
		}
		arr, ok := o1.(types.Array)
		if !ok {
			return nil, fmt.Errorf("invalid dest array: %s", o)
		}
		return arr, nil
	}

	return nil, fmt.Errorf("invalid dest array: %s", o)
}

// DereferenceDestArray resolves the destination for key and supports cancellation.
func (xRefTable *XRefTable) DereferenceDestArray(c context.Context, key string) (types.Array, error) {
	if err := contextutil.Check(c); err != nil {
		return nil, err
	}
	if dNames := xRefTable.Names["Dests"]; dNames != nil {
		o, ok, err := dNames.Value(c, key)
		if err != nil {
			return nil, err
		}
		if ok {
			return xRefTable.dereferenceDestArray(c, o)
		}
	}

	if o, ok := xRefTable.Dests[key]; ok {
		return xRefTable.dereferenceDestArray(c, o)
	}

	return nil, fmt.Errorf("invalid named destination for: %s", key)
}

// DereferenceDictEntry returns a dereferenced dict entry.
func (xRefTable *XRefTable) DereferenceDictEntry(d types.Dict, key string) (types.Object, error) {
	return xRefTable.DereferenceDictEntryContext(context.Background(), d, key)
}

// DereferenceDictEntryContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceDictEntryContext(c context.Context, d types.Dict, key string) (types.Object, error) {
	if err := c.Err(); err != nil {
		return nil, err
	}
	o, found := d.Find(key)
	if !found || o == nil {
		return nil, fmt.Errorf("dict=%s entry=%s missing", d, key)
	}
	return xRefTable.DereferenceContext(c, o)
}

// DereferenceStringEntryBytes returns the bytes of a string entry of d.
func (xRefTable *XRefTable) DereferenceStringEntryBytes(d types.Dict, key string) ([]byte, error) {
	return xRefTable.DereferenceStringEntryBytesContext(context.Background(), d, key)
}

// DereferenceStringEntryBytesContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DereferenceStringEntryBytesContext(c context.Context, d types.Dict, key string) ([]byte, error) {
	if err := c.Err(); err != nil {
		return nil, err
	}
	o, found := d.Find(key)
	if !found || o == nil {
		return nil, nil
	}
	objNr := 0
	if ir, ok := o.(types.IndirectRef); ok {
		objNr = ir.ObjectNumber.Value()
	}
	o, err := xRefTable.DereferenceContext(c, o)
	if err != nil {
		return nil, WithValidationErrorObject(err, objNr)
	}

	switch o := o.(type) {
	case types.StringLiteral:
		bb, err := types.Unescape(o.Value())
		return bb, WithValidationErrorObject(err, objNr)

	case types.HexLiteral:
		bb, err := o.Bytes()
		return bb, WithValidationErrorObject(err, objNr)

	}

	err = fmt.Errorf("entry=%s: expected string or hex literal, got %T", key, o)
	return nil, WithValidationErrorObject(err, objNr)
}

// DestName returns the destination name for o.
func (xRefTable *XRefTable) DestName(obj types.Object) (string, error) {
	return xRefTable.DestNameContext(context.Background(), obj)
}

// DestNameContext resolves under the supplied operation context.
func (xRefTable *XRefTable) DestNameContext(c context.Context, obj types.Object) (string, error) {
	dest, err := xRefTable.DereferenceContext(c, obj)
	if err != nil {
		return "", err
	}

	var s string

	switch d := dest.(type) {
	case types.Name:
		s = d.Value()
	case types.StringLiteral:
		s, err = types.StringLiteralToString(d)
	case types.HexLiteral:
		s, err = types.HexLiteralToString(d)
	}

	return s, err
}
