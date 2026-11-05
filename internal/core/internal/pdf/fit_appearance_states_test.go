// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

// Both pages share an AP dictionary and three original appearance streams, but
// their rectangles, page dimensions and rotations require different mappings.
// Each state has distinct artwork, so substituting the normal appearance fails.
func sharedAppearanceDocument(rotation, flags int) *pdffixture.Doc {
	doc := pdffixture.FitAnnotation("Line", "/L [10 20 70 90]", true, rotation, flags)
	doc.Objs[1] = []byte("<< /Type /Pages /Count 2 /Kids [3 0 R 11 0 R] >>")
	doc.Objs[5] = []byte(fmt.Sprintf("<< /Type /Annot /Subtype /Line /Rect [10 20 70 90] /L [10 20 70 90] /F %d /AP 10 0 R /AS /On >>", flags))
	green := []byte("0 1 0 rg 8 4 58 27 re f")
	blue := []byte("0 0 1 rg 4 4 m 76 8 l 20 36 l h f")
	doc.Objs = append(doc.Objs,
		[]byte(fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 80 40] /Matrix [1 .25 .5 1 3 7] /Resources << >> /Length %d >>\nstream\n%s\nendstream", len(green), green)),
		[]byte(fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 80 40] /Matrix [-1 0 0 1 80 0] /Resources << >> /Length %d >>\nstream\n%s\nendstream", len(blue), blue)),
		[]byte("<< /N << /On 7 0 R /Off 8 0 R >> /R << /On 8 0 R /Off 9 0 R >> /D 9 0 R >>"),
		[]byte(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Rotate %d /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R /Annots [12 0 R] >>", (rotation+90)%360)),
		[]byte(fmt.Sprintf("<< /Type /Annot /Subtype /Line /Rect [30 10 170 70] /L [30 10 170 70] /F %d /AP 10 0 R /AS /On >>", flags)),
	)
	return doc
}

type appearanceChoice struct {
	kind, state string
	object      types.Object
}

func appearanceChoices(t *testing.T, s *model.Context, page int) (types.Dict, []appearanceChoice) {
	t.Helper()
	d, _, _, err := s.PageDict(t.Context(), page, false)
	if err != nil {
		t.Fatal(err)
	}
	annots, err := s.DereferenceArray(d["Annots"])
	if err != nil {
		t.Fatal(err)
	}
	if len(annots) != 1 {
		t.Fatalf("page %d: got %d annotations", page, len(annots))
	}
	annot, err := s.DereferenceDict(annots[0])
	if err != nil {
		t.Fatal(err)
	}
	ap, err := s.DereferenceDict(annot["AP"])
	if err != nil {
		t.Fatal(err)
	}
	var choices []appearanceChoice
	for _, kind := range []string{"N", "R", "D"} {
		value, err := s.Dereference(ap[kind])
		if err != nil {
			t.Fatal(err)
		}
		if states, ok := value.(types.Dict); ok {
			for _, state := range []string{"On", "Off"} {
				if states[state] == nil {
					t.Fatalf("%s/%s state missing", kind, state)
				}
				choices = append(choices, appearanceChoice{kind, state, states[state]})
			}
		} else {
			if value == nil {
				t.Fatalf("%s appearance missing", kind)
			}
			choices = append(choices, appearanceChoice{kind, "", ap[kind]})
		}
	}
	return annot, choices
}

func chooseAppearance(t *testing.T, s *model.Context, annot types.Dict, choice appearanceChoice) {
	t.Helper()
	ap, err := s.DereferenceDict(annot["AP"])
	if err != nil {
		t.Fatal(err)
	}
	ap = clonedDirectDict(ap)
	ap["N"] = choice.object
	annot["AP"] = ap
}

func appearanceStateRaster(t *testing.T, path string, page int, dpi float64) image.Image {
	t.Helper()
	tools := oracle.RequireTools(t)
	prefix := filepath.Join(t.TempDir(), "state")
	result, err := exec.CommandContext(t.Context(), tools.PDFToPPM, "-cropbox", "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), "-singlefile", "-r", strconv.FormatFloat(dpi, 'f', -1, 64), "-png", path, prefix).CombinedOutput()
	if err != nil {
		t.Fatalf("raster: %v %s", err, result)
	}
	data, err := os.ReadFile(prefix + ".png")
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func compareAppearanceState(t *testing.T, source, candidate string, page, rotation int) {
	t.Helper()
	width, height := 100.0, 100.0
	if page == 2 {
		width, height = 200, 100
		if (rotation+90)%180 != 0 {
			width, height = height, width
		}
	}
	scale := math.Min(300/width, 500/height)
	expected := appearanceStateRaster(t, source, page, 72*scale)
	actual := appearanceStateRaster(t, candidate, page, 72)
	if actual.Bounds().Dx() != 300 || actual.Bounds().Dy() != 500 {
		t.Fatalf("target dimensions: %v", actual.Bounds())
	}
	left, top := int(math.Round((300-width*scale)/2)), int(math.Round((500-height*scale)/2))
	mismatches, colored := 0, 0
	for y := range 500 {
		for x := range 300 {
			er, eg, eb := uint32(65535), uint32(65535), uint32(65535)
			if image.Pt(x-left, y-top).In(expected.Bounds()) {
				er, eg, eb, _ = expected.At(x-left, y-top).RGBA()
			}
			if er < 50000 || eg < 50000 || eb < 50000 {
				colored++
			}
			ar, ag, ab, _ := actual.At(x, y).RGBA()
			if math.Abs(float64(ar)-float64(er)) > 24*257 || math.Abs(float64(ag)-float64(eg)) > 24*257 || math.Abs(float64(ab)-float64(eb)) > 24*257 {
				mismatches++
			}
		}
	}
	// Compare against ink coverage, so a blank or substituted state cannot pass.
	// Half-point placement at scale 2.5 changes a thin antialiased edge.
	if colored > 0 && float64(mismatches)/float64(colored) > .06 || colored == 0 && mismatches > 0 {
		t.Fatalf("page %d state differs: %d pixels; source ink %d", page, mismatches, colored)
	}
}

func TestFitActualAppearanceStatesAndSharedStreams(t *testing.T) {
	for _, rotation := range []int{0, 90, 180, 270} {
		for _, flags := range []int{4, 20} {
			for page := 1; page <= 2; page++ {
				for index, label := range []string{"N/On", "N/Off", "R/On", "R/Off", "D"} {
					t.Run(fmt.Sprintf("rotate=%d/flags=%d/page=%d/%s", rotation, flags, page, label), func(t *testing.T) {
						doc := sharedAppearanceDocument(rotation, flags)
						source, _, err := readSource(t.Context(), doc.Bytes())
						if err != nil {
							t.Fatal(err)
						}
						candidate, _, err := readSource(t.Context(), doc.Bytes())
						if err != nil {
							t.Fatal(err)
						}
						if err := fitContext(t.Context(), candidate, &packet.Size{Width: 300, Height: 500}); err != nil {
							t.Fatal(err)
						}
						sourceAnnot, expectedStates := appearanceChoices(t, source, page)
						candidateAnnot, actualStates := appearanceChoices(t, candidate, page)
						if len(actualStates) != len(expectedStates) || len(actualStates) != 5 {
							t.Fatal("appearance state count changed")
						}
						expected, actual := expectedStates[index], actualStates[index]
						if actual.kind != expected.kind || actual.state != expected.state {
							t.Fatal("appearance state identity changed")
						}
						// Select the *already transformed* R/D/named state as N only for
						// rasterization. This exercises the actual state's replacement path.
						chooseAppearance(t, source, sourceAnnot, expected)
						chooseAppearance(t, candidate, candidateAnnot, actual)
						sourcePath := writeFooterContext(t, source, "source-state")
						candidatePath := writeFooterContext(t, candidate, "candidate-state")
						compareAppearanceState(t, sourcePath, candidatePath, page, rotation)
					})
				}
			}
		}
	}
}
