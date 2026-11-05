// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/ascii85"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"

	"github.com/pdfcpu/pdfcpu/pkg/filter"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func compressedContent(t *testing.T, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func ascii85Content(data []byte) []byte {
	b := make([]byte, ascii85.MaxEncodedLen(len(data)))
	n := ascii85.Encode(b, data)
	return append(b[:n], '~', '>')
}

func TestPageContentEncodingFiltersAndIntegrity(t *testing.T) {
	program := []byte("q 1 0 0 1 0 0 cm Q\n")
	valid := compressedContent(t, program)
	innerTruncated := valid[:len(valid)-1]
	badChecksum := bytes.Clone(valid)
	badChecksum[len(badChecksum)-1] ^= 1
	framed := []byte{0, 'a', 'b', 'c', 'd', 0, 'e', 'f', 'g', 'h'}
	for _, tc := range []struct {
		name   string
		stream pdffixture.EncodedStream
		want   []byte
		code   diag.Code
	}{
		{"plain", pdffixture.EncodedStream{Data: program}, program, diag.Code{}},
		{"identity crypt", pdffixture.EncodedStream{Data: program, Filter: "/Crypt", DecodeParms: "<< /Name /Identity >>"}, program, diag.Code{}},
		{"default identity crypt", pdffixture.EncodedStream{Data: program, Filter: "/Crypt"}, program, diag.Code{}},
		{"nonidentity crypt", pdffixture.EncodedStream{Data: program, Filter: "/Crypt", DecodeParms: "<< /Name /Unknown >>"}, nil, diag.SourceUnsupported},
		{"flate", pdffixture.EncodedStream{Data: valid, Filter: "/FlateDecode"}, program, diag.Code{}},
		{"ascii85-flate", pdffixture.EncodedStream{Data: ascii85Content(valid), Filter: "[/ASCII85Decode /FlateDecode]"}, program, diag.Code{}},
		{"identity prefix flate", pdffixture.EncodedStream{Data: valid, Filter: "[/Crypt /FlateDecode]", DecodeParms: "[<< /Name /Identity >> null]"}, program, diag.Code{}},
		{"late identity crypt", pdffixture.EncodedStream{Data: valid, Filter: "[/FlateDecode /Crypt]", DecodeParms: "[null << /Name /Identity >>]"}, nil, diag.SourceUnsupported},
		{"TIFF prediction", pdffixture.EncodedStream{Data: compressedContent(t, []byte{'a', 1, 1, 1, 'e', 1, 1, 1}), Filter: "/FlateDecode", DecodeParms: "<< /Predictor 2 /Columns 4 >>"}, []byte("abcdefgh"), diag.Code{}},
		{"nested flate", pdffixture.EncodedStream{Data: compressedContent(t, valid), Filter: "[/FlateDecode /FlateDecode]"}, program, diag.Code{}},
		{"truncated inner flate", pdffixture.EncodedStream{Data: compressedContent(t, innerTruncated), Filter: "[/FlateDecode /FlateDecode]"}, nil, diag.SourceInvalid},
		{"truncated checksum", pdffixture.EncodedStream{Data: valid[:len(valid)-1], Filter: "/FlateDecode"}, nil, diag.SourceInvalid},
		{"truncated program", pdffixture.EncodedStream{Data: valid[:len(valid)-8], Filter: "/FlateDecode"}, nil, diag.SourceInvalid},
		{"bad checksum", pdffixture.EncodedStream{Data: badChecksum, Filter: "/FlateDecode"}, nil, diag.SourceInvalid},
		{"ascii85 PNG prediction", pdffixture.EncodedStream{Data: ascii85Content(compressedContent(t, framed)), Filter: "[/ASCII85Decode /FlateDecode]", DecodeParms: "[null << /Predictor 15 /Columns 4 >>]"}, []byte("abcdefgh"), diag.Code{}},
		{"truncated PNG checksum", pdffixture.EncodedStream{Data: compressedContent(t, framed)[:len(compressedContent(t, framed))-1], Filter: "/FlateDecode", DecodeParms: "<< /Predictor 15 /Columns 4 >>"}, nil, diag.SourceInvalid},
		{"partial prediction row", pdffixture.EncodedStream{Data: compressedContent(t, []byte{0, 'a', 'b'}), Filter: "/FlateDecode", DecodeParms: "<< /Predictor 15 /Columns 4 >>"}, nil, diag.SourceInvalid},
		{"invalid flate", pdffixture.EncodedStream{Data: []byte("invalid zlib header"), Filter: "/FlateDecode"}, nil, diag.SourceInvalid},
		{"unknown filter", pdffixture.EncodedStream{Data: valid, Filter: "/MadeUpDecode"}, nil, diag.SourceInvalid},
		{"opaque image", pdffixture.EncodedStream{Data: valid, Filter: "/JPXDecode"}, nil, diag.SourceUnsupported},
		{"empty flate", pdffixture.EncodedStream{Data: compressedContent(t, nil), Filter: "/FlateDecode"}, []byte{}, diag.Code{}},
		{"empty truncated flate", pdffixture.EncodedStream{Data: compressedContent(t, nil)[:4], Filter: "/FlateDecode"}, nil, diag.SourceInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := pdffixture.EncodedPages("encoded", []pdffixture.EncodedStream{tc.stream})
			s := parsedFixture(t, doc)
			ref := types.NewIndirectRef(len(doc.Objs), 0)
			object, err := s.Dereference(*ref)
			if err != nil {
				t.Fatal(err)
			}
			stream, ok := object.(types.StreamDict)
			if !ok {
				t.Fatal("fixture is not a stream")
			}
			raw := bytes.Clone(stream.Raw)
			dictionary := stream.Dict.Clone()
			pipeline := stream.Clone()
			err = validateContentEncoding(t.Context(), s)
			if tc.code != (diag.Code{}) {
				if err == nil || diagnostic(err, diag.SourceInvalid).Code != tc.code {
					t.Fatalf("expected %s: %v", tc.code, err)
				}
				location := diagnostic(err, diag.SourceInvalid).PDFLocation
				if location == nil || location.Page != 1 || location.Object != ref.ObjectNumber.Value() {
					t.Fatal("lost original page/stream attribution", location)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			object, err = s.Dereference(*ref)
			if err != nil {
				t.Fatal(err)
			}
			cached, ok := object.(types.StreamDict)
			if !ok {
				t.Fatal("cached object is not a stream")
			}
			original, ok := pipeline.(types.StreamDict)
			if !ok {
				t.Fatal("clone is not a stream")
			}
			pipelineEqual := reflect.DeepEqual(cached.FilterPipeline, original.FilterPipeline) || len(cached.FilterPipeline) == 0 && len(original.FilterPipeline) == 0
			if !bytes.Equal(cached.Content, tc.want) || !bytes.Equal(cached.Raw, raw) || !reflect.DeepEqual(cached.Dict, dictionary) || !pipelineEqual {
				t.Fatal("encoding validation changed original data or did not retain the validated cache")
			}
		})
	}
}

func TestEncodingLimitsPredictionAndSharedArrays(t *testing.T) {
	// Two PNG rows contain a framing byte each: 10 inflated bytes produce 8
	// decoded bytes. The limit applies to decoded content, not framing overhead.
	framed := []byte{0, 'a', 'b', 'c', 'd', 0, 'e', 'f', 'g', 'h'}
	doc := pdffixture.EncodedPages("predictor", []pdffixture.EncodedStream{{Data: compressedContent(t, framed), Filter: "/FlateDecode", DecodeParms: "<< /Predictor 15 /Columns 4 >>"}})
	s := parsedFixture(t, doc)
	s.Limits.MaxDecodeBytes = 8
	if err := validateContentEncoding(t.Context(), s); err != nil {
		t.Fatal("valid predictor framing rejected", err)
	}
	for _, data := range [][]byte{[]byte("123456789"), compressedContent(t, []byte("123456789"))} {
		f := ""
		if len(data) > 9 {
			f = "/FlateDecode"
		}
		s = parsedFixture(t, pdffixture.EncodedPages("limit", []pdffixture.EncodedStream{{Data: data, Filter: f}}))
		s.Limits.MaxDecodeBytes = 8
		if err := validateContentEncoding(t.Context(), s); !errors.Is(err, filter.ErrDecodeLimitExceeded) {
			t.Fatalf("decoded length cap bypassed: %v", err)
		}
	}
	s = parsedFixture(t, pdffixture.Plain("shared"))
	pages, err := rawPages(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	ref := pages[0].dict["Contents"]
	object, err := s.Dereference(ref)
	if err != nil {
		t.Fatal(err)
	}
	stream, ok := object.(types.StreamDict)
	if !ok {
		t.Fatal("fixture content is not a stream")
	}
	s.Limits.MaxDecodeBytes = int64(len(stream.Raw))
	pages[0].dict["Contents"] = types.Array{ref, ref}
	if err = validateContentEncoding(t.Context(), s); !errors.Is(err, filter.ErrDecodeLimitExceeded) {
		t.Fatalf("repeated shared stream did not count twice toward page limit: %v", err)
	}
	pages[0].dict["Contents"] = types.Array{ref}
	if err = validateContentEncoding(t.Context(), s); err != nil {
		t.Fatal("a single shared member was refused", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = validateContentEncoding(ctx, s); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestSerializedOutputEncodingGateIsIndependentOfSourceAdmission(t *testing.T) {
	valid := compressedContent(t, []byte("BT /F1 12 Tf 10 20 Td (OUTPUT) Tj ET\n"))
	for _, truncated := range []bool{false, true} {
		data := valid
		if truncated {
			data = valid[:len(valid)-1]
		}
		doc := pdffixture.EncodedPages("output", []pdffixture.EncodedStream{{Data: data, Filter: "/FlateDecode"}})
		path := filepath.Join(t.TempDir(), "output.pdf")
		if err := doc.WriteFile(path); err != nil {
			t.Fatal(err)
		}
		err := Verify(t.Context(), path, 1)
		if !truncated {
			if err != nil {
				t.Fatal("valid serialized encoding rejected", err)
			}
			continue
		}
		if err == nil || Diagnostic(err).Code != diag.OutputInvalid {
			t.Fatalf("output gate accepted incomplete Flate: %v", err)
		}
	}
}

func TestExistingDecodedCacheCannotHideIncompleteEncoding(t *testing.T) {
	program := []byte("BT /F1 12 Tf 10 20 Td (CACHE) Tj ET\n")
	encoded := compressedContent(t, program)
	doc := pdffixture.EncodedPages("cache", []pdffixture.EncodedStream{{Data: encoded[:len(encoded)-1], Filter: "/FlateDecode"}})
	s := parsedFixture(t, doc)
	ref := types.NewIndirectRef(len(doc.Objs), 0)
	object, err := s.Dereference(*ref)
	if err != nil {
		t.Fatal(err)
	}
	stream, ok := object.(types.StreamDict)
	if !ok {
		t.Fatal("fixture content is not a stream")
	}
	stream.Content = program
	s.Table[ref.ObjectNumber.Value()].Object = stream
	if err = validateContentEncoding(t.Context(), s); err == nil || diagnostic(err, diag.SourceInvalid).Code != diag.SourceInvalid {
		t.Fatalf("cached bytes bypassed encoded integrity: %v", err)
	}
}

func TestOutputVerifiesFittedFormEncoding(t *testing.T) {
	for _, damage := range []bool{false, true} {
		t.Run(fmt.Sprintf("damage=%v", damage), func(t *testing.T) {
			s, _, err := readSource(t.Context(), pdffixture.FitMarks("").Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if err := fitContext(t.Context(), s, &packet.Size{Width: 300, Height: 500}); err != nil {
				t.Fatal(err)
			}
			page, _, attrs, err := s.PageDict(t.Context(), 1, false)
			if err != nil {
				t.Fatal(err)
			}
			resources, err := s.DereferenceDict(attrs.Resources["XObject"])
			if err != nil {
				t.Fatal(err)
			}
			ref, ok := resources["PCSource"].(types.IndirectRef)
			if !ok {
				t.Fatal("fitted source Form missing")
			}
			object, err := s.Dereference(ref)
			if err != nil {
				t.Fatal(err)
			}
			stream, ok := object.(types.StreamDict)
			if !ok {
				t.Fatal("source not stream")
			}
			if damage {
				stream.Raw = stream.Raw[:len(stream.Raw)-1]
				length := int64(len(stream.Raw))
				stream.StreamLength = &length
				stream.Dict["Length"] = types.Integer(len(stream.Raw))
				stream.Content = nil
				s.Table[ref.ObjectNumber.Value()].Object = stream
			}
			if page["Contents"] == nil {
				t.Fatal("outer page content missing")
			}
			// The writer retains encoded Form bytes, including the deliberate fault.
			var out bytes.Buffer
			if err := api.WriteContext(t.Context(), s, &out); err != nil {
				t.Fatal(err)
			}
			_, err = readOutput(t.Context(), out.Bytes())
			if damage {
				if err == nil {
					t.Fatal("serialized source Form checksum bypassed output verification")
				}
				finding := diagnostic(err, diag.SourceInvalid)
				if finding.PDFLocation == nil || finding.PDFLocation.Path != "XObject.Form" {
					t.Fatalf("failed for unrelated reason: %v", err)
				}
			}
			if !damage && err != nil {
				t.Fatal(err)
			}
		})
	}
}
