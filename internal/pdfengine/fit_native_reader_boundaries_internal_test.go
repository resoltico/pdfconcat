// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

const (
	nativeInvalidNumberBody       = "9oops"
	nativeInvalidNumberDiagnostic = "invalid or unrepresentable numeric"
	guardHundredPointBox          = "[0 0 100 100]"
	guardLeafResourcePage         = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] " +
		"/Resources << /XObject << /Leaf 5 0 R >> >> /Contents 4 0 R >>"
	guardPaintLeaf = "/Leaf Do"
)

// The packed value is an actual source body selected by a binary xref stream,
// so errors come from the maintained lazy file reader rather than a decoder callback.
func fitPackedSource(pageExtra, content, packedBody string) []byte {
	data := []byte("%PDF-1.7\n")
	offsets := make([]int, 7)
	add := func(number int, object []byte) {
		offsets[number] = len(data)
		data = append(data, []byte(strconv.Itoa(number)+" 0 obj\n")...)
		data = append(data, object...)
		data = append(data, []byte("\nendobj\n")...)
	}
	add(1, []byte(catalog))
	add(2, []byte(singlePageTreeBody))
	add(3, []byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R "+pageExtra+" >>"))
	add(4, fitFixtureStream("", []byte(content)))
	add(5, fitFixtureStream("/Type /ObjStm /N 1 /First 4", []byte("7 0 "+packedBody)))

	offsets[6] = len(data)
	xref := make([]byte, 8*7)
	binary.BigEndian.PutUint16(xref[5:7], 65535)

	for number := 1; number <= 6; number++ {
		row := xref[number*7 : (number+1)*7]
		row[0] = 1

		offset := offsets[number]
		if offset < 0 || offset > math.MaxUint32 {
			panic("test source exceeds PDF xref offset representation")
		}

		binary.BigEndian.PutUint32(row[1:5], uint32(offset))
	}

	xref[7*7] = 2
	binary.BigEndian.PutUint32(xref[7*7+1:7*7+5], 5)
	add(6, fitFixtureStream("/Type /XRef /Size 8 /Root 1 0 R /W [1 4 2]", xref))

	return append(data, []byte("startxref\n"+strconv.Itoa(offsets[6])+"\n%%EOF\n")...)
}

func fitFixtureStream(entries string, content []byte) []byte {
	header := []byte(fmt.Sprintf("<< %s /Length %d >>\nstream\n", entries, len(content)))
	header = append(header, content...)

	return append(header, []byte("\nendstream")...)
}

func TestFitSerializedPackedColorBodyRetainsNativeReaderFailure(t *testing.T) {
	t.Parallel()

	for _, body := range []string{"/DeviceRGB", nativeInvalidNumberBody} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()

			source := fitPackedSource("/Resources << /ColorSpace << /C 7 0 R >> >>", "BI /W 1 /H 1 /BPC 8 /CS /C ID \x00\x00\x00 EI", body)

			_, err := api.ReadContextWithSourceInspection(t.Context(), bytes.NewReader(source), model.NewDefaultConfiguration(),
				func(ctx context.Context, pdf *model.Context) error {
					_, inspectErr := inspectPageFits(ctx, pdf, PageSize{100, 100})
					return inspectErr
				})
			if body == "/DeviceRGB" && err != nil {
				t.Fatalf("valid packed color body refused: %v", err)
			}

			if body == nativeInvalidNumberBody && (err == nil || !strings.Contains(err.Error(), nativeInvalidNumberDiagnostic)) {
				t.Fatalf("actual packed body reader error lost: %v", err)
			}
		})
	}
}

func TestFitSerializedPackedPageKidsPreservesNativeReaderRefusal(t *testing.T) {
	t.Parallel()

	for _, body := range []string{nullPDFObject, nativeInvalidNumberBody} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()

			source := fitPackedSource("/Resources <<>> /Kids 7 0 R", "", body)
			engine, path := fitBoundaryRawSource(t, source)

			_, err := engine.Inspect(t.Context(), path, &PageSize{100, 100})
			if body == nullPDFObject && err != nil {
				t.Fatalf("real packed null Kids refused: %v", err)
			}

			if body == nativeInvalidNumberBody &&
				(err == nil || !strings.Contains(err.Error(), nativeInvalidNumberDiagnostic) || CodeOf(err) != CodeFitUnsupported) {
				t.Fatalf("packed Page Kids decoder failure lost: %v", err)
			}
		})
	}
}

func fitBoundaryRawSource(t *testing.T, source []byte) (*Engine, string) {
	t.Helper()

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), fitRenderSource)
	if err = os.WriteFile(path, source, 0o600); err != nil {
		t.Fatal(err)
	}

	return engine, path
}
