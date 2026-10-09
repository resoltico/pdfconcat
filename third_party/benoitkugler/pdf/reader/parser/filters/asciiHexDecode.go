package filters

import (
	"errors"
	"io"
)

type SkipperAsciiHex struct{}

const eodHexDecode = '>'

// Skip validates hexadecimal bytes through the exact EOD, allowing an odd final nibble.
func (f SkipperAsciiHex) Skip(encoded io.Reader) (int, error) {
	origin := newCountReader(encoded)
	digits := 0
	for {
		next, err := origin.ReadByte()
		if err != nil {
			return origin.totalRead, unexpectedEOF(err)
		}
		if next == eodHexDecode {
			if bounded, ok := encoded.(*boundedInput); ok {
				bounded.discarded = (digits + 1) / 2
				if bounded.discarded > bounded.decoded {
					return origin.totalRead, errors.New("inline decoded byte limit exceeded")
				}
			}
			return origin.totalRead, nil
		}
		if pdfFilterWhitespace(next) {
			continue
		}
		if !(next >= '0' && next <= '9' || next >= 'a' && next <= 'f' || next >= 'A' && next <= 'F') {
			return origin.totalRead, errors.New("invalid ASCIIHex digit")
		}
		digits++
		if bounded, ok := encoded.(*boundedInput); ok && (digits+1)/2 > bounded.decoded {
			return origin.totalRead, errors.New("inline decoded byte limit exceeded")
		}
	}
}
func pdfFilterWhitespace(next byte) bool {
	switch next {
	case 0, 9, 10, 12, 13, 32:
		return true
	default:
		return false
	}
}
