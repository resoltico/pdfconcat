package filters

import (
	"bytes"
	"math/rand"
	"os"
	"testing"

	"github.com/benoitkugler/pdf/reader/parser/filters/ccitt"
)

var skippers = map[string]Skipper{
	ASCII85:   SkipperAscii85{},
	ASCIIHex:  SkipperAsciiHex{},
	RunLength: SkipperRunLength{},
	LZW:       SkipperLZW{EarlyChange: true},
	Flate:     SkipperFlate{},
	DCT:       SkipperDCT{},
	CCITTFax: SkipperCCITT{
		Params: ccitt.CCITTParams{
			Columns:    153,
			Rows:       55,
			EndOfBlock: true,
		},
	},
}

func forgeEncoded(t *testing.T, fi string) []byte {
	b, err := os.ReadFile("samples/" + fi + ".bin")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDontPassEOD(t *testing.T) {
	for _, fi := range []string{
		ASCII85,
		ASCIIHex,
		RunLength,
		LZW,
		Flate,
		DCT,
		CCITTFax,
	} {
		filtered := forgeEncoded(t, fi)

		fil := skippers[fi]

		// add data passed EOD
		additionalBytes := bytes.Repeat([]byte("')(à'(ààç454658"), 1000)
		filteredPadded := append(filtered, additionalBytes...)

		read1, err := fil.Skip(bytes.NewReader(filteredPadded))
		if err != nil {
			t.Fatal(err)
		}

		// we want to use the number of byte read from the
		// filtered stream to detect EOD
		if read1 != len(filtered) {
			t.Errorf("invalid number of bytes read with filter %s: %d, expected %d", fi, read1, len(filtered))
		}
	}
}

func TestInvalid(t *testing.T) {
	for _, fi := range []string{
		ASCII85,
		ASCIIHex,
		RunLength,
		// LZW,
		Flate,
		DCT,
		CCITTFax,
	} {
		for range [200]int{} {
			// random input
			input := make([]byte, 80)
			_, _ = rand.Read(input)

			// random data may actually be valid since the eod ASCIIHex is easy to get
			if fi == ASCII85 {
				// A random prefix can be a valid empty ASCII85 stream (~>); guarantee malformed grammar.
				input[0] = 0xff
			} else if fi == ASCIIHex {
				input = bytes.ReplaceAll(input, []byte{eodHexDecode}, []byte{eodHexDecode + 1})
			} else if fi == RunLength {
				input = bytes.ReplaceAll(input, []byte{eodRunLength}, []byte{eodRunLength + 1})
			}

			fil := skippers[fi]
			_, err := fil.Skip(bytes.NewReader(input))
			if err == nil {
				t.Fatalf("filter %s: expected error on random data %v", fi, input)
			}
		}
	}
}

// forge 30x30 gray images with various filters
// but this rely on pdfcpu filters
// func TestCreateImageStream(t *testing.T) {
// 	in := make([]byte, 30*30)
// 	rand.Read(in)

// 	filtersName := []string{
// 		ASCII85,
// 		ASCIIHex,
// 		Flate,
// 		LZW,
// 		RunLength,
// 	}
// 	for _, fi := range filtersName {
// 		out, err := filter.NewFilter(string(fi), nil)
// 		if err != nil {
// 			t.Fatal(err)
// 		}
// 		r, err := out.Encode(bytes.NewReader(in))
// 		if err != nil {
// 			t.Fatal(err)
// 		}
// 		encoded, err := ioutil.ReadAll(r)
// 		if err != nil {
// 			t.Fatal(err)
// 		}

// 		err = ioutil.WriteFile("samples/"+string(fi)+"_30x30.bin", encoded, os.ModePerm)
// 		if err != nil {
// 			t.Fatal(err)
// 		}
// 	}
// }

func TestASCII85CapturedInvalidPrefixBeforeEOD(t *testing.T) {
	// Captured native failure contains a real ~> marker but no valid ASCII85 prefix.
	input := []byte{172, 146, 184, 73, 191, 18, 139, 63, 16, 116, 207, 240, 41, 242, 86, 230, 55, 113, 195, 22, 128, 132, 223, 12, 218, 153, 114, 160, 31, 129, 161, 51, 39, 155, 170, 159, 186, 23, 85, 82, 254, 198, 101, 194, 12, 90, 112, 229, 124, 9, 93, 242, 144, 20, 94, 225, 235, 243, 111, 39, 113, 126, 62, 230, 161, 69, 154, 76, 17, 27, 164, 26, 179, 198, 140, 210, 88, 39, 239, 226}
	if _, err := (SkipperAscii85{}).Skip(bytes.NewReader(input)); err == nil {
		t.Fatal("captured invalid prefix accepted at incidental EOD")
	}
	for _, invalid := range []string{"!~>", "!z~>", "uuuuu~>", "!!!!!~x"} {
		if _, err := (SkipperAscii85{}).Skip(bytes.NewBufferString(invalid)); err == nil {
			t.Fatalf("malformed ASCII85 accepted: %q", invalid)
		}
	}
	for _, valid := range []string{"~>", "z~>", "!!!!!~>", "!!~>", "! ! ! ! ! ~>"} {
		length, err := (SkipperAscii85{}).Skip(bytes.NewBufferString(valid + " trailing"))
		if err != nil || length != len(valid) {
			t.Fatalf("valid exact EOD %q: length=%d error=%v", valid, length, err)
		}
	}
}

func TestASCIIHexRejectsInvalidPrefixAndKeepsOddNibble(t *testing.T) {
	for _, invalid := range []string{"GG>", "012X>", "012\x01>", "012"} {
		if _, err := (SkipperAsciiHex{}).Skip(bytes.NewBufferString(invalid)); err == nil {
			t.Fatalf("invalid hexadecimal prefix accepted: %q", invalid)
		}
	}
	for _, valid := range []string{">", "0>", "012>", "0 1 2 3>"} {
		length, err := (SkipperAsciiHex{}).Skip(bytes.NewBufferString(valid + " trailing"))
		if err != nil || length != len(valid) {
			t.Fatalf("valid hex EOD %q: %d %v", valid, length, err)
		}
	}
}
