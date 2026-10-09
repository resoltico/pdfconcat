// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine_test

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

type (
	fitLinkDump struct {
		objects map[string]any
		pages   []string
	}
	fitLinkPage struct {
		Object string `json:"object"`
	}
	fitLinkJSON struct {
		QPDF  []json.RawMessage `json:"qpdf"`
		Pages []fitLinkPage     `json:"pages"`
	}
	fitQuadEncoding int
)

const (
	fitOracleURI                          = "https://example.invalid/a%2Fb?x=1&y=2#part"
	fitOracleStreamFormat                 = "<< /Length %d >>\nstream\n%sendstream"
	fitPerimeterEncoding  fitQuadEncoding = 0
	fitZEncoding          fitQuadEncoding = 1
)

func TestFittedLinksPreserveSerializedVertexSlotsHitRegionsAndActionOccurrences(t *testing.T) {
	t.Parallel()
	pdforacle.RequireTools(t)

	for _, paper := range []string{"A4", "Legal"} {
		for _, rotation := range []int{0, 90, 180, 270} {
			for _, encoding := range []fitQuadEncoding{fitPerimeterEncoding, fitZEncoding} {
				t.Run(fmt.Sprintf("%s/rotation%d/encoding%d", paper, rotation, encoding), func(t *testing.T) {
					t.Parallel()
					verifyFittedLinkOracle(t, paper, rotation, encoding)
				})
			}
		}
	}
}

func TestFittedLinkDestinationsPreserveTypedNamespaceCollision(t *testing.T) {
	t.Parallel()
	pdforacle.RequireTools(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "typed-destinations.pdf")

	doc := &pdffixture.Doc{Version: "1.7", Objs: [][]byte{
		[]byte("<< /Type /Catalog /Pages 2 0 R /Dests << /same [3 0 R /Fit] >> /Names << /Dests << /Names [(same) [4 0 R /Fit]] >> >> >>"),
		[]byte("<< /Type /Pages /Count 2 /Kids [3 0 R 4 0 R] /MediaBox [0 0 100 100] >>"),
		[]byte("<< /Type /Page /Parent 2 0 R /Annots [5 0 R] >>"),
		[]byte("<< /Type /Page /Parent 2 0 R >>"),
		[]byte(
			"<< /Type /Annot /Subtype /Link /P 3 0 R /Rect [10 10 90 90] /Border [0 0 0] " +
				"/A << /S /GoTo /D /same /Next << /S /GoTo /D (same) >> >> >>",
		),
	}}
	if err := doc.WriteFile(source); err != nil {
		t.Fatal(err)
	}

	output := assembleOracleLinks(t, source, dir, pdfengine.PageSize{612, 1008}, []pdfengine.Run{pdfengine.SourcePages(0, 1, 2)})
	link := output.link(t, 0)
	action := output.dictionary(t, link["/A"])

	next := output.dictionary(t, action["/Next"])
	if output.destinationPage(t, action["/D"]) != output.pages[0] || output.destinationPage(t, next["/D"]) != output.pages[1] {
		t.Fatal("named destination namespaces with equal spelling resolved to the wrong serialized pages")
	}
}

func verifyFittedLinkOracle(t *testing.T, paper string, rotation int, encoding fitQuadEncoding) {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "hotspot.pdf")

	quad := fitOracleQuad(encoding)
	if err := fitPolygonFixture(rotation, quad).WriteFile(source); err != nil {
		t.Fatal(err)
	}

	size, err := assembly.ParsePageSize(paper)
	if err != nil {
		t.Fatal(err)
	}

	target := pdfengine.PageSize{Width: float64(size.Dim.Width), Height: float64(size.Dim.Height)}
	output := assembleOracleLinks(t, source, dir, target, []pdfengine.Run{pdfengine.SourcePages(0, 1, 1), pdfengine.SourcePages(0, 1, 1)})

	want := make([]float64, len(quad))
	for index := 0; index < len(quad); index += 2 {
		want[index], want[index+1] = independentFitPoint(quad[index], quad[index+1], rotation, target)
	}

	for page := range output.pages {
		link := output.link(t, page)

		actual := fitOracleNumbers(t, link["/QuadPoints"])
		if !fitSlotsMatch(actual, want) {
			t.Fatalf("serialized vertex identities changed: %v want %v", actual, want)
		}

		verifyFitHitRegion(t, link, quad, actual, rotation, encoding, target)

		if !output.actionOccurrenceMatches(t, link["/A"], output.pages[page]) {
			t.Fatal("ordered actions changed target bytes or source occurrence")
		}

		if output.actionOccurrenceMatches(t, link["/A"], output.pages[1-page]) {
			t.Fatal("wrong-occurrence destination escaped the independent action oracle")
		}
	}
}

func fitOracleQuad(encoding fitQuadEncoding) []float64 {
	if encoding == fitZEncoding {
		return []float64{35, 170, 75, 170, 20, 50, 90, 50}
	}

	return []float64{20, 50, 90, 50, 75, 170, 35, 170}
}

func fitPolygonFixture(rotation int, quad []float64) *pdffixture.Doc {
	doc := pdffixture.URILink("polygon", fitOracleURI, "", false)
	doc.Objs[2] = fmt.Appendf(nil, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 120 240] /CropBox [10 20 110 220] "+
		"/Rotate %d /UserUnit 2 /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R /Annots [6 0 R] >>", rotation)

	var content strings.Builder
	for index := 0; index < len(quad); index += 2 {
		fmt.Fprintf(&content, "BT /F1 6 Tf 1 0 0 1 %g %g Tm (%d) Tj ET\n", quad[index], quad[index+1], index/2+1)
	}

	doc.Objs[3] = fmt.Appendf(nil, fitOracleStreamFormat, content.Len(), content.String())

	coordinates := strings.Trim(fmt.Sprint(quad), "[]")
	doc.Objs[5] = fmt.Appendf(nil, "<< /Type /Annot /Subtype /Link /P 3 0 R /Rect [20 50 90 170] /Border [0 0 0] "+
		"/QuadPoints [%s] /A << /S /URI /URI (%s) /Next [<< /S /URI /URI (%s) >> << /S /GoTo /D [3 0 R /Fit] >>] >> >>",
		coordinates, fitOracleURI, fitOracleURI)

	return doc
}

func assembleOracleLinks(t *testing.T, source, dir string, target pdfengine.PageSize, order []pdfengine.Run) fitLinkDump {
	t.Helper()

	engine, err := pdfengine.New()
	if err != nil {
		t.Fatal(err)
	}

	info, err := engine.Inspect(t.Context(), source, &target)
	if err != nil {
		t.Fatal(err)
	}

	request := pdfengine.AssembleRequest{
		FitTarget: &target, Sources: []pdfengine.SourceFile{{Path: source, Info: info}},
		Order:         order,
		ExpectedPages: 2, Destination: filepath.Join(dir, "fitted.pdf"),
	}
	if err = engine.Assemble(t.Context(), &request); err != nil {
		t.Fatal(err)
	}

	return loadFitLinkDump(t, request.Destination)
}

func loadFitLinkDump(t *testing.T, path string) fitLinkDump {
	t.Helper()

	bytes, err := pdforacle.QPDFJSON(pdforacle.RequireTools(t), path)
	if err != nil {
		t.Fatalf("independent link structure: %v", err)
	}

	var dump fitLinkJSON

	if err = json.Unmarshal(bytes, &dump); err != nil || len(dump.QPDF) != 2 {
		t.Fatalf("independent link dump malformed: %v", err)
	}

	view := fitLinkDump{}
	if err = json.Unmarshal(dump.QPDF[1], &view.objects); err != nil {
		t.Fatal(err)
	}

	for _, page := range dump.Pages {
		view.pages = append(view.pages, page.Object)
	}

	return view
}

func (v fitLinkDump) dereference(t *testing.T, object any) any {
	t.Helper()

	for range 20 {
		name, reference := object.(string)
		if !reference || !strings.HasSuffix(name, " R") {
			return object
		}

		record, present := v.objects["obj:"+name].(map[string]any)
		if !present {
			t.Fatalf("missing independent object %s", name)
		}

		object = record["value"]
	}

	t.Fatal("independent object reference depth exceeded")

	return nil
}

func (v fitLinkDump) dictionary(t *testing.T, object any) map[string]any {
	t.Helper()

	dict, valid := v.dereference(t, object).(map[string]any)
	if !valid {
		t.Fatal("independent dictionary is absent or malformed")
	}

	return dict
}

func (v fitLinkDump) link(t *testing.T, page int) map[string]any {
	t.Helper()
	object := v.dictionary(t, v.pages[page])

	annotations, valid := v.dereference(t, object["/Annots"]).([]any)
	if !valid || len(annotations) != 1 {
		t.Fatal("independent page must own exactly one Link")
	}

	link := v.dictionary(t, annotations[0])
	if link["/P"] != v.pages[page] || link["/Subtype"] != "/Link" {
		t.Fatal("independent annotation identity or ownership changed")
	}

	return link
}

func (v fitLinkDump) actionOccurrenceMatches(t *testing.T, object any, page string) bool {
	t.Helper()

	action := v.dictionary(t, object)
	if action["/S"] != "/URI" || action["/URI"] != "u:"+fitOracleURI {
		return false
	}

	sequence, valid := v.dereference(t, action["/Next"]).([]any)
	if !valid || len(sequence) != 2 {
		return false
	}

	secondary := v.dictionary(t, sequence[0])
	local := v.dictionary(t, sequence[1])
	destination, valid := v.dereference(t, local["/D"]).([]any)

	return secondary["/S"] == "/URI" && secondary["/URI"] == "u:"+fitOracleURI && local["/S"] == "/GoTo" &&
		valid && len(destination) == 2 && destination[0] == page && destination[1] == "/Fit"
}

func (v fitLinkDump) destinationPage(t *testing.T, object any) any {
	t.Helper()

	value := v.dereference(t, object)
	if array, direct := value.([]any); direct {
		if len(array) != 2 || array[1] != "/Fit" {
			t.Fatal("independent destination changed its coordinate-free mode")
		}

		return array[0]
	}

	name, named := value.(string)
	if !named {
		t.Fatal("independent destination is neither direct nor named")
	}

	trailer := v.dictionary(t, v.objects["trailer"])

	root := v.dictionary(t, v.dictionary(t, trailer["value"])["/Root"])
	if strings.HasPrefix(name, "/") {
		return v.destinationPage(t, v.dictionary(t, root["/Dests"])[name])
	}

	names := v.dictionary(t, root["/Names"])
	tree := v.dictionary(t, names["/Dests"])

	entries, valid := v.dereference(t, tree["/Names"]).([]any)
	if !valid || len(entries)%2 != 0 {
		t.Fatal("independent destination tree has malformed pairs")
	}

	for index := 0; index < len(entries); index += 2 {
		if entries[index] == name {
			return v.destinationPage(t, entries[index+1])
		}
	}

	t.Fatal("independent destination tree name is unresolved")

	return nil
}

func fitOracleNumbers(t *testing.T, object any) []float64 {
	t.Helper()

	array, valid := object.([]any)
	if !valid {
		t.Fatal("independent hotspot has no numeric array")
	}

	values := make([]float64, len(array))
	for index, value := range array {
		number, numeric := value.(float64)
		if !numeric {
			t.Fatal("independent hotspot contains nonnumeric data")
		}

		values[index] = number
	}

	return values
}

func independentFitPoint(sourceX, sourceY float64, rotation int, target pdfengine.PageSize) (float64, float64) {
	sourceX, sourceY = sourceX-10, sourceY-20
	width, height := 100.0, 200.0

	switch rotation {
	case 90:
		sourceX, sourceY, width, height = sourceY, 100-sourceX, 200, 100
	case 180:
		sourceX, sourceY = 100-sourceX, 200-sourceY
	case 270:
		sourceX, sourceY, width, height = 200-sourceY, sourceX, 200, 100
	default:
	}

	scale := min(target.Width/width, target.Height/height)

	return (target.Width-width*scale)/2 + sourceX*scale, (target.Height-height*scale)/2 + sourceY*scale
}

func fitSlotsMatch(actual, want []float64) bool {
	if len(actual) != len(want) {
		return false
	}

	for index, value := range actual {
		if math.Abs(value-want[index]) > 0.000001 {
			return false
		}
	}

	return true
}

func verifyFitHitRegion(
	t *testing.T,
	link map[string]any,
	source, output []float64,
	rotation int,
	encoding fitQuadEncoding,
	target pdfengine.PageSize,
) {
	t.Helper()

	for _, point := range [][2]float64{{55, 110}, {22, 160}, {15, 100}} {
		x, y := independentFitPoint(point[0], point[1], rotation, target)
		if fitPolygonContains(source, point[0], point[1], encoding) != fitPolygonContains(output, x, y, encoding) {
			t.Fatal("independent activation polygon changed hit membership")
		}
	}

	x, y := independentFitPoint(22, 160, rotation, target)

	rect := fitOracleNumbers(t, link["/Rect"])
	if len(rect) != 4 || x <= rect[0] || x >= rect[2] || y <= rect[1] || y >= rect[3] || fitPolygonContains(output, x, y, encoding) {
		t.Fatal("outside-quad/inside-Rect negative control no longer distinguishes polygon from Rect fallback")
	}

	wrong := slices.Clone(output)

	wrong[0], wrong[2], wrong[1], wrong[3] = wrong[2], wrong[0], wrong[3], wrong[1]
	if fitSlotsMatch(wrong, output) {
		t.Fatal("incorrect output slot permutation escaped the exact-slot oracle")
	}
}

func fitPolygonContains(points []float64, x, y float64, encoding fitQuadEncoding) bool {
	order := [4]int{0, 2, 4, 6}
	if encoding == fitZEncoding {
		order = [4]int{0, 2, 6, 4}
	}

	inside := false

	previous := order[3]
	for _, current := range order {
		x1, y1, x2, y2 := points[current], points[current+1], points[previous], points[previous+1]
		if (y1 > y) != (y2 > y) && x < (x2-x1)*(y-y1)/(y2-y1)+x1 {
			inside = !inside
		}

		previous = current
	}

	return inside
}
