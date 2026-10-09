// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

// sourceIdentityPDF writes actual classic xref identities, including free entries.
func sourceIdentityPDF(version, trailer string, bodies []string, generations map[int]int, free map[int]bool) []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, "%%PDF-%s\n%%\xe2\xe3\xcf\xd3\n", version)

	offsets := make([]int, len(bodies)+1)
	for index, body := range bodies {
		number := index + 1
		if free[number] {
			continue
		}

		offsets[number] = out.Len()
		fmt.Fprintf(&out, "%d %d obj\n%s\nendobj\n", number, generations[number], body)
	}

	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))

	for number := 1; number < len(offsets); number++ {
		if free[number] {
			fmt.Fprintf(&out, "0000000000 %05d f \n", generations[number])
			continue
		}

		fmt.Fprintf(&out, "%010d %05d n \n", offsets[number], generations[number])
	}

	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R %s >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), trailer, xref)

	return out.Bytes()
}

func TestSourceNullReferencesSurviveFittingAndOrdinaryAssembly(t *testing.T) {
	t.Parallel()

	for _, source := range []struct {
		free       map[int]bool
		name       string
		version    string
		trailer    string
		value      string
		targetBody []string
	}{
		{name: "undefined", version: fitCatalogSourceVersion, value: sourceFiveReference},
		{name: "free", version: fitCatalogSourceVersion, value: sourceFiveReference, targetBody: []string{""}, free: map[int]bool{5: true}},
		{name: "explicit null", version: fitCatalogSourceVersion, value: sourceFiveReference, targetBody: []string{nullPDFObject}},
		{name: "direct info", version: "2.0", value: sourceFiveReference, trailer: "/Info << /Title (source metadata) /Private [5 0 R] >>"},
		{name: "missing info", version: fitCatalogSourceVersion, value: sourceFiveReference, trailer: "/Info 5 0 R"},
	} {
		t.Run(source.name, func(t *testing.T) {
			t.Parallel()

			engine := sourceNewEngine(t)

			bodies := make([]string, 0, 4+len(source.targetBody))
			bodies = append(bodies,
				catalog,
				"<< /Type /Pages /Count 2 /Kids [3 0 R 4 0 R] /MediaBox [0 0 100 100] /Resources <<>> >>",
				"<< /Type /Page /Parent 2 0 R >>",
				fmt.Sprintf(
					"<< /Type /Page /Parent 2 0 R /Annots %s /Private << /Values [%s << /Value %s >>] >> >>",
					source.value,
					source.value,
					source.value,
				),
			)
			bodies = append(bodies, source.targetBody...)
			data := sourceIdentityPDF(source.version, source.trailer, bodies, nil, source.free)
			dir := t.TempDir()

			path := filepath.Join(dir, fitRenderSource)
			sourceWrite(t, path, data)

			for _, target := range []*PageSize{nil, {595.2755905511812, 841.8897637795276}, {612, 1008}} {
				sourceNullRoundTrip(t, engine, path, data, target)
			}
		})
	}
}

func TestSourcePackedNullReferencesAreMaterializedPersistently(t *testing.T) {
	t.Parallel()

	data := fitPackedSource("/Resources <<>> /Private 7 0 R", emptyAppearanceDrawing, "<< /Values [8 0 R << /Value 8 0 R >>] >>")

	pdf, err := api.ReadContext(t.Context(), bytes.NewReader(data), model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}

	object := sourceTableEntry(t, pdf, 7).Object
	if _, lazy := object.(types.LazyObjectStreamObject); lazy {
		t.Fatal("retained packed object still serializes original reference body")
	}

	dict, ok := object.(types.Dict)
	if !ok {
		t.Fatalf("packed object type: %T", object)
	}

	values := sourceArray(t, dict["Values"])
	if len(values) != 2 || values[0] != nil || sourceDict(t, values[1])["Value"] != nil {
		t.Fatalf("packed null positions changed: %#v", values)
	}
}

func TestSourceOptionalTypedNullAndRequiredValues(t *testing.T) {
	t.Parallel()

	engine := sourceNewEngine(t)

	for _, transition := range []string{"/Trans << /S 99 0 R >>", "/Trans << /S null >>"} {
		doc := pdffixture.Pages("optional typed null", 1)
		doc.Objs[3] = bytes.Replace(doc.Objs[3], []byte("/Type /Page"), []byte("/Type /Page "+transition), 1)

		path := filepath.Join(t.TempDir(), fitRenderSource)
		if err := doc.WriteFile(path); err != nil {
			t.Fatal(err)
		}

		if _, err := engine.Inspect(t.Context(), path, nil); err != nil {
			t.Fatalf("optional null transition: %v", err)
		}
	}

	data := sourceIdentityPDF(
		fitCatalogSourceVersion,
		"",
		[]string{
			catalog,
			guardSinglePageTree,
			"<< /Type /Page /Parent 2 0 R /MediaBox 99 0 R /Resources <<>> >>",
		},
		nil,
		nil,
	)

	path := filepath.Join(t.TempDir(), "required-null.pdf")
	sourceWrite(t, path, data)

	if _, err := engine.Inspect(t.Context(), path, nil); err == nil {
		t.Fatal("required null MediaBox admitted")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := engine.Inspect(ctx, path, nil); CodeOf(err) != CodeCanceled {
		t.Fatalf("cancellation changed: %v", err)
	}
}

func TestSourceNullRewriteRetainsLiveIdentityAndArrayPositions(t *testing.T) {
	t.Parallel()

	bodies := []string{
		"<< /Type /Catalog /Pages 2 0 R /Private 5 0 R >>",
		guardSinglePageTree,
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources <<>> /Private << /Values [99 0 R 4 7 R 4 8 R] >> >>",
		"(live value)",
		sourceMissingReference,
	}
	data := sourceIdentityPDF(fitCatalogSourceVersion, "/Info 99 0 R /ID [(source) 99 0 R]", bodies, map[int]int{4: 7}, nil)

	pdf, err := api.ReadContext(t.Context(), bytes.NewReader(data), model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}

	if pdf.Info != nil || len(pdf.ID) != 2 || pdf.ID[1] != nil {
		t.Fatalf("null trailer values changed: %v %v", pdf.Info, pdf.ID)
	}

	if pdf.Table[5].Object != nil {
		t.Fatalf("standalone reference body retained undefined target: %v", pdf.Table[5].Object)
	}

	if pdf.RootDict["Private"] != *types.NewIndirectRef(5, 0) {
		t.Fatal("live reference chain was collapsed")
	}

	page := sourceDict(t, sourceTableEntry(t, pdf, 3).Object)

	values := sourceArray(t, sourceDict(t, page["Private"])["Values"])
	sourceRequireLiveArray(t, values)

	ref, err := pdf.IndRefForNewObject(types.StringLiteral("allocated value"))
	if err != nil {
		t.Fatal(err)
	}

	if ref == nil || values[0] != nil || values[2] != nil {
		t.Fatal("allocation revived null values")
	}
}

func TestSourceNullRewriteHonorsReadLimitsAndCancellation(t *testing.T) {
	t.Parallel()

	packedBody := "[" + strings.Repeat("8 0 R ", 4096) + "]"
	data := fitPackedSource(sourceEmptyResources, emptyAppearanceDrawing, packedBody)

	pdf, err := api.ReadContext(t.Context(), bytes.NewReader(data), model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}

	values := sourceArray(t, sourceTableEntry(t, pdf, 7).Object)
	if len(values) != 4096 {
		t.Fatalf("packed cardinality: %d", len(values))
	}

	for _, value := range values {
		if value != nil {
			t.Fatalf("packed undefined target retained: %v", value)
		}
	}

	ctx, cancel := context.WithCancel(t.Context())

	_, err = api.ReadContextWithSourceInspection(
		ctx,
		bytes.NewReader(data),
		model.NewDefaultConfiguration(),
		func(context.Context, *model.Context) error { cancel(); return nil },
	)
	if err == nil || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("canceled source normalization returned: %v", err)
	}

	conf := model.NewDefaultConfiguration()

	conf.Limits.MaxObjectCount = 4
	if _, err = api.ReadContext(t.Context(), bytes.NewReader(data), conf); err == nil {
		t.Fatal("packed source object count limit ignored")
	}

	nested := strings.Repeat("[", 20) + "8 0 R" + strings.Repeat("]", 20)
	deep := fitPackedSource(sourceEmptyResources, emptyAppearanceDrawing, nested)
	conf = model.NewDefaultConfiguration()

	conf.Limits.MaxRecursionDepth = 10
	if _, err = api.ReadContext(t.Context(), bytes.NewReader(deep), conf); err == nil {
		t.Fatal("source direct-container depth limit ignored")
	}
}
