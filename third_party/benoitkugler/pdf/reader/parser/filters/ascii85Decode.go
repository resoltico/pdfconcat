package filters

import (
	"encoding/ascii85"
	"errors"
	"io"
)

type SkipperAscii85 struct{}

const eodASCII85 = "~>"

// Skip validates the maintained ASCII85 grammar while recognizing its exact EOD.
// The standard decoder receives only pre-EOD bytes; payload is never searched for PDF operators.
func (f SkipperAscii85) Skip(encoded io.Reader) (int, error) {
	origin := newCountReader(encoded)
	input := &ascii85EODReader{source: origin}
	err := discardSkipped(ascii85.NewDecoder(input), encoded)
	return origin.totalRead, err
}

type ascii85EODReader struct {
	source io.ByteReader
	done   bool
	digits int
	value  uint64
}

func (r *ascii85EODReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	for index := range p {
		next, err := r.source.ReadByte()
		if err != nil {
			return index, unexpectedEOF(err)
		}
		if next == '~' {
			end, readErr := r.source.ReadByte()
			if readErr != nil {
				return index, unexpectedEOF(readErr)
			}
			if end != '>' {
				return index, errors.New("invalid ASCII85 EOD")
			}
			if r.digits == 1 {
				return index, errors.New("invalid one-digit ASCII85 final group")
			}
			padded := r.value
			if r.digits > 0 {
				for digits := r.digits; digits < 5; digits++ {
					padded = padded*85 + 84
				}
				if padded > 0xffffffff {
					return index, errors.New("ASCII85 partial tuple exceeds 32-bit range")
				}
			}
			r.done = true
			return index, io.EOF
		}
		if err = r.validate(next); err != nil {
			return index, err
		}
		p[index] = next
	}
	return len(p), nil
}
func (r *ascii85EODReader) validate(next byte) error {
	if pdfFilterWhitespace(next) {
		return nil
	}
	if next == 'z' {
		if r.digits != 0 {
			return errors.New("ASCII85 z inside a tuple")
		}
		return nil
	}
	if next < '!' || next > 'u' {
		return errors.New("invalid ASCII85 digit")
	}
	r.value = r.value*85 + uint64(next-'!')
	r.digits++
	if r.digits == 5 {
		if r.value > 0xffffffff {
			return errors.New("ASCII85 tuple exceeds 32-bit range")
		}
		r.value = 0
		r.digits = 0
	}
	return nil
}
