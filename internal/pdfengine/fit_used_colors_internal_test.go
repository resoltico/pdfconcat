// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type fitInlineColorCase struct {
	object     types.Object
	name       string
	components int
	reject     bool
}

func TestFitInlineImageResourceColorChannelsPreserveActualBinaryBoundary(t *testing.T) {
	t.Parallel()

	profile := types.StreamDict{Dict: types.Dict{"N": types.Integer(3)}, Content: []byte("opaque ICC profile")}

	cases := []fitInlineColorCase{
		{name: "device-gray", object: types.Name(fitDeviceGray), components: 1},
		{name: "device-rgb", object: types.Name(fitDeviceRGB), components: 3},
		{name: "device-cmyk", object: types.Name(fitDeviceCMYK), components: 4},
		{name: "cal-gray", object: types.Array{types.Name(fitCalGray), types.Dict{}}, components: 1},
		{name: "cal-rgb", object: types.Array{types.Name(fitCalRGB), types.Dict{}}, components: 3},
		{name: "lab", object: types.Array{types.Name("Lab"), types.Dict{}}, components: 3},
		{
			name:       "indexed",
			object:     types.Array{types.Name("Indexed"), types.Name(fitDeviceRGB), types.Integer(0), types.StringLiteral("lookup-color")},
			components: 1,
		},
		{
			name:       "separation",
			object:     types.Array{types.Name("Separation"), types.Name("Spot"), types.Name(fitDeviceRGB), types.Dict{}},
			components: 1,
		},
		{name: "icc", object: types.Array{types.Name(fitICCBased), profile}, components: 3},
		{
			name:       "device-n",
			object:     types.Array{types.Name(fitDeviceN), types.Array{types.Name("Cyan"), types.Name("Magenta")}},
			components: 2,
		},
		{name: "absent-color-space", object: nil, reject: true},
		{name: "not-color-space", object: types.Integer(3), reject: true},
		{name: "empty-family", object: types.Array{}, reject: true},
		{name: "unsupported-family", object: types.Array{types.Name(fitPattern)}, reject: true},
		{name: "icc-missing-profile", object: types.Array{types.Name(fitICCBased)}, reject: true},
		{name: "icc-no-stream", object: types.Array{types.Name(fitICCBased), types.Dict{}}, reject: true},
		{
			name:   "icc-invalid-count",
			object: types.Array{types.Name(fitICCBased), types.StreamDict{Dict: types.Dict{"N": types.Integer(2)}}},
			reject: true,
		},
		{
			name:   "icc-non-integer-count",
			object: types.Array{types.Name(fitICCBased), types.StreamDict{Dict: types.Dict{"N": types.Name("invalid")}}},
			reject: true,
		},
		{name: "device-n-no-names", object: types.Array{types.Name(fitDeviceN)}, reject: true},
		{name: "device-n-empty", object: types.Array{types.Name(fitDeviceN), types.Array{}}, reject: true},
		{name: "device-n-non-array", object: types.Array{types.Name(fitDeviceN), types.Integer(1)}, reject: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) { t.Parallel(); checkInlineColorBoundary(t, test) })
	}
}

func checkInlineColorBoundary(t *testing.T, test fitInlineColorCase) {
	t.Helper()

	pdf := guardContext(t)
	scope := types.Dict{fitColorSpace: types.Dict{guardColorAlias: test.object}}
	leaf := guardStream(t, pdf, "", nil)
	scope[keyXObject] = types.Dict{"After": leaf}

	payload := make([]byte, test.components)
	for n := range payload {
		payload[n] = byte(100 + n)
	}

	program := "BI /W 1 /H 1 /BPC 8 /CS /Alias ID " + string(payload) + " EI /After Do"

	err := guardInspect(t, newFitProgramInspector(pdf), program, scope)
	if (err != nil) != test.reject {
		t.Fatalf("color-space binary boundary reject=%t: %v", test.reject, err)
	}
}

func TestFitInlineColorAliasAndDefaultReplacementCyclesAreBounded(t *testing.T) {
	t.Parallel()

	for _, replacement := range []string{guardColorAlias, fitDeviceRGB} {
		t.Run(replacement, func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			spaces := types.Dict{guardColorAlias: types.Name(guardColorAlias), fitDefaultRGB: types.Name(fitDeviceRGB)}
			scope := types.Dict{fitColorSpace: spaces}

			content := "BI /W 1 /H 1 /BPC 8 /CS /" + replacement + " ID abc EI"
			if err := guardInspect(t, newFitProgramInspector(pdf), content, scope); err == nil {
				t.Fatal("active color-space cycle accepted")
			}
		})
	}
}
