// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"encoding/json"
	"reflect"
	"testing"
)

type progressCustomScalar int

func TestProgressEncodingExceptionRetainsClosedInfallibleFieldTypes(t *testing.T) {
	t.Parallel()

	record := reflect.TypeFor[progressRecord]()
	for field := range record.Fields() {
		if !infallibleProgressField(field.Type) {
			t.Fatalf("progressRecord.%s breaks the exact infallible encoding premise: %s", field.Name, field.Type)
		}
	}

	for _, field := range []reflect.Type{
		reflect.TypeFor[float64](), reflect.TypeFor[map[string]string](), reflect.TypeFor[[]int](),
		reflect.TypeFor[*string](), reflect.TypeFor[**int64](), reflect.TypeFor[progressRecord](), reflect.TypeFor[progressCustomScalar](),
		reflect.TypeFor[json.Number](),
	} {
		if infallibleProgressField(field) {
			t.Fatalf("unsafe/custom field type accepted by encoding premise guard: %s", field)
		}
	}
}

func infallibleProgressField(field reflect.Type) bool {
	pointer := field.Kind() == reflect.Pointer
	if pointer {
		field = field.Elem()
	}

	if field.PkgPath() != "" {
		return false
	}

	kind := field.Kind()

	return kind == reflect.String && !pointer || kind >= reflect.Int && kind <= reflect.Int64 ||
		kind >= reflect.Uint && kind <= reflect.Uint64
}
