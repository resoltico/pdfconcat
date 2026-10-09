// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const (
	blendingICCCountFixture = "ICC count"
	blendingRGBSignature    = "RGB "
)

func TestFitBlendingSpacesRetainIndependentComponents(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		scope types.Dict
		space types.Object
		name  string

		components int
	}{
		{nil, types.Name(fitDeviceGray), "gray", 1},
		{nil, types.Name(fitDeviceRGB), "rgb", 3},
		{nil, types.Name(fitDeviceCMYK), "cmyk", 4},
		{nil, types.Array{types.Name(fitCalGray), types.Dict{}}, "calgray", 1},
		{nil, types.Array{types.Name(fitCalRGB), types.Dict{}}, "calrgb", 3},
		{
			types.Dict{fitColorSpace: types.Dict{"DeviceR#47B": types.Array{types.Name(fitCalGray), types.Dict{}}}},
			types.Name("DeviceR#47B"), "literal hash device-looking alias", 1,
		},
		{types.Dict{fitColorSpace: types.Dict{"Alias": types.Name(fitDeviceRGB)}}, types.Name("Alias"), "resource alias", 3},
		{
			types.Dict{fitColorSpace: types.Dict{fitDefaultRGB: types.Array{types.Name(fitCalRGB), types.Dict{}}}},
			types.Name(fitDeviceRGB), "default", 3,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			i := newFitProgramInspector(guardContext(t))

			got, err := i.blendingSpace(t.Context(), tc.scope, tc.space, 0)
			if err != nil || got != tc.components {
				t.Fatalf("components=%d error=%v", got, err)
			}
		})
	}
}

func TestFitBlendingSpaceRefusalsPreserveSafetyAndCancellation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		scope types.Dict
		space types.Object
		name  string
	}{
		{nil, types.Integer(7), "scalar"},
		{nil, types.Array{types.Name(fitCalRGB)}, "wrong arity"},
		{nil, types.Array{types.Name("Indexed"), types.Dict{}}, "non-component family"},
		{nil, types.Name("Missing"), "missing alias"},
		{nil, types.Name("Bad#zz"), "missing literal hash alias"},
		{
			types.Dict{fitColorSpace: types.Dict{fitDefaultRGB: types.Name(fitDeviceGray)}},
			types.Name(fitDeviceRGB), "wrong default components",
		},
		{types.Dict{fitColorSpace: types.Dict{fitDefaultRGB: types.Name(fitDeviceRGB)}}, types.Name(fitDeviceRGB), "cyclic default"},
		{types.Dict{fitColorSpace: types.Integer(1)}, types.Name(fitDeviceRGB), "malformed defaults"},
		{nil, types.Array{types.Integer(1), types.Dict{}}, "invalid family name"},
		{nil, types.Array{types.Name(fitCalGray), types.NewIndirectRef(999, 0)}, "dangling calibration"},
		{nil, types.Array{types.Name(fitICCBased), types.Dict{}}, "ICC requires stream"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			i := newFitProgramInspector(guardContext(t))
			if _, err := i.blendingSpace(t.Context(), tc.scope, tc.space, 0); err == nil {
				t.Fatalf("unsafe space accepted: %v", err)
			}
		})
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := newFitProgramInspector(guardContext(t)).blendingSpace(ctx, nil, types.Name(fitDeviceRGB), 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
}

func TestFitICCHeaderMatchesDeclaredIndependentInputSpace(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		signature string
		declared  int
		valid     bool
	}{
		{"gray", "GRAY", 1, true},
		{"rgb", blendingRGBSignature, 3, true},
		{"cmyk", "CMYK", 4, true},
		{"component mismatch", blendingRGBSignature, 4, false},
		{"non-independent input", "Lab ", 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := inspectBlendingICC(t, tc.signature, tc.declared)
			if tc.valid {
				if err != nil || got != tc.declared {
					t.Fatalf("ICC components=%d error=%v", got, err)
				}
			} else if !errors.Is(err, errFitUnsupported) {
				t.Fatalf("invalid ICC accepted: %v", err)
			}
		})
	}

	for _, data := range [][]byte{nil, make([]byte, 128), append([]byte{0, 0, 0, 127}, make([]byte, 124)...)} {
		if _, err := fitICCComponents(data); !errors.Is(err, errFitUnsupported) {
			t.Fatal("invalid ICC header accepted")
		}
	}
}

func inspectBlendingICC(t *testing.T, signature string, declared int) (int, error) {
	t.Helper()

	pdf := guardContext(t)
	data := make([]byte, 128)
	binary.BigEndian.PutUint32(data[:4], 128)
	copy(data[16:20], signature)
	copy(data[36:40], "acsp")

	stream, err := pdf.NewStreamDictForBuf(data)
	if err != nil {
		t.Fatal(err)
	}

	stream.Dict["N"] = types.Integer(declared)

	ref, err := storeFitStream(t.Context(), pdf, stream)
	if err != nil {
		t.Fatal(err)
	}

	return newFitProgramInspector(pdf).blendingSpace(t.Context(), nil, types.Array{types.Name(fitICCBased), *ref}, 0)
}

func TestFitBlendingRetainsNativeLazyMetadataErrors(t *testing.T) {
	t.Parallel()

	for _, location := range []string{"root color space", "calibration", "calibration member", blendingICCCountFixture, "ICC content"} {
		t.Run(location, func(t *testing.T) {
			t.Parallel()
			assertNativeBlendingError(t, location)
		})
	}
}

func assertNativeBlendingError(t *testing.T, location string) {
	t.Helper()

	pdf := guardContext(t)
	broken := fitMalformedLazyReference(t, pdf)

	var object types.Object = broken

	switch location {
	case "calibration":
		object = types.Array{types.Name(fitCalRGB), broken}
	case "calibration member":
		object = types.Array{types.Name(fitCalRGB), types.Dict{"WhitePoint": broken}}
	case blendingICCCountFixture, "ICC content":
		stream, err := pdf.NewStreamDictForBuf(make([]byte, 128))
		if err != nil {
			t.Fatal(err)
		}

		stream.Dict["N"] = types.Integer(3)
		if location == blendingICCCountFixture {
			stream.Dict["N"] = broken
		} else {
			stream.Content = nil
			stream.Raw = []byte("invalid compressed ICC payload")
		}

		object = types.Array{types.Name(fitICCBased), fitIndirect(t, pdf, *stream)}
	default:
	}

	if _, err := newFitProgramInspector(pdf).blendingSpace(t.Context(), nil, object, 0); err == nil {
		t.Fatal("native malformed blending metadata was silently accepted")
	}
}

func TestFitICCDeclaredLengthMustMatchAvailableHeader(t *testing.T) {
	t.Parallel()

	for _, declared := range []uint32{127, 129} {
		data := make([]byte, 128)
		copy(data[36:40], "acsp")
		copy(data[16:20], blendingRGBSignature)
		binary.BigEndian.PutUint32(data[:4], declared)

		if _, err := fitICCComponents(data); !errors.Is(err, errFitUnsupported) {
			t.Fatalf("invalid ICC declared length %d accepted: %v", declared, err)
		}
	}
}
