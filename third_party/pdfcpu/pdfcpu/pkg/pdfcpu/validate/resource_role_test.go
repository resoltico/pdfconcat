/*
Copyright 2026 The pdfcpu Authors.

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

package validate

import (
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"reflect"
	"testing"
)

func TestResourceValidationPreservesOtherDictionaryRoles(t *testing.T) {
	for _, mode := range []int{model.ValidationStrict, model.ValidationRelaxed} {
		table := validationOrderXRefTable()
		table.ValidationMode = mode
		dictionary := types.Dict{
			"Type": types.Name("Annot"), "Subtype": types.Name("Link"),
			"Rect":   types.Array{types.Integer(10), types.Integer(10), types.Integer(90), types.Integer(90)},
			"Border": types.Array{types.Integer(0), types.Integer(0), types.Integer(0)},
			"A":      types.Dict{"S": types.Name("URI"), "URI": types.StringLiteral("https://example.invalid/")},
		}
		expected := dictionary.Clone()
		if _, err := validateResourceDict(t.Context(), table, dictionary); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(dictionary, expected) {
			t.Fatalf("resource validation changed another role: %v", dictionary)
		}
		dictionary["Font"] = types.Integer(1)
		if _, err := validateResourceDict(t.Context(), table, dictionary); err == nil {
			t.Fatal("malformed recognized font resource accepted")
		}
	}
}
