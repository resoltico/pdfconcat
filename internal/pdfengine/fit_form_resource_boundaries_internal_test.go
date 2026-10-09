// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestFitInvokedXObjectRejectsMissingStreamAndSubtypeOrMalformedFormMetadata(t *testing.T) {
	t.Parallel()

	for n, object := range []types.Object{
		types.Integer(1),
		types.StreamDict{Dict: types.Dict{}},
		types.StreamDict{Dict: types.Dict{keySubtype: types.Integer(1)}},
		types.StreamDict{Dict: types.Dict{keySubtype: types.Name("PS")}},
	} {
		t.Run("object/"+strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)

			scope := types.Dict{keyXObject: types.Dict{"Bad": object}}
			if err := guardInspect(t, newFitProgramInspector(pdf), "/Bad Do", scope); err == nil {
				t.Fatal("invalid invoked XObject stream or subtype accepted")
			}
		})
	}

	changes := []func(types.Dict){
		func(dict types.Dict) { dict[keyResources] = types.Integer(1) },
		func(dict types.Dict) { dict[fitMatrix] = types.NewIntegerArray(1, 0, 0) },
		func(dict types.Dict) { dict[fitGroup] = types.Integer(1) },
	}
	for n, change := range changes {
		t.Run("form/"+strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			ref := guardStream(t, pdf, "", nil)
			entry, _ := pdf.FindTableEntry(ref.ObjectNumber.Value(), 0)

			stream, isStream := entry.Object.(types.StreamDict)
			if !isStream {
				t.Fatal("form fixture is not a stream")
			}

			change(stream.Dict)

			scope := types.Dict{keyXObject: types.Dict{"Bad": ref}}
			if err := guardInspect(t, newFitProgramInspector(pdf), "/Bad Do", scope); err == nil {
				t.Fatal("invalid used Form metadata accepted")
			}
		})
	}
}

func TestFitUsedGroupScalarContractAndNativeFailureIdentity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		object types.Object
		reject bool
	}{
		{object: nil},
		{object: types.Dict{}, reject: true},
		{
			object: types.Dict{
				"S":     types.Name(fitTransparency),
				keyType: types.Name(fitGroup),
				"I":     types.Boolean(true),
				"K":     types.Boolean(false),
			},
		},
		{object: types.Integer(1), reject: true},
		{object: types.Dict{"S": types.Name(fitTransparency), keyType: types.Integer(1)}, reject: true},
		{object: types.Dict{"S": types.Name(fitTransparency), keyType: types.Name(guardInvalidName)}, reject: true},
		{object: types.Dict{"S": types.Integer(1)}, reject: true},
		{object: types.Dict{"S": types.Name(guardInvalidName)}, reject: true},
		{object: types.Dict{"S": types.Name(fitTransparency), "I": types.Integer(1)}, reject: true},
		{object: types.Dict{"S": types.Name(fitTransparency), "K": types.Integer(1)}, reject: true},
	}
	for n, test := range cases {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			if err := newFitProgramInspector(pdf).group(t.Context(), test.object, nil); (err != nil) != test.reject {
				t.Fatalf("used Group scalar contract reject=%t: %v", test.reject, err)
			}
		})
	}

	pdf := guardContext(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := newFitProgramInspector(pdf).group(ctx, types.Dict{"S": types.Name(fitTransparency)}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("used Group resolver erased cancellation: %v", err)
	}
}

func TestFitLongResourceEdgeDiagnosticIsBoundedAndStillLocated(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	scope := types.Dict{}
	name := strings.Repeat("x", fitEdgeLabelByteLimit+100)
	ref := guardStream(t, pdf, "/"+name+" Do", scope)
	scope[keyXObject] = types.Dict{name: ref}

	err := guardInspect(t, newFitProgramInspector(pdf), "/"+name+" Do", scope)
	if err == nil || !strings.Contains(err.Error(), "...") || len(err.Error()) > 2048 {
		t.Fatalf("unbounded or unlocated recursive resource label: %v", err)
	}
}

func TestFitUsedMalformedCompressedProgramsRefuseWithoutBypassingNativeDecoder(t *testing.T) {
	t.Parallel()

	for _, programKind := range []string{guardGroupForm, guardType3Name, fitShading} {
		t.Run(programKind, func(t *testing.T) {
			t.Parallel()

			dictionary := "/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources <<>> /Filter /FlateDecode"
			resource := "/XObject << /Used 5 0 R >>"
			program := "/Used Do"
			extra := nullPDFObject

			switch programKind {
			case guardType3Name:
				resource = "/Font << /Used 6 0 R >>"
				program = "BT /Used 12 Tf (A) Tj"
				extra = "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] /FontMatrix [.001 0 0 .001 0 0] " +
					"/Resources <<>> /FirstChar 65 /LastChar 65 /Widths [0] /Encoding << /Differences [65 /A] >> " +
					"/CharProcs << /A 5 0 R >> >>"
			case fitShading:
				resource = "/Shading << /Used 5 0 R >>"
				program = "/Used sh"
				dictionary = "/ShadingType 4 /ColorSpace /DeviceRGB /Filter /FlateDecode"
			case guardGroupForm:
			default:
				t.Fatal("unknown compressed-program fixture")
			}

			doc := rawDoc(catalog, guardSinglePageTree,
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << "+resource+" >> /Contents 4 0 R >>",
				groupPresenceStream("", program), groupPresenceStream(dictionary, "not compressed data"), extra)

			pdf := readUnvalidated(t, doc)
			if _, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008}); err == nil {
				t.Fatal("used malformed compressed content accepted")
			}
		})
	}
}
